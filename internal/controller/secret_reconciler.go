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

	"github.com/go-logr/logr"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// SecretReconciler reconciles Secret objects to protect them from accidental deletion
// when they are referenced by BackupConfig resources
type SecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;patch

// Reconcile handles Secret resource events
func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logr.FromContextOrDiscard(ctx)
	logger.V(1).Info("Running Reconcile for Secret", "secret", req.NamespacedName)

	// Get the Secret
	secret := &corev1.Secret{}
	if err := r.Get(ctx, req.NamespacedName, secret); err != nil {
		// The Secret resource may have been deleted
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If the Secret is being deleted and has our finalizer
	if !secret.DeletionTimestamp.IsZero() && containsString(secret.Finalizers, v1beta1.BackupConfigSecretFinalizerName) {
		isReferenced, err := isSecretReferencedByAnyBackupConfig(ctx, r.Client, secret)
		if err != nil {
			logger.Error(err, "Failed to check if Secret is referenced by any BackupConfig")
			return ctrl.Result{}, err
		}

		// If the Secret is still referenced, we can't remove the finalizer
		if isReferenced {
			logger.Info(
				"Secret is still referenced by a BackupConfig, cannot remove finalizer, retrying in 1 minute", "secret", secret.Name,
			)
			// Need requeue, because BackupConfig deletion will not trigger related Secret reconcilers by default
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
		}

		// Remove finalizer and update Secret, if it is needed
		if controllerutil.RemoveFinalizer(secret, v1beta1.BackupConfigSecretFinalizerName) {
			if err := r.Update(ctx, secret); err != nil {
				return ctrl.Result{}, fmt.Errorf("while removing finalizer from secret %s: %w", secret.Name, err)
			}
		}
		return ctrl.Result{}, nil
	}

	isReferenced, err := isSecretReferencedByAnyBackupConfig(ctx, r.Client, secret)
	if err != nil {
		logger.Error(err, "Failed to check if Secret is referenced by any BackupConfig")
		return ctrl.Result{}, err
	}

	// If the Secret is not referenced and has our finalizer, remove it
	// If the Secret IS referenced - BackupConfig reconciler should add finalizer on Secret
	if !isReferenced && controllerutil.RemoveFinalizer(secret, v1beta1.BackupConfigSecretFinalizerName) {
		logger.Info("Removing finalizer from Secret", "secret", secret.Name)
		if err := r.Update(ctx, secret); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Named("secret-protection").
		Complete(r)
}
