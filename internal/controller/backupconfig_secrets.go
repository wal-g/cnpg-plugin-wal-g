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

// getSecretReferencesFromBackupConfig returns a list of secret references from a BackupConfig
func getSecretReferencesFromBackupConfig(backupConfig *v1beta1.BackupConfig) []types.NamespacedName {
	secretRefs := make([]types.NamespacedName, 0)

	// Add S3 storage credentials if they exist
	if backupConfig.Spec.Storage.S3 != nil {
		s3 := backupConfig.Spec.Storage.S3
		if s3.AccessKeyIDRef != nil {
			secretRefs = append(secretRefs, types.NamespacedName{
				Namespace: backupConfig.Namespace,
				Name:      s3.AccessKeyIDRef.Name,
			})
		}

		if s3.AccessKeySecretRef != nil {
			secretRefs = append(secretRefs, types.NamespacedName{
				Namespace: backupConfig.Namespace,
				Name:      s3.AccessKeySecretRef.Name,
			})
		}

		if s3.CustomCA != nil && s3.CustomCA.Kind == "Secret" {
			secretRefs = append(secretRefs, types.NamespacedName{
				Namespace: backupConfig.Namespace,
				Name:      s3.CustomCA.Name,
			})
		}
	}

	// Add encryption secret if it exists
	if backupConfig.Spec.Encryption.Method != "" && backupConfig.Spec.Encryption.Method != "none" {
		secretRefs = append(secretRefs, types.NamespacedName{
			Namespace: backupConfig.Namespace,
			Name:      v1beta1.GetBackupConfigEncryptionSecretName(backupConfig),
		})
	}

	return secretRefs
}

func isSecretReferencedByAnyBackupConfig(
	ctx context.Context,
	c client.Client,
	secret *corev1.Secret,
) (bool, error) {
	// List all BackupConfigs in the same namespace
	backupConfigList := &v1beta1.BackupConfigList{}
	if err := c.List(ctx, backupConfigList, &client.ListOptions{Namespace: secret.Namespace}); err != nil {
		return false, fmt.Errorf("failed to list BackupConfigs: %w", err)
	}

	// Check if any BackupConfig references this secret
	for i := range backupConfigList.Items {
		backupConfig := &backupConfigList.Items[i]
		secretRefs := getSecretReferencesFromBackupConfig(backupConfig)

		for _, ref := range secretRefs {
			if ref.Name == secret.Name && ref.Namespace == secret.Namespace {
				return true, nil
			}
		}
	}

	return false, nil
}
