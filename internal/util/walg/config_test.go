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

package walg

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
)

var _ = Describe("WAL-G Config Integration Tests", func() {
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
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta1.AddToScheme(scheme)).To(Succeed())
	})

	Context("End-to-End WAL-G Configuration", func() {
		It("should generate correct wal-g config from BackupConfig with direct values", func() {
			// Create test BackupConfig with direct values
			backupConfig := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: v1beta1.BackupConfigSpec{
					DownloadFileRetries: 10,
					DeltaMaxSteps:       5,
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							Prefix:         "s3://test-bucket/direct-prefix",
							Region:         "us-east-1",
							EndpointURL:    "https://s3.amazonaws.com",
							ForcePathStyle: true,
							StorageClass:   "STANDARD_IA",
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
								Key:                  "secret-key",
							},
						},
					},
					Encryption: v1beta1.BackupEncryptionConfig{
						Method:                       "libsodium",
						ExistingEncryptionSecretName: "encryption-secret",
					},
				},
			}

			// Create test Secret for S3 credentials
			s3Secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-credentials",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"access-key": []byte("test-access-key-id"),
					"secret-key": []byte("test-secret-access-key"),
				},
			}

			// Create test Secret for encryption
			encryptionSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "encryption-secret",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"libsodiumKey": []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(s3Secret, encryptionSecret).
				Build()

			// Prefetch secrets data
			configWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())

			// Generate wal-g configuration
			pgMajorVersion := 16
			// For tests, we don't have a cluster context, so we pass nil
			walgConfig := NewConfigFromBackupConfig(configWithSecrets, pgMajorVersion, nil)

			// Verify wal-g configuration
			Expect(walgConfig.AWSAccessKeyID).To(Equal("test-access-key-id"))
			Expect(walgConfig.AWSSecretAccessKey).To(Equal("test-secret-access-key"))
			Expect(walgConfig.AWSRegion).To(Equal("us-east-1"))
			Expect(walgConfig.AWSEndpoint).To(Equal("https://s3.amazonaws.com"))
			Expect(walgConfig.AWSS3ForcePathStyle).To(BeTrue())
			Expect(walgConfig.WaleS3Prefix).To(Equal("s3://test-bucket/direct-prefix/16"))
			Expect(walgConfig.WalgS3StorageClass).To(Equal("STANDARD_IA"))
			Expect(walgConfig.WalgDownloadFileRetries).To(Equal(10))
			Expect(walgConfig.WalgDeltaMaxSteps).To(Equal(5))
			Expect(walgConfig.WalgLibsodiumKey).To(Equal("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
			Expect(walgConfig.WalgLibsodiumKeyTransform).To(Equal("hex"))

			// Verify environment variables
			envMap := walgConfig.ToEnvMap()
			Expect(envMap["AWS_ACCESS_KEY_ID"]).To(Equal("test-access-key-id"))
			Expect(envMap["AWS_SECRET_ACCESS_KEY"]).To(Equal("test-secret-access-key"))
			Expect(envMap["AWS_REGION"]).To(Equal("us-east-1"))
			Expect(envMap["AWS_ENDPOINT"]).To(Equal("https://s3.amazonaws.com"))
			Expect(envMap["AWS_S3_FORCE_PATH_STYLE"]).To(Equal("true"))
			Expect(envMap["WALE_S3_PREFIX"]).To(Equal("s3://test-bucket/direct-prefix/16"))
			Expect(envMap["WALG_S3_STORAGE_CLASS"]).To(Equal("STANDARD_IA"))
			Expect(envMap["WALG_DOWNLOAD_FILE_RETRIES"]).To(Equal("10"))
			Expect(envMap["WALG_DELTA_MAX_STEPS"]).To(Equal("5"))
			Expect(envMap["WALG_LIBSODIUM_KEY"]).To(Equal("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
			Expect(envMap["WALG_LIBSODIUM_KEY_TRANSFORM"]).To(Equal("hex"))
		})

		It("should generate correct wal-g config from BackupConfig with Secret references", func() {
			// Create test BackupConfig with Secret references
			backupConfig := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: v1beta1.BackupConfigSpec{
					DownloadFileRetries: 15,
					DeltaMaxSteps:       7,
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							PrefixFrom: &v1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config"},
									Key:                  "prefix",
								},
							},
							RegionFrom: &v1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config"},
									Key:                  "region",
								},
							},
							EndpointURLFrom: &v1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config"},
									Key:                  "endpoint",
								},
							},
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
								Key:                  "secret-key",
							},
						},
					},
				},
			}

			// Create test Secrets
			s3ConfigSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-config",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"prefix":   []byte("s3://secret-bucket/secret-prefix"),
					"region":   []byte("eu-west-1"),
					"endpoint": []byte("https://s3.eu-west-1.amazonaws.com"),
				},
			}

			s3CredentialsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-credentials",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"access-key": []byte("secret-access-key-id"),
					"secret-key": []byte("secret-secret-access-key"),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(s3ConfigSecret, s3CredentialsSecret).
				Build()

			// Prefetch secrets data
			configWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())

			// Verify resolved values
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedPrefix).To(Equal("s3://secret-bucket/secret-prefix"))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedRegion).To(Equal("eu-west-1"))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedEndpointURL).To(Equal("https://s3.eu-west-1.amazonaws.com"))

			// Generate wal-g configuration
			pgMajorVersion := 15
			// For tests, we don't have a cluster context, so we pass nil
			walgConfig := NewConfigFromBackupConfig(configWithSecrets, pgMajorVersion, nil)

			// Verify wal-g configuration uses resolved values
			Expect(walgConfig.AWSAccessKeyID).To(Equal("secret-access-key-id"))
			Expect(walgConfig.AWSSecretAccessKey).To(Equal("secret-secret-access-key"))
			Expect(walgConfig.AWSRegion).To(Equal("eu-west-1"))
			Expect(walgConfig.AWSEndpoint).To(Equal("https://s3.eu-west-1.amazonaws.com"))
			Expect(walgConfig.WaleS3Prefix).To(Equal("s3://secret-bucket/secret-prefix/15"))
			Expect(walgConfig.WalgDownloadFileRetries).To(Equal(15))
			Expect(walgConfig.WalgDeltaMaxSteps).To(Equal(7))
		})

		It("should generate correct wal-g config from BackupConfig with ConfigMap references", func() {
			// Create test BackupConfig with ConfigMap references
			backupConfig := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: v1beta1.BackupConfigSpec{
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							PrefixFrom: &v1beta1.ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config"},
									Key:                  "prefix",
								},
							},
							RegionFrom: &v1beta1.ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config"},
									Key:                  "region",
								},
							},
							EndpointURL: "https://s3.direct.amazonaws.com", // Direct value
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
								Key:                  "secret-key",
							},
						},
					},
				},
			}

			// Create test ConfigMap and Secret
			s3ConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-config",
					Namespace: namespace,
				},
				Data: map[string]string{
					"prefix": "s3://configmap-bucket/configmap-prefix",
					"region": "ap-southeast-1",
				},
			}

			s3CredentialsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-credentials",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"access-key": []byte("configmap-access-key-id"),
					"secret-key": []byte("configmap-secret-access-key"),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(s3ConfigMap, s3CredentialsSecret).
				Build()

			// Prefetch secrets data
			configWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())

			// Verify resolved values (mix of ConfigMap, direct, and Secret values)
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedPrefix).To(Equal("s3://configmap-bucket/configmap-prefix"))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedRegion).To(Equal("ap-southeast-1"))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedEndpointURL).To(Equal("https://s3.direct.amazonaws.com"))

			// Generate wal-g configuration
			pgMajorVersion := 14
			// For tests, we don't have a cluster context, so we pass nil
			walgConfig := NewConfigFromBackupConfig(configWithSecrets, pgMajorVersion, nil)

			// Verify wal-g configuration uses resolved values
			Expect(walgConfig.AWSAccessKeyID).To(Equal("configmap-access-key-id"))
			Expect(walgConfig.AWSSecretAccessKey).To(Equal("configmap-secret-access-key"))
			Expect(walgConfig.AWSRegion).To(Equal("ap-southeast-1"))
			Expect(walgConfig.AWSEndpoint).To(Equal("https://s3.direct.amazonaws.com"))
			Expect(walgConfig.WaleS3Prefix).To(Equal("s3://configmap-bucket/configmap-prefix/14"))

			// Verify environment variables
			envMap := walgConfig.ToEnvMap()
			Expect(envMap["AWS_REGION"]).To(Equal("ap-southeast-1"))
			Expect(envMap["AWS_ENDPOINT"]).To(Equal("https://s3.direct.amazonaws.com"))
			Expect(envMap["WALE_S3_PREFIX"]).To(Equal("s3://configmap-bucket/configmap-prefix/14"))
		})

		It("should handle empty resolved values gracefully", func() {
			// Create test BackupConfig with empty values
			backupConfig := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: v1beta1.BackupConfigSpec{
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							// No prefix, region, or endpoint specified
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
								Key:                  "secret-key",
							},
						},
					},
				},
			}

			// Create test Secret
			s3CredentialsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "s3-credentials",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"access-key": []byte("test-access-key-id"),
					"secret-key": []byte("test-secret-access-key"),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(s3CredentialsSecret).
				Build()

			// Prefetch secrets data
			configWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())

			// Verify resolved values are empty
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedPrefix).To(Equal(""))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedRegion).To(Equal(""))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedEndpointURL).To(Equal(""))

			// Generate wal-g configuration
			pgMajorVersion := 13
			// For tests, we don't have a cluster context, so we pass nil
			walgConfig := NewConfigFromBackupConfig(configWithSecrets, pgMajorVersion, nil)

			// Verify wal-g configuration handles empty values
			Expect(walgConfig.AWSAccessKeyID).To(Equal("test-access-key-id"))
			Expect(walgConfig.AWSSecretAccessKey).To(Equal("test-secret-access-key"))
			Expect(walgConfig.AWSRegion).To(Equal(""))
			Expect(walgConfig.AWSEndpoint).To(Equal(""))
			Expect(walgConfig.WaleS3Prefix).To(Equal("/13")) // Empty prefix results in "/13"

			// Verify environment variables omit empty values
			envMap := walgConfig.ToEnvMap()
			Expect(envMap["AWS_ACCESS_KEY_ID"]).To(Equal("test-access-key-id"))
			Expect(envMap["AWS_SECRET_ACCESS_KEY"]).To(Equal("test-secret-access-key"))
			// Empty values should be omitted due to omitempty tag
			Expect(envMap).NotTo(HaveKey("AWS_REGION"))
			Expect(envMap).NotTo(HaveKey("AWS_ENDPOINT"))
			Expect(envMap["WALE_S3_PREFIX"]).To(Equal("/13"))
		})

		It("should demonstrate the complete workflow with mutual exclusivity validation", func() {
			// This test demonstrates the complete workflow:
			// 1. Create BackupConfig with references
			// 2. Validate mutual exclusivity (would be done by webhook)
			// 3. Prefetch secrets data
			// 4. Generate wal-g configuration
			// 5. Verify the final result

			backupConfig := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complete-workflow-config",
					Namespace: namespace,
				},
				Spec: v1beta1.BackupConfigSpec{
					DownloadFileRetries: 20,
					DeltaMaxSteps:       10,
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							// Mix of direct values and references
							Prefix: "s3://workflow-bucket/direct-prefix",
							RegionFrom: &v1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "workflow-secret"},
									Key:                  "region",
								},
							},
							EndpointURLFrom: &v1beta1.ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "workflow-configmap"},
									Key:                  "endpoint",
								},
							},
							ForcePathStyle: true,
							StorageClass:   "GLACIER",
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "workflow-credentials"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "workflow-credentials"},
								Key:                  "secret-key",
							},
						},
					},
					Encryption: v1beta1.BackupEncryptionConfig{
						Method:                       "libsodium",
						ExistingEncryptionSecretName: "workflow-encryption",
					},
				},
			}

			// Create test resources
			workflowSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "workflow-secret",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"region": []byte("us-west-2"),
				},
			}

			workflowConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "workflow-configmap",
					Namespace: namespace,
				},
				Data: map[string]string{
					"endpoint": "https://s3.us-west-2.amazonaws.com",
				},
			}

			workflowCredentials := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "workflow-credentials",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"access-key": []byte("workflow-access-key-id"),
					"secret-key": []byte("workflow-secret-access-key"),
				},
			}

			workflowEncryption := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "workflow-encryption",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"libsodiumKey": []byte("fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(workflowSecret, workflowConfigMap, workflowCredentials, workflowEncryption).
				Build()

			// Step 1: Validate mutual exclusivity (simulated - would be done by webhook)
			// This BackupConfig should pass validation because it doesn't violate mutual exclusivity

			// Step 2: Prefetch secrets data
			configWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())

			// Step 3: Verify resolved values
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedPrefix).To(Equal("s3://workflow-bucket/direct-prefix"))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedRegion).To(Equal("us-west-2"))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedEndpointURL).To(Equal("https://s3.us-west-2.amazonaws.com"))

			// Step 4: Generate wal-g configuration
			pgMajorVersion := 17
			// For tests, we don't have a cluster context, so we pass nil
			walgConfig := NewConfigFromBackupConfig(configWithSecrets, pgMajorVersion, nil)

			// Step 5: Verify the final wal-g configuration
			Expect(walgConfig.AWSAccessKeyID).To(Equal("workflow-access-key-id"))
			Expect(walgConfig.AWSSecretAccessKey).To(Equal("workflow-secret-access-key"))
			Expect(walgConfig.AWSRegion).To(Equal("us-west-2"))
			Expect(walgConfig.AWSEndpoint).To(Equal("https://s3.us-west-2.amazonaws.com"))
			Expect(walgConfig.AWSS3ForcePathStyle).To(BeTrue())
			Expect(walgConfig.WaleS3Prefix).To(Equal("s3://workflow-bucket/direct-prefix/17"))
			Expect(walgConfig.WalgS3StorageClass).To(Equal("GLACIER"))
			Expect(walgConfig.WalgDownloadFileRetries).To(Equal(20))
			Expect(walgConfig.WalgDeltaMaxSteps).To(Equal(10))
			Expect(walgConfig.WalgLibsodiumKey).To(Equal("fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"))
			Expect(walgConfig.WalgLibsodiumKeyTransform).To(Equal("hex"))

			// Step 6: Verify environment variables for final deployment
			envMap := walgConfig.ToEnvMap()
			Expect(envMap).To(HaveKeyWithValue("AWS_ACCESS_KEY_ID", "workflow-access-key-id"))
			Expect(envMap).To(HaveKeyWithValue("AWS_SECRET_ACCESS_KEY", "workflow-secret-access-key"))
			Expect(envMap).To(HaveKeyWithValue("AWS_REGION", "us-west-2"))
			Expect(envMap).To(HaveKeyWithValue("AWS_ENDPOINT", "https://s3.us-west-2.amazonaws.com"))
			Expect(envMap).To(HaveKeyWithValue("AWS_S3_FORCE_PATH_STYLE", "true"))
			Expect(envMap).To(HaveKeyWithValue("WALE_S3_PREFIX", "s3://workflow-bucket/direct-prefix/17"))
			Expect(envMap).To(HaveKeyWithValue("WALG_S3_STORAGE_CLASS", "GLACIER"))
			Expect(envMap).To(HaveKeyWithValue("WALG_DOWNLOAD_FILE_RETRIES", "20"))
			Expect(envMap).To(HaveKeyWithValue("WALG_DELTA_MAX_STEPS", "10"))
			Expect(envMap).To(HaveKeyWithValue("WALG_LIBSODIUM_KEY", "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"))
			Expect(envMap).To(HaveKeyWithValue("WALG_LIBSODIUM_KEY_TRANSFORM", "hex"))
		})
	})
})
