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
	"errors"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
	"github.com/wal-g/cnpg-plugin-wal-g/pkg/walg"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

type statusWALCalls struct {
	read, write, wal, backups, size int
	readErr, writeErr, walErr       error
	backupErr, sizeErr              error
	onArchive                       func()
	onBackups, onSize               func()
}

type fakeStatusWALClient struct {
	pgVersion int
	calls     *statusWALCalls
}

func (f fakeStatusWALClient) StorageCheckReadable(context.Context) (*cmd.RunResult, error) {
	f.calls.read++
	return nil, f.calls.readErr
}

func (f fakeStatusWALClient) StorageCheckWritable(context.Context) (*cmd.RunResult, error) {
	f.calls.write++
	return nil, f.calls.writeErr
}

func (f fakeStatusWALClient) WALShow(context.Context) ([]walg.WALTimelineInfo, error) {
	f.calls.wal++
	if callback := f.calls.onArchive; callback != nil {
		f.calls.onArchive = nil
		callback()
	}
	if f.calls.walErr != nil {
		return nil, f.calls.walErr
	}
	if f.pgVersion != 17 {
		return nil, nil
	}
	return []walg.WALTimelineInfo{{Status: "OK"}}, nil
}

func (f fakeStatusWALClient) GetBackupsList(context.Context) ([]walg.BackupMetadata, error) {
	f.calls.backups++
	if callback := f.calls.onBackups; callback != nil {
		f.calls.onBackups = nil
		callback()
	}
	if f.pgVersion == 18 {
		return nil, f.calls.backupErr
	}
	if f.pgVersion != 17 {
		return nil, nil
	}
	return []walg.BackupMetadata{{CompressedSize: 10}}, nil
}

func (f fakeStatusWALClient) StorageLsTotalSize(context.Context, string) (int64, error) {
	f.calls.size++
	if callback := f.calls.onSize; callback != nil {
		f.calls.onSize = nil
		callback()
	}
	if f.pgVersion == 18 {
		return 0, f.calls.sizeErr
	}
	if f.pgVersion != 17 {
		return 0, nil
	}
	return 20, nil
}

type statusTest struct {
	ctx        context.Context
	controller *BackupConfigStatusController
	clock      *clocktesting.FakeClock
	calls      *statusWALCalls
	key        client.ObjectKey
}

func newStatusTest(ctx context.Context) *statusTest {
	GinkgoHelper()
	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())
	Expect(v1beta1.AddToScheme(scheme)).To(Succeed())
	backupConfig, secret := createTestBackupConfig("archive", "default")
	backupConfig.Spec.Encryption = v1beta1.BackupEncryptionConfig{Method: "none"}
	backupConfig.Status.ConsumedStorage = &v1beta1.ConsumedStorageInfo{TotalBytes: ptr.To(int64(1234))}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupConfig, secret).
		WithStatusSubresource(&v1beta1.BackupConfig{}).Build()
	c := NewBackupConfigStatusController(kube, 2*time.Minute, 5*time.Minute)
	clock := clocktesting.NewFakeClock(time.Unix(1000, 0))
	c.clock = clock
	calls := &statusWALCalls{}
	c.newWALClient = func(_ *v1beta1.BackupConfigWithSecrets, pgVersion int) statusWALClient {
		return fakeStatusWALClient{pgVersion: pgVersion, calls: calls}
	}
	return &statusTest{ctx: ctx, controller: c, clock: clock, calls: calls, key: client.ObjectKeyFromObject(backupConfig)}
}

func (s *statusTest) reconcile() error {
	return s.controller.reconcileStatusByKey(s.ctx, s.key, logr.Discard())
}

func (s *statusTest) config() *v1beta1.BackupConfig {
	GinkgoHelper()
	config := &v1beta1.BackupConfig{}
	Expect(s.controller.client.Get(s.ctx, s.key, config)).To(Succeed())
	return config
}

var _ = Describe("BackupConfig Status Controller", func() {
	var s *statusTest

	BeforeEach(func() {
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)
		s = newStatusTest(ctx)
	})

	It("should check availability while delaying archive scans from their completion", func() {
		s.calls.onArchive = func() {
			// Availability must already be saved before scanning the archive.
			Expect(getConditionStatus(s.config(), v1beta1.ConditionTypeStorageWritable)).To(Equal(metav1.ConditionTrue))
			Expect(getConditionStatus(s.config(), v1beta1.ConditionTypeWALIntegrityCheck)).To(Equal(metav1.ConditionUnknown))
			Expect(s.config().Status.Phase).To(Equal(v1beta1.BackupConfigPhaseUnknown))
			s.clock.Step(time.Minute)
		}
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(9))
		Expect(s.calls.backups).To(Equal(9))
		Expect(s.calls.size).To(Equal(9))
		Expect(*s.config().Status.ConsumedStorage.TotalBytes).To(Equal(int64(30)))

		s.clock.Step(2 * time.Minute)
		s.calls.writeErr = errors.New("write unavailable")
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.read).To(Equal(2))
		Expect(s.calls.write).To(Equal(2))
		Expect(s.calls.wal).To(Equal(9))
		Expect(s.calls.backups).To(Equal(9))
		Expect(s.calls.size).To(Equal(9))
		Expect(getConditionStatus(s.config(), v1beta1.ConditionTypeStorageWritable)).To(Equal(metav1.ConditionFalse))
		Expect(*s.config().Status.ConsumedStorage.TotalBytes).To(Equal(int64(30)))

		// The delay is measured from completion, not from the start of a slow scan.
		s.clock.Step(3*time.Minute - time.Nanosecond)
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(9))
		s.clock.Step(time.Nanosecond)
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(18))
		Expect(s.calls.backups).To(Equal(18))
		Expect(s.calls.size).To(Equal(18))
	})

	DescribeTable("should delay archive retries after storage failures", func(failure string) {
		errUnavailable := errors.New("archive unavailable")
		switch failure {
		case "wal-show":
			s.calls.walErr = errUnavailable
		case "backup-list":
			s.calls.backupErr = errUnavailable
		case "st ls":
			s.calls.sizeErr = errUnavailable
		}
		Expect(s.reconcile()).To(Succeed())
		Expect(getConditionStatus(s.config(), v1beta1.ConditionTypeStorageWritable)).To(Equal(metav1.ConditionTrue))
		walCalls, backupCalls, sizeCalls := s.calls.wal, s.calls.backups, s.calls.size
		s.clock.Step(2 * time.Minute)
		Expect(s.reconcile()).To(Succeed())
		Expect([]int{s.calls.wal, s.calls.backups, s.calls.size}).To(Equal([]int{walCalls, backupCalls, sizeCalls}))
		s.calls.walErr, s.calls.backupErr, s.calls.sizeErr = nil, nil, nil
		s.clock.Step(3 * time.Minute)
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(BeNumerically(">", walCalls))
		Expect(s.calls.backups).To(BeNumerically(">", backupCalls))
		Expect(s.calls.size).To(BeNumerically(">", sizeCalls))
	},
		Entry("wal-show fails", "wal-show"),
		Entry("backup-list fails", "backup-list"),
		Entry("st ls fails", "st ls"),
	)

	It("should preserve availability and delay archive retries after cancellation", func() {
		ctx, cancel := context.WithCancel(s.ctx)
		defer cancel()
		s.calls.onArchive = cancel
		err := s.controller.reconcileStatusByKey(ctx, s.key, logr.Discard())
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(s.calls.wal).To(Equal(9))
		Expect(s.calls.backups).To(BeZero())
		Expect(getConditionStatus(s.config(), v1beta1.ConditionTypeStorageWritable)).To(Equal(metav1.ConditionTrue))
		Expect(*s.config().Status.ConsumedStorage.TotalBytes).To(Equal(int64(1234)))
		Expect(s.config().Status.Phase).To(Equal(v1beta1.BackupConfigPhaseUnknown))
		s.clock.Step(2 * time.Minute)
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(9))
		Expect(s.config().Status.Phase).To(Equal(v1beta1.BackupConfigPhaseUnknown))
	})

	DescribeTable("should return deadline errors without saving an incomplete archive status", func(stage string) {
		ctx, cancel := context.WithTimeout(s.ctx, time.Second)
		defer cancel()
		waitForDeadline := func() { <-ctx.Done() }
		switch stage {
		case "backup-list":
			s.calls.onBackups = waitForDeadline
		case "st ls":
			s.calls.onSize = waitForDeadline
		case "status update":
			s.controller.client = interceptor.NewClient(s.controller.client.(client.WithWatch), interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, kube client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					if s.calls.size > 0 {
						waitForDeadline()
						return ctx.Err()
					}
					return kube.SubResource(subresource).Update(ctx, obj, opts...)
				},
			})
		}
		Expect(errors.Is(s.controller.reconcileStatusByKey(ctx, s.key, logr.Discard()), context.DeadlineExceeded)).To(BeTrue())
		Expect(*s.config().Status.ConsumedStorage.TotalBytes).To(Equal(int64(1234)))
		Expect(s.config().Status.Phase).To(Equal(v1beta1.BackupConfigPhaseUnknown))
	},
		Entry("during backup-list", "backup-list"),
		Entry("during st ls", "st ls"),
		Entry("during status update", "status update"),
	)

	It("should retry conflicts without overwriting current availability or other conditions", func() {
		s.calls.onArchive = func() {
			config := s.config()
			setCondition(config, v1beta1.ConditionTypeStorageWritable, metav1.ConditionFalse, "ConcurrentCheck", "write unavailable")
			setCondition(config, "OtherController", metav1.ConditionTrue, "Checked", "preserve this condition")
			Expect(s.controller.client.Status().Update(s.ctx, config)).To(Succeed())
		}
		writes := 0
		s.controller.client = interceptor.NewClient(s.controller.client.(client.WithWatch), interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, kube client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				writes++
				if writes == 3 {
					// Change metadata between fetching the resource and saving the archive result.
					config := &v1beta1.BackupConfig{}
					Expect(kube.Get(ctx, s.key, config)).To(Succeed())
					config.Annotations = map[string]string{"concurrent": "keep"}
					Expect(kube.Update(ctx, config)).To(Succeed())
				}
				return kube.SubResource(subresource).Update(ctx, obj, opts...)
			},
		})
		Expect(s.reconcile()).To(Succeed())
		Expect(writes).To(Equal(4))
		config := s.config()
		Expect(config.Annotations).To(HaveKeyWithValue("concurrent", "keep"))
		Expect(getConditionStatus(config, v1beta1.ConditionTypeStorageWritable)).To(Equal(metav1.ConditionFalse))
		Expect(getConditionStatus(config, "OtherController")).To(Equal(metav1.ConditionTrue))
		Expect(config.Status.Phase).To(Equal(v1beta1.BackupConfigPhaseDegraded))
		Expect(*config.Status.ConsumedStorage.TotalBytes).To(Equal(int64(30)))
		Expect(s.calls.wal).To(Equal(9))
		s.clock.Step(2 * time.Minute)
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(9))
	})

	DescribeTable("should retry saving archive results without repeating scans before the interval", func(failure error) {
		blocked := false
		s.calls.onArchive = func() { blocked = true }
		s.controller.client = interceptor.NewClient(s.controller.client.(client.WithWatch), interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, kube client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if blocked && getConditionStatus(obj.(*v1beta1.BackupConfig), v1beta1.ConditionTypeWALIntegrityCheck) == metav1.ConditionTrue {
					return failure
				}
				return kube.SubResource(subresource).Update(ctx, obj, opts...)
			},
		})
		Expect(errors.Is(s.reconcile(), failure)).To(BeTrue())
		Expect(*s.config().Status.ConsumedStorage.TotalBytes).To(Equal(int64(1234)))
		Expect(s.calls.wal).To(Equal(9))
		s.clock.Step(2 * time.Minute)
		Expect(errors.Is(s.reconcile(), failure)).To(BeTrue())
		Expect([]int{s.calls.wal, s.calls.backups, s.calls.size}).To(Equal([]int{9, 9, 9}))
		blocked = false
		s.clock.Step(time.Minute)
		Expect(s.reconcile()).To(Succeed())
		Expect([]int{s.calls.wal, s.calls.backups, s.calls.size}).To(Equal([]int{9, 9, 9}))
		Expect(*s.config().Status.ConsumedStorage.TotalBytes).To(Equal(int64(30)))
		s.clock.Step(2 * time.Minute)
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(18))
	},
		Entry("on conflicts", apierrors.NewConflict(schema.GroupResource{Resource: "backupconfigs"}, "archive", errors.New("concurrent update"))),
		Entry("on API errors", errors.New("status update unavailable")),
	)

	It("should update backup timestamps between archive scans", func() {
		s.controller.archiveInterval = time.Hour
		Expect(s.reconcile()).To(Succeed())
		completed := &cnpgv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name: "completed", Namespace: s.key.Namespace,
				OwnerReferences: []metav1.OwnerReference{{APIVersion: v1beta1.GroupVersion.String(), Kind: "BackupConfig", Name: s.key.Name, UID: s.config().UID}},
			},
			Status: cnpgv1.BackupStatus{Phase: cnpgv1.BackupPhaseCompleted, StartedAt: &metav1.Time{Time: s.clock.Now()}},
		}
		failed := completed.DeepCopy()
		failed.Name, failed.Status.Phase = "failed", cnpgv1.BackupPhaseFailed
		failed.Status.StartedAt = &metav1.Time{Time: s.clock.Now().Add(time.Minute)}
		Expect(s.controller.client.Create(s.ctx, completed)).To(Succeed())
		Expect(s.controller.client.Create(s.ctx, failed)).To(Succeed())
		s.clock.Step(2 * time.Minute)
		Expect(s.reconcile()).To(Succeed())
		config := s.config()
		Expect(config.Status.LastSuccessfulBackup).To(Equal(completed.Status.StartedAt))
		Expect(config.Status.LastFailedBackup).To(Equal(failed.Status.StartedAt))
		Expect(config.Status.FirstRecoverabilityPoint).To(Equal(completed.Status.StartedAt))
		Expect([]int{s.calls.wal, s.calls.backups, s.calls.size}).To(Equal([]int{9, 9, 9}))
	})

	DescribeTable("should discard archive results if the resource changes during the scan", func(change string) {
		s.calls.onArchive = func() {
			config := s.config()
			if change == "spec" {
				config.Spec.Storage.S3.Prefix += "/changed"
				config.Generation++
				Expect(s.controller.client.Update(s.ctx, config)).To(Succeed())
			} else {
				Expect(s.controller.client.Delete(s.ctx, config)).To(Succeed())
				config.UID, config.ResourceVersion = "replacement-uid", ""
				Expect(s.controller.client.Create(s.ctx, config)).To(Succeed())
			}
		}
		Expect(s.reconcile()).To(HaveOccurred())
		Expect(*s.config().Status.ConsumedStorage.TotalBytes).To(Equal(int64(1234)))
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(18))
		Expect(*s.config().Status.ConsumedStorage.TotalBytes).To(Equal(int64(30)))
	},
		Entry("spec changes", "spec"),
		Entry("resource is recreated", "recreated"),
	)

	DescribeTable("should reset WAL status and check the archive again when its configuration cannot be reused", func(change string) {
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "archive-prefix", Namespace: s.key.Namespace},
			Data:       map[string]string{"prefix": "s3://archive/original"},
		}
		if change == "configmap" {
			Expect(s.controller.client.Create(s.ctx, configMap)).To(Succeed())
			config := s.config()
			config.Spec.Storage.S3.Prefix = ""
			config.Spec.Storage.S3.PrefixFrom = &v1beta1.ValueFromSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: configMap.Name}, Key: "prefix"},
			}
			Expect(s.controller.client.Update(s.ctx, config)).To(Succeed())
		}
		Expect(s.reconcile()).To(Succeed())
		config := s.config()
		Expect(config.Status.Phase).To(Equal(v1beta1.BackupConfigPhaseHealthy))
		switch change {
		case "generation":
			config.Spec.Storage.S3.Prefix += "/changed"
			config.Generation++
			Expect(s.controller.client.Update(s.ctx, config)).To(Succeed())
		case "credentials":
			secret := &corev1.Secret{}
			key := client.ObjectKey{Namespace: s.key.Namespace, Name: config.Spec.Storage.S3.AccessKeySecretRef.Name}
			Expect(s.controller.client.Get(s.ctx, key, secret)).To(Succeed())
			secret.Data["accessKeySecret"] = []byte("rotated")
			Expect(s.controller.client.Update(s.ctx, secret)).To(Succeed())
		case "configmap":
			configMap.Data["prefix"] = "s3://archive/changed"
			Expect(s.controller.client.Update(s.ctx, configMap)).To(Succeed())
		case "recreated":
			Expect(s.controller.client.Delete(s.ctx, config)).To(Succeed())
			config.UID = "replacement-uid"
			config.ResourceVersion = ""
			Expect(s.controller.client.Create(s.ctx, config)).To(Succeed())
		case "restart":
			restarted := NewBackupConfigStatusController(s.controller.client, 2*time.Minute, 5*time.Minute)
			restarted.newWALClient = s.controller.newWALClient
			restarted.clock = s.clock
			s.controller = restarted
		}
		ctx, cancel := context.WithCancel(s.ctx)
		defer cancel()
		s.calls.onArchive = func() {
			Expect(getConditionStatus(s.config(), v1beta1.ConditionTypeWALIntegrityCheck)).To(Equal(metav1.ConditionUnknown))
			Expect(s.config().Status.Phase).To(Equal(v1beta1.BackupConfigPhaseUnknown))
			cancel()
		}
		Expect(errors.Is(s.controller.reconcileStatusByKey(ctx, s.key, logr.Discard()), context.Canceled)).To(BeTrue())
		Expect(s.calls.wal).To(Equal(18))
		s.clock.Step(2 * time.Minute)
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(18))
		Expect(s.config().Status.Phase).To(Equal(v1beta1.BackupConfigPhaseUnknown))
		s.clock.Step(3 * time.Minute)
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(27))
		Expect(s.config().Status.Phase).To(Equal(v1beta1.BackupConfigPhaseHealthy))
	},
		Entry("prefix and generation change", "generation"),
		Entry("secret values change", "credentials"),
		Entry("configmap values change", "configmap"),
		Entry("resource is recreated", "recreated"),
		Entry("controller restarts", "restart"),
	)

	It("should track archive schedules per resource and remove deleted resources", func() {
		Expect(s.reconcile()).To(Succeed())
		second := s.config()
		second.Name, second.UID, second.ResourceVersion = "second", "second-uid", ""
		Expect(s.controller.client.Create(s.ctx, second)).To(Succeed())
		secondKey := client.ObjectKeyFromObject(second)
		Expect(s.controller.reconcileStatusByKey(s.ctx, secondKey, logr.Discard())).To(Succeed())
		Expect(s.calls.wal).To(Equal(18))
		Expect(s.reconcile()).To(Succeed())
		Expect(s.calls.wal).To(Equal(18))
		Expect(s.controller.client.Delete(s.ctx, second)).To(Succeed())
		s.controller.enqueueAllStatuses(s.ctx)
		Expect(s.controller.archiveChecks).To(HaveLen(1))
		Expect(s.controller.archiveChecks).To(HaveKey(s.key))
	})
})
