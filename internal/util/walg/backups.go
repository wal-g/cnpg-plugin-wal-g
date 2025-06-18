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
	"time"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
)

// WalgBackupMetadata represents single backup metadata returned by wal-g
// on "wal-g backup-list --detail --json"
type WalgBackupMetadata struct {
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

func (m *WalgBackupMetadata) StartTime() (time.Time, error) {
	if m == nil {
		return time.Time{}, fmt.Errorf("WalgBackupMetadata is nil")
	}
	return time.Parse(time.RFC3339Nano, m.StartTimeString)
}

func (m *WalgBackupMetadata) FinishTime() (time.Time, error) {
	if m == nil {
		return time.Time{}, fmt.Errorf("WalgBackupMetadata is nil")
	}
	return time.Parse(time.RFC3339Nano, m.FinishTimeString)
}

func (m *WalgBackupMetadata) Timeline() int {
	timelineStr := "0x" + m.WalFileName[0:8]
	tid, _ := strconv.ParseInt(timelineStr, 16, 64)
	return int(tid)
}
func (m *WalgBackupMetadata) TimelineStr() string {
	return m.WalFileName[0:8]
}

func GetBackupsList(
	ctx context.Context,
	backupConfig v1beta1.BackupConfigWithSecrets,
) ([]WalgBackupMetadata, error) {
	logger := logr.FromContextOrDiscard(ctx)
	result, err := cmd.New("wal-g", "backup-list", "--detail", "--json").
		WithContext(ctx).
		WithEnv(NewWalgConfigFromBackupConfig(backupConfig).ToEnvMap()).
		Run()
	if err != nil {
		logger.Error(err, "GetBackupsList: error on wal-g backup-list --detail --json", "stdout", string(result.Stdout()), "stderr", string(result.Stderr()))
		return nil, fmt.Errorf("failed to do wal-g backup-list: %w", err)
	} else {
		logger.Info("Finished wal-g backup-list", "stdout", string(result.Stdout()), "stderr", string(result.Stderr()))
	}

	backupsMetadata := make([]WalgBackupMetadata, 0)
	if err = json.Unmarshal(result.Stdout(), &backupsMetadata); err != nil {
		logger.Error(err, "GetBackupsList: cannot unmarshal wal-g backup-list stdout", string(result.Stdout()), "stderr", string(result.Stderr()))
		return nil, fmt.Errorf("cannot unmarshal wal-g backup-list output: %w", err)
	}

	return backupsMetadata, nil
}

// Finds wal-g backup matching provided user data
// If any error occured during searching for backup - returns (nil, error)
// If no backup found - returns (nil, nil)
// If more than one backup found - returns most recent backup matching provided user data
func GetBackupByUserData(
	ctx context.Context,
	backupList []WalgBackupMetadata,
	userData map[string]any,
) (*WalgBackupMetadata, error) {
	backup, _, ok := lo.FindLastIndexOf(backupList, func(b WalgBackupMetadata) bool {
		return maps.Equal(b.UserData, userData)
	})

	if !ok {
		return nil, nil
	}

	return &backup, nil
}

// GetLatestBackup returns the latest wal-g backup
func GetLatestBackup(ctx context.Context, backupList []WalgBackupMetadata) (*WalgBackupMetadata, error) {
	if len(backupList) == 0 {
		return nil, fmt.Errorf("no backup found on the remote object storage")
	}

	return &backupList[len(backupList)-1], nil
}

// Finds wal-g backup matching provided name
// If any error occured during searching for backup - returns (nil, error)
// If no backup found - returns (nil, nil)
func GetBackupByName(ctx context.Context, backupList []WalgBackupMetadata, name string) (*WalgBackupMetadata, error) {
	backup, ok := lo.Find(backupList, func(b WalgBackupMetadata) bool {
		return b.BackupName == name
	})

	if !ok {
		return nil, nil
	}

	return &backup, nil
}

// DeleteBackup deletes a backup using WAL-G
func DeleteBackup(
	ctx context.Context,
	backupConfig v1beta1.BackupConfigWithSecrets,
	backupName string,
) (*cmd.CmdRunResult, error) {
	return cmd.New("wal-g", "delete", "target", backupName, "--confirm").
		WithContext(ctx).
		WithEnv(NewWalgConfigFromBackupConfig(backupConfig).ToEnvMap()).
		Run()
}
