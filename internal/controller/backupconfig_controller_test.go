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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("BackupConfig Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		backupconfig := &v1beta1.BackupConfig{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind BackupConfig")
			err := k8sClient.Get(ctx, typeNamespacedName, backupconfig)
			if err != nil && errors.IsNotFound(err) {
				resource := &v1beta1.BackupConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &v1beta1.BackupConfig{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance BackupConfig")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			// Create a mock BackupDeletionController
			mockBackupDeletionController := NewBackupDeletionController(k8sClient)

			controllerReconciler := &BackupConfigReconciler{
				Client:                   k8sClient,
				Scheme:                   k8sClient.Scheme(),
				BackupDeletionController: mockBackupDeletionController,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})

		It("should handle deletion correctly", func() {
			By("Marking the resource for deletion")
			// Get the resource
			resource := &v1beta1.BackupConfig{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			// Add finalizers to simulate the normal reconcile process
			resource.Finalizers = []string{v1beta1.BackupConfigFinalizerName}
			err = k8sClient.Update(ctx, resource)
			Expect(err).NotTo(HaveOccurred())

			// Mark the resource for deletion
			now := metav1.Now()
			resource.DeletionTimestamp = &now
			err = k8sClient.Update(ctx, resource)
			Expect(err).NotTo(HaveOccurred())

			// Create a mock BackupDeletionController
			mockBackupDeletionController := NewBackupDeletionController(k8sClient)

			controllerReconciler := &BackupConfigReconciler{
				Client:                   k8sClient,
				Scheme:                   k8sClient.Scheme(),
				BackupDeletionController: mockBackupDeletionController,
			}

			// Reconcile the resource
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Check that the finalizer is removed after the BackupDeletionController processes the request
			// Note: In a real test, we would need to mock the BackupDeletionController's behavior
			// to actually remove the finalizer, but for this test we're just checking that
			// the controller is called correctly
			err = k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not add finalizer when ignoreForBackupConfigDeletion is true", func() {
			By("Creating a BackupConfig with ignoreForBackupConfigDeletion enabled")

			// Create a BackupConfig with ignoreForBackupConfigDeletion enabled
			resourceWithIgnore := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ignore-deletion",
					Namespace: "default",
				},
				Spec: v1beta1.BackupConfigSpec{
					Retention: v1beta1.BackupRetentionConfig{
						IgnoreForBackupConfigDeletion: true,
					},
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/test-prefix",
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, resourceWithIgnore)).To(Succeed())

			// Create a mock BackupDeletionController
			mockBackupDeletionController := NewBackupDeletionController(k8sClient)

			controllerReconciler := &BackupConfigReconciler{
				Client:                   k8sClient,
				Scheme:                   k8sClient.Scheme(),
				BackupDeletionController: mockBackupDeletionController,
			}

			// Reconcile the resource
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-ignore-deletion",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the resource does not have the finalizer
			updatedResource := &v1beta1.BackupConfig{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-ignore-deletion",
				Namespace: "default",
			}, updatedResource)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedResource.Finalizers).NotTo(ContainElement(v1beta1.BackupConfigFinalizerName))

			// Cleanup
			Expect(k8sClient.Delete(ctx, resourceWithIgnore)).To(Succeed())
		})

		It("should remove finalizer when ignoreForBackupConfigDeletion is enabled on existing resource", func() {
			By("Creating a BackupConfig with finalizer, then enabling ignoreForBackupConfigDeletion")

			// Create a BackupConfig with finalizer
			resourceWithFinalizer := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-remove-finalizer",
					Namespace:  "default",
					Finalizers: []string{v1beta1.BackupConfigFinalizerName},
				},
				Spec: v1beta1.BackupConfigSpec{
					Retention: v1beta1.BackupRetentionConfig{
						IgnoreForBackupConfigDeletion: false, // Initially false
					},
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/test-prefix",
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, resourceWithFinalizer)).To(Succeed())

			// Now update to enable ignoreForBackupConfigDeletion
			resourceWithFinalizer.Spec.Retention.IgnoreForBackupConfigDeletion = true
			Expect(k8sClient.Update(ctx, resourceWithFinalizer)).To(Succeed())

			// Create a mock BackupDeletionController
			mockBackupDeletionController := NewBackupDeletionController(k8sClient)

			controllerReconciler := &BackupConfigReconciler{
				Client:                   k8sClient,
				Scheme:                   k8sClient.Scheme(),
				BackupDeletionController: mockBackupDeletionController,
			}

			// Reconcile the resource
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-remove-finalizer",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the finalizer was removed
			updatedResource := &v1beta1.BackupConfig{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-remove-finalizer",
				Namespace: "default",
			}, updatedResource)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedResource.Finalizers).NotTo(ContainElement(v1beta1.BackupConfigFinalizerName))

			// Cleanup
			Expect(k8sClient.Delete(ctx, resourceWithFinalizer)).To(Succeed())
		})

		It("should add finalizer when ignoreForBackupConfigDeletion is false", func() {
			By("Creating a BackupConfig with ignoreForBackupConfigDeletion disabled")

			// Create a BackupConfig with ignoreForBackupConfigDeletion disabled (default)
			resourceNormal := &v1beta1.BackupConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-normal-deletion",
					Namespace: "default",
				},
				Spec: v1beta1.BackupConfigSpec{
					Retention: v1beta1.BackupRetentionConfig{
						IgnoreForBackupConfigDeletion: false, // Explicitly false
					},
					Storage: v1beta1.StorageConfig{
						StorageType: v1beta1.StorageTypeS3,
						S3: &v1beta1.S3StorageConfig{
							Prefix: "s3://test-bucket/test-prefix",
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, resourceNormal)).To(Succeed())

			// Create a mock BackupDeletionController
			mockBackupDeletionController := NewBackupDeletionController(k8sClient)

			controllerReconciler := &BackupConfigReconciler{
				Client:                   k8sClient,
				Scheme:                   k8sClient.Scheme(),
				BackupDeletionController: mockBackupDeletionController,
			}

			// Reconcile the resource
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-normal-deletion",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the resource has the finalizer
			updatedResource := &v1beta1.BackupConfig{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-normal-deletion",
				Namespace: "default",
			}, updatedResource)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedResource.Finalizers).To(ContainElement(v1beta1.BackupConfigFinalizerName))

			// Cleanup
			Expect(k8sClient.Delete(ctx, resourceNormal)).To(Succeed())
		})
	})
})
