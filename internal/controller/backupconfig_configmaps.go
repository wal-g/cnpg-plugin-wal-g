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

	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getConfigMapReferencesFromBackupConfig returns a list of configmap references from a BackupConfig
func getConfigMapReferencesFromBackupConfig(backupConfig *v1beta1.BackupConfig) []types.NamespacedName {
	configmapRefs := make([]types.NamespacedName, 0)

	// Add S3 custom CA configmap if it exists
	if backupConfig.Spec.Storage.S3 != nil {
		s3 := backupConfig.Spec.Storage.S3
		if s3.CustomCA != nil && s3.CustomCA.Kind == "ConfigMap" {
			configmapRefs = append(configmapRefs, types.NamespacedName{
				Namespace: backupConfig.Namespace,
				Name:      s3.CustomCA.Name,
			})
		}
	}

	return configmapRefs
}

func isConfigMapReferencedByAnyBackupConfig(
	ctx context.Context,
	c client.Client,
	configmap *corev1.ConfigMap,
) (bool, error) {
	// List all BackupConfigs in the same namespace
	backupConfigList := &v1beta1.BackupConfigList{}
	if err := c.List(ctx, backupConfigList, &client.ListOptions{Namespace: configmap.Namespace}); err != nil {
		return false, fmt.Errorf("failed to list BackupConfigs: %w", err)
	}

	// Check if any BackupConfig references this configmap
	for i := range backupConfigList.Items {
		backupConfig := &backupConfigList.Items[i]
		configmapRefs := getConfigMapReferencesFromBackupConfig(backupConfig)

		for _, ref := range configmapRefs {
			if ref.Name == configmap.Name && ref.Namespace == configmap.Namespace {
				return true, nil
			}
		}
	}

	return false, nil
}
