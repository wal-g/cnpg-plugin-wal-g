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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPrefetchSecretsData(t *testing.T) {
	// Create a test context
	ctx := context.Background()

	// Create test secrets
	accessKeyIDSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3-access-key-id",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"access-key-id": []byte("test-access-key-id"),
		},
	}

	accessKeySecretSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3-access-key-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"access-key-secret": []byte("test-access-key-secret"),
		},
	}

	// Create a test BackupConfig with references to the secrets
	backupConfig := &BackupConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup-config",
			Namespace: "default",
		},
		Spec: BackupConfigSpec{
			Storage: StorageConfig{
				StorageType: StorageTypeS3,
				S3: &S3StorageConfig{
					Prefix:      "s3://test-bucket/backups",
					Region:      "us-east-1",
					EndpointUrl: "https://storage.example.com",
					AccessKeyIDRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "s3-access-key-id",
						},
						Key: "access-key-id",
					},
					AccessKeySecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "s3-access-key-secret",
						},
						Key: "access-key-secret",
					},
				},
			},
		},
	}

	// Create a fake client with the secrets
	scheme := client.Scheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(accessKeyIDSecret, accessKeySecretSecret).Build()

	// Test PrefetchSecretsData
	result, err := backupConfig.PrefetchSecretsData(ctx, fakeClient)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-backup-config", result.Name)
	assert.Equal(t, "default", result.Namespace)
	assert.Equal(t, "s3://test-bucket/backups", result.Spec.Storage.S3.Prefix)
	assert.Equal(t, "test-access-key-id", result.Spec.Storage.S3.AccessKeyID)
	assert.Equal(t, "test-access-key-secret", result.Spec.Storage.S3.AccessKeySecret)

	// Test with missing secret
	missingSecretBackupConfig := &BackupConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup-config-missing-secret",
			Namespace: "default",
		},
		Spec: BackupConfigSpec{
			Storage: StorageConfig{
				StorageType: StorageTypeS3,
				S3: &S3StorageConfig{
					Prefix:      "s3://test-bucket/backups",
					Region:      "us-east-1",
					EndpointUrl: "https://storage.example.com",
					AccessKeyIDRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "non-existent-secret",
						},
						Key: "access-key-id",
					},
					AccessKeySecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "s3-access-key-secret",
						},
						Key: "access-key-secret",
					},
				},
			},
		},
	}

	_, err = missingSecretBackupConfig.PrefetchSecretsData(ctx, fakeClient)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "while getting secret non-existent-secret")

	// Test with missing key in secret
	missingKeyBackupConfig := &BackupConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup-config-missing-key",
			Namespace: "default",
		},
		Spec: BackupConfigSpec{
			Storage: StorageConfig{
				StorageType: StorageTypeS3,
				S3: &S3StorageConfig{
					Prefix:      "s3://test-bucket/backups",
					Region:      "us-east-1",
					EndpointUrl: "https://storage.example.com",
					AccessKeyIDRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "s3-access-key-id",
						},
						Key: "non-existent-key",
					},
					AccessKeySecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "s3-access-key-secret",
						},
						Key: "access-key-secret",
					},
				},
			},
		},
	}

	_, err = missingKeyBackupConfig.PrefetchSecretsData(ctx, fakeClient)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing key non-existent-key, inside secret s3-access-key-id")
}

func TestExtractValueFromSecret(t *testing.T) {
	// Create a test context
	ctx := context.Background()

	// Create a test secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"test-key": []byte("test-value"),
		},
	}

	// Create a fake client with the secret
	scheme := client.Scheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	// Test extractValueFromSecret with valid key
	secretRef := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "test-secret",
		},
		Key: "test-key",
	}

	value, err := extractValueFromSecret(ctx, fakeClient, secretRef, "default")
	assert.NoError(t, err)
	assert.Equal(t, []byte("test-value"), value)

	// Test with non-existent secret
	nonExistentSecretRef := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "non-existent-secret",
		},
		Key: "test-key",
	}

	_, err = extractValueFromSecret(ctx, fakeClient, nonExistentSecretRef, "default")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "while getting secret non-existent-secret")

	// Test with non-existent key
	nonExistentKeyRef := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "test-secret",
		},
		Key: "non-existent-key",
	}

	_, err = extractValueFromSecret(ctx, fakeClient, nonExistentKeyRef, "default")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing key non-existent-key, inside secret test-secret")
}
