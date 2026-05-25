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
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgbackup "github.com/cloudnative-pg/cnpg-i/pkg/backup"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"github.com/cloudnative-pg/machinery/pkg/types"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"github.com/spf13/viper"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"github.com/wal-g/cnpg-plugin-wal-g/pkg/walg"
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

	pgMajorVersion := viper.GetInt("pg_major")
	if pgMajorVersion == 0 {
		return nil, fmt.Errorf("backup request failed: no PG_MAJOR env variable specified")
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

	walgClient := walg.NewClientFromBackupConfig(backupConfigWithSecrets, pgMajorVersion)

	backupsListCtx, cancelBackupsListCtx := context.WithTimeout(ctx, 1*time.Minute)
	defer cancelBackupsListCtx()
	backupsList, err := walgClient.GetBackupsList(backupsListCtx)
	if err != nil {
		logger.Error(err, "Failed to create backup: failed to list existing backups")
		return nil, fmt.Errorf("failed to create backup: failed to list existing backups: %w", err)
	}

	// Get current LSN and currentSystemID from local PostgreSQL instance
	currentLSN, currentSystemID, err := b.getCurrentLSNAndSystemID(ctx)
	if err != nil {
		logger.Error(err, "Failed to get current LSN and system ID from PostgreSQL")
		return nil, fmt.Errorf("failed to get current LSN and system ID: %w", err)
	}
	logger.Info("Retrieved current LSN and system ID", "lsn", currentLSN, "systemID", currentSystemID)

	hasBackupWithCurrentLSN := false
	hasBackupWithLaterLSN := false
	hasBackupWithAnotherSystemID := false

	lo.ForEach(backupsList, func(b walg.BackupMetadata, _ int) {
		if b.SystemIdentifier != currentSystemID {
			hasBackupWithAnotherSystemID = true
		}
		if b.FinishLSN == currentLSN {
			hasBackupWithCurrentLSN = true
		}
		if b.FinishLSN > currentLSN {
			hasBackupWithLaterLSN = true
		}
	})

	if hasBackupWithAnotherSystemID {
		return nil, fmt.Errorf("backups with another system ID detected, this usually means that BackupConfig is being used by another database, which is forbidden")
	}

	if hasBackupWithCurrentLSN {
		return nil, fmt.Errorf("no changes in database detected since previous backup, cannot create new backup")
	}

	if hasBackupWithLaterLSN {
		return nil, fmt.Errorf("there are backups with later LSN, this usually means that we are creating backup with a very outdated replica")
	}

	result, err := walgClient.BackupPush(logr.NewContext(ctx, logger), pgdata, string(backupParamsJSON))

	if err != nil {
		logger.Error(err, "Error on wal-g backup-push", "stdout", string(result.Stdout()), "stderr", string(result.Stderr()))
		return nil, fmt.Errorf("failed to do wal-g backup-push: %w (stderr: %s)", err, result.Stderr())
	}
	logger.Info("Finished wal-g backup-push", "stdout", string(result.Stdout()), "stderr", string(result.Stderr()))
	return b.buildBackupResult(ctx, backupHumanName, backupConfigWithSecrets, pgMajorVersion, backupParams)
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
	pgMajorVersion int,
	backupParams map[string]any,
) (*cnpgbackup.BackupResult, error) {
	walgClient := walg.NewClientFromBackupConfig(config, pgMajorVersion)
	walgBackupList, err := walgClient.GetBackupsList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list wal-g backups: %w", err)
	}

	currentBackupMetadata, err := walgClient.GetBackupByUserData(ctx, walgBackupList, backupParams)
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

// getCurrentLSNAndSystemID connects to the local PostgreSQL instance and retrieves
// the current LSN and system identifier
func (b BackupServiceImplementation) getCurrentLSNAndSystemID(ctx context.Context) (uint64, int, error) {
	// Connect to PostgreSQL via Unix socket as postgres user
	connStr := "host=/controller/run user=postgres dbname=postgres sslmode=disable"

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open database connection: %w", err)
	}
	defer db.Close()

	// Set connection timeout
	db.SetConnMaxLifetime(10 * time.Second)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		return 0, 0, fmt.Errorf("failed to ping database: %w", err)
	}

	// Query for current LSN and system identifier
	// Use pg_last_wal_replay_lsn() for replicas (during recovery) and pg_current_wal_lsn() for primary
	var lsnStr string
	var systemID int64

	query := `SELECT
		CASE
			WHEN pg_is_in_recovery() THEN pg_last_wal_replay_lsn()::text
			ELSE pg_current_wal_lsn()::text
		END AS current_lsn,
		system_identifier
	FROM pg_control_system()`

	err = db.QueryRowContext(ctx, query).Scan(&lsnStr, &systemID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query LSN and system ID: %w", err)
	}

	// Convert LSN string to uint64
	lsn, err := types.LSN(lsnStr).Parse()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse LSN %q: %w", lsnStr, err)
	}

	return lsn, int(systemID), nil
}
