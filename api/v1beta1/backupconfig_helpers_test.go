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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	common "github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("BackupConfig Helpers", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		scheme     *runtime.Scheme
		namespace  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "test-namespace"
		scheme = runtime.NewScheme()
		Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())
		Expect(AddToScheme(scheme)).To(Succeed())
	})

	Context("getBackupConfigFromPluginConfig", func() {
		It("should return nil when pluginConfig is nil", func() {
			backupConfig, err := getBackupConfigFromPluginConfig(ctx, fakeClient, nil, namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(backupConfig).To(BeNil())
		})

		It("should return nil when pluginConfig.Parameters is nil", func() {
			pluginConfig := &cnpgv1.PluginConfiguration{
				Name: common.PluginNameDeprecated,
			}
			backupConfig, err := getBackupConfigFromPluginConfig(ctx, fakeClient, pluginConfig, namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(backupConfig).To(BeNil())
		})

		It("should return nil when pluginConfig belongs to a different plugin", func() {
			pluginConfig := &cnpgv1.PluginConfiguration{
				Name: "barman-cloud.cloudnative-pg.io",
				Parameters: map[string]string{
					"barmanObjectName": "s3-barman-cluster-original",
					"serverName":       "original-cluster",
				},
			}
			backupConfig, err := getBackupConfigFromPluginConfig(ctx, fakeClient, pluginConfig, namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(backupConfig).To(BeNil())
		})

		It("should return error when our plugin config is missing backupConfig parameter", func() {
			pluginConfig := &cnpgv1.PluginConfiguration{
				Name:       common.PluginNameDeprecated,
				Parameters: map[string]string{},
			}
			_, err := getBackupConfigFromPluginConfig(ctx, fakeClient, pluginConfig, namespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing backupConfig parameter"))
		})
	})

	Context("GetBackupConfigForClusterRecovery", func() {
		It("should not fail when cluster recovery source uses a different plugin", func() {
			// This test verifies the scenario from the issue:
			// A cluster uses barman-cloud as recovery source and our plugin as WAL archiver.
			// GetBackupConfigForClusterRecovery should return (nil, nil) for the barman-cloud
			// recovery source, not an error about missing backupConfig parameter.
			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{
					Bootstrap: &cnpgv1.BootstrapConfiguration{
						Recovery: &cnpgv1.BootstrapRecovery{
							Source: "objectStoreRecoveryCluster",
						},
					},
					ExternalClusters: []cnpgv1.ExternalCluster{
						{
							Name: "objectStoreRecoveryCluster",
							PluginConfiguration: &cnpgv1.PluginConfiguration{
								Name: "barman-cloud.cloudnative-pg.io",
								Parameters: map[string]string{
									"barmanObjectName": "s3-barman-cluster-original",
									"serverName":       "original-cluster",
								},
							},
						},
					},
					Plugins: []cnpgv1.PluginConfiguration{
						{
							Name:          common.PluginNameDeprecated,
							IsWALArchiver: ptr.To(true),
							Parameters: map[string]string{
								"backupConfig": "s3-walg-cluster-example",
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			backupConfig, err := GetBackupConfigForClusterRecovery(ctx, fakeClient, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(backupConfig).To(BeNil())
		})

		It("should return BackupConfig when recovery source uses our plugin", func() {
			testBackupConfig := &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: BackupConfigSpec{},
			}

			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{
					Bootstrap: &cnpgv1.BootstrapConfiguration{
						Recovery: &cnpgv1.BootstrapRecovery{
							Source: "walGRecoverySource",
						},
					},
					ExternalClusters: []cnpgv1.ExternalCluster{
						{
							Name: "walGRecoverySource",
							PluginConfiguration: &cnpgv1.PluginConfiguration{
								Name: common.PluginNameDeprecated,
								Parameters: map[string]string{
									"backupConfig": "test-backup-config",
								},
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(testBackupConfig).
				Build()

			backupConfig, err := GetBackupConfigForClusterRecovery(ctx, fakeClient, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(backupConfig).NotTo(BeNil())
			Expect(backupConfig.Name).To(Equal("test-backup-config"))
		})

		It("should return nil when cluster has no recovery configuration", func() {
			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{
					Plugins: []cnpgv1.PluginConfiguration{
						{
							Name:          common.PluginNameDeprecated,
							IsWALArchiver: ptr.To(true),
							Parameters: map[string]string{
								"backupConfig": "s3-walg-cluster-example",
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			backupConfig, err := GetBackupConfigForClusterRecovery(ctx, fakeClient, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(backupConfig).To(BeNil())
		})
	})

	Context("GetBackupConfigForCluster", func() {
		It("should return BackupConfig when our plugin is configured", func() {
			testBackupConfig := &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-walg-cluster-example",
					Namespace: namespace,
				},
				Spec: BackupConfigSpec{},
			}

			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{
					Plugins: []cnpgv1.PluginConfiguration{
						{
							Name:          common.PluginNameDeprecated,
							IsWALArchiver: ptr.To(true),
							Parameters: map[string]string{
								"backupConfig": "s3-walg-cluster-example",
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(testBackupConfig).
				Build()

			backupConfig, err := GetBackupConfigForCluster(ctx, fakeClient, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(backupConfig).NotTo(BeNil())
			Expect(backupConfig.Name).To(Equal("s3-walg-cluster-example"))
		})

		It("should return nil when cluster has no plugins configured", func() {
			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			backupConfig, err := GetBackupConfigForCluster(ctx, fakeClient, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(backupConfig).To(BeNil())
		})
	})

	Context("BackupConfigIsUsedForArchivation", func() {
		It("should return true when BackupConfig matches plugin config", func() {
			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{
					Plugins: []cnpgv1.PluginConfiguration{
						{
							Name: common.PluginNameDeprecated,
							Parameters: map[string]string{
								"backupConfig": "my-backup-config",
							},
						},
					},
				},
			}

			result := BackupConfigIsUsedForArchivation(
				types.NamespacedName{Name: "my-backup-config", Namespace: namespace},
				cluster,
			)
			Expect(result).To(BeTrue())
		})

		It("should return false when BackupConfig name does not match", func() {
			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{
					Plugins: []cnpgv1.PluginConfiguration{
						{
							Name: common.PluginNameDeprecated,
							Parameters: map[string]string{
								"backupConfig": "my-backup-config",
							},
						},
					},
				},
			}

			result := BackupConfigIsUsedForArchivation(
				types.NamespacedName{Name: "other-backup-config", Namespace: namespace},
				cluster,
			)
			Expect(result).To(BeFalse())
		})

		It("should return false when namespace does not match", func() {
			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{
					Plugins: []cnpgv1.PluginConfiguration{
						{
							Name: common.PluginNameDeprecated,
							Parameters: map[string]string{
								"backupConfig": "my-backup-config",
							},
						},
					},
				},
			}

			result := BackupConfigIsUsedForArchivation(
				types.NamespacedName{Name: "my-backup-config", Namespace: "other-namespace"},
				cluster,
			)
			Expect(result).To(BeFalse())
		})
	})

	Context("BackupConfigIsUsedForRecovery", func() {
		It("should return true when BackupConfig matches recovery plugin config", func() {
			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{
					Bootstrap: &cnpgv1.BootstrapConfiguration{
						Recovery: &cnpgv1.BootstrapRecovery{
							Source: "recoverySource",
						},
					},
					ExternalClusters: []cnpgv1.ExternalCluster{
						{
							Name: "recoverySource",
							PluginConfiguration: &cnpgv1.PluginConfiguration{
								Name: common.PluginNameDeprecated,
								Parameters: map[string]string{
									"backupConfig": "my-backup-config",
								},
							},
						},
					},
				},
			}

			result := BackupConfigIsUsedForRecovery(
				types.NamespacedName{Name: "my-backup-config", Namespace: namespace},
				cluster,
			)
			Expect(result).To(BeTrue())
		})

		It("should return false when recovery source uses a different plugin", func() {
			cluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-example",
					Namespace: namespace,
				},
				Spec: cnpgv1.ClusterSpec{
					Bootstrap: &cnpgv1.BootstrapConfiguration{
						Recovery: &cnpgv1.BootstrapRecovery{
							Source: "objectStoreRecoveryCluster",
						},
					},
					ExternalClusters: []cnpgv1.ExternalCluster{
						{
							Name: "objectStoreRecoveryCluster",
							PluginConfiguration: &cnpgv1.PluginConfiguration{
								Name: "barman-cloud.cloudnative-pg.io",
								Parameters: map[string]string{
									"barmanObjectName": "s3-barman-cluster-original",
									"serverName":       "original-cluster",
								},
							},
						},
					},
				},
			}

			result := BackupConfigIsUsedForRecovery(
				types.NamespacedName{Name: "my-backup-config", Namespace: namespace},
				cluster,
			)
			Expect(result).To(BeFalse())
		})
	})
})
