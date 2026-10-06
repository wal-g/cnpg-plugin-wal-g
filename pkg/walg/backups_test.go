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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var _ = Describe("BackupMetadata", func() {
	var (
		backupList []BackupMetadata
	)

	BeforeEach(func() {
		// Create a test backup list
		backupList = []BackupMetadata{
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
				fullBackup := BackupMetadata{BackupName: "base_000000010000000100000040"}
				deps := fullBackup.GetDependentBackups(ctx, backupList, false)
				Expect(deps).To(HaveLen(1))
			})

			It("should find direct dependencies for a delta backup", func() {
				deltaBackup := BackupMetadata{BackupName: "base_000000010000000100000046_D_000000010000000100000040"}
				deps := deltaBackup.GetDependentBackups(ctx, backupList, false)
				Expect(deps).To(HaveLen(1))
				Expect(deps[0].BackupName).To(Equal("base_000000010000000100000061_D_000000010000000100000046"))
			})

			It("should return empty list for a backup with no dependencies", func() {
				noDepBackup := BackupMetadata{BackupName: "base_000000010000000100000085_D_000000010000000100000080"}
				deps := noDepBackup.GetDependentBackups(ctx, backupList, false)
				Expect(deps).To(BeEmpty())
			})
		})

		Context("with indirect dependencies included", func() {
			It("should find all dependencies for a full backup", func() {
				fullBackup := BackupMetadata{BackupName: "base_000000010000000100000040"}
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
				fullBackup := BackupMetadata{BackupName: "base_000000010000000100000070"}
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
				noDepBackup := BackupMetadata{BackupName: "base_000000010000000100000085_D_000000010000000100000080"}
				deps := noDepBackup.GetDependentBackups(ctx, backupList, true)
				Expect(deps).To(BeEmpty())
			})
		})
	})
})

var _ = Describe("DeleteBackup", func() {
	const backupName = "base_000000010000000000000005_D_000000010000000000000003"
	const missingBackup = "Backup '" + backupName + "' does not exist."
	const missingMetadata = "object 'prefix/" + backupName + "/metadata.json' not found in storage"

	var (
		testCtx context.Context
		dir     string
		logs    bytes.Buffer
	)

	BeforeEach(func() {
		logs.Reset()
		var cancel context.CancelFunc
		testCtx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		DeferCleanup(cancel)
		testCtx = logr.NewContext(testCtx, zap.New(zap.WriteTo(&logs)))

		reaperCtx, stopReaper := context.WithCancel(context.Background())
		ready, done := make(chan struct{}), make(chan struct{})
		reaperCtx = logr.NewContext(reaperCtx, funcr.New(func(_, message string) {
			if strings.Contains(message, "Starting zombie process reaper") {
				close(ready)
			}
		}, funcr.Options{}))
		go func() {
			defer close(done)
			_ = (&cmd.ZombieProcessReaper{}).Start(reaperCtx)
		}()
		DeferCleanup(func() {
			stopReaper()
			Eventually(done, 5*time.Second).Should(BeClosed())
		})
		Eventually(ready, 5*time.Second).Should(BeClosed())

		dir = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "wal-g"), []byte(`#!/bin/sh
printf '%s\000' "$@" >> "$WALG_TEST_DIR/calls"
printf '\n' >> "$WALG_TEST_DIR/calls"
if [ "$1" = --config ]; then shift 2; fi
case "$1 $2" in
  'backup-mark -i') printf '%s' "$WALG_TEST_UNMARK_ERROR" >&2; test -z "$WALG_TEST_UNMARK_ERROR" ;;
  'delete target') printf '%s' "$WALG_TEST_DELETE_ERROR" >&2; test -z "$WALG_TEST_DELETE_ERROR" ;;
  'delete garbage') printf '%s' "$WALG_TEST_GC_ERROR" >&2; test -z "$WALG_TEST_GC_ERROR" ;;
  *) exit 1 ;;
esac
`), 0700)).To(Succeed())
		GinkgoT().Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		GinkgoT().Setenv("WALG_TEST_DIR", dir)
	})

	type testCase struct {
		unmarkError string
		deleteError string
		gcError     string
	}

	DescribeTable("deleting a backup",
		func(tc testCase) {
			GinkgoT().Setenv("WALG_TEST_UNMARK_ERROR", tc.unmarkError)
			GinkgoT().Setenv("WALG_TEST_DELETE_ERROR", tc.deleteError)
			GinkgoT().Setenv("WALG_TEST_GC_ERROR", tc.gcError)

			result, err := NewClient(&Config{}).DeleteBackup(testCtx, backupName)

			Expect(result).NotTo(BeNil())
			Expect(string(result.Stderr())).To(Equal(tc.deleteError))
			if tc.deleteError != "" && tc.deleteError != missingBackup {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
			calls, readErr := os.ReadFile(filepath.Join(dir, "calls"))
			Expect(readErr).NotTo(HaveOccurred())
			config := "--config\x00" + emptyConfigFile() + "\x00"
			Expect(string(calls)).To(Equal(
				config + "backup-mark\x00-i\x00" + backupName + "\x00\n" +
					config + "delete\x00target\x00" + backupName + "\x00--confirm\x00\n" +
					config + "delete\x00garbage\x00--confirm\x00\n",
			))
			if (tc.unmarkError != "" && tc.unmarkError != missingMetadata) || tc.gcError != "" {
				Expect(logs.String()).To(ContainSubstring(`"level":"error"`))
			} else {
				Expect(logs.String()).NotTo(ContainSubstring(`"level":"error"`))
			}
			if tc.unmarkError == missingMetadata {
				Expect(logs.String()).To(ContainSubstring("Backup metadata is missing, attempting deletion"))
			}
		},
		Entry("deletes a backup after unmarking", testCase{}),
		Entry("accepts a backup already removed with its parent", testCase{unmarkError: missingMetadata, deleteError: missingBackup}),
		Entry("attempts deletion when only metadata is missing", testCase{unmarkError: missingMetadata}),
		Entry("continues after a metadata error for another backup", testCase{unmarkError: "object 'prefix/other/metadata.json' not found in storage"}),
		Entry("preserves deletion errors despite unmark and garbage errors", testCase{unmarkError: "AccessDenied", deleteError: "Unable to delete permanent backup", gcError: "garbage failed"}),
		Entry("does not fail deletion on garbage errors", testCase{gcError: "garbage failed"}),
	)
})
