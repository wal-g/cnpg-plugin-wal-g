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

// Package operator contains tests for the plugin_reconciler.go file

package operator

import (
	"context"
	"encoding/json"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgi "github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/controller"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ReconcilerImplementation", func() {
	var (
		ctx           context.Context
		fakeClient    client.Client
		reconciler    ReconcilerImplementation
		testNamespace string
		testCluster   *cnpgv1.Cluster
		testScheme    *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		testNamespace = "test-namespace"
		testScheme = runtime.NewScheme()
		Expect(clientscheme.AddToScheme(testScheme)).To(Succeed())
		Expect(cnpgv1.AddToScheme(testScheme)).To(Succeed())
		Expect(v1beta1.AddToScheme(testScheme)).To(Succeed())
		Expect(rbacv1.AddToScheme(testScheme)).To(Succeed())

		// Create a test cluster
		testCluster = &cnpgv1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "postgresql.cnpg.io/v1",
				Kind:       "Cluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: testNamespace,
				UID:       "test-uid",
			},
			Spec: cnpgv1.ClusterSpec{
				Instances: 3,
				Plugins: []cnpgv1.PluginConfiguration{
					{
						Name: "cnpg-extensions.yandex.cloud",
						Parameters: map[string]string{
							"backupConfig": "test-backup-config",
						},
					},
				},
			},
		}

		// Initialize fake client
		fakeClient = fake.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(testCluster).
			Build()

		reconciler = ReconcilerImplementation{
			Client: fakeClient,
		}
	})

	Describe("GetCapabilities", func() {
		It("should return the correct capabilities", func() {
			result, err := reconciler.GetCapabilities(ctx, &cnpgi.ReconcilerHooksCapabilitiesRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.ReconcilerCapabilities).To(HaveLen(2))
			Expect(result.ReconcilerCapabilities[0].Kind).To(Equal(cnpgi.ReconcilerHooksCapability_KIND_CLUSTER))
			Expect(result.ReconcilerCapabilities[1].Kind).To(Equal(cnpgi.ReconcilerHooksCapability_KIND_BACKUP))
		})
	})

	Describe("Pre", func() {
		var (
			backupConfig *v1beta1.BackupConfig
			request      *cnpgi.ReconcilerHooksRequest
		)

		BeforeEach(func() {
			// Create a test BackupConfig
			backupConfig = &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: testNamespace,
				},
				Spec: v1beta1.BackupConfigSpec{
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							Prefix:      "s3://test-bucket/test-prefix",
							Region:      "us-east-1",
							EndpointURL: "https://s3.amazonaws.com",
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "test-secret",
								},
								Key: "access-key-id",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "test-secret",
								},
								Key: "access-key-secret",
							},
						},
					},
					Retention: v1beta1.BackupRetentionConfig{
						MinBackupsToKeep:   5,
						DeleteBackupsAfter: "7d",
					},
				},
			}

			// Add the BackupConfig to the fake client
			Expect(fakeClient.Create(ctx, backupConfig)).To(Succeed())

			// Create a request with a Cluster resource
			request = &cnpgi.ReconcilerHooksRequest{
				ResourceDefinition: []byte(`{"apiVersion":"postgresql.cnpg.io/v1","kind":"Cluster"}`),
				ClusterDefinition: []byte(`{
					"apiVersion":"postgresql.cnpg.io/v1",
					"kind":"Cluster",
					"metadata":{
						"name":"test-cluster",
						"namespace":"test-namespace",
						"uid":"test-uid"
					},
					"spec":{
						"instances":3,
						"plugins":[
							{
								"name":"cnpg-extensions.yandex.cloud",
								"parameters":{
									"backupConfig":"test-backup-config"
								}
							}
						]
					}
				}`),
			}
		})

		It("should create Role and RoleBinding for the cluster", func() {
			result, err := reconciler.Pre(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Behavior).To(Equal(cnpgi.ReconcilerHooksResult_BEHAVIOR_CONTINUE))

			// Check if Role was created
			var role rbacv1.Role
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: testNamespace,
				Name:      controller.GetRoleNameForBackupConfig(testCluster.Name),
			}, &role)
			Expect(err).NotTo(HaveOccurred())
			Expect(role.Rules).To(HaveLen(3))

			// Check if the Role has the correct rules
			Expect(role.Rules[0].APIGroups).To(ContainElement("cnpg-extensions.yandex.cloud"))
			Expect(role.Rules[0].Resources).To(ContainElement("backupconfigs"))
			Expect(role.Rules[0].Verbs).To(ContainElements("watch", "get", "list"))
			Expect(role.Rules[0].ResourceNames).To(ContainElement("test-backup-config"))

			Expect(role.Rules[1].APIGroups).To(ContainElement("cnpg-extensions.yandex.cloud"))
			Expect(role.Rules[1].Resources).To(ContainElement("backupconfigs/status"))
			Expect(role.Rules[1].Verbs).To(ContainElement("update"))

			Expect(role.Rules[2].APIGroups).To(ContainElement(""))
			Expect(role.Rules[2].Resources).To(ContainElement("secrets"))
			Expect(role.Rules[2].Verbs).To(ContainElements("watch", "get", "list"))
			Expect(role.Rules[2].ResourceNames).To(ContainElement("test-secret"))

			// Check if RoleBinding was created
			var roleBinding rbacv1.RoleBinding
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: testNamespace,
				Name:      controller.GetRoleNameForBackupConfig(testCluster.Name),
			}, &roleBinding)
			Expect(err).NotTo(HaveOccurred())
			Expect(roleBinding.Subjects).To(HaveLen(1))
			Expect(roleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(roleBinding.Subjects[0].Name).To(Equal(testCluster.Name))
			Expect(roleBinding.RoleRef.Kind).To(Equal("Role"))
			Expect(roleBinding.RoleRef.Name).To(Equal(controller.GetRoleNameForBackupConfig(testCluster.Name)))
		})

		It("should update existing Role when BackupConfig changes", func() {
			// First create the Role and RoleBinding
			result, err := reconciler.Pre(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// Create a new BackupConfig with different secret
			newBackupConfig := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: testNamespace,
				},
				Spec: v1beta1.BackupConfigSpec{
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							Prefix:      "s3://test-bucket/test-prefix",
							Region:      "us-east-1",
							EndpointURL: "https://s3.amazonaws.com",
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "new-test-secret",
								},
								Key: "access-key-id",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "new-test-secret",
								},
								Key: "access-key-secret",
							},
						},
					},
					Retention: v1beta1.BackupRetentionConfig{
						MinBackupsToKeep:   5,
						DeleteBackupsAfter: "7d",
					},
				},
			}

			// Update the BackupConfig
			Expect(fakeClient.Delete(ctx, backupConfig)).To(Succeed())
			Expect(fakeClient.Create(ctx, newBackupConfig)).To(Succeed())

			// Run Pre hook again
			result, err = reconciler.Pre(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// Check if Role was updated with new secret
			var role rbacv1.Role
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: testNamespace,
				Name:      controller.GetRoleNameForBackupConfig(testCluster.Name),
			}, &role)
			Expect(err).NotTo(HaveOccurred())
			Expect(role.Rules).To(HaveLen(3))
			Expect(role.Rules[2].ResourceNames).To(ContainElement("new-test-secret"))
		})

		It("should handle recovery BackupConfig", func() {
			// Add recovery plugin configuration to the cluster
			testCluster.Spec.Bootstrap = &cnpgv1.BootstrapConfiguration{
				Recovery: &cnpgv1.BootstrapRecovery{
					Source: "test-cluster",
				},
			}
			testCluster.Spec.ExternalClusters = []cnpgv1.ExternalCluster{
				{
					Name: "test-cluster",
					PluginConfiguration: &cnpgv1.PluginConfiguration{
						Name: "cnpg-extensions.yandex.cloud",
						Parameters: map[string]string{
							"backupConfig": "recovery-backup-config",
						},
					},
				},
			}

			// Create a recovery BackupConfig
			recoveryBackupConfig := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recovery-backup-config",
					Namespace: testNamespace,
				},
				Spec: v1beta1.BackupConfigSpec{
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							Prefix:      "s3://recovery-bucket/recovery-prefix",
							Region:      "us-west-1",
							EndpointURL: "https://s3.amazonaws.com",
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "recovery-secret",
								},
								Key: "access-key-id",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "recovery-secret",
								},
								Key: "access-key-secret",
							},
						},
					},
					Retention: v1beta1.BackupRetentionConfig{
						MinBackupsToKeep:   5,
						DeleteBackupsAfter: "7d",
					},
				},
			}
			// Add the recovery BackupConfig to the fake client
			Expect(fakeClient.Create(ctx, recoveryBackupConfig)).To(Succeed())

			// Update the cluster in the fake client
			Expect(fakeClient.Update(ctx, testCluster)).To(Succeed())

			// Update the request with the updated cluster
			request.ClusterDefinition, _ = json.Marshal(testCluster)

			// Run Pre hook
			result, err := reconciler.Pre(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// Check if Role includes both BackupConfigs and their secrets
			var role rbacv1.Role
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: testNamespace,
				Name:      controller.GetRoleNameForBackupConfig(testCluster.Name),
			}, &role)
			Expect(err).NotTo(HaveOccurred())
			Expect(role.Rules).To(HaveLen(3))
			Expect(role.Rules[0].ResourceNames).To(ContainElements("test-backup-config", "recovery-backup-config"))
			Expect(role.Rules[2].ResourceNames).To(ContainElements("test-secret", "recovery-secret"))
		})

		It("should skip reconciliation for non-Cluster resources", func() {
			// Create a request with a non-Cluster resource
			nonClusterRequest := &cnpgi.ReconcilerHooksRequest{
				ResourceDefinition: []byte(`{"apiVersion":"postgresql.cnpg.io/v1","kind":"Backup"}`),
			}

			result, err := reconciler.Pre(ctx, nonClusterRequest)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Behavior).To(Equal(cnpgi.ReconcilerHooksResult_BEHAVIOR_CONTINUE))

			// Check that no Role was created
			var role rbacv1.Role
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: testNamespace,
				Name:      controller.GetRoleNameForBackupConfig(testCluster.Name),
			}, &role)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		})

		It("should handle missing BackupConfig gracefully", func() {
			// Delete the BackupConfig
			Expect(fakeClient.Delete(ctx, backupConfig)).To(Succeed())

			// Run Pre hook
			result, err := reconciler.Pre(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Behavior).To(Equal(cnpgi.ReconcilerHooksResult_BEHAVIOR_REQUEUE))
		})
	})

	Describe("Post", func() {
		It("should return CONTINUE behavior", func() {
			result, err := reconciler.Post(ctx, &cnpgi.ReconcilerHooksRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Behavior).To(Equal(cnpgi.ReconcilerHooksResult_BEHAVIOR_CONTINUE))
		})
	})

	Describe("ensureRole", func() {
		var backupConfig *v1beta1.BackupConfig

		BeforeEach(func() {
			// Create a test BackupConfig
			backupConfig = &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backup-config",
					Namespace: testNamespace,
				},
				Spec: v1beta1.BackupConfigSpec{
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							Prefix:      "s3://test-bucket/test-prefix",
							Region:      "us-east-1",
							EndpointURL: "https://s3.amazonaws.com",
							AccessKeyIDRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "test-secret",
								},
								Key: "access-key-id",
							},
							AccessKeySecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "test-secret",
								},
								Key: "access-key-secret",
							},
						},
					},
					Retention: v1beta1.BackupRetentionConfig{
						MinBackupsToKeep:   5,
						DeleteBackupsAfter: "7d",
					},
				},
			}

			// Add the BackupConfig to the fake client
			Expect(fakeClient.Create(ctx, backupConfig)).To(Succeed())
		})

		It("should create a new Role when it doesn't exist", func() {
			err := reconciler.ensureRole(ctx, testCluster, []v1beta1.BackupConfig{*backupConfig})
			Expect(err).NotTo(HaveOccurred())

			var role rbacv1.Role
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: testNamespace,
				Name:      controller.GetRoleNameForBackupConfig(testCluster.Name),
			}, &role)
			Expect(err).NotTo(HaveOccurred())
			Expect(role.Rules).To(HaveLen(3))
		})

		It("should update an existing Role when it changes", func() {
			// First create the Role
			err := reconciler.ensureRole(ctx, testCluster, []v1beta1.BackupConfig{*backupConfig})
			Expect(err).NotTo(HaveOccurred())

			// Create a new BackupConfig with different secret
			newBackupConfig := backupConfig.DeepCopy()
			newBackupConfig.Spec.Storage.S3.AccessKeyIDRef.Name = "new-test-secret"
			newBackupConfig.Spec.Storage.S3.AccessKeySecretRef.Name = "new-test-secret"

			// Update the Role
			err = reconciler.ensureRole(ctx, testCluster, []v1beta1.BackupConfig{*newBackupConfig})
			Expect(err).NotTo(HaveOccurred())

			// Check if Role was updated
			var role rbacv1.Role
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: testNamespace,
				Name:      controller.GetRoleNameForBackupConfig(testCluster.Name),
			}, &role)
			Expect(err).NotTo(HaveOccurred())
			Expect(role.Rules).To(HaveLen(3))
			Expect(role.Rules[2].ResourceNames).To(ContainElement("new-test-secret"))
		})
	})

	Describe("ensureRoleBinding", func() {
		It("should create a new RoleBinding when it doesn't exist", func() {
			err := reconciler.ensureRoleBinding(ctx, testCluster)
			Expect(err).NotTo(HaveOccurred())

			var roleBinding rbacv1.RoleBinding
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: testNamespace,
				Name:      controller.GetRoleNameForBackupConfig(testCluster.Name),
			}, &roleBinding)
			Expect(err).NotTo(HaveOccurred())
			Expect(roleBinding.Subjects).To(HaveLen(1))
			Expect(roleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(roleBinding.Subjects[0].Name).To(Equal(testCluster.Name))
		})

		It("should do nothing when RoleBinding already exists", func() {
			// First create the RoleBinding
			err := reconciler.ensureRoleBinding(ctx, testCluster)
			Expect(err).NotTo(HaveOccurred())

			// Try to ensure it again
			err = reconciler.ensureRoleBinding(ctx, testCluster)
			Expect(err).NotTo(HaveOccurred())

			// Check that RoleBinding still exists
			var roleBinding rbacv1.RoleBinding
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: testNamespace,
				Name:      controller.GetRoleNameForBackupConfig(testCluster.Name),
			}, &roleBinding)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
