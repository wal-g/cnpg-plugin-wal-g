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
	"time"

	"github.com/go-logr/logr"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ConfigMapReconciler reconciles ConfigMap objects to protect them from accidental deletion
// when they are referenced by BackupConfig resources
type ConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;update;patch

// Reconcile handles ConfigMap resource events
func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logr.FromContextOrDiscard(ctx)
	logger.V(1).Info("Running Reconcile for ConfigMap", "configmap", req.NamespacedName)

	// Get the ConfigMap
	configmap := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, configmap); err != nil {
		// The ConfigMap resource may have been deleted
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If the ConfigMap is being deleted and has our finalizer
	if !configmap.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(configmap, v1beta1.BackupConfigCMFinalizerName) {
		isReferenced, err := isConfigMapReferencedByAnyBackupConfig(ctx, r.Client, configmap)
		if err != nil {
			logger.Error(err, "Failed to check if ConfigMap is referenced by any BackupConfig")
			return ctrl.Result{}, err
		}

		// If the ConfigMap is still referenced, we can't remove the finalizer
		if isReferenced {
			logger.Info(
				"ConfigMap is still referenced by a BackupConfig, cannot remove finalizer, retrying in 1 minute", "configmap", configmap.Name,
			)
			// Need requeue, because BackupConfig deletion will not trigger related ConfigMap reconcilers by default
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
		}

		// Remove finalizer and update ConfigMap, if it is needed
		if controllerutil.RemoveFinalizer(configmap, v1beta1.BackupConfigCMFinalizerName) {
			if err := r.Update(ctx, configmap); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	isReferenced, err := isConfigMapReferencedByAnyBackupConfig(ctx, r.Client, configmap)
	if err != nil {
		logger.Error(err, "Failed to check if ConfigMap is referenced by any BackupConfig")
		return ctrl.Result{}, err
	}

	// If the ConfigMap is not referenced and has our finalizer, remove it
	// If the ConfigMap IS referenced - BackupConfig reconciler should add finalizer on ConfigMap
	if !isReferenced && controllerutil.RemoveFinalizer(configmap, v1beta1.BackupConfigCMFinalizerName) {
		logger.Info("Removing finalizer from ConfigMap", "configmap", configmap.Name)
		if err := r.Update(ctx, configmap); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		Named("configmap-protection").
		Complete(r)
}
