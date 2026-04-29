/*
Copyright 2025 YANDEX LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/pkg/walg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Maximum number of concurrent status reconciliation workers.
	// Controls the static goroutine pool size to prevent overloading S3 storage.
	maxStatusReconcileConcurrency = 3

	// Timeout for a single BackupConfig status reconciliation
	statusReconcileTimeout = 10 * time.Minute

	// Size of the shared reconciliation request queue
	statusReconcileQueueSize = 128
)

// BackupConfigStatusController periodically reconciles BackupConfigStatus fields
// for all BackupConfig resources. It uses a static goroutine pool with a single
// shared queue to limit concurrency and prevent overloading S3 storage.
//
// Per-BackupConfig deduplication ensures that the same resource is not reconciled
// concurrently by multiple workers. Manual status updates can be enqueued
// via EnqueueStatusUpdate.
type BackupConfigStatusController struct {
	client        client.Client
	checkInterval time.Duration

	// queue is the single shared channel for all reconciliation requests
	queue chan types.NamespacedName

	// inFlight tracks BackupConfigs currently being reconciled to prevent
	// parallel reconciliation of the same resource
	inFlight   map[string]struct{}
	inFlightMu sync.Mutex

	// ctx is the root context for the controller, set during Start
	ctx    context.Context
	logger logr.Logger
}

// NewBackupConfigStatusController creates a new BackupConfigStatusController
func NewBackupConfigStatusController(client client.Client, checkInterval time.Duration) *BackupConfigStatusController {
	return &BackupConfigStatusController{
		client:        client,
		checkInterval: checkInterval,
		queue:         make(chan types.NamespacedName, statusReconcileQueueSize),
		inFlight:      make(map[string]struct{}),
	}
}

// Start begins the status controller's periodic reconciliation loop and worker pool
func (c *BackupConfigStatusController) Start(ctx context.Context) error {
	c.ctx = ctx
	c.logger = logr.FromContextOrDiscard(ctx).WithName("BackupConfigStatusController")
	c.logger.Info("Starting BackupConfig status controller",
		"checkInterval", c.checkInterval,
		"workers", maxStatusReconcileConcurrency,
	)

	// Start the static worker pool
	var wg sync.WaitGroup
	for i := 0; i < maxStatusReconcileConcurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			c.worker(ctx, workerID)
		}(i)
	}

	// Periodic enqueue loop
	ticker := time.NewTicker(c.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Stopping BackupConfig status controller, waiting for workers to finish")
			wg.Wait()
			c.logger.Info("All workers stopped")
			return nil
		case <-ticker.C:
			c.logger.Info("Running periodic BackupConfig status reconciliation")
			c.enqueueAllStatuses(ctx)
		}
	}
}

// EnqueueStatusUpdate enqueues a status reconciliation for a specific BackupConfig.
// This can be called externally (e.g. after backup creation or BackupConfig changes)
// to trigger an immediate status update. Non-blocking: if the queue is full,
// the request is dropped (the periodic reconciliation will pick it up later).
func (c *BackupConfigStatusController) EnqueueStatusUpdate(key types.NamespacedName) {
	select {
	case c.queue <- key:
		c.logger.V(1).Info("Enqueued status update", "backupConfig", key)
	default:
		c.logger.V(1).Info("Status reconciliation queue is full, dropping request", "backupConfig", key)
	}
}

// enqueueAllStatuses lists all BackupConfig resources and enqueues status reconciliation for each
func (c *BackupConfigStatusController) enqueueAllStatuses(ctx context.Context) {
	backupConfigList := &v1beta1.BackupConfigList{}
	if err := c.client.List(ctx, backupConfigList); err != nil {
		c.logger.Error(err, "Failed to list BackupConfig resources")
		return
	}

	for i := range backupConfigList.Items {
		backupConfig := &backupConfigList.Items[i]

		// Skip BackupConfigs that are being deleted
		if !backupConfig.DeletionTimestamp.IsZero() {
			continue
		}

		c.EnqueueStatusUpdate(types.NamespacedName{
			Namespace: backupConfig.Namespace,
			Name:      backupConfig.Name,
		})
	}
}

// worker is a single worker goroutine that processes reconciliation requests from the shared queue
func (c *BackupConfigStatusController) worker(ctx context.Context, workerID int) {
	logger := c.logger.WithValues("worker", workerID)
	logger.V(1).Info("Worker started")

	for {
		select {
		case <-ctx.Done():
			logger.V(1).Info("Worker stopping")
			return
		case key := <-c.queue:
			queueKey := key.String()

			// Check if this BackupConfig is already being reconciled
			if !c.tryAcquire(queueKey) {
				logger.V(1).Info("BackupConfig already being reconciled, skipping", "backupConfig", queueKey)
				continue
			}

			func() {
				defer c.release(queueKey)

				reconcileCtx, cancel := context.WithTimeout(ctx, statusReconcileTimeout)
				defer cancel()

				bcLogger := logger.WithValues("backupConfig", key.Name, "namespace", key.Namespace)
				if err := c.reconcileStatusByKey(reconcileCtx, key, bcLogger); err != nil {
					bcLogger.Error(err, "Failed to reconcile BackupConfig status")
				}
			}()
		}
	}
}

// tryAcquire attempts to mark a BackupConfig as in-flight.
// Returns true if the BackupConfig was not already in-flight and is now marked.
func (c *BackupConfigStatusController) tryAcquire(queueKey string) bool {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()

	if _, exists := c.inFlight[queueKey]; exists {
		return false
	}
	c.inFlight[queueKey] = struct{}{}
	return true
}

// release removes a BackupConfig from the in-flight set
func (c *BackupConfigStatusController) release(queueKey string) {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	delete(c.inFlight, queueKey)
}

// reconcileStatusByKey reconciles the status of a BackupConfig identified by NamespacedName
func (c *BackupConfigStatusController) reconcileStatusByKey(
	ctx context.Context,
	key types.NamespacedName,
	logger logr.Logger,
) error {
	backupConfig := &v1beta1.BackupConfig{}
	if err := c.client.Get(ctx, key, backupConfig); err != nil {
		return client.IgnoreNotFound(err)
	}

	// Skip if being deleted
	if !backupConfig.DeletionTimestamp.IsZero() {
		return nil
	}

	return c.reconcileStatus(ctx, backupConfig, logger)
}

// reconcileStatus reconciles the status of a single BackupConfig
func (c *BackupConfigStatusController) reconcileStatus(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
	logger logr.Logger,
) error {
	logger.Info("Reconciling BackupConfig status")

	// Re-fetch the BackupConfig to get the latest version
	latestBackupConfig := &v1beta1.BackupConfig{}
	if err := c.client.Get(ctx, client.ObjectKeyFromObject(backupConfig), latestBackupConfig); err != nil {
		return fmt.Errorf("failed to get latest BackupConfig: %w", err)
	}
	backupConfig = latestBackupConfig

	// Prefetch secrets for wal-g client
	backupConfigWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, c.client)
	if err != nil {
		logger.Error(err, "Failed to prefetch secrets data, marking credentials as invalid")
		setCondition(backupConfig, v1beta1.ConditionTypeCredentialsValid, metav1.ConditionFalse,
			"SecretsFetchFailed", fmt.Sprintf("Failed to prefetch secrets: %v", err))
		backupConfig.Status.Phase = v1beta1.BackupConfigPhaseFailed
		return c.client.Status().Update(ctx, backupConfig)
	}

	// Use a fixed PG version for storage checks (version doesn't matter for st check commands)
	walgClient := walg.NewClientFromBackupConfig(backupConfigWithSecrets, 16)

	// Mark credentials as valid since we successfully fetched secrets
	setCondition(backupConfig, v1beta1.ConditionTypeCredentialsValid, metav1.ConditionTrue,
		"CredentialsResolved", "All referenced secrets and configmaps resolved successfully")

	// Check storage readability
	c.checkStorageReadable(ctx, backupConfig, walgClient, logger)

	// Check storage writability
	c.checkStorageWritable(ctx, backupConfig, walgClient, logger)

	// Check WAL integrity and reconcile WAL-related status fields via wal-g wal-show
	c.reconcileWALStatus(ctx, backupConfig, backupConfigWithSecrets, logger)

	// Reconcile backup-related status fields from wal-g backup-list and CNPG Backup resources
	c.reconcileBackupFields(ctx, backupConfig, backupConfigWithSecrets, logger)

	// Determine overall phase from conditions
	backupConfig.Status.Phase = determinePhase(backupConfig)

	// Update the status
	if err := c.client.Status().Update(ctx, backupConfig); err != nil {
		return fmt.Errorf("failed to update BackupConfig status: %w", err)
	}

	logger.Info("Successfully reconciled BackupConfig status", "phase", backupConfig.Status.Phase)
	return nil
}

// checkStorageReadable checks if the storage is accessible for reading
func (c *BackupConfigStatusController) checkStorageReadable(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
	walgClient *walg.Client,
	logger logr.Logger,
) {
	_, err := walgClient.StorageCheckReadable(ctx)
	if err != nil {
		logger.Error(err, "Storage read check failed")
		setCondition(backupConfig, v1beta1.ConditionTypeStorageReadable, metav1.ConditionFalse,
			"StorageReadCheckFailed", fmt.Sprintf("Storage read check failed: %v", err))
	} else {
		setCondition(backupConfig, v1beta1.ConditionTypeStorageReadable, metav1.ConditionTrue,
			"StorageReadCheckPassed", "Storage is accessible for reading")
	}
}

// checkStorageWritable checks if the storage is accessible for writing
func (c *BackupConfigStatusController) checkStorageWritable(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
	walgClient *walg.Client,
	logger logr.Logger,
) {
	_, err := walgClient.StorageCheckWritable(ctx)
	if err != nil {
		logger.Error(err, "Storage write check failed")
		setCondition(backupConfig, v1beta1.ConditionTypeStorageWritable, metav1.ConditionFalse,
			"StorageWriteCheckFailed", fmt.Sprintf("Storage write check failed: %v", err))
	} else {
		setCondition(backupConfig, v1beta1.ConditionTypeStorageWritable, metav1.ConditionTrue,
			"StorageWriteCheckPassed", "Storage is accessible for writing")
	}
}

// reconcileWALStatus checks WAL integrity and evaluates recoverability points
// by running `wal-g wal-show --detailed-json` across all known PG major versions
func (c *BackupConfigStatusController) reconcileWALStatus(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
	backupConfigWithSecrets *v1beta1.BackupConfigWithSecrets,
	logger logr.Logger,
) {
	allTimelinesOK := true
	hasMissingSegments := false
	var earliestBackupTime *time.Time

	// Iterate over all known PG major versions to collect WAL info
	for pgVersion := 11; pgVersion <= 19; pgVersion++ {
		walgClient := walg.NewClientFromBackupConfig(backupConfigWithSecrets, pgVersion)
		timelines, err := walgClient.WALShow(ctx)
		if err != nil {
			// Not all PG versions will have WAL data, so we just log and continue
			logger.V(1).Info("Failed to get WAL info for PG version, skipping",
				"pgVersion", pgVersion, "error", err)
			continue
		}

		for i := range timelines {
			timeline := &timelines[i]

			if !timeline.IsOK() {
				allTimelinesOK = false
			}
			if timeline.HasMissingSegments() {
				hasMissingSegments = true
			}

			// Find the earliest backup start time across all timelines for first recoverability point
			for j := range timeline.Backups {
				startTime, err := timeline.Backups[j].StartTime()
				if err != nil {
					logger.V(1).Info("Failed to parse backup start time", "error", err)
					continue
				}
				if earliestBackupTime == nil || startTime.Before(*earliestBackupTime) {
					t := startTime
					earliestBackupTime = &t
				}
			}
		}
	}

	// Update first recoverability point from wal-g data if available
	if earliestBackupTime != nil {
		backupConfig.Status.FirstRecoverabilityPoint = &metav1.Time{Time: *earliestBackupTime}
	}

	// Set WAL integrity condition
	if allTimelinesOK && !hasMissingSegments {
		setCondition(backupConfig, v1beta1.ConditionTypeWALIntegrityCheck, metav1.ConditionTrue,
			"WALIntegrityCheckPassed", "All WAL timelines are OK with no missing segments")
	} else {
		reason := "WALIntegrityCheckFailed"
		message := "WAL integrity issues detected:"
		if !allTimelinesOK {
			message += " some timelines have non-OK status"
		}
		if hasMissingSegments {
			if !allTimelinesOK {
				message += ","
			}
			message += " missing WAL segments detected"
		}
		setCondition(backupConfig, v1beta1.ConditionTypeWALIntegrityCheck, metav1.ConditionFalse,
			reason, message)
	}
}

// reconcileBackupFields reconciles backup-related status fields by fetching backup metadata
// from wal-g backup-list (for storage size via CompressedSize) and from CNPG Backup resources
// (for last successful/failed backup timestamps and first recoverability point)
func (c *BackupConfigStatusController) reconcileBackupFields(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
	backupConfigWithSecrets *v1beta1.BackupConfigWithSecrets,
	logger logr.Logger,
) {
	// Reconcile timestamps from CNPG Backup resources
	c.reconcileBackupTimestamps(ctx, backupConfig, logger)

	// Reconcile consumed storage from wal-g backup-list across all known PG versions
	c.reconcileConsumedStorage(ctx, backupConfig, backupConfigWithSecrets, logger)
}

// reconcileBackupTimestamps reconciles timestamp-related status fields from CNPG Backup resources
func (c *BackupConfigStatusController) reconcileBackupTimestamps(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
	logger logr.Logger,
) {
	// List all Backup resources in the same namespace
	backupList := &cnpgv1.BackupList{}
	if err := c.client.List(ctx, backupList, client.InNamespace(backupConfig.Namespace)); err != nil {
		logger.Error(err, "Failed to list Backup resources")
		return
	}

	// Filter backups owned by this BackupConfig
	var relevantBackups []cnpgv1.Backup
	for i := range backupList.Items {
		backup := &backupList.Items[i]
		if lo.ContainsBy(backup.OwnerReferences, func(ref metav1.OwnerReference) bool {
			return ref.Kind == "BackupConfig" && ref.Name == backupConfig.Name
		}) {
			relevantBackups = append(relevantBackups, *backup)
		}
	}

	if len(relevantBackups) == 0 {
		return
	}

	// Sort backups by start time (oldest first)
	sort.Slice(relevantBackups, func(i, j int) bool {
		timeI := getBackupTime(&relevantBackups[i])
		timeJ := getBackupTime(&relevantBackups[j])
		return timeI.Before(timeJ)
	})

	// Calculate first recoverability point (earliest successful backup start time)
	successfulBackups := lo.Filter(relevantBackups, func(b cnpgv1.Backup, _ int) bool {
		return b.Status.Phase == cnpgv1.BackupPhaseCompleted
	})
	if len(successfulBackups) > 0 {
		firstTime := getBackupTime(&successfulBackups[0])
		backupConfig.Status.FirstRecoverabilityPoint = &metav1.Time{Time: firstTime}

		lastTime := getBackupTime(&successfulBackups[len(successfulBackups)-1])
		backupConfig.Status.LastSuccessfulBackup = &metav1.Time{Time: lastTime}
	}

	// Find last failed backup
	failedBackups := lo.Filter(relevantBackups, func(b cnpgv1.Backup, _ int) bool {
		return b.Status.Phase == cnpgv1.BackupPhaseFailed
	})
	if len(failedBackups) > 0 {
		lastFailedTime := getBackupTime(&failedBackups[len(failedBackups)-1])
		backupConfig.Status.LastFailedBackup = &metav1.Time{Time: lastFailedTime}
	}
}

// reconcileConsumedStorage calculates total consumed storage space by fetching backup metadata
// from wal-g backup-list (for backup CompressedSize) and wal-g st ls (for WAL file sizes)
// across all known PG major versions
func (c *BackupConfigStatusController) reconcileConsumedStorage(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
	backupConfigWithSecrets *v1beta1.BackupConfigWithSecrets,
	logger logr.Logger,
) {
	var backupBytes int64
	var walBytes int64

	// Iterate over all known PG major versions to collect backup and WAL sizes
	// (same approach as in deleteBackupConfig)
	for pgVersion := 11; pgVersion <= 19; pgVersion++ {
		walgClient := walg.NewClientFromBackupConfig(backupConfigWithSecrets, pgVersion)

		// Sum backup compressed sizes from wal-g backup-list
		backupsList, err := walgClient.GetBackupsList(ctx)
		if err != nil {
			// Not all PG versions will have backups, so we just log and continue
			logger.V(1).Info("Failed to get backups list for PG version, skipping",
				"pgVersion", pgVersion, "error", err)
			continue
		}

		for i := range backupsList {
			backupBytes += int64(backupsList[i].CompressedSize)
		}

		// Sum WAL file sizes from wal-g st ls wal_005/
		// wal_005/ is the wal-g internal directory for WAL segments
		walSize, err := walgClient.StorageLsTotalSize(ctx, "wal_005/")
		if err != nil {
			logger.V(1).Info("Failed to get WAL directory size for PG version, skipping",
				"pgVersion", pgVersion, "error", err)
		} else {
			walBytes += walSize
		}
	}

	totalBytes := backupBytes + walBytes
	if totalBytes > 0 {
		storageInfo := &v1beta1.ConsumedStorageInfo{
			TotalBytes: &totalBytes,
			Total:      formatBytesHumanReadable(totalBytes),
		}
		if backupBytes > 0 {
			storageInfo.BackupsBytes = &backupBytes
			storageInfo.Backups = formatBytesHumanReadable(backupBytes)
		}
		if walBytes > 0 {
			storageInfo.WALBytes = &walBytes
			storageInfo.WAL = formatBytesHumanReadable(walBytes)
		}
		backupConfig.Status.ConsumedStorage = storageInfo
	}
}

// formatBytesHumanReadable converts bytes to a human-readable string using binary (IEC) units
func formatBytesHumanReadable(bytes int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
		tib = 1024 * gib
	)

	switch {
	case bytes >= tib:
		return fmt.Sprintf("%.2f TiB", float64(bytes)/float64(tib))
	case bytes >= gib:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/float64(gib))
	case bytes >= mib:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/float64(mib))
	case bytes >= kib:
		return fmt.Sprintf("%.2f KiB", float64(bytes)/float64(kib))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// getBackupTime returns the backup start time, falling back to creation timestamp
func getBackupTime(backup *cnpgv1.Backup) time.Time {
	if backup.Status.StartedAt != nil {
		return backup.Status.StartedAt.Time
	}
	return backup.CreationTimestamp.Time
}

// setCondition sets or updates a condition on the BackupConfig status
func setCondition(backupConfig *v1beta1.BackupConfig, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	newCondition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: backupConfig.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	// Find existing condition and update it
	for i, existing := range backupConfig.Status.Conditions {
		if existing.Type == conditionType {
			// Only update LastTransitionTime if the status actually changed
			if existing.Status == status {
				newCondition.LastTransitionTime = existing.LastTransitionTime
			}
			backupConfig.Status.Conditions[i] = newCondition
			return
		}
	}

	// Condition not found, append it
	backupConfig.Status.Conditions = append(backupConfig.Status.Conditions, newCondition)
}

// determinePhase determines the overall phase based on conditions
func determinePhase(backupConfig *v1beta1.BackupConfig) v1beta1.BackupConfigPhase {
	if len(backupConfig.Status.Conditions) == 0 {
		return v1beta1.BackupConfigPhaseUnknown
	}

	hasFalse := false
	allTrue := true

	for _, condition := range backupConfig.Status.Conditions {
		if condition.Status == metav1.ConditionFalse {
			hasFalse = true
			allTrue = false
		} else if condition.Status != metav1.ConditionTrue {
			allTrue = false
		}
	}

	// If credentials are invalid or storage is not accessible for both read and write, it's Failed
	credentialsValid := getConditionStatus(backupConfig, v1beta1.ConditionTypeCredentialsValid)
	storageReadable := getConditionStatus(backupConfig, v1beta1.ConditionTypeStorageReadable)
	storageWritable := getConditionStatus(backupConfig, v1beta1.ConditionTypeStorageWritable)

	if credentialsValid == metav1.ConditionFalse {
		return v1beta1.BackupConfigPhaseFailed
	}

	if storageReadable == metav1.ConditionFalse && storageWritable == metav1.ConditionFalse {
		return v1beta1.BackupConfigPhaseFailed
	}

	if allTrue {
		return v1beta1.BackupConfigPhaseHealthy
	}

	if hasFalse {
		return v1beta1.BackupConfigPhaseDegraded
	}

	return v1beta1.BackupConfigPhaseUnknown
}

// getConditionStatus returns the status of a specific condition, or empty string if not found
func getConditionStatus(backupConfig *v1beta1.BackupConfig, conditionType string) metav1.ConditionStatus {
	for _, condition := range backupConfig.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return ""
}
