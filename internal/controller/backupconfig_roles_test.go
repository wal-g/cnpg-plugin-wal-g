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

package controller

import (
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("BackupConfig RBAC", func() {
	var (
		cluster *cnpgv1.Cluster
		role    *rbacv1.Role
	)

	BeforeEach(func() {
		cluster = &cnpgv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-namespace",
			},
		}

		role = &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GetRoleNameForBackupConfig(cluster.Name),
				Namespace: cluster.Namespace,
			},
		}
	})

	Context("BuildRoleForBackupConfigs", func() {
		It("should include permissions for direct S3 configuration", func() {
			backupConfigs := []v1beta1.BackupConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-backup-config",
						Namespace: "test-namespace",
					},
					Spec: v1beta1.BackupConfigSpec{
						Storage: v1beta1.StorageConfig{
							StorageType: v1beta1.StorageTypeS3,
							S3: &v1beta1.S3StorageConfig{
								Prefix:      "s3://test-bucket/prefix",
								Region:      "us-east-1",
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
				},
			}

			BuildRoleForBackupConfigs(role, cluster, backupConfigs)

			// Verify BackupConfig permissions
			Expect(role.Rules).To(HaveLen(4))
			backupConfigRule := role.Rules[0]
			Expect(backupConfigRule.APIGroups).To(ContainElement("cnpg-extensions.yandex.cloud"))
			Expect(backupConfigRule.Resources).To(ContainElement("backupconfigs"))
			Expect(backupConfigRule.ResourceNames).To(ContainElement("test-backup-config"))
			Expect(backupConfigRule.Verbs).To(ContainElements("watch", "get", "list"))

			// Verify Secret permissions
			secretRule := role.Rules[2]
			Expect(secretRule.APIGroups).To(ContainElement(""))
			Expect(secretRule.Resources).To(ContainElement("secrets"))
			Expect(secretRule.ResourceNames).To(ContainElement("s3-credentials"))
			Expect(secretRule.Verbs).To(ContainElements("watch", "get", "list"))
		})

		It("should include permissions for ValueFromSource Secret references", func() {
			backupConfigs := []v1beta1.BackupConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-backup-config",
						Namespace: "test-namespace",
					},
					Spec: v1beta1.BackupConfigSpec{
						Storage: v1beta1.StorageConfig{
							StorageType: v1beta1.StorageTypeS3,
							S3: &v1beta1.S3StorageConfig{
								PrefixFrom: &v1beta1.ValueFromSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config-secret"},
										Key:                  "prefix",
									},
								},
								RegionFrom: &v1beta1.ValueFromSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config-secret"},
										Key:                  "region",
									},
								},
								EndpointURLFrom: &v1beta1.ValueFromSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "s3-endpoint-secret"},
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
				},
			}

			BuildRoleForBackupConfigs(role, cluster, backupConfigs)

			// Verify Secret permissions include all referenced secrets
			secretRule := role.Rules[2]
			Expect(secretRule.APIGroups).To(ContainElement(""))
			Expect(secretRule.Resources).To(ContainElement("secrets"))
			Expect(secretRule.ResourceNames).To(ContainElements(
				"s3-config-secret",
				"s3-endpoint-secret",
				"s3-credentials",
			))
			Expect(secretRule.Verbs).To(ContainElements("watch", "get", "list"))
		})

		It("should include permissions for ValueFromSource ConfigMap references", func() {
			backupConfigs := []v1beta1.BackupConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-backup-config",
						Namespace: "test-namespace",
					},
					Spec: v1beta1.BackupConfigSpec{
						Storage: v1beta1.StorageConfig{
							StorageType: v1beta1.StorageTypeS3,
							S3: &v1beta1.S3StorageConfig{
								PrefixFrom: &v1beta1.ValueFromSource{
									ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config-cm"},
										Key:                  "prefix",
									},
								},
								RegionFrom: &v1beta1.ValueFromSource{
									ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config-cm"},
										Key:                  "region",
									},
								},
								EndpointURLFrom: &v1beta1.ValueFromSource{
									ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "s3-endpoint-cm"},
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
				},
			}

			BuildRoleForBackupConfigs(role, cluster, backupConfigs)

			// Verify ConfigMap permissions include all referenced configmaps
			configMapRule := role.Rules[3]
			Expect(configMapRule.APIGroups).To(ContainElement(""))
			Expect(configMapRule.Resources).To(ContainElement("configmaps"))
			Expect(configMapRule.ResourceNames).To(ContainElements(
				"s3-config-cm",
				"s3-endpoint-cm",
			))
			Expect(configMapRule.Verbs).To(ContainElements("watch", "get", "list"))

			// Verify Secret permissions still include credentials
			secretRule := role.Rules[2]
			Expect(secretRule.ResourceNames).To(ContainElement("s3-credentials"))
		})

		It("should include permissions for mixed Secret and ConfigMap references", func() {
			backupConfigs := []v1beta1.BackupConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-backup-config",
						Namespace: "test-namespace",
					},
					Spec: v1beta1.BackupConfigSpec{
						Storage: v1beta1.StorageConfig{
							StorageType: v1beta1.StorageTypeS3,
							S3: &v1beta1.S3StorageConfig{
								PrefixFrom: &v1beta1.ValueFromSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config-secret"},
										Key:                  "prefix",
									},
								},
								RegionFrom: &v1beta1.ValueFromSource{
									ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "s3-config-cm"},
										Key:                  "region",
									},
								},
								EndpointURL: "https://s3.amazonaws.com", // Direct value
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
				},
			}

			BuildRoleForBackupConfigs(role, cluster, backupConfigs)

			// Verify Secret permissions
			secretRule := role.Rules[2]
			Expect(secretRule.ResourceNames).To(ContainElements(
				"s3-config-secret",
				"s3-credentials",
			))

			// Verify ConfigMap permissions
			configMapRule := role.Rules[3]
			Expect(configMapRule.ResourceNames).To(ContainElement("s3-config-cm"))
		})

		It("should include permissions for CustomCA references", func() {
			backupConfigs := []v1beta1.BackupConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-backup-config",
						Namespace: "test-namespace",
					},
					Spec: v1beta1.BackupConfigSpec{
						Storage: v1beta1.StorageConfig{
							StorageType: v1beta1.StorageTypeS3,
							S3: &v1beta1.S3StorageConfig{
								Prefix: "s3://test-bucket/prefix",
								CustomCA: &v1beta1.CustomCAReference{
									Kind: "Secret",
									Name: "custom-ca-secret",
									Key:  "ca.crt",
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
				},
			}

			BuildRoleForBackupConfigs(role, cluster, backupConfigs)

			// Verify Secret permissions include CustomCA secret
			secretRule := role.Rules[2]
			Expect(secretRule.ResourceNames).To(ContainElements(
				"custom-ca-secret",
				"s3-credentials",
			))
		})

		It("should include permissions for encryption secrets", func() {
			backupConfigs := []v1beta1.BackupConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-backup-config",
						Namespace: "test-namespace",
					},
					Spec: v1beta1.BackupConfigSpec{
						Storage: v1beta1.StorageConfig{
							StorageType: v1beta1.StorageTypeS3,
							S3: &v1beta1.S3StorageConfig{
								Prefix: "s3://test-bucket/prefix",
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
				},
			}

			BuildRoleForBackupConfigs(role, cluster, backupConfigs)

			// Verify Secret permissions include encryption secret
			secretRule := role.Rules[2]
			Expect(secretRule.ResourceNames).To(ContainElements(
				"encryption-secret",
				"s3-credentials",
			))
		})

		It("should handle multiple BackupConfigs with different reference types", func() {
			backupConfigs := []v1beta1.BackupConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup-config-1",
						Namespace: "test-namespace",
					},
					Spec: v1beta1.BackupConfigSpec{
						Storage: v1beta1.StorageConfig{
							StorageType: v1beta1.StorageTypeS3,
							S3: &v1beta1.S3StorageConfig{
								PrefixFrom: &v1beta1.ValueFromSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "secret-1"},
										Key:                  "prefix",
									},
								},
								AccessKeyIDRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "credentials-1"},
									Key:                  "access-key",
								},
								AccessKeySecretRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "credentials-1"},
									Key:                  "secret-key",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup-config-2",
						Namespace: "test-namespace",
					},
					Spec: v1beta1.BackupConfigSpec{
						Storage: v1beta1.StorageConfig{
							StorageType: v1beta1.StorageTypeS3,
							S3: &v1beta1.S3StorageConfig{
								RegionFrom: &v1beta1.ValueFromSource{
									ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "configmap-1"},
										Key:                  "region",
									},
								},
								AccessKeyIDRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "credentials-2"},
									Key:                  "access-key",
								},
								AccessKeySecretRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "credentials-2"},
									Key:                  "secret-key",
								},
							},
						},
					},
				},
			}

			BuildRoleForBackupConfigs(role, cluster, backupConfigs)

			// Verify BackupConfig permissions
			backupConfigRule := role.Rules[0]
			Expect(backupConfigRule.ResourceNames).To(ContainElements(
				"backup-config-1",
				"backup-config-2",
			))

			// Verify Secret permissions
			secretRule := role.Rules[2]
			Expect(secretRule.ResourceNames).To(ContainElements(
				"secret-1",
				"credentials-1",
				"credentials-2",
			))

			// Verify ConfigMap permissions
			configMapRule := role.Rules[3]
			Expect(configMapRule.ResourceNames).To(ContainElement("configmap-1"))
		})
	})

	Context("BuildRoleBindingForBackupConfig", func() {
		It("should create correct RoleBinding", func() {
			roleBinding := BuildRoleBindingForBackupConfig(cluster)

			Expect(roleBinding.Name).To(Equal(GetRoleNameForBackupConfig(cluster.Name)))
			Expect(roleBinding.Namespace).To(Equal(cluster.Namespace))
			Expect(roleBinding.Subjects).To(HaveLen(1))
			Expect(roleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(roleBinding.Subjects[0].Name).To(Equal(cluster.Name))
			Expect(roleBinding.Subjects[0].Namespace).To(Equal(cluster.Namespace))
			Expect(roleBinding.RoleRef.Kind).To(Equal("Role"))
			Expect(roleBinding.RoleRef.Name).To(Equal(GetRoleNameForBackupConfig(cluster.Name)))
		})
	})

	Context("GetRoleNameForBackupConfig", func() {
		It("should generate correct role name", func() {
			roleName := GetRoleNameForBackupConfig("test-cluster")
			Expect(roleName).To(Equal("test-cluster-backups"))
		})
	})
})
