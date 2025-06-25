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
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
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
const backupTypeAnnotationName = "cnpg-plugin-wal-g.yandex.cloud/backup-type"
const backupAllDependentsAnnotationName = "cnpg-plugin-wal-g.yandex.cloud/dependent-backups-all"
const backupDirectDependentsAnnotationName = "cnpg-plugin-wal-g.yandex.cloud/dependent-backups-direct"

type BackupType string

const BackupTypeFull BackupType = "FULL"
const BackupTypeIncremental BackupType = "INCREMENTAL"

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups/finalizers,verbs=update

// SetupWithManager sets up the controller with the Manager.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cnpgv1.Backup{}).
		Complete(r)
}

// Reconcile handles Backup resource events
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx).WithName("BackupReconciler").WithValues("namespace", req.Namespace, "name", req.Name)

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

	// Set owner reference to BackupConfig
	err := r.reconcileOwnerReference(ctx, backup)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile backup (and probably related backups) annotations
	err = r.reconcileRelatedBackupsAnnotations(ctx, backup)
	if err != nil {
		return ctrl.Result{}, err
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

// reconcileOwnerReference sets the BackupConfig as the owner of the Backup resource
// This is needed to properly track which backups belong to which BackupConfig
// for retention policy and other operations
func (r *BackupReconciler) reconcileOwnerReference(ctx context.Context, backup *cnpgv1.Backup) error {
	logger := logr.FromContextOrDiscard(ctx).WithValues("backup.Name", backup.Name, "backup.Namespace", backup.Namespace)

	// Skip if backup already has an owner reference to a BackupConfig
	for _, ownerRef := range backup.OwnerReferences {
		if ownerRef.Kind == "BackupConfig" {
			logger.V(1).Info("Backup already has BackupConfig owner reference", "ownerRef", ownerRef)
			return nil
		}
	}

	// Get the cluster
	cluster := &cnpgv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: backup.Namespace, Name: backup.Spec.Cluster.Name}, cluster); err != nil {
		logger.Error(err, "Failed to get Cluster for Backup")
		return client.IgnoreNotFound(err)
	}

	// Get the BackupConfig for this cluster
	backupConfig, err := v1beta1.GetBackupConfigForCluster(ctx, r.Client, *cluster)
	if err != nil || backupConfig == nil {
		logger.Info("No BackupConfig found for cluster", "error", err)
		return err
	}

	// Set the BackupConfig as the owner of the Backup
	backup.OwnerReferences = append(backup.OwnerReferences, metav1.OwnerReference{
		APIVersion:         "cnpg-extensions.yandex.cloud/v1beta1",
		Kind:               "BackupConfig",
		Name:               backupConfig.Name,
		UID:                backupConfig.UID,
		BlockOwnerDeletion: ptr.To(true),
	})

	logger.Info("Setting BackupConfig as owner of Backup",
		"backupConfig.Name", backupConfig.Name,
		"backup.Name", backup.Name)

	return r.Update(ctx, backup)
}

func (r *BackupReconciler) reconcileRelatedBackupsAnnotations(ctx context.Context, backup *cnpgv1.Backup) error {
	logger := logr.FromContextOrDiscard(ctx).WithValues("backup.Name", backup.Name, "backup.Namespace", backup.Namespace)

	// Get BackupConfig with secrets
	backupConfigWithSecrets, err := r.getBackupConfigForBackup(ctx, backup)
	if err != nil {
		logger.Error(err, "while prefetching secrets data")
		return err
	}

	// Get all WAL-G backups
	walgBackups, err := walg.GetBackupsList(ctx, *backupConfigWithSecrets)
	if err != nil {
		logger.Error(err, "while getting WAL-G backups list")
		return err
	}

	updated, err := r.reconcileBackupAnnotations(ctx, backup, backupConfigWithSecrets, walgBackups)
	if err != nil {
		return fmt.Errorf("while reconciling backup %s annotations: %w", backup.Name, err)
	}

	// If current backup is incremental - we need to update other backups annotations
	// to update their dependent backups list
	if updated && backup.Annotations[backupTypeAnnotationName] == string(BackupTypeIncremental) {
		backupsToReconcile, err := r.listBackupsOwnedByBackupConfig(ctx, backupConfigWithSecrets)
		if err != nil {
			return err
		}
		for _, otherBackup := range backupsToReconcile {
			if otherBackup.Name == backup.Name {
				continue
			}
			r.reconcileBackupAnnotations(ctx, &otherBackup, backupConfigWithSecrets, walgBackups)
		}
	}

	return nil
}

func (r *BackupReconciler) reconcileBackupAnnotations(
	ctx context.Context,
	backup *cnpgv1.Backup,
	backupConfig *v1beta1.BackupConfigWithSecrets,
	walgBackups []walg.WalgBackupMetadata,
) (bool, error) {
	logger := logr.FromContextOrDiscard(ctx).WithValues("backup.Name", backup.Name, "backup.Namespace", backup.Namespace)

	// If backup has not BackupID - skipping
	if backup.Status.BackupID == "" {
		return false, nil
	}

	changedBackup := backup.DeepCopy()

	if strings.Contains(changedBackup.Status.BackupID, "_D_") {
		changedBackup.Annotations[backupTypeAnnotationName] = string(BackupTypeIncremental)
	} else {
		changedBackup.Annotations[backupTypeAnnotationName] = string(BackupTypeFull)
	}

	backups, err := r.listBackupsOwnedByBackupConfig(ctx, backupConfig)
	if err != nil {
		return false, err
	}

	directDependentBackups, err := buildDependentBackupsForBackup(ctx, backup, backups, walgBackups, false)
	if err != nil {
		return false, err
	}
	dependentBackupsNames := lo.Map(directDependentBackups, func(b cnpgv1.Backup, _ int) string {
		return b.Name
	})
	changedBackup.Annotations[backupDirectDependentsAnnotationName] = strings.Join(dependentBackupsNames, "; ")

	allDependentBackups, err := buildDependentBackupsForBackup(ctx, backup, backups, walgBackups, true)
	if err != nil {
		return false, err
	}
	allDependentBackupsNames := lo.Map(allDependentBackups, func(b cnpgv1.Backup, _ int) string {
		return b.Name
	})
	changedBackup.Annotations[backupAllDependentsAnnotationName] = strings.Join(allDependentBackupsNames, "; ")

	if equality.Semantic.DeepEqual(changedBackup, backup) {
		// There's no need to hit the API server again
		return false, nil
	}

	logger.V(1).Info(
		"Patching CNPG Backup annotations",
		"backup.Name", changedBackup.Name,
		"backup.Namespace", changedBackup.Namespace,
		"backup.Annotations", changedBackup.Annotations,
	)

	return true, r.Client.Patch(ctx, changedBackup, client.MergeFrom(backup))
}

// handleBackupDeletion removes actual backup from storage via wal-g
// and removes finalizer from Backup object after successful cleanup
//
// IMPORTANT: This function is long-running and should be started in separate goroutine
func (r *BackupReconciler) handleBackupDeletion(ctx context.Context, backup *cnpgv1.Backup) {
	logger := logr.FromContextOrDiscard(ctx)

	if backup.Annotations[backupAllDependentsAnnotationName] != "" {
		// TODO: emit event for backup
		logger.Info("Cannot delete backup because it still has dependent backups", "backup", backup.Name, "dependents", backup.Annotations[backupAllDependentsAnnotationName])
		return
	}

	// Delete physical backup from storage (if only having proper finalizer)
	if containsString(backup.ObjectMeta.Finalizers, backupFinalizerName) {
		err := r.deleteWALGBackup(ctx, backup)
		if err != nil {
			// If there's an error, don't remove the finalizer so we can retry
			logger.Error(err, "while deleting Backup via wal-g")
			return
		}
	}

	// If deleting incremental backup - we need to update annotations for other backups with same BackupConfig
	// to make their depentent backups list consistent after current backup deletion
	if backup.Annotations[backupTypeAnnotationName] == string(BackupTypeIncremental) {
		backupConfigWithSecrets, err := r.getBackupConfigForBackup(ctx, backup)
		if err != nil {
			logger.Error(err, "while prefetching secrets data")
			return
		}

		walgBackups, err := walg.GetBackupsList(ctx, *backupConfigWithSecrets)
		if err != nil {
			logger.Error(err, "while getting WAL-G backups list")
			return
		}

		backups, err := r.listBackupsOwnedByBackupConfig(ctx, backupConfigWithSecrets)
		if err != nil {
			return
		}

		// Reconcile other backups annotations for same backupConfig - some of them can have this backup as dependency
		for _, otherBackup := range backups {
			r.reconcileBackupAnnotations(ctx, &otherBackup, backupConfigWithSecrets, walgBackups)
		}
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

func (r *BackupReconciler) listBackupsOwnedByBackupConfig(ctx context.Context, backupConfig *v1beta1.BackupConfigWithSecrets) ([]cnpgv1.Backup, error) {
	// List all backups in the same namespace
	backupsList := cnpgv1.BackupList{}
	err := r.Client.List(ctx, &backupsList, &client.ListOptions{Namespace: backupConfig.Namespace})
	if err != nil {
		return make([]cnpgv1.Backup, 0), fmt.Errorf("while getting Backup List: %w", err)
	}

	// Filter only backups owned by backupConfig
	backups := lo.Filter(backupsList.Items, func(b cnpgv1.Backup, _ int) bool {
		return lo.ContainsBy(b.OwnerReferences, func(o metav1.OwnerReference) bool {
			return o.Kind == "BackupConfig" && o.Name == backupConfig.Name
		})
	})

	return backups, nil
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

func buildDependentBackupsForBackup(
	ctx context.Context,
	backup *cnpgv1.Backup,
	backups []cnpgv1.Backup,
	walgBackups []walg.WalgBackupMetadata,
	includeIndirect bool,
) ([]cnpgv1.Backup, error) {
	logger := logr.FromContextOrDiscard(ctx).WithValues("backup.Name", backup.Name, "backup.Namespace", backup.Namespace) // Only debug logs for this method

	// Nothing to do with backups without BackupID
	if backup.Status.BackupID == "" {
		return make([]cnpgv1.Backup, 0), nil
	}

	// For getting dependent backups it is enough to have backupName only
	currentWalgBackup := walg.WalgBackupMetadata{
		BackupName: backup.Status.BackupID,
	}

	// Find all dependent backups (including indirect ones)
	dependentWalgBackups := currentWalgBackup.GetDependentBackups(ctx, walgBackups, includeIndirect)
	logger.V(1).Info("Found dependent WAL-G backups", "backups", dependentWalgBackups)

	// Filter Backup resources - leave only some of them, matching dependent wal-g backups && not being deleted
	result := lo.Filter(backups, func(candidate cnpgv1.Backup, _ int) bool {
		return candidate.DeletionTimestamp.IsZero() && lo.ContainsBy(dependentWalgBackups, func(walgBackup walg.WalgBackupMetadata) bool {
			return walgBackup.BackupName == candidate.Status.BackupID
		})
	})

	logger.V(1).Info("Found dependent Backup resources", "backups", result)
	return result, nil
}
