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
	"sync"

	"github.com/cloudnative-pg/cnpg-i/pkg/wal"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	v1beta1 "github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"github.com/wal-g/cnpg-plugin-wal-g/pkg/walg"
)

var (
	lastArchivedWALTime   *v1.Time // IMPORTANT: Do not access directly, use only thread-safe methods SetLastArchivedWALTime/GetLastArchivedWALTime
	lastArchivedWALTimeMu sync.Mutex
)

// WALServiceImplementation is the implementation of the WAL Service
type WALServiceImplementation struct {
	wal.UnimplementedWALServer
	Client client.Client
}

// GetCapabilities implements the WALService interface
func (w WALServiceImplementation) GetCapabilities(
	_ context.Context,
	_ *wal.WALCapabilitiesRequest,
) (*wal.WALCapabilitiesResult, error) {
	return &wal.WALCapabilitiesResult{
		Capabilities: []*wal.WALCapability{
			{
				Type: &wal.WALCapability_Rpc{
					Rpc: &wal.WALCapability_RPC{
						Type: wal.WALCapability_RPC_TYPE_ARCHIVE_WAL,
					},
				},
			},
			{
				Type: &wal.WALCapability_Rpc{
					Rpc: &wal.WALCapability_RPC{
						Type: wal.WALCapability_RPC_TYPE_RESTORE_WAL,
					},
				},
			},
		},
	}, nil
}

// Archive implements the WALService interface
func (w WALServiceImplementation) Archive(
	ctx context.Context,
	request *wal.WALArchiveRequest,
) (*wal.WALArchiveResult, error) {
	if viper.GetString("mode") != common.PluginModeNormal {
		return nil, fmt.Errorf("WAL archivation restricted while running not in 'normal' mode")
	}

	pgMajorVersion := viper.GetInt("pg_major")
	if pgMajorVersion == 0 {
		return nil, fmt.Errorf("backup request failed: no PG_MAJOR env variable specified")
	}

	logger := logr.FromContextOrDiscard(ctx).WithName("plugin_wal").WithValues("method", "Archive")
	childrenCtx := logr.NewContext(ctx, logger)

	cluster, err := common.CnpgClusterFromJSON(request.ClusterDefinition)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cluser: %w", err)
	}

	backupConfig, err := w.getBackupConfig(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get backup config: %w", err)
	}

	walgClient := walg.NewClientFromBackupConfig(backupConfig, pgMajorVersion)
	result, err := walgClient.WALPush(childrenCtx, request.SourceFileName)

	logger = logger.WithValues("stdout", string(result.Stdout()), "stderr", string(result.Stderr()))

	if err != nil {
		return nil, fmt.Errorf(
			"'wal-g wal-push' for wal %s: %w\nstdout: %s\n stderr: %s",
			request.SourceFileName, err, string(result.Stdout()), string(result.Stderr()),
		)
	}

	now := v1.Now()
	SetLastArchivedWALTime(&now)

	logger.Info(fmt.Sprintf("Successful run wal-g wal-push %s", request.SourceFileName))
	return &wal.WALArchiveResult{}, nil
}

// Restore implements the WALService interface
func (w WALServiceImplementation) Restore(
	ctx context.Context,
	request *wal.WALRestoreRequest,
) (*wal.WALRestoreResult, error) {
	logger := logr.FromContextOrDiscard(ctx).WithName("plugin_wal").WithValues("method", "Restore")
	childrenCtx := logr.NewContext(ctx, logger)

	pgMajorVersion := viper.GetInt("pg_major")
	if pgMajorVersion == 0 {
		return nil, fmt.Errorf("backup request failed: no PG_MAJOR env variable specified")
	}

	cluster, err := common.CnpgClusterFromJSON(request.ClusterDefinition)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cluser: %w", err)
	}

	backupConfig, err := w.getBackupConfig(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get backup config: %w", err)
	}

	walgClient := walg.NewClientFromBackupConfig(backupConfig, pgMajorVersion)
	result, err := walgClient.WALFetch(childrenCtx, request.SourceWalName, request.DestinationFileName)

	logger = logger.WithValues("stdout", string(result.Stdout()), "stderr", string(result.Stderr()))

	if err != nil && result.State().ExitCode() == 74 {
		logger.Info(fmt.Sprintf("Run wal-g fetch %s %s: no WAL archive found in storage", request.SourceWalName, request.DestinationFileName))
		return &wal.WALRestoreResult{}, err
	}
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to run wal-g fetch %s %s", request.SourceWalName, request.DestinationFileName))
		return &wal.WALRestoreResult{}, err
	}
	logger.Info(fmt.Sprintf("Successful run wal-g fetch %s %s", request.SourceWalName, request.DestinationFileName))
	return &wal.WALRestoreResult{}, nil
}

func (w WALServiceImplementation) getBackupConfig(ctx context.Context, cluster *cnpgv1.Cluster) (*v1beta1.BackupConfigWithSecrets, error) {
	var backupConfig *v1beta1.BackupConfig
	var err error

	if viper.GetString("mode") == common.PluginModeRecovery {
		backupConfig, err = v1beta1.GetBackupConfigForClusterRecovery(ctx, w.Client, cluster)
	} else {
		backupConfig, err = v1beta1.GetBackupConfigForCluster(ctx, w.Client, cluster)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to fetch BackupConfig object: %w", err)
	}

	backupConfigWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, w.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch BackupConfig secrets: %w", err)
	}

	return backupConfigWithSecrets, nil
}

// Status implements the WALService interface
func (w WALServiceImplementation) Status(
	_ context.Context,
	_ *wal.WALStatusRequest,
) (*wal.WALStatusResult, error) {
	panic("implement me")
}

// SetFirstRequired implements the WALService interface
func (w WALServiceImplementation) SetFirstRequired(
	_ context.Context,
	_ *wal.SetFirstRequiredRequest,
) (*wal.SetFirstRequiredResult, error) {
	panic("implement me")
}

// GetLastArchivedWALTime returns time of last successfully uploaded WAL thread-safely
func GetLastArchivedWALTime() *v1.Time {
	lastArchivedWALTimeMu.Lock()
	defer lastArchivedWALTimeMu.Unlock()

	if lastArchivedWALTime == nil {
		return nil
	}

	return lastArchivedWALTime.DeepCopy()
}

// SetLastArchivedWALTime sets the time of last successfully uploaded WAL thread-safely
func SetLastArchivedWALTime(time *v1.Time) {
	lastArchivedWALTimeMu.Lock()
	defer lastArchivedWALTimeMu.Unlock()

	lastArchivedWALTime = time
}
