package controller

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
	"golang.org/x/sync/semaphore"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResourceType defines the type of resource being deleted
type ResourceType string

const (
	// ResourceTypeBackup represents a Backup resource
	ResourceTypeBackup ResourceType = "Backup"
	// ResourceTypeBackupConfig represents a BackupConfig resource
	ResourceTypeBackupConfig ResourceType = "BackupConfig"

	// Timeout for resource cleanup, if exceeded - deletion process will be aborted and retried
	// Needed to prevent stuck on deletion when incorrect BackupConfig / etc.
	DeletionRequestTimeout time.Duration = 10 * time.Minute
)

// DeletionRequest represents a request to delete a resource
type DeletionRequest struct {
	// Type of resource to delete
	Type ResourceType
	// NamespacedName of the resource
	Key types.NamespacedName
}

// BackupDeletionController handles Backup and BackupConfig deletion requests and processes them
// sequentially per BackupConfig to prevent operator from overloading
// and wal-g collisions at deleting backups at the same storage
type BackupDeletionController struct {
	client.Client

	// Deletion queues, each BackupConfig should have its own queue to make its deletions serialized
	// Key - BackupConfig.Namespace + "/" + BackupConfig.Name
	// Values - DeletionRequest objects
	deletionQueues    map[string](chan DeletionRequest)
	queueMX           sync.Mutex
	queueCtxCancels   map[string]context.CancelFunc // Cancel functions for queue processing goroutines contexts
	deletionSemaphore *semaphore.Weighted
}

// NewBackupDeletionController creates a new BackupDeletionController
func NewBackupDeletionController(client client.Client) *BackupDeletionController {
	deletionSemaphore := semaphore.NewWeighted(int64(10))
	return &BackupDeletionController{
		Client:            client,
		deletionQueues:    make(map[string]chan DeletionRequest),
		queueCtxCancels:   map[string]context.CancelFunc{},
		deletionSemaphore: deletionSemaphore,
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
			delete(b.deletionQueues, queueKey)
		}()
	}

	return nil
}

// EnqueueBackupDeletion adds a Backup to the deletion queue for its BackupConfig
// If the queue doesn't exist, it creates a new one and starts a goroutine to process it
func (b *BackupDeletionController) EnqueueBackupDeletion(ctx context.Context, backup *cnpgv1.Backup) error {
	// Get BackupConfig with secrets for the backup
	backupConfig, err := v1beta1.GetBackupConfigForBackup(ctx, b.Client, backup)
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to get BackupConfig for Backup: %w", err)
	}
	if err != nil {
		// If BackupConfig not found, then it's already deleted, just remove finalizer from Backup and do nothing else
		return removeBackupFinalizer(ctx, backup, b.Client)
	}

	backupKey := client.ObjectKeyFromObject(backup)

	// Create queue key from BackupConfig namespace and name
	queueKey := backupConfig.Namespace + "/" + backupConfig.Name

	// Create deletion request
	request := DeletionRequest{
		Type: ResourceTypeBackup,
		Key:  backupKey,
	}

	// Enqueue the deletion request
	return b.enqueueDeletionRequest(ctx, queueKey, request)
}

// EnqueueBackupConfigDeletion adds a BackupConfig to the deletion queue
// If the queue doesn't exist, it creates a new one and starts a goroutine to process it
func (b *BackupDeletionController) EnqueueBackupConfigDeletion(ctx context.Context, backupConfig *v1beta1.BackupConfig) error {
	backupConfigKey := client.ObjectKeyFromObject(backupConfig)

	// Create queue key from BackupConfig namespace and name
	queueKey := backupConfig.Namespace + "/" + backupConfig.Name

	// Create deletion request
	request := DeletionRequest{
		Type: ResourceTypeBackupConfig,
		Key:  backupConfigKey,
	}

	// Enqueue the deletion request
	return b.enqueueDeletionRequest(ctx, queueKey, request)
}

// enqueueDeletionRequest adds a deletion request to the appropriate queue
// If the queue doesn't exist, it creates a new one and starts a goroutine to process it
func (b *BackupDeletionController) enqueueDeletionRequest(
	ctx context.Context,
	queueKey string,
	request DeletionRequest,
) error {
	logger := logr.FromContextOrDiscard(ctx).WithName("BackupDeletionController")
	// Check if queue exists, if not create it
	b.queueMX.Lock()
	defer b.queueMX.Unlock()
	_, exists := b.deletionQueues[queueKey]

	if !exists {
		// Need to create a new queue
		// Create a new buffered queue with size 128
		b.deletionQueues[queueKey] = make(chan DeletionRequest, 128)

		// Create a derived context for this queue's processing goroutine
		queueCtx, cancelFunc := context.WithCancel(logr.NewContext(context.Background(), logger))

		b.queueCtxCancels[queueKey] = cancelFunc

		// Start a goroutine to process this queue
		go b.processDeletionQueue(queueCtx, queueKey)
	}

	// Try to add the request to the queue
	select {
	case b.deletionQueues[queueKey] <- request:
		logger.Info("Enqueued resource for deletion", "type", request.Type, "key", request.Key)
		return nil
	default:
		// Queue is full
		return fmt.Errorf("deletion queue for %s is full", queueKey)
	}
}

// processDeletionQueue processes deletion requests from the queue
func (b *BackupDeletionController) processDeletionQueue(
	ctx context.Context,
	queueKey string,
) {
	logger := logr.FromContextOrDiscard(ctx)
	logger.Info("Starting deletion queue processor")

	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping deletion queue processor")
			return
		case request := <-b.deletionQueues[queueKey]:
			logger.Info("Processing deletion request", "type", request.Type, "key", request.Key)

			ctxWithTimeout, cancel := context.WithTimeout(ctx, DeletionRequestTimeout)
			defer cancel() // releases timeout resources if deletion completes before timeout elapses

			var err error
			switch request.Type {
			case ResourceTypeBackup:
				logger.Info("Dequeued wal-g backup for deletion", "backup", request.Key)
				err = b.deleteWALGBackup(ctxWithTimeout, request.Key)
			case ResourceTypeBackupConfig:
				err = b.deleteBackupConfig(ctxWithTimeout, request.Key)
			default:
				err = fmt.Errorf("unknown resource type: %s", request.Type)
			}

			if err != nil {
				logger.Error(err, "Failed to process deletion request", "type", request.Type, "key", request.Key)
			}
		}
	}
}

// deleteWALGBackup deletes the physical backup via WAL-G (could be long running) and removes finalizer from Backup resource after successful deletion
func (b *BackupDeletionController) deleteWALGBackup(ctx context.Context, backupKey client.ObjectKey) error {
	logger := logr.FromContextOrDiscard(ctx)
	err := b.deletionSemaphore.Acquire(ctx, 1)
	if err != nil {
		return fmt.Errorf("acquire deletion semaphore for backup %v: %w", backupKey, err)
	}
	defer b.deletionSemaphore.Release(1)

	logger.Info("Started processing wal-g backup deletion", "backup", backupKey)
	backup := &cnpgv1.Backup{}
	err = b.Get(ctx, backupKey, backup)
	if err != nil {
		return fmt.Errorf("while getting backup %v: %w", backupKey, err)
	}

	if backup.Labels == nil || backup.Annotations == nil {
		logger.Info(
			"Cannot delete backup because it has missing plugin labels or annotations",
			"backup", backup.Name, "dependents", backup.Annotations[v1beta1.BackupAllDependentsAnnotationName],
		)
		return nil
	}

	if backup.Annotations[v1beta1.BackupAllDependentsAnnotationName] != "" {
		// TODO: emit event for backup
		logger.Info(
			"Cannot delete backup because it still has dependent backups",
			"backup", backup.Name, "dependents", backup.Annotations[v1beta1.BackupAllDependentsAnnotationName],
		)
		return nil
	}

	if !containsString(backup.Finalizers, v1beta1.BackupFinalizerName) {
		return nil // Nothing to do if finalizer is not specified
	}

	if backup.Status.BackupID == "" {
		// if .Status.BackupID not present - physical backup does not exist and just removing finalizer
		return removeBackupFinalizer(ctx, backup, b.Client)
	}

	pgVersion, err := strconv.Atoi(backup.Labels[v1beta1.BackupPgVersionLabelName])
	if err != nil {
		return fmt.Errorf(
			"while wal-g backup removal: error cannot parse pgVersion from Backup label %s: '%s': %w",
			v1beta1.BackupPgVersionLabelName, backup.Labels[v1beta1.BackupPgVersionLabelName], err,
		)
	}

	backupConfigWithSecrets, err := v1beta1.GetBackupConfigWithSecretsForBackup(ctx, b.Client, backup)
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("while getting backupconfig with secrets for backup %v: %w", backupKey, err)
	}
	if err != nil {
		// If BackupConfig not found, then it's already deleted, just remove finalizer and do nothing else
		return removeBackupFinalizer(ctx, backup, b.Client)
	}

	// Check if deletion management is disabled for individual backups
	if backupConfigWithSecrets.Spec.Retention.IgnoreForBackupDeletion {
		logger.Info("Deletion management disabled for individual backups, skipping storage cleanup", "backupID", backup.Status.BackupID)
		return removeBackupFinalizer(ctx, backup, b.Client)
	}

	// Delete the backup using WAL-G
	logger.Info("Deleting backup from storage via WAL-G", "backupID", backup.Status.BackupID)
	result, err := walg.DeleteBackup(ctx, backupConfigWithSecrets, pgVersion, backup.Status.BackupID)
	if err != nil {
		return fmt.Errorf(
			"while wal-g backup removal: error %w\nWAL-G stdout: %s\nWAL-G stderr: %s",
			err,
			string(result.Stdout()),
			string(result.Stderr()),
		)
	}

	return removeBackupFinalizer(ctx, backup, b.Client)
}

func removeBackupFinalizer(ctx context.Context, backup *cnpgv1.Backup, client client.Client) error {
	if !containsString(backup.Finalizers, v1beta1.BackupFinalizerName) {
		return nil
	}

	backup.Finalizers = removeString(backup.Finalizers, v1beta1.BackupFinalizerName)
	if err := client.Update(ctx, backup); err != nil {
		return fmt.Errorf("while removing cleanup finalizer from Backup: %w", err)
	}
	return nil
}

// deleteBackupConfig cleans up backup storage specified in BackupConfig (if needed) and removes finalizer from BackupConfig object
func (b *BackupDeletionController) deleteBackupConfig(ctx context.Context, backupConfigKey client.ObjectKey) error {
	logger := logr.FromContextOrDiscard(ctx)
	backupConfig := &v1beta1.BackupConfig{}
	err := b.Get(ctx, backupConfigKey, backupConfig)
	if err != nil {
		// If the BackupConfig is already gone, there's nothing to do
		return client.IgnoreNotFound(err)
	}

	// Check if deletion management is disabled for BackupConfig
	if backupConfig.Spec.Retention.IgnoreForBackupConfigDeletion {
		logger.Info("Deletion management disabled for BackupConfig, skipping storage cleanup", "backupConfig", backupConfigKey)
	} else {
		backupConfigWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, b)
		if err != nil {
			return fmt.Errorf(
				"while fetching secrets data for backupConfig %v: %w",
				client.ObjectKeyFromObject(backupConfig),
				err,
			)
		}

		// Performing cleanup for all known PG versions (to remove both old-version backups and current backups)
		for pgVersion := 11; pgVersion <= 19; pgVersion++ {
			result, err := walg.DeleteAllBackupsAndWALsInStorage(ctx, backupConfigWithSecrets, pgVersion)
			if err != nil {
				return fmt.Errorf(
					"while wal-g storage cleanup: error %w\nWAL-G stdout: %s\nWAL-G stderr: %s",
					err,
					string(result.Stdout()),
					string(result.Stderr()),
				)
			}
		}
	}

	// Remove our finalizer from the list and update
	if containsString(backupConfig.Finalizers, v1beta1.BackupConfigFinalizerName) {
		backupConfig.Finalizers = removeString(backupConfig.Finalizers, v1beta1.BackupConfigFinalizerName)
		if err := b.Update(ctx, backupConfig); err != nil {
			logger.Error(err, "while removing cleanup finalizer from BackupConfig")
			return fmt.Errorf("while removing cleanup finalizer from BackupConfig: %w", err)
		}
	}

	return nil
}
