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

package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgbackup "github.com/cloudnative-pg/cnpg-i/pkg/backup"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"github.com/cloudnative-pg/machinery/pkg/types"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BackupServiceImplementation is the implementation
// of the Backup CNPG capability
type BackupServiceImplementation struct {
	cnpgbackup.UnimplementedBackupServer
	Client client.Client
}

// GetCapabilities implements the BackupService interface
func (b BackupServiceImplementation) GetCapabilities(
	_ context.Context, _ *cnpgbackup.BackupCapabilitiesRequest,
) (*cnpgbackup.BackupCapabilitiesResult, error) {
	return &cnpgbackup.BackupCapabilitiesResult{
		Capabilities: []*cnpgbackup.BackupCapability{
			{
				Type: &cnpgbackup.BackupCapability_Rpc{
					Rpc: &cnpgbackup.BackupCapability_RPC{
						Type: cnpgbackup.BackupCapability_RPC_TYPE_BACKUP,
					},
				},
			},
		},
	}, nil
}

// Backup implements the Backup interface
func (b BackupServiceImplementation) Backup(
	ctx context.Context,
	request *cnpgbackup.BackupRequest,
) (*cnpgbackup.BackupResult, error) {
	logger := logr.FromContextOrDiscard(ctx).WithName("plugin_backup").WithValues("method", "Backup")
	logger.Info("Starting backup")

	pgdata := viper.GetString("pgdata")
	if pgdata == "" {
		return nil, fmt.Errorf("backup request failed: no PGDATA env variable specified")
	}

	backup, err := common.CnpgBackupFromJSON(request.BackupDefinition)
	if err != nil {
		return nil, fmt.Errorf("backup request failed: cannot parse cnpg backup: %w", err)
	}

	cluster, err := common.CnpgClusterFromJSON(request.ClusterDefinition)
	if err != nil {
		return nil, fmt.Errorf("backup request failed: cannot parse cnpg cluster: %w", err)
	}

	backupConfigWithSecrets, err := b.getBackupConfig(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("backup request failed: %w", err)
	}

	backupHumanName := fmt.Sprintf("backup-%v", pgTime.ToCompactISO8601(time.Now()))

	backupParams := map[string]any{
		"cnpg-backup-name":       backup.Name,
		"cnpg-backup-namespace":  backup.Namespace,
		"cnpg-backup-human-name": backupHumanName,
	}

	backupParamsJSON, err := json.Marshal(backupParams)
	if err != nil {
		return nil, fmt.Errorf("backup request failed: cannot marshal backup params: %w", err)
	}

	logger.WithValues("pgdata", pgdata, "user-data", string(backupParamsJSON))

	result, err := cmd.New("wal-g", "backup-push", pgdata, "--add-user-data", string(backupParamsJSON)).
		WithContext(logr.NewContext(ctx, logger)).
		WithEnv(walg.NewConfigFromBackupConfig(backupConfigWithSecrets).ToEnvMap()).
		Run()

	if err != nil {
		logger.Error(err, "Error on wal-g backup-push", "stdout", string(result.Stdout()), "stderr", string(result.Stderr()))
		return nil, fmt.Errorf("failed to do wal-g backup-push: %w", err)
	}
	logger.Info("Finished wal-g backup-push", "stdout", string(result.Stdout()), "stderr", string(result.Stderr()))
	return b.buildBackupResult(ctx, backupHumanName, backupConfigWithSecrets, backupParams)
}

func (b BackupServiceImplementation) getBackupConfig(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
) (*v1beta1.BackupConfigWithSecrets, error) {
	backupConfig, err := v1beta1.GetBackupConfigForCluster(ctx, b.Client, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch BackupConfig object: %w", err)
	}

	backupConfigWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, b.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch BackupConfig secrets: %w", err)
	}

	return backupConfigWithSecrets, nil
}

func (b BackupServiceImplementation) buildBackupResult(
	ctx context.Context,
	backupHumanName string,
	config *v1beta1.BackupConfigWithSecrets,
	backupParams map[string]any,
) (*cnpgbackup.BackupResult, error) {
	walgBackupList, err := walg.GetBackupsList(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to list wal-g backups: %w", err)
	}

	currentBackupMetadata, err := walg.GetBackupByUserData(ctx, walgBackupList, backupParams)
	if currentBackupMetadata == nil || err != nil {
		return nil, fmt.Errorf("cannot make backup metadata, medatata: %v err: %w", currentBackupMetadata, err)
	}

	backupStartTime, err := currentBackupMetadata.StartTime()
	if err != nil {
		return nil, fmt.Errorf("cannot parse wal-g backup start_time: %w", err)
	}

	backupFinishTime, err := currentBackupMetadata.FinishTime()
	if err != nil {
		return nil, fmt.Errorf("cannot parse wal-g backup finish_time: %w", err)
	}

	return &cnpgbackup.BackupResult{
		BackupId:   currentBackupMetadata.BackupName,
		BackupName: backupHumanName,
		StartedAt:  backupStartTime.Unix(),
		StoppedAt:  backupFinishTime.Unix(),
		BeginWal:   currentBackupMetadata.WalFileName,
		BeginLsn:   string(types.Int64ToLSN(currentBackupMetadata.StartLSN)),
		EndLsn:     string(types.Int64ToLSN(currentBackupMetadata.FinishLSN)),
		InstanceId: currentBackupMetadata.Hostname,
		Online:     true,
	}, nil
}
