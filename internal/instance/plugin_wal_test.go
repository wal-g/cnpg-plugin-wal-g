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
	"github.com/cloudnative-pg/cnpg-i/pkg/wal"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetWALCapabilities(t *testing.T) {
	// Create a WALServiceImplementation
	walService := WALServiceImplementation{}

	// Call GetCapabilities
	result, err := walService.GetCapabilities(context.Background(), &wal.WALCapabilitiesRequest{})

	// Verify the result
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Capabilities, 2)

	// Check that the capabilities include ARCHIVE_WAL and RESTORE_WAL
	var hasArchiveWAL, hasRestoreWAL bool
	for _, capability := range result.Capabilities {
		if capability.GetRpc().Type == wal.WALCapability_RPC_TYPE_ARCHIVE_WAL {
			hasArchiveWAL = true
		}
		if capability.GetRpc().Type == wal.WALCapability_RPC_TYPE_RESTORE_WAL {
			hasRestoreWAL = true
		}
	}
	assert.True(t, hasArchiveWAL, "Should have ARCHIVE_WAL capability")
	assert.True(t, hasRestoreWAL, "Should have RESTORE_WAL capability")
}

func TestArchive(t *testing.T) {
	// Set up test environment
	ctx := logr.NewContext(context.Background(), logr.Discard())

	// Set the plugin mode to normal
	viper.Set("mode", common.PluginModeNormal)
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

	// Create a WALServiceImplementation with the fake client
	walService := WALServiceImplementation{
		Client: fakeClient,
	}

	// Create a test WAL archive request
	clusterJSON, err := json.Marshal(cluster)
	require.NoError(t, err)

	archiveRequest := &wal.WALArchiveRequest{
		ClusterDefinition: clusterJSON,
		SourceFileName:    "/tmp/000000010000000000000001",
	}

	// Skip the actual archive execution since we can't easily mock the wal-g command
	// In a real test, you would use a more sophisticated approach to mock the command execution
	t.Skip("Skipping test that requires mocking command execution")

	// Call Archive
	result, err := walService.Archive(ctx, archiveRequest)

	// Verify the result
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestArchiveInRecoveryMode(t *testing.T) {
	// Set up test environment
	ctx := context.Background()

	// Set the plugin mode to recovery
	viper.Set("mode", common.PluginModeRecovery)
	defer viper.Reset()

	// Create a WALServiceImplementation
	walService := WALServiceImplementation{}

	// Create a test WAL archive request
	archiveRequest := &wal.WALArchiveRequest{
		ClusterDefinition: []byte("{}"),
		SourceFileName:    "/tmp/000000010000000000000001",
	}

	// Call Archive
	_, err := walService.Archive(ctx, archiveRequest)

	// Verify that archiving is restricted in recovery mode
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "WAL archivation restricted while running not in 'normal' mode")
}

func TestRestore(t *testing.T) {
	// Set up test environment
	ctx := logr.NewContext(context.Background(), logr.Discard())

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

	// Create a WALServiceImplementation with the fake client
	walService := WALServiceImplementation{
		Client: fakeClient,
	}

	// Create a test WAL restore request
	clusterJSON, err := json.Marshal(cluster)
	require.NoError(t, err)

	restoreRequest := &wal.WALRestoreRequest{
		ClusterDefinition:   clusterJSON,
		SourceWalName:       "000000010000000000000001",
		DestinationFileName: "/tmp/000000010000000000000001",
	}

	// Skip the actual restore execution since we can't easily mock the wal-g command
	// In a real test, you would use a more sophisticated approach to mock the command execution
	t.Skip("Skipping test that requires mocking command execution")

	// Call Restore
	result, err := walService.Restore(ctx, restoreRequest)

	// Verify the result
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestGetBackupConfigForWAL(t *testing.T) {
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

	// Create a WALServiceImplementation with the fake client
	walService := WALServiceImplementation{
		Client: fakeClient,
	}

	// Test getBackupConfig in normal mode
	viper.Set("mode", common.PluginModeNormal)
	result, err := walService.getBackupConfig(ctx, cluster)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-backup-config", result.Name)
	assert.Equal(t, "default", result.Namespace)
	assert.Equal(t, "s3://test-bucket/backups", result.Spec.Storage.S3.Prefix)
	assert.Equal(t, "test-access-key-id", result.Spec.Storage.S3.AccessKeyID)
	assert.Equal(t, "test-secret-access-key", result.Spec.Storage.S3.AccessKeySecret)

	// Test getBackupConfig in recovery mode
	viper.Set("mode", common.PluginModeRecovery)

	// Create a test cluster with recovery configuration
	clusterWithRecovery := cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-recovery",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			Bootstrap: &cnpgv1.BootstrapConfiguration{
				Recovery: &cnpgv1.BootstrapRecovery{
					Source: "test-cluster-source",
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
		},
	}

	result, err = walService.getBackupConfig(ctx, clusterWithRecovery)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-backup-config", result.Name)
	assert.Equal(t, "default", result.Namespace)
	assert.Equal(t, "s3://test-bucket/backups", result.Spec.Storage.S3.Prefix)
	assert.Equal(t, "test-access-key-id", result.Spec.Storage.S3.AccessKeyID)
	assert.Equal(t, "test-secret-access-key", result.Spec.Storage.S3.AccessKeySecret)
}
