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
	"slices"
	"strconv"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	DeletionController *BackupDeletionController
}

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
	logger := logr.FromContextOrDiscard(ctx)
	ctx = logr.NewContext(ctx, logger)

	logger.V(1).Info("Reconciling backup")

	backup := &cnpgv1.Backup{}
	if err := r.Get(ctx, req.NamespacedName, backup); err != nil {
		// The Backup resource may have been deleted
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip backups not managed by plugin
	if backup.Spec.PluginConfiguration == nil ||
		backup.Spec.PluginConfiguration.Name != common.PluginName ||
		backup.Spec.Method != cnpgv1.BackupMethodPlugin {
		return ctrl.Result{}, nil
	}

	// Backup is being deleted - need to reconcile related backups annotations and to remove physical wal-g backup
	if !backup.DeletionTimestamp.IsZero() {
		// If backup has dependents or is dependent, then its deletion requires to update annotations
		if err := r.reconcileRelatedBackupsMetadata(ctx, backup); err != nil {
			return ctrl.Result{}, err
		}

		// Handle backup deletion in background, because cleaning physical backup could take long time
		if err := r.DeletionController.EnqueueBackupDeletion(ctx, backup); err != nil {
			logger.Error(err, "Cannot process backup deletion, requeing in 3 minutes")
			return ctrl.Result{
				RequeueAfter: 3 * time.Minute,
			}, nil
		}
		return ctrl.Result{}, nil
	}

	// Set owner reference to BackupConfig
	err := r.reconcileOwnerReference(ctx, backup)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile backup (and probably related backups) annotations
	err = r.reconcileRelatedBackupsMetadata(ctx, backup)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Get BackupConfig to check if deletion management is disabled
	backupConfig, err := v1beta1.GetBackupConfigForBackup(ctx, r.Client, backup)
	if err != nil {
		logger.Error(err, "Failed to get BackupConfig for Backup when checking deletion management")
		return ctrl.Result{}, err
	}

	if err := r.reconcileFinalizers(ctx, backup, backupConfig); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile WAL-G backup permanence - mark manual backups as permanent to prevent infinite WAL archiving
	err = r.reconcileBackupPermanence(ctx, backup)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BackupReconciler) reconcileFinalizers(ctx context.Context, backup *cnpgv1.Backup, backupConfig *v1beta1.BackupConfig) error {
	logger := logr.FromContextOrDiscard(ctx)

	// Add finalizer if it doesn't exist and deletion management is not disabled
	shouldAddFinalizer := !containsString(backup.Finalizers, v1beta1.BackupFinalizerName) &&
		!backupConfig.Spec.Retention.IgnoreForBackupDeletion

	if shouldAddFinalizer {
		logger.Info("Detected new Backup managed by plugin, setting up finalizer")
		backup.Finalizers = append(backup.Finalizers, v1beta1.BackupFinalizerName)
		if err := r.Update(ctx, backup); err != nil {
			return err
		}
	} else if backupConfig.Spec.Retention.IgnoreForBackupDeletion && containsString(backup.Finalizers, v1beta1.BackupFinalizerName) {
		// If deletion management is disabled but finalizer exists, remove it
		logger.Info("Deletion management disabled for BackupConfig, removing finalizer from Backup")
		backup.Finalizers = removeString(backup.Finalizers, v1beta1.BackupFinalizerName)
		if err := r.Update(ctx, backup); err != nil {
			return err
		}
	}

	return nil
}

// reconcileOwnerReference sets the BackupConfig as the owner of the Backup resource
// This is needed to properly track which backups belong to which BackupConfig
// for retention policy and other operations
func (r *BackupReconciler) reconcileOwnerReference(ctx context.Context, backup *cnpgv1.Backup) error {
	logger := logr.FromContextOrDiscard(ctx)

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
	backupConfig, err := v1beta1.GetBackupConfigForCluster(ctx, r.Client, cluster)
	if err != nil || backupConfig == nil {
		logger.Info("No BackupConfig found for cluster", "error", err)
		return err
	}

	// Set the BackupConfig as the owner of the Backup
	backup.OwnerReferences = append(backup.OwnerReferences, metav1.OwnerReference{
		APIVersion:         fmt.Sprintf("%s/%s", v1beta1.GroupVersion.Group, v1beta1.GroupVersion.Version),
		Kind:               "BackupConfig",
		Name:               backupConfig.Name,
		UID:                backupConfig.UID,
		BlockOwnerDeletion: ptr.To(true),
	})

	logger.Info("Setting BackupConfig as owner of Backup",
		"backupConfigName", backupConfig.Name)

	return r.Update(ctx, backup)
}

// reconcileBackupPermanence checks that Backup is manual (created directly by user, not by ScheduledBackup)
// and in that case marks it as permanent (to prevent keeping WALs indefinitely for that backup)
func (r *BackupReconciler) reconcileBackupPermanence(ctx context.Context, backup *cnpgv1.Backup) error {
	logger := logr.FromContextOrDiscard(ctx)

	logger.V(1).Info("Reconciling backup permanence")

	if backup.Status.Phase != cnpgv1.BackupPhaseCompleted {
		return nil // skipping Backups in not completed phase
	}

	// Get the cluster
	cluster := &cnpgv1.Cluster{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: backup.Namespace, Name: backup.Spec.Cluster.Name}, cluster); err != nil {
		logger.Error(err, "Failed to get Cluster for Backup")
		return client.IgnoreNotFound(err)
	}

	// Get the BackupConfig for this cluster
	backupConfig, err := v1beta1.GetBackupConfigForCluster(ctx, r.Client, cluster)
	if err != nil || backupConfig == nil {
		logger.Info("No BackupConfig found for cluster", "error", err)
		return err
	}

	pgVersion, err := strconv.Atoi(backup.Labels[v1beta1.BackupPgVersionLabelName])
	if err != nil {
		logger.Error(err,
			"Cannot parse pgVersion from backup version annotation, skipping reconciling Backup permanence",
			"pg-major-label",
			backup.Labels[v1beta1.BackupPgVersionLabelName],
		)
		return nil
	}

	backupConfigWithSecrets, err := v1beta1.GetBackupConfigWithSecretsForBackup(ctx, r.Client, backup)
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("while getting backupconfig with secrets: %w", err)
	}

	// Manual backup is a backup not owned by anything except BackupConfig
	// (for automated backups owner is either Cluster or ScheduledBackup resource)
	// We assume that all backups passed there are owned by BackupConfig,
	// so it is enough to check that there's only 1 OwnerReference
	backupIsManual := len(backup.OwnerReferences) == 1
	if backupIsManual {
		err = walg.MarkBackupPermanent(ctx, backupConfigWithSecrets, pgVersion, backup.Status.BackupID, cluster)
	}

	// We do NOT unmark backups as inpermanent - it will be done during backup deletion operation
	return err
}

func (r *BackupReconciler) reconcileRelatedBackupsMetadata(ctx context.Context, backup *cnpgv1.Backup) error {
	logger := logr.FromContextOrDiscard(ctx)

	// Get the cluster
	cluster := &cnpgv1.Cluster{}
	err := r.Get(ctx, client.ObjectKey{Namespace: backup.Namespace, Name: backup.Spec.Cluster.Name}, cluster)
	// Tolerating non-existing cluster, this only blocks PG version evaluation
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("on reconcileRelatedBackupsMetadata: %w", err)
	}

	// Get BackupConfig
	backupConfig, err := v1beta1.GetBackupConfigForBackup(ctx, r.Client, backup)
	if err != nil {
		return fmt.Errorf("on reconcileRelatedBackupsMetadata: %w", err)
	}

	_, err = r.reconcileBackupMetadata(ctx, backup, backupConfig, cluster)
	if err != nil {
		return fmt.Errorf("while reconciling backup %s annotations: %w", backup.Name, err)
	}

	// If current backup is incremental - we need to update other backups annotations
	// to update their dependent backups list
	if backup.Labels[v1beta1.BackupTypeLabelName] == string(v1beta1.BackupTypeIncremental) {
		backupsToReconcile, err := r.listBackupsOwnedByBackupConfig(ctx, backupConfig)
		if err != nil {
			return err
		}
		for i := range backupsToReconcile {
			if backupsToReconcile[i].Name == backup.Name {
				continue
			}
			_, err = r.reconcileBackupMetadata(ctx, &backupsToReconcile[i], backupConfig, cluster)
			if err != nil {
				logger.Error(err, "while reconciling backup annotations", "backup.Name", backupsToReconcile[i].Name)
			}
		}
	}

	return nil
}

// Reconciles labels and annotations for this backup, including:
//
// label "cnpg-plugin-wal-g.yandex.cloud/pg-major" which shows postgresql version for this backup
//
// label "cnpg-plugin-wal-g.yandex.cloud/backup-type" which marks whether backup is full or incremental
//
// annotation "cnpg-plugin-wal-g.yandex.cloud/dependent-backups-direct" showing direct dependent backups of this backup
//
// annotation "cnpg-plugin-wal-g.yandex.cloud/dependent-backups-all" showing any dependent backups of this backup
//
// returns true if any of annotations changed
func (r *BackupReconciler) reconcileBackupMetadata(
	ctx context.Context,
	backup *cnpgv1.Backup,
	backupConfig *v1beta1.BackupConfig,
	cluster *cnpgv1.Cluster,
) (bool, error) {
	logger := logr.FromContextOrDiscard(ctx)

	oldBackup := backup.DeepCopy()

	if backup.Annotations == nil {
		backup.Annotations = make(map[string]string)
	}
	if backup.Labels == nil {
		backup.Labels = make(map[string]string)
	}

	if backup.Status.BackupID != "" {
		if strings.Contains(backup.Status.BackupID, "_D_") {
			backup.Labels[v1beta1.BackupTypeLabelName] = string(v1beta1.BackupTypeIncremental)
		} else {
			backup.Labels[v1beta1.BackupTypeLabelName] = string(v1beta1.BackupTypeFull)
		}
	}

	if backup.Labels[v1beta1.BackupPgVersionLabelName] == "" && cluster == nil {
		logger.Error(
			fmt.Errorf("cluster %s does not exist", backup.Spec.Cluster.Name),
			"while reconciling PG version annotation for backup",
		)
	} else if backup.Labels[v1beta1.BackupPgVersionLabelName] == "" && cluster != nil {
		pgVersion, err := cluster.GetPostgresqlVersion()
		if err != nil {
			logger.Error(err, "while getting PG version for cluster %v", client.ObjectKeyFromObject(cluster))
		}
		backup.Labels[v1beta1.BackupPgVersionLabelName] = strconv.FormatInt(int64(pgVersion.Major()), 10)
	}

	backups, err := r.listBackupsOwnedByBackupConfig(ctx, backupConfig)
	if err != nil {
		return false, err
	}

	directDependentBackups := buildDependentBackupsForBackup(ctx, backup, backups, false)
	dependentBackupsNames := lo.Map(directDependentBackups, func(b cnpgv1.Backup, _ int) string {
		return b.Name
	})
	backup.Annotations[v1beta1.BackupDirectDependentsAnnotationName] = strings.Join(dependentBackupsNames, " ")

	allDependentBackups := buildDependentBackupsForBackup(ctx, backup, backups, true)
	allDependentBackupsNames := lo.Map(allDependentBackups, func(b cnpgv1.Backup, _ int) string {
		return b.Name
	})
	backup.Annotations[v1beta1.BackupAllDependentsAnnotationName] = strings.Join(allDependentBackupsNames, " ")

	if equality.Semantic.DeepEqual(backup, oldBackup) {
		// There's no need to hit the API server again
		return false, nil
	}

	patchData, _ := client.MergeFrom(oldBackup).Data(backup)
	logger.V(1).Info("Patching CNPG Backup", "patch", string(patchData))

	err = r.Patch(ctx, backup, client.MergeFrom(oldBackup))
	return true, err
}

func (r *BackupReconciler) listBackupsOwnedByBackupConfig(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
) ([]cnpgv1.Backup, error) {
	// List all backups in the same namespace
	backupsList := cnpgv1.BackupList{}
	err := r.List(ctx, &backupsList, &client.ListOptions{Namespace: backupConfig.Namespace})
	if err != nil {
		return make([]cnpgv1.Backup, 0), fmt.Errorf("while getting Backup List: %w", err)
	}

	// Filter only backups owned by backupConfig and not being deleted
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
	includeIndirect bool,
) []cnpgv1.Backup {
	// Nothing to do with backups without BackupID
	if backup.Status.BackupID == "" {
		return make([]cnpgv1.Backup, 0)
	}

	// For getting dependent backups it is enough to have backupName only
	currentWalgBackup := walg.BackupMetadata{
		BackupName: backup.Status.BackupID,
	}
	walgBackups := lo.Map(backups, func(b cnpgv1.Backup, _ int) walg.BackupMetadata {
		return walg.BackupMetadata{
			BackupName: b.Status.BackupID,
		}
	})

	// Find all dependent backups (including indirect ones)
	dependentWalgBackups := currentWalgBackup.GetDependentBackups(ctx, walgBackups, includeIndirect)

	// Filter Backup resources - leave only some of them, matching dependent wal-g backups && not being deleted
	result := lo.Filter(backups, func(candidate cnpgv1.Backup, _ int) bool {
		return candidate.DeletionTimestamp.IsZero() && lo.ContainsBy(dependentWalgBackups, func(walgBackup walg.BackupMetadata) bool {
			return walgBackup.BackupName == candidate.Status.BackupID
		})
	})

	// Sort Backup list by name to make result idempotent between runs
	slices.SortFunc(result, func(a, b cnpgv1.Backup) int {
		if a.Name < b.Name {
			return -1
		} else if a.Name > b.Name {
			return 1
		}
		return 0
	})

	return result
}
