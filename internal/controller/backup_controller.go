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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const backupFinalizerName = "cnpg-plugin-wal-g.yandex.cloud/backup-cleanup"

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups/finalizers,verbs=update

// Reconcile handles Backup resource events
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithName("BackupReconciler").WithValues("namespace", req.Namespace, "name", req.Name)

	// Get the Backup resource
	backup := &cnpgv1.Backup{}
	if err := r.Get(ctx, req.NamespacedName, backup); err != nil {
		// The Backup resource may have been deleted
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip backups not managed by plugin
	if backup.Spec.PluginConfiguration.Name != common.PluginName {
		return ctrl.Result{}, nil
	}

	// Backup is being deleted
	if !backup.ObjectMeta.DeletionTimestamp.IsZero() {
		// Handle backup deletion in background, because cleaning physical backup could take long time
		go r.handleBackupDeletion(ctx, backup)
		return ctrl.Result{}, nil
	}

	// Add finalizer if it doesn't exist
	if !containsString(backup.ObjectMeta.Finalizers, backupFinalizerName) {
		logger.Info("Detected new Backup managed by plugin, setting up finalizer")
		backup.ObjectMeta.Finalizers = append(backup.ObjectMeta.Finalizers, backupFinalizerName)
		if err := r.Update(ctx, backup); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// handleBackupDeletion removes actual backup from storage via wal-g
// and removes finalizer from Backup object after successful cleanup
//
// IMPORTANT: This function is long-running and should be started in separate goroutine
func (r *BackupReconciler) handleBackupDeletion(ctx context.Context, backup *cnpgv1.Backup) {
	logger := logr.FromContextOrDiscard(ctx)

	if !containsString(backup.ObjectMeta.Finalizers, backupFinalizerName) {
		// do nothing if backup has no cleanup finalizer
		return
	}

	if err := r.deleteWALGBackup(ctx, backup); err != nil {
		// If there's an error, don't remove the finalizer so we can retry
		logger.Error(err, "while deleting Backup via wal-g")
		return
	}

	// Remove our finalizer from the list and update
	backup.ObjectMeta.Finalizers = removeString(backup.ObjectMeta.Finalizers, backupFinalizerName)
	if err := r.Update(ctx, backup); err != nil {
		logger.Error(err, "while removing cleanup finalizer from Backup")
		return
	}
}

// deleteWALGBackup deletes the backup via WAL-G (could be long running)
func (r *BackupReconciler) deleteWALGBackup(ctx context.Context, backup *cnpgv1.Backup) error {
	logger := logr.FromContextOrDiscard(ctx)
	// Skip if BackupID is not set
	if backup.Status.BackupID == "" {
		logger.Info("Backup has no BackupID, skipping WAL-G deletion")
		return nil
	}

	// Get BackupConfig with secrets
	backupConfigWithSecrets, err := r.getBackupConfigForBackup(ctx, backup)
	if err != nil {
		return err
	}

	// Delete the backup using WAL-G
	logger.Info("Deleting backup from WAL-G", "backupID", backup.Status.BackupID)
	result, err := walg.DeleteBackup(ctx, *backupConfigWithSecrets, backup.Status.BackupID)
	if err != nil {
		logger.Error(err, "Failed to delete backup from WAL-G",
			"backupID", backup.Status.BackupID,
			"stdout", string(result.Stdout()),
			"stderr", string(result.Stderr()))
		return err
	}

	logger.Info("Successfully deleted backup from WAL-G", "backupID", backup.Status.BackupID)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cnpgv1.Backup{}).
		Complete(r)
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

func (r *BackupReconciler) getBackupConfigForBackup(ctx context.Context, backup *cnpgv1.Backup) (*v1beta1.BackupConfigWithSecrets, error) {
	logger := logr.FromContextOrDiscard(ctx).WithValues("backup.Name", backup.Name, "backup.Namespace", backup.Namespace)

	// Get the cluster
	cluster := &cnpgv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: backup.Namespace, Name: backup.Spec.Cluster.Name}, cluster); err != nil {
		logger.Error(err, "while getting Cluster")
		return nil, client.IgnoreNotFound(err)
	}

	// Get the BackupConfig for this cluster
	backupConfig, err := v1beta1.GetBackupConfigForCluster(ctx, r.Client, *cluster)
	if err != nil || backupConfig == nil {
		logger.Info("No BackupConfig found for cluster", "error", err)
		return nil, err
	}

	// Get BackupConfig with secrets
	backupConfigWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, r.Client)
	if err != nil {
		logger.Error(err, "while prefetching secrets data")
		return nil, err
	}

	return &backupConfigWithSecrets, nil
}

func (r *BackupReconciler) GetDependantBackups(ctx context.Context, backup *cnpgv1.Backup) ([]*cnpgv1.Backup, error) {
	logger := logr.FromContextOrDiscard(ctx).WithValues("backup.Name", backup.Name, "backup.Namespace", backup.Namespace).V(1) // Only debug logs for this method

	// Nothing to do with backups without BackupID
	if backup.Status.BackupID == "" {
		return make([]*cnpgv1.Backup, 0), nil
	}

	// Get BackupConfig with secrets
	backupConfigWithSecrets, err := r.getBackupConfigForBackup(ctx, backup)
	if err != nil {
		logger.Error(err, "while prefetching secrets data")
		return nil, err
	}

	// List all backups in the same namespace
	backupsList := cnpgv1.BackupList{}
	err = r.Client.List(ctx, &backupsList, &client.ListOptions{Namespace: backup.Namespace})
	if err != nil {
		logger.Error(err, "while getting Backup list")
		return nil, err
	}

	// Get all WAL-G backups
	walgBackups, err := walg.GetBackupsList(ctx, *backupConfigWithSecrets)
	if err != nil {
		logger.Error(err, "while getting WAL-G backups list")
		return nil, err
	}

	// Find the WAL-G backup metadata for the current backup
	currentWalgBackup, err := walg.GetBackupByName(ctx, walgBackups, backup.Status.BackupID)
	if err != nil {
		logger.Error(err, "while finding current backup in WAL-G")
		return nil, err
	}

	if currentWalgBackup == nil {
		logger.Info("Current backup not found in WAL-G", "backupID", backup.Status.BackupID)
		return make([]*cnpgv1.Backup, 0), nil
	}

	// Find all dependent backups (including indirect ones)
	dependentWalgBackups := currentWalgBackup.GetDependantBackups(ctx, walgBackups, true)
	logger.Info("Found dependent WAL-G backups", "count", len(dependentWalgBackups))

	// Create a map of backupID -> Backup for quick lookup
	backupIDMap := make(map[string]*cnpgv1.Backup)
	for _, backup := range backupsList.Items {
		if backup.Status.BackupID != "" {
			backupIDMap[backup.Status.BackupID] = &backup
		}
	}

	// Match WAL-G dependant backups with Backup resources
	result := make([]*cnpgv1.Backup, 0, len(dependentWalgBackups))
	for _, walgBackup := range dependentWalgBackups {
		if k8sBackup, exists := backupIDMap[walgBackup.BackupName]; exists {
			result = append(result, k8sBackup)
		}
	}

	logger.Info("Found dependent Backup resources", "count", len(result))
	return result, nil
}
