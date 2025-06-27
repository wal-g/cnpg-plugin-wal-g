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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
)

var _ = Describe("BackupConfig Controller", func() {
	Context("When reconciling a BackupConfig resource", func() {
		const (
			BackupConfigName      = "test-backupconfig"
			BackupConfigNamespace = "default"
			timeout               = time.Second * 10
			interval              = time.Millisecond * 250
		)

		ctx := context.Background()

		// Create a BackupConfig object with S3 storage configuration
		backupConfig := &v1beta1.BackupConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      BackupConfigName,
				Namespace: BackupConfigNamespace,
			},
			Spec: v1beta1.BackupConfigSpec{
				DownloadConcurrency:    10,
				DownloadFileRetries:    15,
				UploadDiskRateLimit:    1024 * 1024,     // 1MB/s
				UploadNetworkRateLimit: 2 * 1024 * 1024, // 2MB/s
				UploadConcurrency:      16,
				UploadDiskConcurrency:  1,
				DeltaMaxSteps:          5,
				Storage: v1beta1.StorageConfig{
					StorageType: v1beta1.StorageTypeS3,
					S3: &v1beta1.S3StorageConfig{
						Prefix:         "s3://test-bucket/backups",
						Region:         "us-east-1",
						EndpointUrl:    "https://storage.example.com",
						ForcePathStyle: true,
						StorageClass:   "STANDARD",
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

		// Create the S3 credentials secret
		s3CredentialsSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "s3-credentials",
				Namespace: BackupConfigNamespace,
			},
			Data: map[string][]byte{
				"access-key-id":     []byte("test-access-key-id"),
				"secret-access-key": []byte("test-secret-access-key"),
			},
		}

		BeforeEach(func() {
			// Create the S3 credentials secret
			Expect(k8sClient.Create(ctx, s3CredentialsSecret)).Should(Succeed())

			// Create the BackupConfig
			Expect(k8sClient.Create(ctx, backupConfig)).Should(Succeed())

			// Verify that the BackupConfig was created
			createdBackupConfig := &v1beta1.BackupConfig{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      BackupConfigName,
					Namespace: BackupConfigNamespace,
				}, createdBackupConfig)
			}, timeout, interval).Should(Succeed())
		})

		AfterEach(func() {
			// Clean up the BackupConfig
			Expect(k8sClient.Delete(ctx, backupConfig)).Should(Succeed())

			// Clean up the S3 credentials secret
			Expect(k8sClient.Delete(ctx, s3CredentialsSecret)).Should(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			// Create a BackupConfigReconciler
			reconciler := &BackupConfigReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Reconcile the BackupConfig
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      BackupConfigName,
					Namespace: BackupConfigNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify that the BackupConfig still exists
			existingBackupConfig := &v1beta1.BackupConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      BackupConfigName,
				Namespace: BackupConfigNamespace,
			}, existingBackupConfig)).Should(Succeed())

			// Verify the BackupConfig spec
			Expect(existingBackupConfig.Spec.DownloadConcurrency).To(Equal(int(10)))
			Expect(existingBackupConfig.Spec.Storage.StorageType).To(Equal(v1beta1.StorageTypeS3))
			Expect(existingBackupConfig.Spec.Storage.S3.Prefix).To(Equal("s3://test-bucket/backups"))
		})
	})
})
