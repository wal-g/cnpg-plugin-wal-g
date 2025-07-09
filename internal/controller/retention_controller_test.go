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
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Helper function to create a fake client with the given objects
func setupFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(v1beta1.AddToScheme(scheme)).To(Succeed())
	Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

// Helper function to create a BackupConfig with the given retention settings
func createBackupConfig(name, namespace string, minBackupsToKeep int, deleteBackupsAfter string, ignoreManual bool) *v1beta1.BackupConfig {
	return &v1beta1.BackupConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BackupConfig",
			APIVersion: "cnpg-extensions.yandex.cloud/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1beta1.BackupConfigSpec{
			Retention: v1beta1.BackupRetentionConfig{
				MinBackupsToKeep:       minBackupsToKeep,
				DeleteBackupsAfter:     deleteBackupsAfter,
				IgnoreForManualBackups: ignoreManual,
			},
			Storage: v1beta1.StorageConfig{
				StorageType: v1beta1.StorageTypeS3,
				S3: &v1beta1.S3StorageConfig{
					Prefix: "s3://test-bucket/test-prefix",
				},
			},
		},
	}
}

// Helper function to create a Backup with the given parameters
func createBackup(name, namespace, backupConfigName string, startTime time.Time, isManual bool) cnpgv1.Backup {
	metaTime := metav1.NewTime(startTime)

	ownerRefs := []metav1.OwnerReference{
		{
			APIVersion: "cnpg-extensions.yandex.cloud/v1beta1",
			Kind:       "BackupConfig",
			Name:       backupConfigName,
			UID:        "test-uid",
		},
	}

	// Add ScheduledBackup owner reference if not a manual backup
	if !isManual {
		ownerRefs = append(ownerRefs, metav1.OwnerReference{
			APIVersion: "postgresql.cnpg.io/v1",
			Kind:       "ScheduledBackup",
			Name:       "test-scheduled-backup",
			UID:        "test-scheduled-uid",
		})
	}

	return cnpgv1.Backup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Backup",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			OwnerReferences:   ownerRefs,
			CreationTimestamp: metaTime,
		},
		Status: cnpgv1.BackupStatus{
			StartedAt: &metaTime,
		},
	}
}

// Helper function to check if a string is in a slice
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

var _ = Describe("RetentionController", func() {
	Describe("calculateRetentionThreshold", func() {
		type testCase struct {
			retentionValue string
			expectError    bool
			expectedDiff   time.Duration
		}

		DescribeTable("calculating retention threshold",
			func(tc testCase) {
				now := time.Now()
				threshold, err := calculateRetentionThreshold(tc.retentionValue)

				if tc.expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())

					// For months, we can only approximate the check
					if tc.retentionValue == "1m" {
						// Check that it's roughly a month ago (between 28 and 31 days)
						diff := now.Sub(threshold)
						Expect(diff).To(And(
							BeNumerically(">=", 28*24*time.Hour),
							BeNumerically("<=", 31*24*time.Hour),
						))
					} else {
						// For other units, we can be more precise
						expectedTime := now.Add(-tc.expectedDiff)

						// Allow for a small difference due to test execution time
						timeDiff := expectedTime.Sub(threshold)
						Expect(timeDiff).To(BeNumerically("<", 2*time.Second), "Time difference too large: %v", timeDiff)
					}
				}
			},
			Entry("valid hours retention", testCase{
				retentionValue: "3h",
				expectError:    false,
				expectedDiff:   3 * time.Hour,
			}),
			Entry("valid days retention", testCase{
				retentionValue: "7d",
				expectError:    false,
				expectedDiff:   7 * 24 * time.Hour,
			}),
			Entry("valid weeks retention", testCase{
				retentionValue: "2w",
				expectError:    false,
				expectedDiff:   14 * 24 * time.Hour,
			}),
			Entry("valid months retention", testCase{
				retentionValue: "1m",
				expectError:    false,
				expectedDiff:   30 * 24 * time.Hour, // Approximate
			}),
			Entry("invalid format", testCase{
				retentionValue: "invalid",
				expectError:    true,
			}),
			Entry("invalid unit", testCase{
				retentionValue: "7y",
				expectError:    true,
			}),
			Entry("invalid value", testCase{
				retentionValue: "ad",
				expectError:    true,
			}),
		)
	})

	Describe("getBackupsToDelete", func() {
		var controller *RetentionController

		BeforeEach(func() {
			controller = &RetentionController{}
		})

		type testCase struct {
			description   string
			backupConfig  *v1beta1.BackupConfig
			backups       []cnpgv1.Backup
			expectedCount int
			expectedNames []string
		}

		DescribeTable("determining backups to delete",
			func(tc testCase) {
				backupsToDelete, err := controller.getBackupsToDelete(ctx, tc.backupConfig, tc.backups)

				Expect(err).NotTo(HaveOccurred())
				Expect(backupsToDelete).To(HaveLen(tc.expectedCount))

				// Check that the expected backups are marked for deletion
				for _, expectedName := range tc.expectedNames {
					found := false
					for _, backup := range backupsToDelete {
						if backup.Name == expectedName {
							found = true
							break
						}
					}
					Expect(found).To(BeTrue(), "Expected backup %s to be deleted but it wasn't", expectedName)
				}
			},
			Entry("no retention policy", testCase{
				description: "no retention policy",
				backupConfig: &v1beta1.BackupConfig{
					Spec: v1beta1.BackupConfigSpec{
						Retention: v1beta1.BackupRetentionConfig{},
					},
				},
				backups: []cnpgv1.Backup{
					createBackup("backup1", "default", "test-config", time.Now().Add(-24*time.Hour), false),
					createBackup("backup2", "default", "test-config", time.Now().Add(-48*time.Hour), false),
				},
				expectedCount: 0,
				expectedNames: []string{},
			}),
			Entry("fewer backups than minBackupsToKeep", testCase{
				description: "fewer backups than minBackupsToKeep",
				backupConfig: &v1beta1.BackupConfig{
					Spec: v1beta1.BackupConfigSpec{
						Retention: v1beta1.BackupRetentionConfig{
							MinBackupsToKeep: 5,
						},
					},
				},
				backups: []cnpgv1.Backup{
					createBackup("backup1", "default", "test-config", time.Now().Add(-24*time.Hour), false),
					createBackup("backup2", "default", "test-config", time.Now().Add(-48*time.Hour), false),
				},
				expectedCount: 0,
				expectedNames: []string{},
			}),
			Entry("more backups than minBackupsToKeep with time threshold", testCase{
				description: "more backups than minBackupsToKeep with time threshold",
				backupConfig: &v1beta1.BackupConfig{
					Spec: v1beta1.BackupConfigSpec{
						Retention: v1beta1.BackupRetentionConfig{
							MinBackupsToKeep:   2,
							DeleteBackupsAfter: "1d",
						},
					},
				},
				backups: []cnpgv1.Backup{
					createBackup("backup1", "default", "test-config", time.Now().Add(-12*time.Hour), false),
					createBackup("backup2", "default", "test-config", time.Now().Add(-36*time.Hour), false),
					createBackup("backup3", "default", "test-config", time.Now().Add(-48*time.Hour), false),
				},
				expectedCount: 1,
				expectedNames: []string{"backup3"},
			}),
			Entry("ignore manual backups", testCase{
				description: "ignore manual backups",
				backupConfig: &v1beta1.BackupConfig{
					Spec: v1beta1.BackupConfigSpec{
						Retention: v1beta1.BackupRetentionConfig{
							MinBackupsToKeep:       1,
							DeleteBackupsAfter:     "1d",
							IgnoreForManualBackups: true,
						},
					},
				},
				backups: []cnpgv1.Backup{
					createBackup("backup1", "default", "test-config", time.Now().Add(-12*time.Hour), false),
					createBackup("backup2", "default", "test-config", time.Now().Add(-36*time.Hour), true), // Manual backup
					createBackup("backup3", "default", "test-config", time.Now().Add(-48*time.Hour), false),
				},
				expectedCount: 1,
				expectedNames: []string{"backup3"},
			}),
			Entry("keep minimum backups despite age", testCase{
				description: "keep minimum backups despite age",
				backupConfig: &v1beta1.BackupConfig{
					Spec: v1beta1.BackupConfigSpec{
						Retention: v1beta1.BackupRetentionConfig{
							MinBackupsToKeep:   2,
							DeleteBackupsAfter: "1d",
						},
					},
				},
				backups: []cnpgv1.Backup{
					createBackup("backup1", "default", "test-config", time.Now().Add(-36*time.Hour), false),
					createBackup("backup2", "default", "test-config", time.Now().Add(-48*time.Hour), false),
				},
				expectedCount: 0,
				expectedNames: []string{},
			}),
		)
	})

	Describe("runRetentionForBackupConfig", func() {
		var (
			logger logr.Logger
		)

		BeforeEach(func() {
			logger = logr.Discard()
		})

		type testCase struct {
			description     string
			backupConfig    *v1beta1.BackupConfig
			backups         []cnpgv1.Backup
			expectedDeleted []string
		}

		DescribeTable("running retention for backup config",
			func(tc testCase) {
				// Convert backups to client.Object for the fake client
				var objs []client.Object
				objs = append(objs, tc.backupConfig)
				for _, backup := range tc.backups {
					backupCopy := backup
					objs = append(objs, &backupCopy)
				}

				// Create fake client with objects
				fakeClient := setupFakeClient(objs...)

				// Create controller with fake client
				controller := &RetentionController{
					client:        fakeClient,
					checkInterval: 1 * time.Hour,
				}

				// Run retention
				err := controller.runRetentionForBackupConfig(ctx, tc.backupConfig, logger)
				Expect(err).NotTo(HaveOccurred())

				// Check that expected backups were deleted
				for _, backup := range tc.backups {
					err := fakeClient.Get(ctx, client.ObjectKey{Name: backup.Name, Namespace: backup.Namespace}, &cnpgv1.Backup{})

					if contains(tc.expectedDeleted, backup.Name) {
						// Should be deleted
						Expect(err).To(HaveOccurred(), "Backup %s should have been deleted", backup.Name)
					} else {
						// Should still exist
						Expect(err).NotTo(HaveOccurred(), "Backup %s should not have been deleted", backup.Name)
					}
				}
			},
			Entry("delete old backups", testCase{
				description:  "delete old backups",
				backupConfig: createBackupConfig("test-config", "default", 2, "1d", false),
				backups: []cnpgv1.Backup{
					createBackup("backup1", "default", "test-config", time.Now().Add(-12*time.Hour), false),
					createBackup("backup2", "default", "test-config", time.Now().Add(-36*time.Hour), false),
					createBackup("backup3", "default", "test-config", time.Now().Add(-48*time.Hour), false),
				},
				expectedDeleted: []string{"backup3"},
			}),
			Entry("keep all backups when fewer than minimum", testCase{
				description:  "keep all backups when fewer than minimum",
				backupConfig: createBackupConfig("test-config", "default", 5, "1d", false),
				backups: []cnpgv1.Backup{
					createBackup("backup1", "default", "test-config", time.Now().Add(-36*time.Hour), false),
					createBackup("backup2", "default", "test-config", time.Now().Add(-48*time.Hour), false),
				},
				expectedDeleted: []string{},
			}),
			Entry("respect ignore manual backups setting", testCase{
				description:  "respect ignore manual backups setting",
				backupConfig: createBackupConfig("test-config", "default", 2, "1d", true),
				backups: []cnpgv1.Backup{
					createBackup("backup1", "default", "test-config", time.Now().Add(-12*time.Hour), false),
					createBackup("backup2", "default", "test-config", time.Now().Add(-36*time.Hour), true), // Manual backup
					createBackup("backup3", "default", "test-config", time.Now().Add(-48*time.Hour), false),
				},
				expectedDeleted: []string{"backup3"},
			}),
		)
	})

	Describe("Multiple BackupConfigs with different retention policies", func() {
		It("should apply retention policies independently without interference", func() {
			// Create BackupConfigs with different retention policies
			backupConfig1 := createBackupConfig("config1", "namespace1", 2, "1d", false)
			backupConfig2 := createBackupConfig("config2", "namespace1", 5, "7d", false)
			backupConfig3 := createBackupConfig("config3", "namespace2", 3, "2d", true)

			// Create backups for each config with different timestamps
			now := time.Now()

			// Backups for config1 - should delete backup1-3 (older than 1d)
			backups1 := []cnpgv1.Backup{
				createBackup("backup1-1", "namespace1", "config1", now.Add(-12*time.Hour), false),
				createBackup("backup1-2", "namespace1", "config1", now.Add(-20*time.Hour), false),
				createBackup("backup1-3", "namespace1", "config1", now.Add(-30*time.Hour), false),
			}

			// Backups for config2 - should keep all (within 7d and fewer than min 5)
			backups2 := []cnpgv1.Backup{
				createBackup("backup2-1", "namespace1", "config2", now.Add(-24*time.Hour), false),
				createBackup("backup2-2", "namespace1", "config2", now.Add(-48*time.Hour), false),
				createBackup("backup2-3", "namespace1", "config2", now.Add(-72*time.Hour), false),
			}

			// Backups for config3 - should delete backup3-4 (older than 2d) but keep backup3-3 (manual)
			backups3 := []cnpgv1.Backup{
				createBackup("backup3-1", "namespace2", "config3", now.Add(-12*time.Hour), false),
				createBackup("backup3-2", "namespace2", "config3", now.Add(-36*time.Hour), false),
				createBackup("backup3-3", "namespace2", "config3", now.Add(-60*time.Hour), true), // Manual backup
				createBackup("backup3-4", "namespace2", "config3", now.Add(-72*time.Hour), false),
			}

			// Combine all objects for the fake client
			var objs []client.Object
			objs = append(objs, backupConfig1, backupConfig2, backupConfig3)

			for _, backup := range append(append(backups1, backups2...), backups3...) {
				backupCopy := backup
				objs = append(objs, &backupCopy)
			}

			// Create fake client with objects
			fakeClient := setupFakeClient(objs...)

			// Create controller with fake client
			controller := &RetentionController{
				client:        fakeClient,
				checkInterval: 1 * time.Hour,
			}

			// Run retention for each BackupConfig
			logger := logr.Discard()

			err := controller.runRetentionForBackupConfig(ctx, backupConfig1, logger)
			Expect(err).NotTo(HaveOccurred())

			err = controller.runRetentionForBackupConfig(ctx, backupConfig2, logger)
			Expect(err).NotTo(HaveOccurred())

			err = controller.runRetentionForBackupConfig(ctx, backupConfig3, logger)
			Expect(err).NotTo(HaveOccurred())

			// Check backups for config1 - backup1-3 should be deleted (older than 1d)
			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup1-1", Namespace: "namespace1"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup1-1 should not have been deleted")

			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup1-2", Namespace: "namespace1"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup1-2 should not have been deleted")

			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup1-3", Namespace: "namespace1"}, &cnpgv1.Backup{})
			Expect(err).To(HaveOccurred(), "Backup backup1-3 should have been deleted")

			// Check backups for config2 - all should be kept (within 7d and fewer than min 5)
			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup2-1", Namespace: "namespace1"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup2-1 should not have been deleted")

			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup2-2", Namespace: "namespace1"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup2-2 should not have been deleted")

			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup2-3", Namespace: "namespace1"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup2-3 should not have been deleted")

			// Check backups for config3 - backup3-4 should be deleted (older than 2d) but backup3-3 kept (manual)
			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup3-1", Namespace: "namespace2"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup3-1 should not have been deleted")

			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup3-2", Namespace: "namespace2"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup3-2 should not have been deleted")

			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup3-3", Namespace: "namespace2"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup3-3 should not have been deleted (manual backup)")

			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup3-4", Namespace: "namespace2"}, &cnpgv1.Backup{})
			Expect(err).To(HaveOccurred(), "Backup backup3-4 should have been deleted")
		})

		It("should handle backups with same names in different namespaces", func() {
			// Create BackupConfigs in different namespaces
			backupConfig1 := createBackupConfig("same-config", "namespace1", 0, "1d", false)
			backupConfig2 := createBackupConfig("same-config", "namespace2", 0, "7d", false)

			// Create backups with same names in different namespaces
			now := time.Now()

			// Backups for config1 - should delete backup-old (older than 1d)
			backups1 := []cnpgv1.Backup{
				createBackup("backup-new", "namespace1", "same-config", now.Add(-12*time.Hour), false),
				createBackup("backup-old", "namespace1", "same-config", now.Add(-30*time.Hour), false),
			}

			// Backups for config2 - should keep all (within 7d and fewer than min 5)
			backups2 := []cnpgv1.Backup{
				createBackup("backup-new", "namespace2", "same-config", now.Add(-24*time.Hour), false),
				createBackup("backup-old", "namespace2", "same-config", now.Add(-48*time.Hour), false),
			}

			// Combine all objects for the fake client
			var objs []client.Object
			objs = append(objs, backupConfig1, backupConfig2)

			for _, backup := range append(backups1, backups2...) {
				backupCopy := backup
				objs = append(objs, &backupCopy)
			}

			// Create fake client with objects
			fakeClient := setupFakeClient(objs...)

			// Create controller with fake client
			controller := &RetentionController{
				client:        fakeClient,
				checkInterval: 1 * time.Hour,
			}

			// Run retention for each BackupConfig
			logger := logr.Discard()

			err := controller.runRetentionForBackupConfig(ctx, backupConfig1, logger)
			Expect(err).NotTo(HaveOccurred())

			err = controller.runRetentionForBackupConfig(ctx, backupConfig2, logger)
			Expect(err).NotTo(HaveOccurred())

			// Check backups for config1 - backup-old should be deleted (older than 1d)
			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup-new", Namespace: "namespace1"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup-new in namespace1 should not have been deleted")

			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup-old", Namespace: "namespace1"}, &cnpgv1.Backup{})
			Expect(err).To(HaveOccurred(), "Backup backup-old in namespace1 should have been deleted")

			// Check backups for config2 - all should be kept (within 7d and fewer than min 5)
			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup-new", Namespace: "namespace2"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup-new in namespace2 should not have been deleted")

			err = fakeClient.Get(ctx, client.ObjectKey{Name: "backup-old", Namespace: "namespace2"}, &cnpgv1.Backup{})
			Expect(err).NotTo(HaveOccurred(), "Backup backup-old in namespace2 should not have been deleted")
		})
	})

	Describe("runBackupsRetentionCheck", func() {
		It("should process multiple BackupConfigs with different retention policies correctly", func() {
			// Create BackupConfigs with different retention policies
			backupConfig1 := createBackupConfig("config1", "namespace1", 2, "1d", false)
			backupConfig2 := createBackupConfig("config2", "namespace1", 5, "7d", false)
			backupConfig3 := createBackupConfig("config3", "namespace2", 3, "2d", true)
			backupConfig4 := createBackupConfig("config4", "namespace2", 0, "", false) // No retention policy

			// Create backups for each config with different timestamps
			now := time.Now()

			// Backups for config1 - should delete backup1-3 (older than 1d)
			backups1 := []cnpgv1.Backup{
				createBackup("backup1-1", "namespace1", "config1", now.Add(-12*time.Hour), false),
				createBackup("backup1-2", "namespace1", "config1", now.Add(-20*time.Hour), false),
				createBackup("backup1-3", "namespace1", "config1", now.Add(-30*time.Hour), false),
			}

			// Backups for config2 - should keep all (within 7d and fewer than min 5)
			backups2 := []cnpgv1.Backup{
				createBackup("backup2-1", "namespace1", "config2", now.Add(-24*time.Hour), false),
				createBackup("backup2-2", "namespace1", "config2", now.Add(-48*time.Hour), false),
				createBackup("backup2-3", "namespace1", "config2", now.Add(-72*time.Hour), false),
			}

			// Backups for config3 - should delete backup3-4 (older than 2d) but keep backup3-3 (manual)
			backups3 := []cnpgv1.Backup{
				createBackup("backup3-1", "namespace2", "config3", now.Add(-12*time.Hour), false),
				createBackup("backup3-2", "namespace2", "config3", now.Add(-36*time.Hour), false),
				createBackup("backup3-3", "namespace2", "config3", now.Add(-60*time.Hour), true), // Manual backup
				createBackup("backup3-4", "namespace2", "config3", now.Add(-72*time.Hour), false),
			}

			// Backups for config4 - should keep all (no retention policy)
			backups4 := []cnpgv1.Backup{
				createBackup("backup4-1", "namespace2", "config4", now.Add(-24*time.Hour), false),
				createBackup("backup4-2", "namespace2", "config4", now.Add(-48*time.Hour), false),
				createBackup("backup4-3", "namespace2", "config4", now.Add(-72*time.Hour), false),
				createBackup("backup4-4", "namespace2", "config4", now.Add(-96*time.Hour), false),
			}

			// Combine all objects for the fake client
			var objs []client.Object
			objs = append(objs, backupConfig1, backupConfig2, backupConfig3, backupConfig4)

			allBackups := append(append(append(backups1, backups2...), backups3...), backups4...)
			for _, backup := range allBackups {
				backupCopy := backup
				objs = append(objs, &backupCopy)
			}

			// Create fake client with objects
			fakeClient := setupFakeClient(objs...)

			// Create controller with fake client
			controller := &RetentionController{
				client:        fakeClient,
				checkInterval: 1 * time.Hour,
			}

			// Run retention check for all BackupConfigs at once
			controller.runBackupsRetentionCheck(ctx)

			// Expected deleted backups
			expectedDeleted := map[string]bool{
				"backup1-3": true, // From config1 (older than 1d)
				"backup3-4": true, // From config3 (older than 2d)
			}

			// Check all backups
			for _, backup := range allBackups {
				err := fakeClient.Get(ctx, client.ObjectKey{Name: backup.Name, Namespace: backup.Namespace}, &cnpgv1.Backup{})

				if expectedDeleted[backup.Name] {
					Expect(err).To(HaveOccurred(), "Backup %s should have been deleted", backup.Name)
				} else {
					Expect(err).NotTo(HaveOccurred(), "Backup %s should not have been deleted", backup.Name)
				}
			}
		})

		It("should handle complex scenarios with overlapping retention policies", func() {
			// Create two BackupConfigs with different retention policies but sharing a cluster
			backupConfig1 := createBackupConfig("daily-backups", "shared-ns", 3, "2d", false)
			backupConfig2 := createBackupConfig("weekly-backups", "shared-ns", 2, "14d", false)

			// Create a third BackupConfig in a different namespace
			backupConfig3 := createBackupConfig("other-backups", "other-ns", 1, "1d", false)

			now := time.Now()

			// Create backups with multiple owner references (simulating backups owned by multiple configs)
			// This is a special test case to ensure that even if a backup has multiple owners,
			// it's correctly processed by each retention policy independently

			// Create a shared backup that is owned by both backupConfig1 and backupConfig2
			sharedBackup := cnpgv1.Backup{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Backup",
					APIVersion: "postgresql.cnpg.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:              "shared-backup",
					Namespace:         "shared-ns",
					CreationTimestamp: metav1.NewTime(now.Add(-10 * 24 * time.Hour)), // 10 days old
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "cnpg-extensions.yandex.cloud/v1beta1",
							Kind:       "BackupConfig",
							Name:       "daily-backups",
							UID:        "test-uid-1",
						},
						{
							APIVersion: "cnpg-extensions.yandex.cloud/v1beta1",
							Kind:       "BackupConfig",
							Name:       "weekly-backups",
							UID:        "test-uid-2",
						},
						{
							APIVersion: "postgresql.cnpg.io/v1",
							Kind:       "Cluster",
							Name:       "test-cluster",
							UID:        "test-cluster-uid",
						},
					},
				},
				Status: cnpgv1.BackupStatus{
					StartedAt: &metav1.Time{Time: now.Add(-10 * 24 * time.Hour)},
				},
			}

			// Regular backups for each config
			backups1 := []cnpgv1.Backup{
				createBackup("daily-1", "shared-ns", "daily-backups", now.Add(-1*24*time.Hour), false),
				createBackup("daily-2", "shared-ns", "daily-backups", now.Add(-2*24*time.Hour), false),
				createBackup("daily-3", "shared-ns", "daily-backups", now.Add(-3*24*time.Hour), false),
				createBackup("daily-4", "shared-ns", "daily-backups", now.Add(-4*24*time.Hour), false), // Should be deleted (>2d)
			}

			backups2 := []cnpgv1.Backup{
				createBackup("weekly-1", "shared-ns", "weekly-backups", now.Add(-7*24*time.Hour), false),
				createBackup("weekly-2", "shared-ns", "weekly-backups", now.Add(-14*24*time.Hour), false),
				createBackup("weekly-3", "shared-ns", "weekly-backups", now.Add(-21*24*time.Hour), false), // Should be deleted (>14d)
			}

			backups3 := []cnpgv1.Backup{
				createBackup("other-1", "other-ns", "other-backups", now.Add(-12*time.Hour), false),
				createBackup("other-2", "other-ns", "other-backups", now.Add(-36*time.Hour), false), // Should be deleted (>1d)
			}

			// Combine all objects for the fake client
			var objs []client.Object
			objs = append(objs, backupConfig1, backupConfig2, backupConfig3)

			allBackups := append(append(backups1, backups2...), backups3...)
			for _, backup := range allBackups {
				backupCopy := backup
				objs = append(objs, &backupCopy)
			}

			// Add the shared backup
			objs = append(objs, &sharedBackup)

			// Create fake client with objects
			fakeClient := setupFakeClient(objs...)

			// Create controller with fake client
			controller := &RetentionController{
				client:        fakeClient,
				checkInterval: 1 * time.Hour,
			}

			// Run retention check for all BackupConfigs at once
			controller.runBackupsRetentionCheck(ctx)

			// Expected deleted backups
			expectedDeleted := map[string]bool{
				"daily-4":  true, // From backupConfig1 (older than 2d)
				"weekly-3": true, // From backupConfig2 (older than 14d)
				"other-2":  true, // From backupConfig3 (older than 1d)
			}

			// The shared backup should be deleted by backupConfig1 (2d policy)
			// but kept by backupConfig2 (14d policy)
			// Since the retention controller processes each BackupConfig independently,
			// the shared backup will be deleted if any policy says to delete it
			expectedDeleted["shared-backup"] = true

			// Check all backups
			for _, backup := range append(allBackups, sharedBackup) {
				err := fakeClient.Get(ctx, client.ObjectKey{Name: backup.Name, Namespace: backup.Namespace}, &cnpgv1.Backup{})

				if expectedDeleted[backup.Name] {
					Expect(err).To(HaveOccurred(), "Backup %s should have been deleted", backup.Name)
				} else {
					Expect(err).NotTo(HaveOccurred(), "Backup %s should not have been deleted", backup.Name)
				}
			}
		})
	})
})
