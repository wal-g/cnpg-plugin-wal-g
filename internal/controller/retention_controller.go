package controller

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RetentionController periodically checks all BackupConfig resources and applies
// retention policies to delete old backups
type RetentionController struct {
	checkInterval time.Duration
	client        client.Client
}

// NewRetentionController creates a new RetentionController
func NewRetentionController(client client.Client, checkInterval time.Duration) *RetentionController {
	return &RetentionController{
		checkInterval: checkInterval,
		client:        client,
	}
}

// Start begins the retention controller's periodic check
func (r *RetentionController) Start(ctx context.Context) error {
	logger := logr.FromContextOrDiscard(ctx).WithName("RetentionController")
	logger.Info("Starting retention controller", "checkInterval", r.checkInterval)

	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping retention controller")
			return nil
		case <-ticker.C:
			logger.Info("Running backup retention check")
			r.runBackupsRetentionCheck(ctx)
		}
	}
}

// runBackupsRetentionCheck lists all BackupConfig resources and applies retention policies
func (r *RetentionController) runBackupsRetentionCheck(ctx context.Context) {
	logger := logr.FromContextOrDiscard(ctx).WithName("RetentionController")

	// List all BackupConfig resources
	backupConfigList := &v1beta1.BackupConfigList{}
	if err := r.client.List(ctx, backupConfigList); err != nil {
		logger.Error(err, "Failed to list BackupConfig resources")
		return
	}

	// Process each BackupConfig
	for i := range backupConfigList.Items {
		backupConfig := &backupConfigList.Items[i]
		backupConfigLogger := logger.WithValues(
			"backupConfig", backupConfig.Name,
			"namespace", backupConfig.Namespace,
		)

		backupConfigLogger.Info("Processing BackupConfig")

		// Skip if retention policy is not configured
		if backupConfig.Spec.Retention.DeleteBackupsAfter == "" {
			backupConfigLogger.Info("No retention policy configured, skipping")
			continue
		}

		// Process the BackupConfig
		if err := r.runRetentionForBackupConfig(ctx, backupConfig, backupConfigLogger); err != nil {
			backupConfigLogger.Error(err, "Failed to process BackupConfig")
		}
	}
}

// runRetentionForBackupConfig applies the retention policy for a single BackupConfig
func (r *RetentionController) runRetentionForBackupConfig(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
	logger logr.Logger,
) error {
	// List all Backup resources in the same namespace
	backupList := &cnpgv1.BackupList{}
	opts := &client.ListOptions{
		Namespace: backupConfig.Namespace,
	}

	if err := r.client.List(ctx, backupList, opts); err != nil {
		return fmt.Errorf("failed to list Backup resources: %w", err)
	}

	// Filter backups that are owned by this BackupConfig
	var relevantBackups []cnpgv1.Backup
	for i := range backupList.Items {
		backup := backupList.Items[i].DeepCopy()
		if len(backup.OwnerReferences) == 0 || backup.DeletionTimestamp != nil {
			continue
		}

		if lo.ContainsBy(backup.OwnerReferences, func(ref metav1.OwnerReference) bool {
			return ref.Kind == backupConfig.Kind && ref.Name == backupConfig.Name
		}) {
			relevantBackups = append(relevantBackups, *backup)
		}
	}

	if len(relevantBackups) == 0 {
		logger.Info("No relevant backups found for this BackupConfig")
		return nil
	}

	// Get backups to delete based on retention policy
	backupsToDelete, err := r.getBackupsToDelete(ctx, backupConfig, relevantBackups)
	if err != nil {
		return fmt.Errorf("failed to determine backups to delete: %w", err)
	}

	// Delete Backup resources
	for i := range backupsToDelete {
		if err := r.client.Delete(ctx, &backupsToDelete[i]); err != nil {
			logger.Error(err, "failed to delete Backup resource")
		} else {
			logger.Info("Successfully deleted Backup resource", "backupName", backupsToDelete[i].Name)
		}
	}

	return nil
}

// getBackupsToDelete determines which backups should be deleted based on retention policy
func (r *RetentionController) getBackupsToDelete(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
	backupList []cnpgv1.Backup,
) ([]cnpgv1.Backup, error) {
	logger := logr.FromContextOrDiscard(ctx)
	retention := backupConfig.Spec.Retention

	// If no retention policy is configured, return empty list
	if retention.DeleteBackupsAfter == "" {
		return []cnpgv1.Backup{}, nil
	}

	// Sort backups by start time (oldest first). If StartedAt not specified - use CreationTimestamp
	sort.Slice(backupList, func(i, j int) bool {
		timeI := backupList[i].Status.StartedAt
		if timeI == nil {
			return backupList[i].CreationTimestamp.Before(&backupList[j].CreationTimestamp)
		}

		timeJ := backupList[j].Status.StartedAt
		if timeJ == nil {
			return backupList[i].CreationTimestamp.Before(&backupList[j].CreationTimestamp)
		}

		return timeI.Before(timeJ)
	})

	// Calculate retention threshold time if DeleteBackupsAfter is set
	var retentionThreshold *time.Time
	if retention.DeleteBackupsAfter != "" {
		threshold, err := calculateRetentionThreshold(retention.DeleteBackupsAfter)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate retention threshold: %w", err)
		}
		retentionThreshold = &threshold
	}

	// Filter backups that should be kept based on MinBackupsToKeep
	minBackupsToKeep := 5 // Default value
	if retention.MinBackupsToKeep >= 0 {
		minBackupsToKeep = retention.MinBackupsToKeep
	}

	// If we have fewer successful backups than minBackupsToKeep, keep all
	successfulBackups := lo.Filter(backupList, func(b cnpgv1.Backup, _ int) bool {
		return b.Status.Phase == cnpgv1.BackupPhaseCompleted && b.DeletionTimestamp == nil
	})
	if len(successfulBackups) <= minBackupsToKeep {
		logger.Info("Number of successful backups is less than or equal to MinBackupsToKeep, keeping all",
			"backupCount", len(backupList),
			"minBackupsToKeep", minBackupsToKeep)
		return []cnpgv1.Backup{}, nil
	}

	backupsToDelete := make([]cnpgv1.Backup, 0)
	// Process backups from oldest to newest (they're already sorted)
	for i := range backupList {
		if shouldDeleteBackup(ctx, backupList, i, retention.IgnoreForManualBackups, minBackupsToKeep, retentionThreshold) {
			backupsToDelete = append(backupsToDelete, backupList[i])
		}
	}

	return backupsToDelete, nil
}

// Checks whether need to delete backup from with index `backupIndex` at slice `backupList`
// according to retention constraints
func shouldDeleteBackup(ctx context.Context, backupList []cnpgv1.Backup, backupIndex int, ignoreForManualBackups bool, minBackupsToKeep int, retentionThreshold *time.Time) bool {
	backup := backupList[backupIndex]

	logger := logr.FromContextOrDiscard(ctx)

	// Skip if this is one of the newest backups we want to keep
	remainingBackups := len(backupList) - backupIndex
	if remainingBackups <= minBackupsToKeep {
		logger.Info("Keeping backup as part of MinBackupsToKeep", "backupName", backup.Name)
		return false
	}

	// Check if backup is a manual backup and should be ignored
	// Manual backup is a backup not owned by anything except BackupConfig
	// (for automated backups owner is either Cluster or ScheduledBackup resource)
	// We assume that all backups passed there are owned by BackupConfig,
	// so it is enough to check that there's only 1 OwnerReference
	if ignoreForManualBackups && len(backup.OwnerReferences) == 1 {
		logger.Info("Skipping manual backup", "backupName", backup.Name)
		return false
	}

	// Check if backup is older than retention threshold
	if retentionThreshold != nil {
		var backupTime time.Time

		// Use StartedAt if available, otherwise use creation timestamp
		if backup.Status.StartedAt != nil {
			backupTime = backup.Status.StartedAt.Time
		} else {
			backupTime = backup.CreationTimestamp.Time
		}

		if backupTime.Before(*retentionThreshold) {
			logger.Info("Marking backup for deletion based on age",
				"backupName", backup.Name,
				"backupTime", backupTime,
				"retentionThreshold", *retentionThreshold)
			return true
		}
	}
	return false
}

// calculateRetentionThreshold calculates the retention threshold time based on the retention policy
func calculateRetentionThreshold(retentionPolicy string) (time.Time, error) {
	// Parse the retention policy string (e.g., "7d", "4w", "1m")
	re := regexp.MustCompile(`^(\d+)([dwmh])$`)
	matches := re.FindStringSubmatch(retentionPolicy)
	if len(matches) != 3 {
		return time.Time{}, fmt.Errorf("invalid retention policy format: %s", retentionPolicy)
	}

	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid retention policy value: %s", matches[1])
	}

	unit := matches[2]
	now := time.Now()

	switch unit {
	case "h":
		return now.Add(-time.Duration(value) * time.Hour), nil
	case "d":
		return now.AddDate(0, 0, -value), nil
	case "w":
		return now.AddDate(0, 0, -value*7), nil
	case "m":
		return now.AddDate(0, -value, 0), nil
	default:
		return time.Time{}, fmt.Errorf("invalid retention policy unit: %s", unit)
	}
}
