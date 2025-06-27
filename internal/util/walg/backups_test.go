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

package walg

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WalgBackupMetadata", func() {
	var (
		backupList []WalgBackupMetadata
	)

	BeforeEach(func() {
		// Create a test backup list
		backupList = []WalgBackupMetadata{
			{BackupName: "base_000000010000000100000040"},                            // Full backup 1
			{BackupName: "base_000000010000000100000046_D_000000010000000100000040"}, // Delta 1 (depends on Full 1)
			{BackupName: "base_000000010000000100000061_D_000000010000000100000046"}, // Delta 2 (depends on Delta 1)
			{BackupName: "base_000000010000000100000070"},                            // Full backup 2
			{BackupName: "base_000000010000000100000075_D_000000010000000100000070"}, // Delta 3 (depends on Full 2)
			{BackupName: "base_000000010000000100000080_D_000000010000000100000075"}, // Delta 4 (depends on Delta 3)
			{BackupName: "base_000000010000000100000085_D_000000010000000100000080"}, // Delta 5 (depends on Delta 4)
		}
	})

	Describe("GetDependentBackups", func() {
		Context("with direct dependencies only", func() {
			It("should find direct dependencies for a full backup", func() {
				fullBackup := WalgBackupMetadata{BackupName: "base_000000010000000100000040"}
				deps := fullBackup.GetDependentBackups(ctx, backupList, false)
				Expect(deps).To(HaveLen(1))
			})

			It("should find direct dependencies for a delta backup", func() {
				deltaBackup := WalgBackupMetadata{BackupName: "base_000000010000000100000046_D_000000010000000100000040"}
				deps := deltaBackup.GetDependentBackups(ctx, backupList, false)
				Expect(deps).To(HaveLen(1))
				Expect(deps[0].BackupName).To(Equal("base_000000010000000100000061_D_000000010000000100000046"))
			})

			It("should return empty list for a backup with no dependencies", func() {
				noDepBackup := WalgBackupMetadata{BackupName: "base_000000010000000100000085_D_000000010000000100000080"}
				deps := noDepBackup.GetDependentBackups(ctx, backupList, false)
				Expect(deps).To(BeEmpty())
			})
		})

		Context("with indirect dependencies included", func() {
			It("should find all dependencies for a full backup", func() {
				fullBackup := WalgBackupMetadata{BackupName: "base_000000010000000100000040"}
				deps := fullBackup.GetDependentBackups(ctx, backupList, true)
				Expect(deps).To(HaveLen(2))

				// Check that both direct and indirect dependencies are included
				backupNames := []string{}
				for _, dep := range deps {
					backupNames = append(backupNames, dep.BackupName)
				}

				Expect(backupNames).To(ContainElement("base_000000010000000100000046_D_000000010000000100000040"))
				Expect(backupNames).To(ContainElement("base_000000010000000100000061_D_000000010000000100000046"))
			})

			It("should find all dependencies for a full backup with multiple levels", func() {
				fullBackup := WalgBackupMetadata{BackupName: "base_000000010000000100000070"}
				deps := fullBackup.GetDependentBackups(ctx, backupList, true)
				Expect(deps).To(HaveLen(3))

				// Check that all levels of dependencies are included
				backupNames := []string{}
				for _, dep := range deps {
					backupNames = append(backupNames, dep.BackupName)
				}

				Expect(backupNames).To(ContainElement("base_000000010000000100000075_D_000000010000000100000070"))
				Expect(backupNames).To(ContainElement("base_000000010000000100000080_D_000000010000000100000075"))
				Expect(backupNames).To(ContainElement("base_000000010000000100000085_D_000000010000000100000080"))
			})

			It("should handle a backup with no dependencies", func() {
				noDepBackup := WalgBackupMetadata{BackupName: "base_000000010000000100000085_D_000000010000000100000080"}
				deps := noDepBackup.GetDependentBackups(ctx, backupList, true)
				Expect(deps).To(BeEmpty())
			})
		})
	})
})
