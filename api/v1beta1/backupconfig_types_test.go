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

package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBackupConfig_DeepCopy(t *testing.T) {
	// Create a sample BackupConfig
	original := &BackupConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup-config",
			Namespace: "default",
		},
		Spec: BackupConfigSpec{
			DownloadConcurrency:    10,
			DownloadFileRetries:    15,
			UploadDiskRateLimit:    1024 * 1024,     // 1MB/s
			UploadNetworkRateLimit: 2 * 1024 * 1024, // 2MB/s
			UploadConcurrency:      16,
			UploadDiskConcurrency:  1,
			DeltaMaxSteps:          5,
			Storage: StorageConfig{
				StorageType: StorageTypeS3,
				S3: &S3StorageConfig{
					Prefix:         "s3://test-bucket/backups",
					Region:         "us-east-1",
					EndpointUrl:    "https://storage.example.com",
					ForcePathStyle: true,
					StorageClass:   "STANDARD",
					AccessKeyIDRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "s3-credentials",
						},
						Key: "access-key-id",
					},
					AccessKeySecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "s3-credentials",
						},
						Key: "secret-access-key",
					},
				},
			},
		},
	}

	// Test DeepCopy
	copied := original.DeepCopy()
	assert.Equal(t, original.Name, copied.Name)
	assert.Equal(t, original.Namespace, copied.Namespace)
	assert.Equal(t, original.Spec.DownloadConcurrency, copied.Spec.DownloadConcurrency)
	assert.Equal(t, original.Spec.Storage.StorageType, copied.Spec.Storage.StorageType)
	assert.Equal(t, original.Spec.Storage.S3.Prefix, copied.Spec.Storage.S3.Prefix)
	assert.Equal(t, original.Spec.Storage.S3.AccessKeyIDRef.Name, copied.Spec.Storage.S3.AccessKeyIDRef.Name)

	// Modify the copy and ensure the original is unchanged
	copied.Name = "modified-name"
	copied.Spec.DownloadConcurrency = 20
	copied.Spec.Storage.S3.Prefix = "s3://modified-bucket/backups"

	assert.NotEqual(t, original.Name, copied.Name)
	assert.NotEqual(t, original.Spec.DownloadConcurrency, copied.Spec.DownloadConcurrency)
	assert.NotEqual(t, original.Spec.Storage.S3.Prefix, copied.Spec.Storage.S3.Prefix)
}

func TestBackupConfigList_DeepCopy(t *testing.T) {
	// Create a sample BackupConfigList
	original := &BackupConfigList{
		Items: []BackupConfig{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config-1",
					Namespace: "default",
				},
				Spec: BackupConfigSpec{
					DownloadConcurrency: 10,
					Storage: StorageConfig{
						StorageType: StorageTypeS3,
						S3: &S3StorageConfig{
							Prefix: "s3://test-bucket/backups-1",
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config-2",
					Namespace: "default",
				},
				Spec: BackupConfigSpec{
					DownloadConcurrency: 20,
					Storage: StorageConfig{
						StorageType: StorageTypeS3,
						S3: &S3StorageConfig{
							Prefix: "s3://test-bucket/backups-2",
						},
					},
				},
			},
		},
	}

	// Test DeepCopy
	copied := original.DeepCopy()
	assert.Equal(t, len(original.Items), len(copied.Items))
	assert.Equal(t, original.Items[0].Name, copied.Items[0].Name)
	assert.Equal(t, original.Items[1].Name, copied.Items[1].Name)

	// Modify the copy and ensure the original is unchanged
	copied.Items[0].Name = "modified-name"
	assert.NotEqual(t, original.Items[0].Name, copied.Items[0].Name)
}
