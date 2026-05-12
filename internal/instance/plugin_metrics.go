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
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cnpg-i/pkg/metrics"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
)

// Sanitize the plugin name to be a valid Prometheus metric namespace
var metricsDomain = strings.NewReplacer(".", "_", "-", "_").Replace(common.PluginName)

type MetricsServerImplementation struct {
	// important the client should be one with a underlying cache
	Client client.Client
	metrics.UnimplementedMetricsServer
}

func buildFqName(name string) string {
	// Build the fully qualified name for the metric
	return fmt.Sprintf("%s_%s", metricsDomain, strings.NewReplacer(".", "_", "-", "_").Replace(name))
}

var (
	firstRecoverabilityPointMetricName     = buildFqName("first_recoverability_point")
	lastAvailableBackupTimestampMetricName = buildFqName("last_available_backup_timestamp")
	lastFailedBackupTimestampMetricName    = buildFqName("last_failed_backup_timestamp")
	lastArchivedWALTimestampMetricName     = buildFqName("last_archived_wal_timestamp")
	totalWALS3UsageBytesMetricName         = buildFqName("total_wal_s3_usage_bytes")
	totalBackupsS3UsageBytesMetricName     = buildFqName("total_backups_s3_usage_bytes")
	s3ReadAvailabilityMetricName           = buildFqName("s3_read_availability")
	s3WriteAvailabilityMetricName          = buildFqName("s3_write_availability")
)

func (m MetricsServerImplementation) GetCapabilities(
	ctx context.Context,
	_ *metrics.MetricsCapabilitiesRequest,
) (*metrics.MetricsCapabilitiesResult, error) {
	return &metrics.MetricsCapabilitiesResult{
		Capabilities: []*metrics.MetricsCapability{
			{
				Type: &metrics.MetricsCapability_Rpc{
					Rpc: &metrics.MetricsCapability_RPC{
						Type: metrics.MetricsCapability_RPC_TYPE_METRICS,
					},
				},
			},
		},
	}, nil
}

func (m MetricsServerImplementation) Define(
	ctx context.Context,
	_ *metrics.DefineMetricsRequest,
) (*metrics.DefineMetricsResult, error) {
	return &metrics.DefineMetricsResult{
		Metrics: []*metrics.Metric{
			{
				FqName:    firstRecoverabilityPointMetricName,
				Help:      "The first point of recoverability for the cluster as a unix timestamp",
				ValueType: &metrics.MetricType{Type: metrics.MetricType_TYPE_GAUGE},
			},
			{
				FqName:    lastAvailableBackupTimestampMetricName,
				Help:      "The last available backup as a unix timestamp",
				ValueType: &metrics.MetricType{Type: metrics.MetricType_TYPE_GAUGE},
			},
			{
				FqName:    lastFailedBackupTimestampMetricName,
				Help:      "The last failed backup as a unix timestamp",
				ValueType: &metrics.MetricType{Type: metrics.MetricType_TYPE_GAUGE},
			},
			{
				FqName:    lastArchivedWALTimestampMetricName,
				Help:      "The last successfully archived WAL as a unix timestamp",
				ValueType: &metrics.MetricType{Type: metrics.MetricType_TYPE_GAUGE},
			},
			{
				FqName:    totalWALS3UsageBytesMetricName,
				Help:      "Total space consumed in S3 by WAL archives in bytes",
				ValueType: &metrics.MetricType{Type: metrics.MetricType_TYPE_GAUGE},
			},
			{
				FqName:    totalBackupsS3UsageBytesMetricName,
				Help:      "Total space consumed in S3 by basebackups in bytes",
				ValueType: &metrics.MetricType{Type: metrics.MetricType_TYPE_GAUGE},
			},
			{
				FqName:    s3ReadAvailabilityMetricName,
				Help:      "Remote object storage read availability (0 meaning false, 1 - true)",
				ValueType: &metrics.MetricType{Type: metrics.MetricType_TYPE_GAUGE},
			},
			{
				FqName:    s3WriteAvailabilityMetricName,
				Help:      "Remote object storage write availability (0 meaning false, 1 - true)",
				ValueType: &metrics.MetricType{Type: metrics.MetricType_TYPE_GAUGE},
			},
		},
	}, nil
}

func (m MetricsServerImplementation) Collect(
	ctx context.Context,
	req *metrics.CollectMetricsRequest,
) (*metrics.CollectMetricsResult, error) {
	cluster, err := common.CnpgClusterFromJSON(req.ClusterDefinition)
	if err != nil {
		return nil, fmt.Errorf("while creating configuration from cluster definition: %w", err)
	}

	backupConfig, err := v1beta1.GetBackupConfigForCluster(ctx, m.Client, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch BackupConfig object: %w", err)
	}

	var firstRecoverabilityPointTS float64
	if backupConfig.Status.FirstRecoverabilityPoint != nil {
		firstRecoverabilityPointTS = float64(backupConfig.Status.FirstRecoverabilityPoint.Unix())
	}
	var lastSuccessfulBackupTS float64
	if backupConfig.Status.LastSuccessfulBackup != nil {
		lastSuccessfulBackupTS = float64(backupConfig.Status.FirstRecoverabilityPoint.Unix())
	}
	var lastFailedBackupTS float64
	if backupConfig.Status.LastFailedBackup != nil {
		lastFailedBackupTS = float64(backupConfig.Status.LastFailedBackup.Unix())
	}

	var lastArchivedWALTimeTS float64
	walTime := GetLastArchivedWALTime()
	if walTime != nil {
		lastArchivedWALTimeTS = float64(walTime.Unix())
	}

	var totalWALS3UsageBytes float64
	if backupConfig.Status.ConsumedStorage.WALBytes != nil {
		totalWALS3UsageBytes = float64(*backupConfig.Status.ConsumedStorage.WALBytes)
	}
	var totalBackupS3UsageBytes float64
	if backupConfig.Status.ConsumedStorage.BackupsBytes != nil {
		totalBackupS3UsageBytes = float64(*backupConfig.Status.ConsumedStorage.BackupsBytes)
	}

	var s3ReadAvailability float64
	if cond := meta.FindStatusCondition(backupConfig.Status.Conditions, v1beta1.ConditionTypeStorageReadable); cond != nil {
		if cond.Status == v1.ConditionTrue {
			s3ReadAvailability = 1
		}
	}

	var s3WriteAvailability float64
	if cond := meta.FindStatusCondition(backupConfig.Status.Conditions, v1beta1.ConditionTypeStorageWritable); cond != nil {
		if cond.Status == v1.ConditionTrue {
			s3WriteAvailability = 1
		}
	}

	return &metrics.CollectMetricsResult{
		Metrics: []*metrics.CollectMetric{
			{
				FqName: firstRecoverabilityPointMetricName,
				Value:  firstRecoverabilityPointTS,
			},
			{
				FqName: lastAvailableBackupTimestampMetricName,
				Value:  lastSuccessfulBackupTS,
			},
			{
				FqName: lastFailedBackupTimestampMetricName,
				Value:  lastFailedBackupTS,
			},
			{
				FqName: lastArchivedWALTimestampMetricName,
				Value:  lastArchivedWALTimeTS,
			},
			{
				FqName: totalWALS3UsageBytesMetricName,
				Value:  totalWALS3UsageBytes,
			},
			{
				FqName: totalBackupsS3UsageBytesMetricName,
				Value:  totalBackupS3UsageBytes,
			},
			{
				FqName: s3ReadAvailabilityMetricName,
				Value:  s3ReadAvailability,
			},
			{
				FqName: s3WriteAvailabilityMetricName,
				Value:  s3WriteAvailability,
			},
		},
	}, nil
}
