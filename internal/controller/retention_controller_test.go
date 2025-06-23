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
			Entry("valid minutes retention (for testing)", testCase{
				retentionValue: "30min",
				expectError:    false,
				expectedDiff:   30 * time.Minute,
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
			backupConfig  v1beta1.BackupConfig
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
				backupConfig: v1beta1.BackupConfig{
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
				backupConfig: v1beta1.BackupConfig{
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
				backupConfig: v1beta1.BackupConfig{
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
				backupConfig: v1beta1.BackupConfig{
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
				backupConfig: v1beta1.BackupConfig{
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
				err := controller.runRetentionForBackupConfig(ctx, *tc.backupConfig, logger)
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
})
