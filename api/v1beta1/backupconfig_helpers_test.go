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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBackupConfigIsUsedForArchivation(t *testing.T) {
	tests := []struct {
		name             string
		backupConfigName types.NamespacedName
		cluster          cnpgv1.Cluster
		expected         bool
	}{
		{
			name: "BackupConfig is used for archivation",
			backupConfigName: types.NamespacedName{
				Name:      "test-backup-config",
				Namespace: "default",
			},
			cluster: cnpgv1.Cluster{
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
			},
			expected: true,
		},
		{
			name: "BackupConfig is not used for archivation - different namespace",
			backupConfigName: types.NamespacedName{
				Name:      "test-backup-config",
				Namespace: "different-namespace",
			},
			cluster: cnpgv1.Cluster{
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
			},
			expected: false,
		},
		{
			name: "BackupConfig is not used for archivation - different name",
			backupConfigName: types.NamespacedName{
				Name:      "different-backup-config",
				Namespace: "default",
			},
			cluster: cnpgv1.Cluster{
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
			},
			expected: false,
		},
		{
			name: "BackupConfig is not used for archivation - no plugin config",
			backupConfigName: types.NamespacedName{
				Name:      "test-backup-config",
				Namespace: "default",
			},
			cluster: cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: cnpgv1.ClusterSpec{
					Backup: cnpgv1.BackupConfiguration{
						BarmanObjectStore: &cnpgv1.BarmanObjectStoreConfiguration{
							BarmanCredentials: cnpgv1.BarmanCredentials{},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BackupConfigIsUsedForArchivation(tt.backupConfigName, tt.cluster)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBackupConfigIsUsedForRecovery(t *testing.T) {
	tests := []struct {
		name             string
		backupConfigName types.NamespacedName
		cluster          cnpgv1.Cluster
		expected         bool
	}{
		{
			name: "BackupConfig is used for recovery",
			backupConfigName: types.NamespacedName{
				Name:      "test-backup-config",
				Namespace: "default",
			},
			cluster: cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
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
			},
			expected: true,
		},
		{
			name: "BackupConfig is not used for recovery - different namespace",
			backupConfigName: types.NamespacedName{
				Name:      "test-backup-config",
				Namespace: "different-namespace",
			},
			cluster: cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
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
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BackupConfigIsUsedForRecovery(tt.backupConfigName, tt.cluster)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetBackupConfigForCluster(t *testing.T) {
	// Create a test context
	ctx := context.Background()

	// Create a test BackupConfig
	backupConfig := &BackupConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup-config",
			Namespace: "default",
		},
		Spec: BackupConfigSpec{
			Storage: StorageConfig{
				StorageType: StorageTypeS3,
				S3: &S3StorageConfig{
					Prefix: "s3://test-bucket/backups",
				},
			},
		},
	}

	// Create a test cluster with a reference to the BackupConfig
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

	// Create a fake client with the BackupConfig
	scheme := client.Scheme()
	_ = AddToScheme(scheme)
	_ = cnpgv1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupConfig).Build()

	// Test GetBackupConfigForCluster
	result, err := GetBackupConfigForCluster(ctx, fakeClient, cluster)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-backup-config", result.Name)
	assert.Equal(t, "default", result.Namespace)
	assert.Equal(t, "s3://test-bucket/backups", result.Spec.Storage.S3.Prefix)

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

	result, err = GetBackupConfigForCluster(ctx, fakeClient, clusterWithoutBackupConfig)
	assert.NoError(t, err)
	assert.Nil(t, result)

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

	result, err = GetBackupConfigForCluster(ctx, fakeClient, clusterWithNonExistentBackupConfig)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestGetBackupConfigForClusterRecovery(t *testing.T) {
	// Create a test context
	ctx := context.Background()

	// Create a test BackupConfig
	backupConfig := &BackupConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-recovery-backup-config",
			Namespace: "default",
		},
		Spec: BackupConfigSpec{
			Storage: StorageConfig{
				StorageType: StorageTypeS3,
				S3: &S3StorageConfig{
					Prefix: "s3://test-bucket/recovery-backups",
				},
			},
		},
	}

	// Create a test cluster with a reference to the BackupConfig for recovery
	cluster := cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
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
									"backupConfig": "test-recovery-backup-config",
								},
							},
						},
					},
				},
			},
		},
	}

	// Create a fake client with the BackupConfig
	scheme := client.Scheme()
	_ = AddToScheme(scheme)
	_ = cnpgv1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupConfig).Build()

	// Test GetBackupConfigForClusterRecovery
	result, err := GetBackupConfigForClusterRecovery(ctx, fakeClient, cluster)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-recovery-backup-config", result.Name)
	assert.Equal(t, "default", result.Namespace)
	assert.Equal(t, "s3://test-bucket/recovery-backups", result.Spec.Storage.S3.Prefix)
}
