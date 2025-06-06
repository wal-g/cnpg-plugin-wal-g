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

package instance

import (
	"context"
	"encoding/json"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgbackup "github.com/cloudnative-pg/cnpg-i/pkg/backup"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// MockClient is a mock implementation of client.Client
type MockClient struct {
	mock.Mock
	client.Client
}

func (m *MockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	args := m.Called(ctx, key, obj)
	return args.Error(0)
}

func TestGetCapabilities(t *testing.T) {
	// Create a BackupServiceImplementation
	backupService := BackupServiceImplementation{}

	// Call GetCapabilities
	result, err := backupService.GetCapabilities(context.Background(), &cnpgbackup.BackupCapabilitiesRequest{})

	// Verify the result
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Capabilities, 1)
	assert.Equal(t, cnpgbackup.BackupCapability_RPC_TYPE_BACKUP, result.Capabilities[0].GetRpc().Type)
}

func TestBackup(t *testing.T) {
	// Set up test environment
	ctx := logr.NewContext(context.Background(), logr.Discard())
	viper.Set("pgdata", "/tmp/pgdata")
	defer viper.Reset()

	// Create a test cluster
	cluster := cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			Backup: cnpgv1.BackupConfiguration{
				BarmanObjectStore: &cnpgv1.BarmanObjectStoreConfiguration{
					BarmanCredentials: cnpgv1.BarmanCredentials{
						Plugin: &cnpgv1.PluginConfiguration{
							Name: "cnpg-extensions.yandex.cloud/wal-g",
							Parameters: map[string]string{
								"backupConfig": "test-backup-config",
							},
						},
					},
				},
			},
		},
	}

	// Create a test BackupConfig
	backupConfig := &v1beta1.BackupConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup-config",
			Namespace: "default",
		},
		Spec: v1beta1.BackupConfigSpec{
			Storage: v1beta1.StorageConfig{
				StorageType: v1beta1.StorageTypeS3,
				S3: &v1beta1.S3StorageConfig{
					Prefix:      "s3://test-bucket/backups",
					Region:      "us-east-1",
					EndpointUrl: "https://storage.example.com",
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

	// Create a test S3 credentials secret
	s3CredentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3-credentials",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"access-key-id":     []byte("test-access-key-id"),
			"secret-access-key": []byte("test-secret-access-key"),
		},
	}

	// Create a fake client with the BackupConfig and S3 credentials secret
	scheme := client.Scheme()
	_ = v1beta1.AddToScheme(scheme)
	_ = cnpgv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupConfig, s3CredentialsSecret).Build()

	// Create a mock for the walg.GetBackupsList and walg.GetBackupByUserData functions
	// This is a simplified approach since we can't easily mock the actual wal-g command execution
	// In a real test, you would use a more sophisticated approach to mock the command execution

	// Create a BackupServiceImplementation with the fake client
	backupService := BackupServiceImplementation{
		Client: fakeClient,
	}

	// Create a test backup request
	clusterJSON, err := json.Marshal(cluster)
	require.NoError(t, err)

	backupRequest := &cnpgbackup.BackupRequest{
		ClusterDefinition: clusterJSON,
	}

	// Skip the actual backup execution since we can't easily mock the wal-g command
	// In a real test, you would use a more sophisticated approach to mock the command execution
	t.Skip("Skipping test that requires mocking command execution")

	// Call Backup
	result, err := backupService.Backup(ctx, backupRequest)

	// Verify the result
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result.BackupId)
	assert.NotEmpty(t, result.BackupName)
	assert.True(t, result.StartedAt > 0)
	assert.True(t, result.StoppedAt > 0)
	assert.NotEmpty(t, result.BeginWal)
	assert.NotEmpty(t, result.BeginLsn)
	assert.NotEmpty(t, result.EndLsn)
	assert.NotEmpty(t, result.InstanceId)
	assert.True(t, result.Online)
}

func TestGetBackupConfig(t *testing.T) {
	// Set up test environment
	ctx := context.Background()

	// Create a test cluster
	cluster := cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			Backup: cnpgv1.BackupConfiguration{
				BarmanObjectStore: &cnpgv1.BarmanObjectStoreConfiguration{
					BarmanCredentials: cnpgv1.BarmanCredentials{
						Plugin: &cnpgv1.PluginConfiguration{
							Name: "cnpg-extensions.yandex.cloud/wal-g",
							Parameters: map[string]string{
								"backupConfig": "test-backup-config",
							},
						},
					},
				},
			},
		},
	}

	// Create a test BackupConfig
	backupConfig := &v1beta1.BackupConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup-config",
			Namespace: "default",
		},
		Spec: v1beta1.BackupConfigSpec{
			Storage: v1beta1.StorageConfig{
				StorageType: v1beta1.StorageTypeS3,
				S3: &v1beta1.S3StorageConfig{
					Prefix:      "s3://test-bucket/backups",
					Region:      "us-east-1",
					EndpointUrl: "https://storage.example.com",
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

	// Create a test S3 credentials secret
	s3CredentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3-credentials",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"access-key-id":     []byte("test-access-key-id"),
			"secret-access-key": []byte("test-secret-access-key"),
		},
	}

	// Create a fake client with the BackupConfig and S3 credentials secret
	scheme := client.Scheme()
	_ = v1beta1.AddToScheme(scheme)
	_ = cnpgv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupConfig, s3CredentialsSecret).Build()

	// Create a BackupServiceImplementation with the fake client
	backupService := BackupServiceImplementation{
		Client: fakeClient,
	}

	// Call getBackupConfig
	result, err := backupService.getBackupConfig(ctx, cluster)

	// Verify the result
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-backup-config", result.Name)
	assert.Equal(t, "default", result.Namespace)
	assert.Equal(t, "s3://test-bucket/backups", result.Spec.Storage.S3.Prefix)
	assert.Equal(t, "test-access-key-id", result.Spec.Storage.S3.AccessKeyID)
	assert.Equal(t, "test-secret-access-key", result.Spec.Storage.S3.AccessKeySecret)

	// Test with a cluster that doesn't have a BackupConfig reference
	clusterWithoutBackupConfig := cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-without-backup-config",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			Backup: cnpgv1.BackupConfiguration{
				BarmanObjectStore: &cnpgv1.BarmanObjectStoreConfiguration{
					BarmanCredentials: cnpgv1.BarmanCredentials{},
				},
			},
		},
	}

	_, err = backupService.getBackupConfig(ctx, clusterWithoutBackupConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing backupConfig parameter")

	// Test with a cluster that has a BackupConfig reference but the BackupConfig doesn't exist
	clusterWithNonExistentBackupConfig := cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-with-non-existent-backup-config",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			Backup: cnpgv1.BackupConfiguration{
				BarmanObjectStore: &cnpgv1.BarmanObjectStoreConfiguration{
					BarmanCredentials: cnpgv1.BarmanCredentials{
						Plugin: &cnpgv1.PluginConfiguration{
							Name: "cnpg-extensions.yandex.cloud/wal-g",
							Parameters: map[string]string{
								"backupConfig": "non-existent-backup-config",
							},
						},
					},
				},
			},
		},
	}

	_, err = backupService.getBackupConfig(ctx, clusterWithNonExistentBackupConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch BackupConfig object")
}
