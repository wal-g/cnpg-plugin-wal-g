package controller

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BackupDeletionController handles Backup deletion requests and processes them
// sequentally per BackupConfig to prevent operator from overloading
// and wal-g collisions at deleting backups at the same storage
type BackupDeletionController struct {
	client.Client

	// Backup deletion queues, each BackupConfig should have its own queue to make its backups deletion serialized
	// Key - BackupConfig.Namespace + "/" + BackupConfig.Name
	// Values - Backup object keys
	backupDeletionQueues map[string](chan types.NamespacedName)
	queueMX              sync.Mutex
	queueCtxCancels      map[string]context.CancelFunc // Cancel functions for queue processing goroutines contexts
}

// NewBackupDeletionController creates a new BackupDeletionController
func NewBackupDeletionController(client client.Client) *BackupDeletionController {
	return &BackupDeletionController{
		Client:               client,
		backupDeletionQueues: make(map[string]chan types.NamespacedName),
		queueCtxCancels:      map[string]context.CancelFunc{},
	}
}

// Start begins the backup deletion controller's periodic check
func (b *BackupDeletionController) Start(ctx context.Context) error {
	logger := logr.FromContextOrDiscard(ctx).WithName("BackupDeletionController")
	logger.Info("Starting backup deletion controller")

	// Wait for context cancellation
	for range ctx.Done() {
		// This block executes only once when context is done
		logger.Info("Stopping backup deletion controller")
		break
	}

	// Cleaning up all queues on exit
	for queueKey, cancelFunc := range b.queueCtxCancels {
		func() { // Wrap in separate function to use "defer mutex.Unlock()"
			b.queueMX.Lock()
			defer b.queueMX.Unlock()

			// Cancel the context for this queue's goroutine
			cancelFunc()
			// Remove queue
			delete(b.backupDeletionQueues, queueKey)
		}()
	}

	return nil
}

// EnqueueBackupDeletion adds a Backup to the deletion queue for its BackupConfig
// If the queue doesn't exist, it creates a new one and starts a goroutine to process it
func (b *BackupDeletionController) EnqueueBackupDeletion(ctx context.Context, backup *cnpgv1.Backup) error {
	logger := logr.FromContextOrDiscard(ctx).WithName("BackupDeletionController")
	// Get BackupConfig with secrets for the backup
	backupConfigWithSecrets, err := v1beta1.GetBackupConfigWithSecretsForBackup(ctx, b.Client, backup)
	if err != nil {
		return fmt.Errorf("failed to get BackupConfig for Backup: %w", err)
	}

	backupKey := client.ObjectKeyFromObject(backup)

	// Create queue key from BackupConfig namespace and name
	queueKey := backupConfigWithSecrets.Namespace + "/" + backupConfigWithSecrets.Name

	// Check if queue exists, if not create it
	b.queueMX.Lock()
	defer b.queueMX.Unlock()
	_, exists := b.backupDeletionQueues[queueKey]

	if !exists {
		// Need to create a new queue
		// Check again in case another goroutine created it while we were waiting for the lock
		_, exists = b.backupDeletionQueues[queueKey]
		if !exists {
			// Create a new buffered queue with size 128
			b.backupDeletionQueues[queueKey] = make(chan types.NamespacedName, 128)

			// Create a derived context for this queue's processing goroutine
			queueCtx, cancelFunc := context.WithCancel(logr.NewContext(context.Background(), logger))

			b.queueCtxCancels[queueKey] = cancelFunc

			// Start a goroutine to process this queue
			go b.processBackupDeletionQueue(queueCtx, queueKey)
		}
	}

	// Try to add the backup to the queue
	select {
	case b.backupDeletionQueues[queueKey] <- backupKey:
		logger.Info("Enqueued backup for deletion", "backup", backupKey)
		return nil
	default:
		// Queue is full
		return fmt.Errorf("deletion queue for BackupConfig %s is full", queueKey)
	}
}

// processBackupDeletionQueue processes backups from the deletion queue
func (b *BackupDeletionController) processBackupDeletionQueue(
	ctx context.Context,
	queueKey string,
) {
	logger := logr.FromContextOrDiscard(ctx).WithName("BackupDeletionQueue").WithValues("queueKey", queueKey)
	logger.Info("Starting backup deletion queue processor")

	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping backup deletion queue processor")
			return
		case backupKey := <-b.backupDeletionQueues[queueKey]:
			logger.Info("Processing wal-g backup deletion", "backup", backupKey)
			err := b.deleteWALGBackup(ctx, backupKey)
			if err != nil {
				logger.Error(err, "Failed to delete backup", "backup", backupKey)
			}
		}
	}
}

// deleteWALGBackup deletes the physical backup via WAL-G (could be long running) and removes finalizer from Backup resource after successful deletion
func (b *BackupDeletionController) deleteWALGBackup(ctx context.Context, backupKey client.ObjectKey) error {
	logger := logr.FromContextOrDiscard(ctx)
	backup := &cnpgv1.Backup{}
	err := b.Get(ctx, backupKey, backup)
	if err != nil {
		return fmt.Errorf("while getting backup %v: %w", backupKey, err)
	}

	backupConfig, err := v1beta1.GetBackupConfigWithSecretsForBackup(ctx, b.Client, backup)
	if err != nil {
		return fmt.Errorf("while getting backupconfig for backup %v: %w", backupKey, err)
	}

	if backup.Labels == nil || backup.Annotations == nil {
		logger.Info(
			"Cannot delete backup because it has missing plugin labels or annotations",
			"backup", backup.Name, "dependents", backup.Annotations[backupAllDependentsAnnotationName],
		)
		return nil
	}

	if backup.Annotations[backupAllDependentsAnnotationName] != "" {
		// TODO: emit event for backup
		logger.Info(
			"Cannot delete backup because it still has dependent backups",
			"backup", backup.Name, "dependents", backup.Annotations[backupAllDependentsAnnotationName],
		)
		return nil
	}

	if !containsString(backup.Finalizers, backupFinalizerName) {
		return nil // Nothing to do if finalizer is not specified
	}

	// Delete physical backup only if .Status.BackupID is present
	if backup.Status.BackupID != "" {
		// Delete the backup using WAL-G
		logger.Info("Deleting backup from WAL-G", "backupID", backup.Status.BackupID)
		pgVersion, err := strconv.Atoi(backup.Labels[backupPgVersionLabelName])
		if err != nil {
			return fmt.Errorf(
				"while wal-g backup removal: error cannot parse pgVersion from Backup label %s: '%s': %w",
				backupPgVersionLabelName, backup.Labels[backupPgVersionLabelName], err,
			)
		}
		result, err := walg.DeleteBackup(ctx, backupConfig, pgVersion, backup.Status.BackupID)
		if err != nil {
			return fmt.Errorf(
				"while wal-g backup removal: error %w\nWAL-G stdout: %s\nWAL-G stderr: %s",
				err,
				string(result.Stdout()),
				string(result.Stderr()),
			)
		}
	}

	// Remove our finalizer from the list and update
	backup.Finalizers = removeString(backup.Finalizers, backupFinalizerName)
	if err := b.Update(ctx, backup); err != nil {
		logger.Error(err, "while removing cleanup finalizer from Backup")
		return fmt.Errorf("while removing cleanup finalizer from Backup: %w", err)
	}

	return nil
}
