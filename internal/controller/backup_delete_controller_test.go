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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("BackupDeletionController", func() {
	It("should remove the finalizer of a read-only BackupConfig without cleaning up its storage", func() {
		backupConfig := createBackupConfig("source", "default", 0, "", false)
		backupConfig.Spec.ReadOnly = true
		backupConfig.Finalizers = []string{v1beta1.BackupConfigFinalizerName}
		fakeClient := setupFakeClient(backupConfig)

		// No credentials are set up, so reaching WAL-G would fail before removing the finalizer
		Expect(NewBackupDeletionController(fakeClient).deleteBackupConfig(ctx, client.ObjectKeyFromObject(backupConfig))).To(Succeed())

		updated := &v1beta1.BackupConfig{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(backupConfig), updated)).To(Succeed())
		Expect(updated.Finalizers).NotTo(ContainElement(v1beta1.BackupConfigFinalizerName))
	})
})
