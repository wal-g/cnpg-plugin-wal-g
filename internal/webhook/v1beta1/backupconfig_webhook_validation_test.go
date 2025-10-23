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

	cnpgextensionsv1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
)

var _ = Describe("BackupConfig Webhook Validation", func() {
	var (
		ctx       context.Context
		validator BackupConfigCustomValidator
	)

	BeforeEach(func() {
		ctx = context.Background()
		validator = BackupConfigCustomValidator{}
	})

	Context("validateS3MutualExclusivity", func() {
		It("should pass validation when only direct values are provided", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Prefix:      "s3://test-bucket/prefix",
							Region:      "us-east-1",
							EndpointURL: "https://s3.amazonaws.com",
						},
					},
				},
			}

			warnings, err := validator.validateS3MutualExclusivity(backupConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should pass validation when only reference values are provided", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "prefix",
								},
							},
							RegionFrom: &cnpgextensionsv1beta1.ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
									Key:                  "region",
								},
							},
							EndpointURLFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "endpoint",
								},
							},
						},
					},
				},
			}

			warnings, err := validator.validateS3MutualExclusivity(backupConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should pass validation when mixing direct and reference values for different fields", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/prefix", // Direct value
							RegionFrom: &cnpgextensionsv1beta1.ValueFromSource{ // Reference value
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "region",
								},
							},
							EndpointURL: "https://s3.amazonaws.com", // Direct value
						},
					},
				},
			}

			warnings, err := validator.validateS3MutualExclusivity(backupConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should fail validation when both prefix and prefixFrom are provided", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/prefix",
							PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "prefix",
								},
							},
						},
					},
				},
			}

			warnings, err := validator.validateS3MutualExclusivity(backupConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot specify both prefix and prefixFrom"))
			Expect(warnings).To(BeEmpty())
		})

		It("should fail validation when both region and regionFrom are provided", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Region: "us-east-1",
							RegionFrom: &cnpgextensionsv1beta1.ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
									Key:                  "region",
								},
							},
						},
					},
				},
			}

			warnings, err := validator.validateS3MutualExclusivity(backupConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot specify both region and regionFrom"))
			Expect(warnings).To(BeEmpty())
		})

		It("should fail validation when both endpointUrl and endpointUrlFrom are provided", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							EndpointURL: "https://s3.amazonaws.com",
							EndpointURLFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "endpoint",
								},
							},
						},
					},
				},
			}

			warnings, err := validator.validateS3MutualExclusivity(backupConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot specify both endpointUrl and endpointUrlFrom"))
			Expect(warnings).To(BeEmpty())
		})

		It("should pass validation when S3 config is nil", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3:          nil,
					},
				},
			}

			warnings, err := validator.validateS3MutualExclusivity(backupConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("validateValueFromSource", func() {
		It("should pass validation for valid SecretKeyRef", func() {
			valueFrom := &cnpgextensionsv1beta1.ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
					Key:                  "test-key",
				},
			}

			err := validator.validateValueFromSource(valueFrom, "testField")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should pass validation for valid ConfigMapKeyRef", func() {
			valueFrom := &cnpgextensionsv1beta1.ValueFromSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
					Key:                  "test-key",
				},
			}

			err := validator.validateValueFromSource(valueFrom, "testField")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should pass validation when valueFrom is nil", func() {
			err := validator.validateValueFromSource(nil, "testField")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail validation when both SecretKeyRef and ConfigMapKeyRef are specified", func() {
			valueFrom := &cnpgextensionsv1beta1.ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
					Key:                  "test-key",
				},
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
					Key:                  "test-key",
				},
			}

			err := validator.validateValueFromSource(valueFrom, "testField")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("testField cannot specify both secretKeyRef and configMapKeyRef"))
		})

		It("should fail validation when neither SecretKeyRef nor ConfigMapKeyRef are specified", func() {
			valueFrom := &cnpgextensionsv1beta1.ValueFromSource{}

			err := validator.validateValueFromSource(valueFrom, "testField")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("testField must specify either secretKeyRef or configMapKeyRef"))
		})

		It("should fail validation when SecretKeyRef name is empty", func() {
			valueFrom := &cnpgextensionsv1beta1.ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ""},
					Key:                  "test-key",
				},
			}

			err := validator.validateValueFromSource(valueFrom, "testField")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("testField.secretKeyRef.name cannot be empty"))
		})

		It("should fail validation when SecretKeyRef key is empty", func() {
			valueFrom := &cnpgextensionsv1beta1.ValueFromSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
					Key:                  "",
				},
			}

			err := validator.validateValueFromSource(valueFrom, "testField")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("testField.secretKeyRef.key cannot be empty"))
		})

		It("should fail validation when ConfigMapKeyRef name is empty", func() {
			valueFrom := &cnpgextensionsv1beta1.ValueFromSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ""},
					Key:                  "test-key",
				},
			}

			err := validator.validateValueFromSource(valueFrom, "testField")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("testField.configMapKeyRef.name cannot be empty"))
		})

		It("should fail validation when ConfigMapKeyRef key is empty", func() {
			valueFrom := &cnpgextensionsv1beta1.ValueFromSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-configmap"},
					Key:                  "",
				},
			}

			err := validator.validateValueFromSource(valueFrom, "testField")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("testField.configMapKeyRef.key cannot be empty"))
		})
	})

	Context("validateBackupConfig integration", func() {
		It("should pass validation for a complete valid BackupConfig", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
									Key:                  "prefix",
								},
							},
							RegionFrom: &cnpgextensionsv1beta1.ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config"},
									Key:                  "region",
								},
							},
							EndpointURL: "https://s3.amazonaws.com",
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

			warnings, err := validator.validateBackupConfig(backupConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should fail validation for BackupConfig with multiple mutual exclusivity violations", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/prefix",
							PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
									Key:                  "prefix",
								},
							},
							Region: "us-east-1",
							RegionFrom: &cnpgextensionsv1beta1.ValueFromSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config"},
									Key:                  "region",
								},
							},
						},
					},
				},
			}

			warnings, err := validator.validateBackupConfig(backupConfig)
			Expect(err).To(HaveOccurred())
			// Should fail on the first mutual exclusivity violation (prefix)
			Expect(err.Error()).To(ContainSubstring("cannot specify both prefix and prefixFrom"))
			Expect(warnings).To(BeEmpty())
		})

		It("should fail validation for BackupConfig with invalid ValueFromSource", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: ""},
									Key:                  "prefix",
								},
							},
						},
					},
				},
			}

			warnings, err := validator.validateBackupConfig(backupConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("prefixFrom.secretKeyRef.name cannot be empty"))
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("ValidateCreate", func() {
		It("should validate a BackupConfig on creation", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/prefix",
							Region: "us-east-1",
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, backupConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should reject invalid BackupConfig on creation", func() {
			backupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/prefix",
							PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "prefix",
								},
							},
						},
					},
				},
			}

			warnings, err := validator.ValidateCreate(ctx, backupConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot specify both prefix and prefixFrom"))
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("ValidateUpdate", func() {
		It("should validate a BackupConfig on update", func() {
			oldBackupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/old-prefix",
						},
					},
				},
			}

			newBackupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "prefix",
								},
							},
						},
					},
				},
			}

			warnings, err := validator.ValidateUpdate(ctx, oldBackupConfig, newBackupConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should reject invalid BackupConfig on update", func() {
			oldBackupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/old-prefix",
						},
					},
				},
			}

			newBackupConfig := &cnpgextensionsv1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: cnpgextensionsv1beta1.BackupConfigSpec{
					Storage: cnpgextensionsv1beta1.StorageConfig{
						StorageType: cnpgextensionsv1beta1.StorageTypeS3,
						S3: &cnpgextensionsv1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/new-prefix",
							PrefixFrom: &cnpgextensionsv1beta1.ValueFromSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
									Key:                  "prefix",
								},
							},
						},
					},
				},
			}

			warnings, err := validator.ValidateUpdate(ctx, oldBackupConfig, newBackupConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot specify both prefix and prefixFrom"))
			Expect(warnings).To(BeEmpty())
		})
	})
})
