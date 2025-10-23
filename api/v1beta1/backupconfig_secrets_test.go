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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("BackupConfig Secrets", func() {
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
		Expect(AddToScheme(scheme)).To(Succeed())
	})

	Context("extractValueFromSource", func() {
		var (
			testSecret    *corev1.Secret
			testConfigMap *corev1.ConfigMap
		)

		BeforeEach(func() {
			testSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"prefix":     []byte("s3://test-bucket/prefix"),
					"region":     []byte("us-west-2"),
					"endpoint":   []byte("https://s3.example.com"),
					"access-key": []byte("test-access-key"),
					"secret-key": []byte("test-secret-key"),
				},
			}

			testConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: namespace,
				},
				Data: map[string]string{
					"prefix":   "s3://test-bucket/cm-prefix",
					"region":   "eu-west-1",
					"endpoint": "https://s3.configmap.com",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(testSecret, testConfigMap).
				Build()
		})

		It("should extract value from Secret reference", func() {
			valueFrom := &ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
					Key:                  "prefix",
				},
			}

			value, err := extractValueFromSource(ctx, fakeClient, valueFrom, namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal("s3://test-bucket/prefix"))
		})

		It("should extract value from ConfigMap reference", func() {
			valueFrom := &ValueFromSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
					Key:                  "region",
				},
			}

			value, err := extractValueFromSource(ctx, fakeClient, valueFrom, namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal("eu-west-1"))
		})

		It("should return error when both SecretKeyRef and ConfigMapKeyRef are specified", func() {
			valueFrom := &ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
					Key:                  "prefix",
				},
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
					Key:                  "prefix",
				},
			}

			_, err := extractValueFromSource(ctx, fakeClient, valueFrom, namespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot specify both secretKeyRef and configMapKeyRef"))
		})

		It("should return error when neither SecretKeyRef nor ConfigMapKeyRef are specified", func() {
			valueFrom := &ValueFromSource{}

			_, err := extractValueFromSource(ctx, fakeClient, valueFrom, namespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must specify either secretKeyRef or configMapKeyRef"))
		})

		It("should return error when valueFrom is nil", func() {
			_, err := extractValueFromSource(ctx, fakeClient, nil, namespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("valueFrom is nil"))
		})

		It("should return error when Secret does not exist", func() {
			valueFrom := &ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "non-existent-secret"},
					Key:                  "prefix",
				},
			}

			_, err := extractValueFromSource(ctx, fakeClient, valueFrom, namespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("while getting secret non-existent-secret"))
		})

		It("should return error when ConfigMap does not exist", func() {
			valueFrom := &ValueFromSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "non-existent-configmap"},
					Key:                  "prefix",
				},
			}

			_, err := extractValueFromSource(ctx, fakeClient, valueFrom, namespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("while getting ConfigMap non-existent-configmap"))
		})

		It("should return error when Secret key does not exist", func() {
			valueFrom := &ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
					Key:                  "non-existent-key",
				},
			}

			_, err := extractValueFromSource(ctx, fakeClient, valueFrom, namespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing key non-existent-key, inside secret test-secret"))
		})

		It("should return error when ConfigMap key does not exist", func() {
			valueFrom := &ValueFromSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
					Key:                  "non-existent-key",
				},
			}

			_, err := extractValueFromSource(ctx, fakeClient, valueFrom, namespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing key non-existent-key, inside ConfigMap test-configmap"))
		})
	})

	Context("resolveStringValue", func() {
		var (
			backupConfig  *BackupConfig
			testSecret    *corev1.Secret
			testConfigMap *corev1.ConfigMap
		)

		BeforeEach(func() {
			testSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"prefix": []byte("s3://test-bucket/secret-prefix"),
				},
			}

			testConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: namespace,
				},
				Data: map[string]string{
					"prefix": "s3://test-bucket/cm-prefix",
				},
			}

			backupConfig = &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(testSecret, testConfigMap).
				Build()
		})

		It("should return direct value when provided", func() {
			directValue := "s3://test-bucket/direct-prefix"

			value, err := backupConfig.resolveStringValue(ctx, fakeClient, directValue, nil, "prefix")
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal(directValue))
		})

		It("should return value from Secret reference when direct value is empty", func() {
			valueFrom := &ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
					Key:                  "prefix",
				},
			}

			value, err := backupConfig.resolveStringValue(ctx, fakeClient, "", valueFrom, "prefix")
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal("s3://test-bucket/secret-prefix"))
		})

		It("should return value from ConfigMap reference when direct value is empty", func() {
			valueFrom := &ValueFromSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
					Key:                  "prefix",
				},
			}

			value, err := backupConfig.resolveStringValue(ctx, fakeClient, "", valueFrom, "prefix")
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal("s3://test-bucket/cm-prefix"))
		})

		It("should return empty string when both direct value and reference are nil", func() {
			value, err := backupConfig.resolveStringValue(ctx, fakeClient, "", nil, "prefix")
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal(""))
		})

		It("should return error when both direct value and reference are provided", func() {
			directValue := "s3://test-bucket/direct-prefix"
			valueFrom := &ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
					Key:                  "prefix",
				},
			}

			_, err := backupConfig.resolveStringValue(ctx, fakeClient, directValue, valueFrom, "prefix")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot specify both prefix and prefixFrom"))
		})
	})

	Context("makeS3StorageConfigWithPrefilledSecrets", func() {
		var (
			backupConfig  *BackupConfig
			testSecret    *corev1.Secret
			testConfigMap *corev1.ConfigMap
		)

		BeforeEach(func() {
			testSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"access-key": []byte("test-access-key"),
					"secret-key": []byte("test-secret-key"),
					"prefix":     []byte("s3://test-bucket/secret-prefix"),
					"region":     []byte("us-west-2"),
					"endpoint":   []byte("https://s3.secret.com"),
				},
			}

			testConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: namespace,
				},
				Data: map[string]string{
					"prefix":   "s3://test-bucket/cm-prefix",
					"region":   "eu-west-1",
					"endpoint": "https://s3.configmap.com",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(testSecret, testConfigMap).
				Build()
		})

		It("should resolve values from direct fields", func() {
			backupConfig = &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: BackupConfigSpec{
					Storage: StorageConfig{
						StorageType: StorageTypeS3,
						S3: &S3StorageConfig{
							Prefix:      "s3://test-bucket/direct-prefix",
							Region:      "us-east-1",
							EndpointURL: "https://s3.direct.com",
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "secret-key",
							},
						},
					},
				},
			}

			s3Config, err := backupConfig.makeS3StorageConfigWithPrefilledSecrets(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(s3Config).NotTo(BeNil())
			Expect(s3Config.AccessKeyID).To(Equal("test-access-key"))
			Expect(s3Config.AccessKeySecret).To(Equal("test-secret-key"))
			Expect(s3Config.ResolvedPrefix).To(Equal("s3://test-bucket/direct-prefix"))
			Expect(s3Config.ResolvedRegion).To(Equal("us-east-1"))
			Expect(s3Config.ResolvedEndpointURL).To(Equal("https://s3.direct.com"))
		})

		It("should resolve values from Secret references", func() {
			backupConfig = &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: BackupConfigSpec{
					Storage: StorageConfig{
						StorageType: StorageTypeS3,
						S3: &S3StorageConfig{
							PrefixFrom: &ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "prefix",
								},
							},
							RegionFrom: &ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "region",
								},
							},
							EndpointURLFrom: &ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "endpoint",
								},
							},
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "secret-key",
							},
						},
					},
				},
			}

			s3Config, err := backupConfig.makeS3StorageConfigWithPrefilledSecrets(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(s3Config).NotTo(BeNil())
			Expect(s3Config.AccessKeyID).To(Equal("test-access-key"))
			Expect(s3Config.AccessKeySecret).To(Equal("test-secret-key"))
			Expect(s3Config.ResolvedPrefix).To(Equal("s3://test-bucket/secret-prefix"))
			Expect(s3Config.ResolvedRegion).To(Equal("us-west-2"))
			Expect(s3Config.ResolvedEndpointURL).To(Equal("https://s3.secret.com"))
		})

		It("should resolve values from ConfigMap references", func() {
			backupConfig = &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: BackupConfigSpec{
					Storage: StorageConfig{
						StorageType: StorageTypeS3,
						S3: &S3StorageConfig{
							PrefixFrom: &ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
									Key:                  "prefix",
								},
							},
							RegionFrom: &ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
									Key:                  "region",
								},
							},
							EndpointURLFrom: &ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
									Key:                  "endpoint",
								},
							},
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "secret-key",
							},
						},
					},
				},
			}

			s3Config, err := backupConfig.makeS3StorageConfigWithPrefilledSecrets(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(s3Config).NotTo(BeNil())
			Expect(s3Config.AccessKeyID).To(Equal("test-access-key"))
			Expect(s3Config.AccessKeySecret).To(Equal("test-secret-key"))
			Expect(s3Config.ResolvedPrefix).To(Equal("s3://test-bucket/cm-prefix"))
			Expect(s3Config.ResolvedRegion).To(Equal("eu-west-1"))
			Expect(s3Config.ResolvedEndpointURL).To(Equal("https://s3.configmap.com"))
		})

		It("should return nil when S3 config is nil", func() {
			backupConfig = &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: BackupConfigSpec{
					Storage: StorageConfig{
						StorageType: StorageTypeS3,
						S3:          nil,
					},
				},
			}

			s3Config, err := backupConfig.makeS3StorageConfigWithPrefilledSecrets(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(s3Config).To(BeNil())
		})

		It("should return error when both direct value and reference are provided", func() {
			backupConfig = &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: BackupConfigSpec{
					Storage: StorageConfig{
						StorageType: StorageTypeS3,
						S3: &S3StorageConfig{
							Prefix: "s3://test-bucket/direct-prefix",
							PrefixFrom: &ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "prefix",
								},
							},
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "secret-key",
							},
						},
					},
				},
			}

			_, err := backupConfig.makeS3StorageConfigWithPrefilledSecrets(ctx, fakeClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot specify both prefix and prefixFrom"))
		})

		It("should handle mixed direct values and references", func() {
			backupConfig = &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: BackupConfigSpec{
					Storage: StorageConfig{
						StorageType: StorageTypeS3,
						S3: &S3StorageConfig{
							Prefix: "s3://test-bucket/direct-prefix", // Direct value
							RegionFrom: &ValueFromSource{ // From Secret
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "region",
								},
							},
							EndpointURLFrom: &ValueFromSource{ // From ConfigMap
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
									Key:                  "endpoint",
								},
							},
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "secret-key",
							},
						},
					},
				},
			}

			s3Config, err := backupConfig.makeS3StorageConfigWithPrefilledSecrets(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(s3Config).NotTo(BeNil())
			Expect(s3Config.AccessKeyID).To(Equal("test-access-key"))
			Expect(s3Config.AccessKeySecret).To(Equal("test-secret-key"))
			Expect(s3Config.ResolvedPrefix).To(Equal("s3://test-bucket/direct-prefix"))
			Expect(s3Config.ResolvedRegion).To(Equal("us-west-2"))
			Expect(s3Config.ResolvedEndpointURL).To(Equal("https://s3.configmap.com"))
		})
	})

	Context("PrefetchSecretsData", func() {
		var (
			backupConfig  *BackupConfig
			testSecret    *corev1.Secret
			testConfigMap *corev1.ConfigMap
		)

		BeforeEach(func() {
			testSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"access-key":   []byte("test-access-key"),
					"secret-key":   []byte("test-secret-key"),
					"prefix":       []byte("s3://test-bucket/secret-prefix"),
					"libsodiumKey": []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
				},
			}

			testConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: namespace,
				},
				Data: map[string]string{
					"region": "eu-west-1",
				},
			}

			backupConfig = &BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: namespace,
				},
				Spec: BackupConfigSpec{
					Storage: StorageConfig{
						StorageType: StorageTypeS3,
						S3: &S3StorageConfig{
							PrefixFrom: &ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "prefix",
								},
							},
							RegionFrom: &ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
									Key:                  "region",
								},
							},
							EndpointURL: "https://s3.direct.com",
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "access-key",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "secret-key",
							},
						},
					},
					Encryption: BackupEncryptionConfig{
						Method:                       "libsodium",
						ExistingEncryptionSecretName: "test-secret",
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(testSecret, testConfigMap).
				Build()
		})

		It("should prefetch all secrets data including resolved values", func() {
			configWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(configWithSecrets).NotTo(BeNil())

			// Check S3 configuration
			Expect(configWithSecrets.Spec.Storage.S3).NotTo(BeNil())
			Expect(configWithSecrets.Spec.Storage.S3.AccessKeyID).To(Equal("test-access-key"))
			Expect(configWithSecrets.Spec.Storage.S3.AccessKeySecret).To(Equal("test-secret-key"))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedPrefix).To(Equal("s3://test-bucket/secret-prefix"))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedRegion).To(Equal("eu-west-1"))
			Expect(configWithSecrets.Spec.Storage.S3.ResolvedEndpointURL).To(Equal("https://s3.direct.com"))

			// Check encryption configuration
			Expect(configWithSecrets.Spec.Encryption).NotTo(BeNil())
			Expect(configWithSecrets.Spec.Encryption.EncryptionKeyData).To(Equal("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))

			// Check that original BackupConfig is preserved
			Expect(configWithSecrets.BackupConfig.Name).To(Equal("test-backup-config"))
			Expect(configWithSecrets.BackupConfig.Namespace).To(Equal(namespace))
		})
	})
})
