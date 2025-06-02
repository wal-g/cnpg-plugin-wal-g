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
	"strconv"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/machinery/pkg/types"
)

// FindBackupInfo finds the backup info that should be used to file
// a PITR request via target parameters specified within `RecoveryTarget`
func FindMostSuitableBackupForRecovery(
	ctx context.Context,
	backupList []WalgBackupMetadata,
	recoveryTarget cnpgv1.RecoveryTarget,
) (*WalgBackupMetadata, error) {

	// Check that BackupID is not empty. In such case, always use the
	// backup ID provided by the user.
	if recoveryTarget.GetBackupID() != "" {
		return GetBackupByName(ctx, backupList, recoveryTarget.GetBackupID())
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
	backups []WalgBackupMetadata,
	targetLSNString string,
	targetTLI string,
) (*WalgBackupMetadata, error) {
	targetLSN := types.LSN(targetLSNString)
	if _, err := targetLSN.Parse(); err != nil {
		return nil, fmt.Errorf("while parsing recovery target targetLSN: %s", err.Error())
	}
	for i := len(backups) - 1; i >= 0; i-- {
		backup := backups[i]
		if backupHasMatchingTimeline(backup, targetTLI) && types.Int64ToLSN(backup.FinishLSN).Less(targetLSN) {
			return &backup, nil
		}
	}
	return nil, nil
}

func findClosestBackupFromTargetTime(backups []WalgBackupMetadata, targetTime string, targetTLI string) (*WalgBackupMetadata, error) {
	target, err := types.ParseTargetTime(nil, targetTime)
	if err != nil {
		return nil, fmt.Errorf("while parsing recovery target targetTime: %s", err.Error())
	}
	for i := len(backups) - 1; i >= 0; i-- {
		backup := backups[i]
		backupFinishTime, err := backup.FinishTime()
		if err != nil {
			continue
		}
		if backupHasMatchingTimeline(backup, targetTLI) && !backupFinishTime.After(target) {
			return &backup, nil
		}
	}
	return nil, nil
}

func findLatestBackupFromTimeline(backups []WalgBackupMetadata, targetTLI string) *WalgBackupMetadata {
	for i := len(backups) - 1; i >= 0; i-- {
		backup := backups[i]
		if backupHasMatchingTimeline(backup, targetTLI) {
			return &backup
		}
	}
	return nil
}

func backupHasMatchingTimeline(backup WalgBackupMetadata, targetTimeline string) bool {
	if targetTimeline == "" || targetTimeline == "latest" {
		return true // Any timeline will match, if we do not specify targetTimeline
	}

	targetTimelineId, err := strconv.ParseInt(targetTimeline, 16, 64)
	if err != nil {
		return true // Passed incorrect timeline - allowing any timeline
	}

	return backup.Timeline() <= int(targetTimelineId)
}
