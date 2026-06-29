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
	"encoding/json"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cnpg-i/pkg/metrics"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Helper function to create a fake client with the given objects
func setupFakeClientForMetrics(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(v1beta1.AddToScheme(scheme)).To(Succeed())
	Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

// Helper function to create a BackupConfig for testing
func createBackupConfigForMetrics(name, namespace string) *v1beta1.BackupConfig {
	return &v1beta1.BackupConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BackupConfig",
			APIVersion: "cnpg-extensions.yandex.cloud/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1beta1.BackupConfigSpec{
			Storage: v1beta1.StorageConfig{
				StorageType: v1beta1.StorageTypeS3,
				S3: &v1beta1.S3StorageConfig{
					Prefix: "s3://test-bucket/test-prefix",
				},
			},
		},
	}
}

// Helper function to create a CNPG Cluster with plugin configuration
func createClusterWithPluginConfig(name, namespace, backupConfigName string) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: cnpgv1.ClusterSpec{
			Plugins: []cnpgv1.PluginConfiguration{
				{
					Name:       common.PluginNameDeprecated,
					Parameters: map[string]string{"backupConfig": backupConfigName},
				},
			},
		},
	}
}

var _ = Describe("MetricsServerImplementation", func() {
	Describe("GetCapabilities", func() {
		It("should return metrics capabilities", func() {
			metricsServer := MetricsServerImplementation{}
			result, err := metricsServer.GetCapabilities(ctx, &metrics.MetricsCapabilitiesRequest{})

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Capabilities).To(HaveLen(1))
			Expect(result.Capabilities[0].GetRpc()).NotTo(BeNil())
			Expect(result.Capabilities[0].GetRpc().Type).To(Equal(metrics.MetricsCapability_RPC_TYPE_METRICS))
		})
	})

	Describe("Define", func() {
		It("should return all metric definitions", func() {
			metricsServer := MetricsServerImplementation{}
			result, err := metricsServer.Define(ctx, &metrics.DefineMetricsRequest{})

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Metrics).To(HaveLen(8))

			// Verify all expected metrics are defined
			metricNames := make(map[string]bool)
			for _, metric := range result.Metrics {
				metricNames[metric.FqName] = true
				Expect(metric.Help).NotTo(BeEmpty())
				Expect(metric.ValueType).NotTo(BeNil())
				Expect(metric.ValueType.Type).To(Equal(metrics.MetricType_TYPE_GAUGE))
			}

			Expect(metricNames).To(HaveKey(firstRecoverabilityPointMetricName))
			Expect(metricNames).To(HaveKey(lastAvailableBackupTimestampMetricName))
			Expect(metricNames).To(HaveKey(lastFailedBackupTimestampMetricName))
			Expect(metricNames).To(HaveKey(lastArchivedWALTimestampMetricName))
			Expect(metricNames).To(HaveKey(totalWALS3UsageBytesMetricName))
			Expect(metricNames).To(HaveKey(totalBackupsS3UsageBytesMetricName))
			Expect(metricNames).To(HaveKey(s3ReadAvailabilityMetricName))
			Expect(metricNames).To(HaveKey(s3WriteAvailabilityMetricName))
		})
	})

	Describe("Collect", func() {
		var (
			metricsServer MetricsServerImplementation
			backupConfig  *v1beta1.BackupConfig
			cluster       *cnpgv1.Cluster
		)

		BeforeEach(func() {
			backupConfig = createBackupConfigForMetrics("test-backup-config", "default")
			cluster = createClusterWithPluginConfig("test-cluster", "default", "test-backup-config")
		})

		Context("when ConsumedStorage is nil", func() {
			It("should not panic and return zero values for storage metrics", func() {
				backupConfig.Status.ConsumedStorage = nil

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				// This should not panic
				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.Metrics).To(HaveLen(8))

				// Verify storage metrics are zero when ConsumedStorage is nil
				for _, metric := range result.Metrics {
					if metric.FqName == totalWALS3UsageBytesMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
					if metric.FqName == totalBackupsS3UsageBytesMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
				}
			})
		})

		Context("when ConsumedStorage.WALBytes is nil", func() {
			It("should not panic and return zero for WAL storage metric", func() {
				backupConfig.Status.ConsumedStorage = &v1beta1.ConsumedStorageInfo{
					WALBytes:     nil,
					BackupsBytes: int64Ptr(1000000),
				}

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				// Verify WAL metric is zero and Backups metric has value
				for _, metric := range result.Metrics {
					if metric.FqName == totalWALS3UsageBytesMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
					if metric.FqName == totalBackupsS3UsageBytesMetricName {
						Expect(metric.Value).To(Equal(float64(1000000)))
					}
				}
			})
		})

		Context("when ConsumedStorage.BackupsBytes is nil", func() {
			It("should not panic and return zero for backups storage metric", func() {
				backupConfig.Status.ConsumedStorage = &v1beta1.ConsumedStorageInfo{
					WALBytes:     int64Ptr(500000),
					BackupsBytes: nil,
				}

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				// Verify Backups metric is zero and WAL metric has value
				for _, metric := range result.Metrics {
					if metric.FqName == totalWALS3UsageBytesMetricName {
						Expect(metric.Value).To(Equal(float64(500000)))
					}
					if metric.FqName == totalBackupsS3UsageBytesMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
				}
			})
		})

		Context("when ConsumedStorage has all values", func() {
			It("should return correct storage metrics", func() {
				backupConfig.Status.ConsumedStorage = &v1beta1.ConsumedStorageInfo{
					WALBytes:     int64Ptr(1234567890),
					BackupsBytes: int64Ptr(9876543210),
				}

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				// Verify both metrics have correct values
				for _, metric := range result.Metrics {
					if metric.FqName == totalWALS3UsageBytesMetricName {
						Expect(metric.Value).To(Equal(float64(1234567890)))
					}
					if metric.FqName == totalBackupsS3UsageBytesMetricName {
						Expect(metric.Value).To(Equal(float64(9876543210)))
					}
				}
			})
		})

		Context("when FirstRecoverabilityPoint is nil", func() {
			It("should return zero for first recoverability point metric", func() {
				backupConfig.Status.FirstRecoverabilityPoint = nil

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				for _, metric := range result.Metrics {
					if metric.FqName == firstRecoverabilityPointMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
				}
			})
		})

		Context("when LastSuccessfulBackup is nil", func() {
			It("should return zero for last successful backup metric", func() {
				backupConfig.Status.LastSuccessfulBackup = nil

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				for _, metric := range result.Metrics {
					if metric.FqName == lastAvailableBackupTimestampMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
				}
			})
		})

		Context("when LastFailedBackup is nil", func() {
			It("should return zero for last failed backup metric", func() {
				backupConfig.Status.LastFailedBackup = nil

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				for _, metric := range result.Metrics {
					if metric.FqName == lastFailedBackupTimestampMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
				}
			})
		})

		Context("when all timestamp fields are set", func() {
			It("should return correct timestamp metrics", func() {
				now := time.Now()
				firstRecoveryTime := metav1.NewTime(now.Add(-7 * 24 * time.Hour))
				lastSuccessfulTime := metav1.NewTime(now.Add(-1 * time.Hour))
				lastFailedTime := metav1.NewTime(now.Add(-2 * time.Hour))

				backupConfig.Status.FirstRecoverabilityPoint = &firstRecoveryTime
				backupConfig.Status.LastSuccessfulBackup = &lastSuccessfulTime
				backupConfig.Status.LastFailedBackup = &lastFailedTime

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				for _, metric := range result.Metrics {
					if metric.FqName == firstRecoverabilityPointMetricName {
						Expect(metric.Value).To(Equal(float64(firstRecoveryTime.Unix())))
					}
					if metric.FqName == lastFailedBackupTimestampMetricName {
						Expect(metric.Value).To(Equal(float64(lastFailedTime.Unix())))
					}
				}
			})
		})

		Context("when storage conditions are set", func() {
			It("should return correct availability metrics when storage is readable and writable", func() {
				backupConfig.Status.Conditions = []metav1.Condition{
					{
						Type:   v1beta1.ConditionTypeStorageReadable,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   v1beta1.ConditionTypeStorageWritable,
						Status: metav1.ConditionTrue,
					},
				}

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				for _, metric := range result.Metrics {
					if metric.FqName == s3ReadAvailabilityMetricName {
						Expect(metric.Value).To(Equal(float64(1)))
					}
					if metric.FqName == s3WriteAvailabilityMetricName {
						Expect(metric.Value).To(Equal(float64(1)))
					}
				}
			})

			It("should return zero when storage is not readable or writable", func() {
				backupConfig.Status.Conditions = []metav1.Condition{
					{
						Type:   v1beta1.ConditionTypeStorageReadable,
						Status: metav1.ConditionFalse,
					},
					{
						Type:   v1beta1.ConditionTypeStorageWritable,
						Status: metav1.ConditionFalse,
					},
				}

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				for _, metric := range result.Metrics {
					if metric.FqName == s3ReadAvailabilityMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
					if metric.FqName == s3WriteAvailabilityMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
				}
			})

			It("should return zero when storage conditions are not set", func() {
				backupConfig.Status.Conditions = []metav1.Condition{}

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				for _, metric := range result.Metrics {
					if metric.FqName == s3ReadAvailabilityMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
					if metric.FqName == s3WriteAvailabilityMetricName {
						Expect(metric.Value).To(Equal(float64(0)))
					}
				}
			})
		})

		Context("when BackupConfig is not found", func() {
			It("should return an error", func() {
				// Create cluster pointing to non-existent BackupConfig
				cluster := createClusterWithPluginConfig("test-cluster", "default", "non-existent-config")

				fakeClient := setupFakeClientForMetrics(cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})
		})

		Context("when cluster definition is invalid", func() {
			It("should return an error", func() {
				fakeClient := setupFakeClientForMetrics(backupConfig)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: []byte("invalid json"),
				}

				result, err := metricsServer.Collect(ctx, request)

				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})
		})

		Context("comprehensive nil safety test", func() {
			It("should handle all nil fields gracefully without panicking", func() {
				// Set all optional fields to nil
				backupConfig.Status.FirstRecoverabilityPoint = nil
				backupConfig.Status.LastSuccessfulBackup = nil
				backupConfig.Status.LastFailedBackup = nil
				backupConfig.Status.ConsumedStorage = nil
				backupConfig.Status.Conditions = nil

				fakeClient := setupFakeClientForMetrics(backupConfig, cluster)
				metricsServer = MetricsServerImplementation{Client: fakeClient}

				clusterJSON, err := json.Marshal(cluster)
				Expect(err).NotTo(HaveOccurred())

				request := &metrics.CollectMetricsRequest{
					ClusterDefinition: clusterJSON,
				}

				// This should not panic even with all fields nil
				result, err := metricsServer.Collect(ctx, request)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.Metrics).To(HaveLen(8))

				// All metrics should have zero values
				for _, metric := range result.Metrics {
					Expect(metric.Value).To(Equal(float64(0)))
				}
			})
		})
	})
})

// Helper function to create int64 pointer
func int64Ptr(i int64) *int64 {
	return &i
}
