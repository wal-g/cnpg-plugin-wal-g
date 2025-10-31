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
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
)

// BackupMetadata represents single backup metadata returned by wal-g
// on "wal-g backup-list --detail --json"
type BackupMetadata struct {
	BackupName       string         `json:"backup_name"`
	Time             string         `json:"time"`
	WalFileName      string         `json:"wal_file_name"`
	StorageName      string         `json:"storage_name"`
	StartTimeString  string         `json:"start_time"`
	FinishTimeString string         `json:"finish_time"`
	DateFmt          string         `json:"date_fmt"`
	Hostname         string         `json:"hostname"`
	DataDir          string         `json:"data_dir"`
	PGVersion        int            `json:"pg_version"`
	StartLSN         uint64         `json:"start_lsn"`
	FinishLSN        uint64         `json:"finish_lsn"`
	IsPermanent      bool           `json:"is_permanent"`
	SystemIdentifier int            `json:"system_identifier"`
	UncompressedSize int            `json:"uncompressed_size"`
	CompressedSize   int            `json:"compressed_size"`
	UserData         map[string]any `json:"user_data"`
}

func (m *BackupMetadata) StartTime() (time.Time, error) {
	if m == nil {
		return time.Time{}, fmt.Errorf("BackupMetadata is nil")
	}
	return time.Parse(time.RFC3339Nano, m.StartTimeString)
}

func (m *BackupMetadata) FinishTime() (time.Time, error) {
	if m == nil {
		return time.Time{}, fmt.Errorf("BackupMetadata is nil")
	}
	return time.Parse(time.RFC3339Nano, m.FinishTimeString)
}

func (m *BackupMetadata) Timeline() int {
	timelineStr := "0x" + m.WalFileName[0:8]
	tid, _ := strconv.ParseInt(timelineStr, 16, 64)
	return int(tid)
}

func (m *BackupMetadata) TimelineStr() string {
	return m.WalFileName[0:8]
}

func (m *BackupMetadata) HasMatchingTimeline(targetTimeline string) bool {
	if targetTimeline == "" || targetTimeline == "latest" {
		return true // Any timeline will match, if we do not specify targetTimeline
	}

	targetTimelineID, err := strconv.ParseInt(targetTimeline, 16, 64)
	if err != nil {
		return true // Passed incorrect timeline - allowing any timeline
	}

	return m.Timeline() <= int(targetTimelineID)
}

func GetBackupsList(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfigWithSecrets,
	pgMajorVersion int,
) ([]BackupMetadata, error) {
	logger := logr.FromContextOrDiscard(ctx)
	result, err := cmd.New("wal-g", "backup-list", "--detail", "--json").
		WithContext(ctx).
		WithEnv(NewConfigFromBackupConfig(backupConfig, pgMajorVersion).ToEnvMap()).
		Run()
	if err != nil {
		logger.Error(
			err, "GetBackupsList: error on wal-g backup-list --detail --json",
			"stdout", string(result.Stdout()),
			"stderr", string(result.Stderr()),
		)
		return nil, fmt.Errorf("failed to do wal-g backup-list: %w", err)
	}
	logger.Info("Finished wal-g backup-list", "stdout", string(result.Stdout()), "stderr", string(result.Stderr()))

	backupsMetadata := make([]BackupMetadata, 0)
	if err = json.Unmarshal(result.Stdout(), &backupsMetadata); err != nil {
		logger.Error(
			err, "GetBackupsList: cannot unmarshal wal-g backup-list",
			"stdout", string(result.Stdout()),
			"stderr", string(result.Stderr()),
		)
		return nil, fmt.Errorf("cannot unmarshal wal-g backup-list output: %w", err)
	}

	return backupsMetadata, nil
}

// Finds wal-g backup matching provided user data
// If any error occurred during searching for backup - returns (nil, error)
// If no backup found - returns (nil, nil)
// If more than one backup found - returns most recent backup matching provided user data
func GetBackupByUserData(
	ctx context.Context,
	backupList []BackupMetadata,
	userData map[string]any,
) (*BackupMetadata, error) {
	backup, _, ok := lo.FindLastIndexOf(backupList, func(b BackupMetadata) bool {
		return maps.Equal(b.UserData, userData)
	})

	if !ok {
		return nil, nil
	}

	return &backup, nil
}

// GetLatestBackup returns the latest wal-g backup
func GetLatestBackup(ctx context.Context, backupList []BackupMetadata) (*BackupMetadata, error) {
	if len(backupList) == 0 {
		return nil, fmt.Errorf("no backup found on the remote object storage")
	}

	return &backupList[len(backupList)-1], nil
}

// Finds wal-g backup matching provided name among backups in backupList
// If no backup found - returns nil
func GetBackupByName(ctx context.Context, backupList []BackupMetadata, name string) *BackupMetadata {
	backup, ok := lo.Find(backupList, func(b BackupMetadata) bool {
		return b.BackupName == name
	})

	if !ok {
		return nil
	}
	return &backup
}

// Marking backup as permanent. These backups cannot be deleted with `wal-g delete everything` and
// are ignored when running WAL retention.
//
// Marking backups as permanent is useful for user-created backups which can be stored indefinitely
func MarkBackupPermanent(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfigWithSecrets,
	pgMajorVersion int,
	backupName string,
) error {
	logger := logr.FromContextOrDiscard(ctx)

	result, err := cmd.New("wal-g", "backup-mark", backupName).
		WithContext(ctx).
		WithEnv(NewConfigFromBackupConfig(backupConfig, pgMajorVersion).ToEnvMap()).
		Run()

	if err != nil {
		logger.Error(
			err, fmt.Sprintf("Error while 'wal-g backup-mark %s'", backupName),
			"stdout", result.Stdout(), "stderr", result.Stderr(),
		)
	}
	return err
}

// Removing permanent mark from backup
// This is useful when need to delete backup, which was previously marked as permanent.
// This will NOT do anything and will NOT throw an error if backup is already NOT marked permanent.
func UnmarkBackupPermanent(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfigWithSecrets,
	pgMajorVersion int,
	backupName string,
) error {
	logger := logr.FromContextOrDiscard(ctx)

	result, err := cmd.New("wal-g", "backup-mark", "-i", backupName).
		WithContext(ctx).
		WithEnv(NewConfigFromBackupConfig(backupConfig, pgMajorVersion).ToEnvMap()).
		Run()

	if err != nil {
		logger.Error(
			err, fmt.Sprintf("Error while 'wal-g backup-mark -i %s'", backupName),
			"stdout", result.Stdout(), "stderr", result.Stderr(),
		)
	}
	return err
}

// DeleteBackup deletes a backup using WAL-G
func DeleteBackup(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfigWithSecrets,
	pgMajorVersion int,
	backupName string,
) (*cmd.RunResult, error) {
	logger := logr.FromContextOrDiscard(ctx)

	// Ignore errors on UnmarkBackupPermanent and try our best to remove backup
	_ = UnmarkBackupPermanent(ctx, backupConfig, pgMajorVersion, backupName)

	result, err := cmd.New("wal-g", "delete", "target", backupName, "--confirm").
		WithContext(ctx).
		WithEnv(NewConfigFromBackupConfig(backupConfig, pgMajorVersion).ToEnvMap()).
		Run()

	backupDoesNotExistStr := fmt.Sprintf("Backup '%s' does not exist.", backupName)
	// If backup already not exists in storage - do not treat this as an error, return success
	if err != nil && strings.Contains(string(result.Stderr()), backupDoesNotExistStr) {
		err = nil
	}
	// Do not abort if error not-nil and try anyway to perform `wal-g delete garbage` anyway

	gcResult, gcErr := cmd.New("wal-g", "delete", "garbage", "--confirm").
		WithContext(ctx).
		WithEnv(NewConfigFromBackupConfig(backupConfig, pgMajorVersion).ToEnvMap()).
		Run()
	if gcErr != nil {
		// Actually errors on garbage collect do not block us from deleting backup
		// So only logging them and not reporting them to caller
		logger.Error(
			gcErr, "Error while 'wal-g delete garbage'",
			"stdout", gcResult.Stdout(), "stderr", gcResult.Stderr(),
		)
	}

	return result, err
}

func DeleteAllBackupsAndWALsInStorage(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfigWithSecrets,
	pgMajorVersion int,
) (*cmd.RunResult, error) {
	logger := logr.FromContextOrDiscard(ctx)
	backupsList, err := GetBackupsList(ctx, backupConfig, pgMajorVersion)
	if err != nil {
		logger.Error(err, "Error occurred while deleting all backups: cannot fetch backups list")
	} else {
		// Unmarking permanent backups from latest to first to perform full cleanup on storage
		for i := len(backupsList) - 1; i >= 0; i-- {
			if backupsList[i].IsPermanent {
				err := UnmarkBackupPermanent(ctx, backupConfig, pgMajorVersion, backupsList[i].BackupName)
				if err != nil {
					logger.Error(err, "Error occurred while deleting all backups: cannot unmark permanent backup")
				}
			}
		}
	}

	return cmd.New("wal-g", "delete", "everything", "FORCE", "--confirm").
		WithContext(ctx).
		WithEnv(NewConfigFromBackupConfig(backupConfig, pgMajorVersion).ToEnvMap()).
		Run()
}

// GetDependentBackups returns a list of backups that depend on the current backup.
// This is determined by analyzing backup names, where delta backups include their base backup's WAL ID.
// For example:
// - base_000000010000000100000040 (full backup)
// - base_000000010000000100000046_D_000000010000000100000040 (delta backup depending on the full backup)
// - base_000000010000000100000061_D_000000010000000100000046 (delta backup depending on the previous delta)
//
// If includeIndirect is true, it will also include indirect dependencies (dependencies of dependencies).
// For example, if A depends on the current backup, and B depends on A, then B is an indirect dependency
// of the current backup and will be included in the result if includeIndirect is true.
func (m *BackupMetadata) GetDependentBackups(ctx context.Context, backupList []BackupMetadata, includeIndirect bool) []BackupMetadata {
	// Find direct dependencies
	directDependencies := findDirectDependencies(m, backupList)

	// If we don't need indirect dependencies, return just the direct ones
	if !includeIndirect {
		return directDependencies
	}

	// Find also indirect dependencies via queue by processing each of "new" dependency
	depsQueue := make([]*BackupMetadata, 0, len(directDependencies))
	knownDependencies := make(map[string]*BackupMetadata)

	// Add direct dependencies to the result map and to the processing queue
	for i := range directDependencies {
		backup := &directDependencies[i]
		depsQueue = append(depsQueue, backup)
		knownDependencies[backup.BackupName] = backup
	}

	for len(depsQueue) > 0 {
		dependencyBackup := depsQueue[0] // Getting first from queue
		depsQueue = depsQueue[1:]        // Removing first element from queue
		newDependencyBackups := findDirectDependencies(dependencyBackup, backupList)
		for i := range newDependencyBackups {
			newDepBackup := &newDependencyBackups[i]
			_, alreadyKnown := knownDependencies[newDepBackup.BackupName]
			if !alreadyKnown {
				knownDependencies[newDepBackup.BackupName] = newDepBackup
				depsQueue = append(depsQueue, newDepBackup)
			}
		}
	}

	// Convert map to slice
	result := make([]BackupMetadata, 0, len(knownDependencies))
	for i := range knownDependencies {
		result = append(result, *knownDependencies[i])
	}

	return result
}

// findDirectDependencies finds all direct dependencies of the given backup
func findDirectDependencies(parent *BackupMetadata, backupList []BackupMetadata) []BackupMetadata {
	// Extract the WAL ID from the backup name
	parentWalID := strings.TrimPrefix(parent.BackupName, "base_")
	if strings.Contains(parentWalID, "_D_") {
		parentWalID = strings.Split(parentWalID, "_D_")[0]
	}

	// Find direct dependencies of this backup
	result := make([]BackupMetadata, 0)
	for i := range backupList {
		backup := &backupList[i]
		// Skip if this is the current backup
		if backup.BackupName == parent.BackupName {
			continue
		}

		// Skipping non-delta backups, they cannot be dependent
		if !strings.Contains(backup.BackupName, "_D_") {
			continue
		}

		parts := strings.Split(backup.BackupName, "_D_")
		if len(parts) == 2 && parts[1] == parentWalID {
			result = append(result, *backup)
		}
	}

	return result
}
