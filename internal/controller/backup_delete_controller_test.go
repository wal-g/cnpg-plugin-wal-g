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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
)

// setupDeadlineAwareFakeClient returns a fake client whose Update fails once the request
// context is done, like a real API client, which the plain fake client does not do.
func setupDeadlineAwareFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(v1beta1.AddToScheme(scheme)).To(Succeed())
	Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				return c.Update(ctx, obj, opts...)
			},
		}).
		Build()
}

func deletingBackupWithFinalizer(name, backupID string) *cnpgv1.Backup {
	backup := createTestBackup(name, "default", "test-cluster", backupID, true)
	now := metav1.Now()
	backup.Finalizers = append(backup.Finalizers, v1beta1.BackupFinalizerName)
	backup.DeletionTimestamp = &now
	return backup
}

var _ = Describe("BackupDeletionController", func() {
	var testCtx context.Context

	BeforeEach(func() {
		testCtx = context.Background()
	})

	It("removes the cleanup finalizer although the deletion context has already expired", func() {
		backup := deletingBackupWithFinalizer("expired-ctx-backup", "base_000000010000000100000001")
		fakeClient := setupDeadlineAwareFakeClient(backup)

		// The deletion context ran out while WAL-G was still deleting the physical backup.
		expiredCtx, cancel := context.WithDeadline(testCtx, time.Now().Add(-time.Second))
		defer cancel()

		// Using the expired context directly fails, which left Backups stuck in Terminating.
		Expect(removeBackupFinalizer(expiredCtx, backup.DeepCopy(), fakeClient)).To(MatchError(context.DeadlineExceeded))

		Expect(removeBackupFinalizerDetached(expiredCtx, backup, fakeClient)).To(Succeed())

		// With its last finalizer removed, the Backup that is being deleted is gone.
		err := fakeClient.Get(testCtx, client.ObjectKeyFromObject(backup), &cnpgv1.Backup{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("treats an already deleted Backup as done", func() {
		controller := NewBackupDeletionController(setupFakeBackupClient())
		err := controller.deleteWALGBackup(testCtx, client.ObjectKey{Namespace: "default", Name: "missing-backup"})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("BackupReconciler deletion retry", func() {
	It("requeues a Backup that is being deleted so a failed or postponed deletion is retried", func() {
		testCtx := context.Background()
		cluster := createTestCluster("test-cluster", "default")
		backupConfig, s3Secret := createTestBackupConfig("test-backupconfig", "default")
		// No backup ID: the physical backup does not exist, so deletion only removes the finalizer.
		backup := deletingBackupWithFinalizer("deleting-backup", "")

		fakeClient := setupFakeBackupClient(backupConfig, s3Secret, backup, cluster)
		reconciler := &BackupReconciler{
			Client:             fakeClient,
			Scheme:             fakeClient.Scheme(),
			DeletionController: NewBackupDeletionController(fakeClient),
		}

		result, err := reconciler.Reconcile(testCtx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(backup)})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(DeletionRetryInterval))

		// The queued deletion removes the finalizer, after which the Backup is gone and a
		// later reconcile does not requeue it.
		Eventually(func() bool {
			err := fakeClient.Get(testCtx, client.ObjectKeyFromObject(backup), &cnpgv1.Backup{})
			return apierrors.IsNotFound(err)
		}, 5*time.Second, 50*time.Millisecond).Should(BeTrue())

		result, err = reconciler.Reconcile(testCtx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(backup)})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))
	})
})
