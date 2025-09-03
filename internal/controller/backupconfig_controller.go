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
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BackupConfigReconciler reconciles a BackupConfig object
type BackupConfigReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	BackupDeletionController *BackupDeletionController
}

// Alphabetical order to not repeat or miss permissions
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;patch
// +kubebuilder:rbac:groups=cnpg-extensions.yandex.cloud,resources=backupconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cnpg-extensions.yandex.cloud,resources=backupconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cnpg-extensions.yandex.cloud,resources=backupconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create;patch;update;get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=create;patch;update;get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;patch;list;get;watch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=patch;update;list;get;watch
// +kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;create;delete;update;patch;list;watch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *BackupConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logr.FromContextOrDiscard(ctx)
	logger.V(1).Info("Running Reconcile for BackupConfig")

	backupConfig := &v1beta1.BackupConfig{}
	if err := r.Get(ctx, req.NamespacedName, backupConfig); err != nil {
		// The Backup resource may have been deleted
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// TODO(endevir): Check && report connection status into Status subresource
	// TODO(endevir): Watch for secrets name change and reconcile cluster role

	if !backupConfig.DeletionTimestamp.IsZero() {
		// BackupConfig is marked for deletion
		if !containsString(backupConfig.Finalizers, v1beta1.BackupConfigFinalizerName) {
			return ctrl.Result{}, nil // Nothing to do if finalizer is not specified
		}

		// List all backups potentially referencing this backup config
		backupList := cnpgv1.BackupList{}
		if err := r.List(ctx, &backupList, client.InNamespace(req.Namespace)); err != nil {
			return ctrl.Result{}, err
		}

		// Check if there is at leas one Backup resource with OwnerReference matching current Backup
		foundChildBackup := lo.ContainsBy(backupList.Items, func(b cnpgv1.Backup) bool {
			return lo.ContainsBy(b.OwnerReferences, func(owner metav1.OwnerReference) bool {
				return owner.Kind == "BackupConfig" && owner.Name == backupConfig.Name
			})
		})

		if foundChildBackup {
			logger.Info("Blocking BackupConfig deletion: still has one or more dependent Backup, retrying in 30 seconds")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		// Handle backup config cleanup finalizer
		if containsString(backupConfig.Finalizers, v1beta1.BackupConfigFinalizerName) {
			logger.Info("Enqueuing BackupConfig for cleanup", "backupConfig", backupConfig.Name)

			if err := r.BackupDeletionController.EnqueueBackupConfigDeletion(ctx, backupConfig); err != nil {
				logger.Error(err, "Failed to enqueue BackupConfig for cleanup")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}

			// After handling the backup cleanup finalizer, requeue to handle the secret protection finalizer
			return ctrl.Result{RequeueAfter: 45 * time.Second}, nil
		}

		return ctrl.Result{}, nil
	}

	// Add finalizers if they don't exist
	finalizersChanged := false

	if !containsString(backupConfig.Finalizers, v1beta1.BackupConfigFinalizerName) {
		backupConfig.Finalizers = append(backupConfig.Finalizers, v1beta1.BackupConfigFinalizerName)
		finalizersChanged = true
	}

	if finalizersChanged {
		if err := r.Update(ctx, backupConfig); err != nil {
			logger.Error(err, "while adding finalizers to BackupConfig")
			return ctrl.Result{}, fmt.Errorf("while adding finalizers to BackupConfig: %w", err)
		}
	}

	backupConfigSecrets := getSecretReferencesFromBackupConfig(backupConfig)
	for _, secretRef := range backupConfigSecrets {
		err := setFinalizerOnSecret(ctx, r.Client, secretRef)
		if err != nil {
			logger.Error(err, "Failed to set finalizer on secret", "secretName", secretRef.Name)
			// continuing anyway
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupConfigReconciler) SetupWithManager(mgr ctrl.Manager, backupDeletionController *BackupDeletionController) error {
	r.BackupDeletionController = backupDeletionController

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.BackupConfig{}).
		Named("backupconfig").
		Complete(r)
}
