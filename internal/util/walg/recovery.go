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
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/machinery/pkg/types"
)

// FindBackupInfo finds the backup info that should be used to file
// a PITR request via target parameters specified within `RecoveryTarget`
func FindMostSuitableBackupForRecovery(
	ctx context.Context,
	backupList []BackupMetadata,
	recoveryTarget cnpgv1.RecoveryTarget,
) (*BackupMetadata, error) {
	// Check that BackupID is not empty. In such case, always use the
	// backup ID provided by the user.
	if recoveryTarget.GetBackupID() != "" {
		return GetBackupByName(ctx, backupList, recoveryTarget.GetBackupID()), nil
	}

	// Set the timeline
	targetTLI := recoveryTarget.GetTargetTLI()

	// The first step is to check any time based research
	if t := recoveryTarget.GetTargetTime(); t != "" {
		return findClosestBackupFromTargetTime(backupList, t, targetTLI)
	}

	// The second step is to check any LSN based research
	if t := recoveryTarget.GetTargetLSN(); t != "" {
		return findClosestBackupFromTargetLSN(backupList, t, targetTLI)
	}

	// The fallback is to use the latest available backup in chronological order
	return findLatestBackupFromTimeline(backupList, targetTLI), nil
}

func findClosestBackupFromTargetLSN(
	backups []BackupMetadata,
	targetLSNString string,
	targetTLI string,
) (*BackupMetadata, error) {
	targetLSN := types.LSN(targetLSNString)
	if _, err := targetLSN.Parse(); err != nil {
		return nil, fmt.Errorf("while parsing recovery target targetLSN: %s", err.Error())
	}
	for i := len(backups) - 1; i >= 0; i-- {
		if backups[i].HasMatchingTimeline(targetTLI) && types.Int64ToLSN(backups[i].FinishLSN).Less(targetLSN) {
			return &backups[i], nil
		}
	}
	return nil, nil
}

func findClosestBackupFromTargetTime(backups []BackupMetadata, targetTime string, targetTLI string) (*BackupMetadata, error) {
	target, err := types.ParseTargetTime(nil, targetTime)
	if err != nil {
		return nil, fmt.Errorf("while parsing recovery target targetTime: %s", err.Error())
	}
	for i := len(backups) - 1; i >= 0; i-- {
		backupFinishTime, err := backups[i].FinishTime()
		if err != nil {
			continue
		}
		if backups[i].HasMatchingTimeline(targetTLI) && !backupFinishTime.After(target) {
			return &backups[i], nil
		}
	}
	return nil, nil
}

func findLatestBackupFromTimeline(backups []BackupMetadata, targetTLI string) *BackupMetadata {
	for i := len(backups) - 1; i >= 0; i-- {
		if backups[i].HasMatchingTimeline(targetTLI) {
			return &backups[i]
		}
	}
	return nil
}
