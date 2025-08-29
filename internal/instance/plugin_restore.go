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
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	restore "github.com/cloudnative-pg/cnpg-i/pkg/restore/job"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
)

const (
	// ScratchDataDirectory is the directory to be used for scratch data
	ScratchDataDirectory = "/controller"

	// RecoveryTemporaryDirectory provides a path to store temporary files
	// needed in the recovery process
	RecoveryTemporaryDirectory = ScratchDataDirectory + "/recovery"
)

// RestoreJobHooksImpl is the implementation of the restore job hooks
type RestoreJobHooksImpl struct {
	restore.UnimplementedRestoreJobHooksServer

	Client client.Client
}

// GetCapabilities returns the capabilities of the restore job hooks
func (r RestoreJobHooksImpl) GetCapabilities(
	_ context.Context,
	_ *restore.RestoreJobHooksCapabilitiesRequest,
) (*restore.RestoreJobHooksCapabilitiesResult, error) {
	return &restore.RestoreJobHooksCapabilitiesResult{
		Capabilities: []*restore.RestoreJobHooksCapability{
			{
				Kind: restore.RestoreJobHooksCapability_KIND_RESTORE,
			},
		},
	}, nil
}

// Restore restores the cluster from a backup
func (r RestoreJobHooksImpl) Restore(
	ctx context.Context,
	req *restore.RestoreRequest,
) (*restore.RestoreResponse, error) {
	logger := logr.FromContextOrDiscard(ctx).WithName("plugin_restore").WithValues("method", "Restore")

	cluster, err := common.CnpgClusterFromJSON(req.ClusterDefinition)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cnpg cluster from json: %w", err)
	}

	restoreConfig, err := v1beta1.GetBackupConfigForClusterRecovery(ctx, r.Client, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch BackupConfig object: %w", err)
	}

	restoreConfigWithSecrets, err := restoreConfig.PrefetchSecretsData(ctx, r.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch BackupConfig secrets: %w", err)
	}

	pgdata := viper.GetString("pgdata")
	if pgdata == "" {
		return nil, fmt.Errorf("no PGDATA env variable specified")
	}

	pgMajorVersion := viper.GetInt("pg_major")
	if pgMajorVersion == 0 {
		return nil, fmt.Errorf("backup request failed: no PG_MAJOR env variable specified")
	}

	recoveryPluginConfig := common.GetRecoveryPluginConfigFromCluster(cluster)
	if recoveryPluginConfig == nil {
		return nil, fmt.Errorf("failed to get recovery plugin config: %w", err)
	}
	if recoveryPluginConfig.Name != common.PluginMetadata.Name {
		return nil, fmt.Errorf(
			"incorrect externalCluster plugin name %s, expected %s",
			recoveryPluginConfig.Name,
			common.PluginMetadata.Name,
		)
	}

	walgBackupList, err := walg.GetBackupsList(ctx, restoreConfigWithSecrets, pgMajorVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to list wal-g backups: %w", err)
	}

	var walgBackup *walg.BackupMetadata
	if cluster.Spec.Bootstrap.Recovery.RecoveryTarget == nil {
		walgBackup, err = walg.GetLatestBackup(ctx, walgBackupList)
	} else {
		walgBackup, err = walg.FindMostSuitableBackupForRecovery(ctx, walgBackupList, *cluster.Spec.Bootstrap.Recovery.RecoveryTarget)
	}

	if err != nil || walgBackup == nil {
		return nil, fmt.Errorf("cannot find appropriate basebackup to restore from: %w", err)
	}
	backupName := walgBackup.BackupName

	logger.Info("Starting restore backup into data directory", "backupName", backupName, "pgdata", pgdata)
	err = r.downloadBackupIntoDir(ctx, restoreConfigWithSecrets, backupName, pgdata)
	if err != nil {
		logger.Error(err, "Failed restore backup into data directory", "backupName", backupName, "pgdata", pgdata)
		return nil, err
	}

	logger.Info("Successful restore backup into data directory", "backupName", backupName, "pgdata", pgdata)
	return &restore.RestoreResponse{
		RestoreConfig: getPostgresRestoreWalConfig(),
		Envs:          nil,
	}, nil
}

// downloadBackupIntoDir restores PGDATA from an existing backup
func (r RestoreJobHooksImpl) downloadBackupIntoDir(
	ctx context.Context,
	config *v1beta1.BackupConfigWithSecrets,
	walgBackupName string,
	targetDir string,
) error {
	logger := logr.FromContextOrDiscard(ctx)
	logger.Info("Starting downloadBackupIntoDir with wal-g")

	pgMajorVersion := viper.GetInt("pg_major")
	if pgMajorVersion == 0 {
		return fmt.Errorf("backup request failed: no PG_MAJOR env variable specified")
	}

	result, err := cmd.New("wal-g", "backup-fetch", "--turbo", targetDir, walgBackupName).
		WithContext(ctx).
		WithEnv(walg.NewConfigFromBackupConfig(config, pgMajorVersion).ToEnvMap()).
		Run()

	if err != nil {
		logger.Error(err, "Error on wal-g backup-fetch", "stdout", string(result.Stdout()), "stderr", string(result.Stderr()))
		return fmt.Errorf("failed to do wal-g backup-fetch: %w", err)
	}
	logger.Info("Finished wal-g backup-fetch", "stdout", string(result.Stdout()), "stderr", string(result.Stderr()))
	logger.Info("downloadBackupIntoDir completed")
	return nil
}

// getPostgresRestoreWalConfig obtains the content to append to `custom.conf` allowing PostgreSQL
// to complete the WAL recovery from the object storage and then start
// as a new primary
func getPostgresRestoreWalConfig() string {
	restoreCmd := fmt.Sprintf(
		"/controller/manager wal-restore --log-destination %s/%s.json %%f %%p",
		postgres.LogPath, postgres.LogFileName)

	recoveryFileContents := fmt.Sprintf(
		"recovery_target_action = promote\n"+
			"restore_command = '%s'\n",
		restoreCmd)

	return recoveryFileContents
}
