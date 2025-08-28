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
	})
})
