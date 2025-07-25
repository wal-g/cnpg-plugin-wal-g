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
	"context"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Helper function to create a fake client with the given objects
func setupFakeBackupClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(v1beta1.AddToScheme(scheme)).To(Succeed())
	Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

// Helper function to create a Secret for S3 credentials
func createTestS3Secret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"accessKeyId":     []byte("test-access-key-id"),
			"accessKeySecret": []byte("test-access-key-secret"),
		},
	}
}

// Helper function to create a BackupConfig with S3 secret references
func createTestBackupConfig(name, namespace string) (*v1beta1.BackupConfig, *corev1.Secret) {
	// Create the S3 credentials secret
	secretName := name + "-s3-credentials"
	s3Secret := createTestS3Secret(secretName, namespace)

	// Create the BackupConfig with references to the secret
	backupConfig := &v1beta1.BackupConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BackupConfig",
			APIVersion: "cnpg-extensions.yandex.cloud/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-backupconfig-uid"),
		},
		Spec: v1beta1.BackupConfigSpec{
			Storage: v1beta1.StorageConfig{
				StorageType: v1beta1.StorageTypeS3,
				S3: &v1beta1.S3StorageConfig{
					Prefix: "s3://test-bucket/test-prefix",
					AccessKeyIDRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Key: "accessKeyId",
					},
					AccessKeySecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Key: "accessKeySecret",
					},
				},
			},
		},
	}

	return backupConfig, s3Secret
}

// Helper function to create a Cluster
func createTestCluster(name, namespace string) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-cluster-uid"),
		},
		Spec: cnpgv1.ClusterSpec{
			ImageName: "ghcr.io/cloudnative-pg/postgresql:14.0",
			Plugins: []cnpgv1.PluginConfiguration{
				{
					Name: common.PluginName,
					Parameters: map[string]string{
						"backupConfig": "test-backupconfig",
					},
				},
			},
		},
	}
}

// Helper function to create a Backup
func createTestBackup(name, namespace, clusterName string, backupID string, withOwnerRef bool) *cnpgv1.Backup {
	backup := &cnpgv1.Backup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Backup",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Spec: cnpgv1.BackupSpec{
			Cluster: cnpgv1.LocalObjectReference{
				Name: clusterName,
			},
			Method: "plugin",
			PluginConfiguration: &cnpgv1.BackupPluginConfiguration{
				Name: common.PluginName,
			},
		},
		Status: cnpgv1.BackupStatus{
			BackupID: backupID,
		},
	}

	if withOwnerRef {
		backup.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         "cnpg-extensions.yandex.cloud/v1beta1",
				Kind:               "BackupConfig",
				Name:               "test-backupconfig",
				UID:                types.UID("test-backupconfig-uid"),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}

	return backup
}

var _ = Describe("BackupReconciler", func() {
	var (
		reconciler *BackupReconciler
		testCtx    context.Context
	)

	BeforeEach(func() {
		testCtx = context.Background()
	})

	Describe("Reconcile", func() {
		It("should do nothing with Backup without PluginConfiguration", func() {
			// Create test objects
			cluster := createTestCluster("test-cluster", "default")
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			backup := createTestBackup("test-backup", "default", "test-cluster", "base_000000010000000100000001", false)
			backup.Spec.PluginConfiguration = nil

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(cluster, backupConfig, s3Secret, backup)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileOwnerReference
			result, err := reconciler.Reconcile(
				testCtx,
				reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(backup),
				},
			)
			Expect(result).To(BeEquivalentTo(ctrl.Result{}))
			Expect(err).NotTo(HaveOccurred())

			updatedBackup := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "test-backup"}, updatedBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(equality.Semantic.DeepEqual(backup, updatedBackup)).To(BeTrue())
		})
	})

	Describe("reconcileOwnerReference", func() {
		It("should add owner reference to backup without one", func() {
			// Create test objects
			cluster := createTestCluster("test-cluster", "default")
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			backup := createTestBackup("test-backup", "default", "test-cluster", "base_000000010000000100000001", false)

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(cluster, backupConfig, s3Secret, backup)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileOwnerReference
			err := reconciler.reconcileOwnerReference(testCtx, backup)
			Expect(err).NotTo(HaveOccurred())

			// Verify backup has owner reference
			updatedBackup := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "test-backup"}, updatedBackup)
			Expect(err).NotTo(HaveOccurred())

			// Check owner reference
			Expect(updatedBackup.OwnerReferences).To(HaveLen(1))
			Expect(updatedBackup.OwnerReferences[0].Kind).To(Equal("BackupConfig"))
			Expect(updatedBackup.OwnerReferences[0].Name).To(Equal("test-backupconfig"))
		})

		It("should not modify backup that already has owner reference", func() {
			// Create test objects
			cluster := createTestCluster("test-cluster", "default")
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			backup := createTestBackup("test-backup", "default", "test-cluster", "base_000000010000000100000001", true)

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(cluster, backupConfig, s3Secret, backup)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileOwnerReference
			err := reconciler.reconcileOwnerReference(testCtx, backup)
			Expect(err).NotTo(HaveOccurred())

			// Verify backup still has the same owner reference
			updatedBackup := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "test-backup"}, updatedBackup)
			Expect(err).NotTo(HaveOccurred())

			// Check owner reference
			Expect(updatedBackup.OwnerReferences).To(HaveLen(1))
			Expect(updatedBackup.OwnerReferences[0].Kind).To(Equal("BackupConfig"))
			Expect(updatedBackup.OwnerReferences[0].Name).To(Equal("test-backupconfig"))
		})
	})

	Describe("listBackupsOwnedByBackupConfig", func() {
		It("should list backups owned by a BackupConfig", func() {
			// Create test objects
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")

			// Create a mock BackupConfigWithSecrets
			backupConfigWithSecrets := &v1beta1.BackupConfigWithSecrets{
				BackupConfig: *backupConfig,
				Spec: v1beta1.BackupConfigSpecWithSecrets{
					BackupConfigSpec: backupConfig.Spec,
					Storage: v1beta1.StorageConfigWithSecrets{
						StorageConfig: backupConfig.Spec.Storage,
						S3: &v1beta1.S3StorageConfigWithSecrets{
							S3StorageConfig: *backupConfig.Spec.Storage.S3,
							AccessKeyID:     "test-access-key-id",
							AccessKeySecret: "test-access-key-secret",
						},
					},
				},
			}

			backup1 := createTestBackup("test-backup-1", "default", "test-cluster", "base_000000010000000100000001", true)
			backup2 := createTestBackup("test-backup-2", "default", "test-cluster", "base_000000010000000100000002", true)
			backup3 := createTestBackup("test-backup-3", "default", "test-cluster", "base_000000010000000100000003", false) // No owner ref

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(backupConfig, s3Secret, backup1, backup2, backup3)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call listBackupsOwnedByBackupConfig
			backups, err := reconciler.listBackupsOwnedByBackupConfig(testCtx, backupConfigWithSecrets)
			Expect(err).NotTo(HaveOccurred())
			Expect(backups).To(HaveLen(2))

			// Check that the correct backups are returned
			backupNames := []string{backups[0].Name, backups[1].Name}
			Expect(backupNames).To(ContainElements("test-backup-1", "test-backup-2"))
			Expect(backupNames).NotTo(ContainElement("test-backup-3"))
		})
	})

	Describe("reconcileBackupMetadata", func() {
		It("should do nothing for backup without BackupID", func() {
			// Create test objects
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			backup := createTestBackup("test-backup", "default", "test-cluster", "", true)
			cluster := createTestCluster("test-cluster", "default")

			// Create a mock BackupConfigWithSecrets
			backupConfigWithSecrets := &v1beta1.BackupConfigWithSecrets{
				BackupConfig: *backupConfig,
				Spec: v1beta1.BackupConfigSpecWithSecrets{
					BackupConfigSpec: backupConfig.Spec,
					Storage: v1beta1.StorageConfigWithSecrets{
						StorageConfig: backupConfig.Spec.Storage,
						S3: &v1beta1.S3StorageConfigWithSecrets{
							S3StorageConfig: *backupConfig.Spec.Storage.S3,
							AccessKeyID:     "test-access-key-id",
							AccessKeySecret: "test-access-key-secret",
						},
					},
				},
			}

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(backupConfig, s3Secret, backup, cluster)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileBackupMetadata
			updated, err := reconciler.reconcileBackupMetadata(testCtx, backup, backupConfigWithSecrets, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())

			// Verify backup was not updated
			updatedBackup := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "test-backup"}, updatedBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBackup.Labels).ToNot(HaveKey(v1beta1.BackupTypeLabelName))
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "14"))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, ""))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, ""))
		})

		It("should set full backup type label and PG version label for full backup", func() {
			// Create test objects
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			backup := createTestBackup("test-backup", "default", "test-cluster", "base_000000010000000100000001", true)
			cluster := createTestCluster("test-cluster", "default")

			// Set PostgreSQL version in cluster image
			cluster.Spec.ImageName = "ghcr.io/cloudnative-pg/postgresql:14.0"

			// Create a mock BackupConfigWithSecrets
			backupConfigWithSecrets := &v1beta1.BackupConfigWithSecrets{
				BackupConfig: *backupConfig,
				Spec: v1beta1.BackupConfigSpecWithSecrets{
					BackupConfigSpec: backupConfig.Spec,
					Storage: v1beta1.StorageConfigWithSecrets{
						StorageConfig: backupConfig.Spec.Storage,
						S3: &v1beta1.S3StorageConfigWithSecrets{
							S3StorageConfig: *backupConfig.Spec.Storage.S3,
							AccessKeyID:     "test-access-key-id",
							AccessKeySecret: "test-access-key-secret",
						},
					},
				},
			}

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(backupConfig, s3Secret, backup, cluster)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileBackupMetadata
			updated, err := reconciler.reconcileBackupMetadata(testCtx, backup, backupConfigWithSecrets, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())
			Expect(backup.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeFull)))
			Expect(backup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "14"))
			Expect(backup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, ""))
			Expect(backup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, ""))

			// Verify backup was updated with correct labels and annotations
			updatedBackup := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "test-backup"}, updatedBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeFull)))
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "14"))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, ""))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, ""))
		})

		It("should set incremental backup type label and PG version label for incremental backup", func() {
			// Create test objects
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			backup := createTestBackup("test-backup", "default", "test-cluster", "base_000000010000000100000002_D_000000010000000100000001", true)
			cluster := createTestCluster("test-cluster", "default")

			// Set PostgreSQL version in cluster image
			cluster.Spec.ImageName = "ghcr.io/cloudnative-pg/postgresql:15.0"

			// Create a mock BackupConfigWithSecrets
			backupConfigWithSecrets := &v1beta1.BackupConfigWithSecrets{
				BackupConfig: *backupConfig,
				Spec: v1beta1.BackupConfigSpecWithSecrets{
					BackupConfigSpec: backupConfig.Spec,
					Storage: v1beta1.StorageConfigWithSecrets{
						StorageConfig: backupConfig.Spec.Storage,
						S3: &v1beta1.S3StorageConfigWithSecrets{
							S3StorageConfig: *backupConfig.Spec.Storage.S3,
							AccessKeyID:     "test-access-key-id",
							AccessKeySecret: "test-access-key-secret",
						},
					},
				},
			}

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(backupConfig, s3Secret, backup, cluster)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileBackupMetadata
			updated, err := reconciler.reconcileBackupMetadata(testCtx, backup, backupConfigWithSecrets, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())
			Expect(backup.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeIncremental)))
			Expect(backup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "15"))
			Expect(backup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, ""))
			Expect(backup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, ""))

			// Verify backup was updated with correct labels and annotations
			updatedBackup := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "test-backup"}, updatedBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeIncremental)))
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "15"))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, ""))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, ""))
		})

		It("should set direct dependent backups annotation", func() {
			// Create test objects
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			baseBackup := createTestBackup("base-backup", "default", "test-cluster", "base_000000010000000100000001", true)
			dependentBackup := createTestBackup("dependent-backup", "default", "test-cluster", "base_000000010000000100000002_D_000000010000000100000001", true)
			cluster := createTestCluster("test-cluster", "default")

			// Set PostgreSQL version in cluster image
			cluster.Spec.ImageName = "ghcr.io/cloudnative-pg/postgresql:14.0"

			// Create a mock BackupConfigWithSecrets
			backupConfigWithSecrets := &v1beta1.BackupConfigWithSecrets{
				BackupConfig: *backupConfig,
				Spec: v1beta1.BackupConfigSpecWithSecrets{
					BackupConfigSpec: backupConfig.Spec,
					Storage: v1beta1.StorageConfigWithSecrets{
						StorageConfig: backupConfig.Spec.Storage,
						S3: &v1beta1.S3StorageConfigWithSecrets{
							S3StorageConfig: *backupConfig.Spec.Storage.S3,
							AccessKeyID:     "test-access-key-id",
							AccessKeySecret: "test-access-key-secret",
						},
					},
				},
			}

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(backupConfig, s3Secret, baseBackup, dependentBackup, cluster)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileBackupMetadata
			updated, err := reconciler.reconcileBackupMetadata(testCtx, baseBackup, backupConfigWithSecrets, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())
			Expect(baseBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeFull)))
			Expect(baseBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "14"))
			Expect(baseBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, "dependent-backup"))
			Expect(baseBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, "dependent-backup"))

			// Verify backup was updated with correct labels and annotations
			updatedBackup := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "base-backup"}, updatedBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeFull)))
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "14"))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, "dependent-backup"))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, "dependent-backup"))
		})

		It("should set both direct and indirect dependent backups annotations", func() {
			// Create test objects
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			baseBackup := createTestBackup("base-backup", "default", "test-cluster", "base_000000010000000100000001", true)
			directDependentBackup := createTestBackup("direct-dependent", "default", "test-cluster", "base_000000010000000100000002_D_000000010000000100000001", true)
			indirectDependentBackup := createTestBackup("indirect-dependent", "default", "test-cluster", "base_000000010000000100000003_D_000000010000000100000002", true)
			cluster := createTestCluster("test-cluster", "default")

			// Set PostgreSQL version in cluster image
			cluster.Spec.ImageName = "ghcr.io/cloudnative-pg/postgresql:14.0"

			// Create a mock BackupConfigWithSecrets
			backupConfigWithSecrets := &v1beta1.BackupConfigWithSecrets{
				BackupConfig: *backupConfig,
				Spec: v1beta1.BackupConfigSpecWithSecrets{
					BackupConfigSpec: backupConfig.Spec,
					Storage: v1beta1.StorageConfigWithSecrets{
						StorageConfig: backupConfig.Spec.Storage,
						S3: &v1beta1.S3StorageConfigWithSecrets{
							S3StorageConfig: *backupConfig.Spec.Storage.S3,
							AccessKeyID:     "test-access-key-id",
							AccessKeySecret: "test-access-key-secret",
						},
					},
				},
			}

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(backupConfig, s3Secret, baseBackup, directDependentBackup, indirectDependentBackup, cluster)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileBackupMetadata
			updated, err := reconciler.reconcileBackupMetadata(testCtx, baseBackup, backupConfigWithSecrets, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())
			Expect(baseBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeFull)))
			Expect(baseBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "14"))
			Expect(baseBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, "direct-dependent"))
			Expect(baseBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, "direct-dependent indirect-dependent"))

			// Verify backup was updated with correct labels and annotations
			updatedBackup := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "base-backup"}, updatedBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeFull)))
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "14"))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, "direct-dependent"))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, "direct-dependent indirect-dependent"))
		})

		It("should not update backup if metadata is already correct", func() {
			// Create test objects
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			backup := createTestBackup("test-backup", "default", "test-cluster", "base_000000010000000100000001", true)
			cluster := createTestCluster("test-cluster", "default")
			backup.Labels = map[string]string{
				v1beta1.BackupTypeLabelName:      string(v1beta1.BackupTypeFull),
				v1beta1.BackupPgVersionLabelName: "14",
			}
			backup.Annotations = map[string]string{
				v1beta1.BackupDirectDependentsAnnotationName: "",
				v1beta1.BackupAllDependentsAnnotationName:    "",
			}

			// Create a mock BackupConfigWithSecrets
			backupConfigWithSecrets := &v1beta1.BackupConfigWithSecrets{
				BackupConfig: *backupConfig,
				Spec: v1beta1.BackupConfigSpecWithSecrets{
					BackupConfigSpec: backupConfig.Spec,
					Storage: v1beta1.StorageConfigWithSecrets{
						StorageConfig: backupConfig.Spec.Storage,
						S3: &v1beta1.S3StorageConfigWithSecrets{
							S3StorageConfig: *backupConfig.Spec.Storage.S3,
							AccessKeyID:     "test-access-key-id",
							AccessKeySecret: "test-access-key-secret",
						},
					},
				},
			}

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(backupConfig, s3Secret, backup, cluster)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileBackupMetadata
			updated, err := reconciler.reconcileBackupMetadata(testCtx, backup, backupConfigWithSecrets, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeFalse())
		})

		It("should handle backup deletion by updating dependent backups annotations", func() {
			// This test simulates the scenario where a backup is being deleted
			// and we need to update the annotations of other backups

			// Create test objects
			cluster := createTestCluster("test-cluster", "default")
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			baseBackup := createTestBackup("base-backup", "default", "test-cluster", "base_000000010000000100000001", true)
			dependentBackup := createTestBackup("dependent-backup", "default", "test-cluster", "base_000000010000000100000002_D_000000010000000100000001", true)

			// Set deletion timestamp on dependent backup
			now := metav1.Now()
			dependentBackup.Finalizers = append(dependentBackup.Finalizers, v1beta1.BackupFinalizerName)
			dependentBackup.DeletionTimestamp = &now

			// Create a mock BackupConfigWithSecrets
			backupConfigWithSecrets := &v1beta1.BackupConfigWithSecrets{
				BackupConfig: *backupConfig,
				Spec: v1beta1.BackupConfigSpecWithSecrets{
					BackupConfigSpec: backupConfig.Spec,
					Storage: v1beta1.StorageConfigWithSecrets{
						StorageConfig: backupConfig.Spec.Storage,
						S3: &v1beta1.S3StorageConfigWithSecrets{
							S3StorageConfig: *backupConfig.Spec.Storage.S3,
							AccessKeyID:     "test-access-key-id",
							AccessKeySecret: "test-access-key-secret",
						},
					},
				},
			}

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(backupConfig, s3Secret, baseBackup, dependentBackup, cluster)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileBackupMetadata on the base backup
			updated, err := reconciler.reconcileBackupMetadata(testCtx, baseBackup, backupConfigWithSecrets, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue())

			// Verify base backup was updated with empty dependent annotations
			// since the dependent backup is being deleted
			updatedBackup := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "base-backup"}, updatedBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeFull)))
			Expect(updatedBackup.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "14"))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, ""))
			Expect(updatedBackup.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, ""))
		})

		It("should update parent backup annotations when new dependent backup is created", func() {
			// Create test objects
			backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
			cluster := createTestCluster("test-cluster", "default")

			// Create parent backup with no dependents initially
			parentBackup := createTestBackup("parent-backup", "default", "test-cluster", "base_000000010000000100000001", true)
			parentBackup.Labels = map[string]string{
				v1beta1.BackupTypeLabelName:      string(v1beta1.BackupTypeFull),
				v1beta1.BackupPgVersionLabelName: "14",
			}
			parentBackup.Annotations = map[string]string{
				v1beta1.BackupDirectDependentsAnnotationName: "",
				v1beta1.BackupAllDependentsAnnotationName:    "",
			}

			// Create a new dependent backup
			newDependentBackup := createTestBackup("new-dependent", "default", "test-cluster", "base_000000010000000100000002_D_000000010000000100000001", true)

			// Create a mock BackupConfigWithSecrets
			backupConfigWithSecrets := &v1beta1.BackupConfigWithSecrets{
				BackupConfig: *backupConfig,
				Spec: v1beta1.BackupConfigSpecWithSecrets{
					BackupConfigSpec: backupConfig.Spec,
					Storage: v1beta1.StorageConfigWithSecrets{
						StorageConfig: backupConfig.Spec.Storage,
						S3: &v1beta1.S3StorageConfigWithSecrets{
							S3StorageConfig: *backupConfig.Spec.Storage.S3,
							AccessKeyID:     "test-access-key-id",
							AccessKeySecret: "test-access-key-secret",
						},
					},
				},
			}

			// Create fake client with objects
			fakeClient := setupFakeBackupClient(backupConfig, s3Secret, parentBackup, newDependentBackup, cluster)

			// Create reconciler with fake client
			reconciler = &BackupReconciler{
				Client: fakeClient,
				Scheme: runtime.NewScheme(),
			}

			// Call reconcileBackupMetadata on the parent backup
			updated, err := reconciler.reconcileBackupMetadata(testCtx, parentBackup, backupConfigWithSecrets, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue(), "Parent backup should be updated with new dependent")

			// Verify parent backup was updated with the new dependent in its annotations
			updatedParent := &cnpgv1.Backup{}
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "parent-backup"}, updatedParent)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedParent.Labels).To(HaveKeyWithValue(v1beta1.BackupTypeLabelName, string(v1beta1.BackupTypeFull)))
			Expect(updatedParent.Labels).To(HaveKeyWithValue(v1beta1.BackupPgVersionLabelName, "14"))
			Expect(updatedParent.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, "new-dependent"))
			Expect(updatedParent.Annotations).To(HaveKeyWithValue(v1beta1.BackupAllDependentsAnnotationName, "new-dependent"))

			// Also test the scenario where we add a second level dependent (indirect dependent)
			secondLevelBackup := createTestBackup("second-level", "default", "test-cluster", "base_000000010000000100000003_D_000000010000000100000002", true)

			// Update the fake client with the new backup
			fakeClient = setupFakeBackupClient(backupConfig, s3Secret, parentBackup, newDependentBackup, secondLevelBackup)
			reconciler.Client = fakeClient

			// Call reconcileBackupMetadata again on the parent backup
			updated, err = reconciler.reconcileBackupMetadata(testCtx, parentBackup, backupConfigWithSecrets, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(BeTrue(), "Parent backup should be updated with indirect dependent")

			// Verify parent backup was updated with both direct and indirect dependents
			err = fakeClient.Get(testCtx, client.ObjectKey{Namespace: "default", Name: "parent-backup"}, updatedParent)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedParent.Annotations).To(HaveKeyWithValue(v1beta1.BackupDirectDependentsAnnotationName, "new-dependent"))

			// The all dependents annotation should include both direct and indirect dependents
			allDependents := updatedParent.Annotations[v1beta1.BackupAllDependentsAnnotationName]
			Expect(allDependents).To(ContainSubstring("new-dependent"))
			Expect(allDependents).To(ContainSubstring("second-level"))
		})
	})
})
