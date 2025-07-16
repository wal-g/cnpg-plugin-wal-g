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

package v1beta1

import (
	"context"
	"fmt"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	common "github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const backupConfigNameParameter = "backupConfig"

// BackupConfigIsUsedForArchivation checks whether BackupConfig with specified name
// is used by CNPG Cluster as configuration for new backups && wal archive
func BackupConfigIsUsedForArchivation(backupConfigName types.NamespacedName, cluster *cnpgv1.Cluster) bool {
	if backupConfigName.Namespace != cluster.Namespace {
		return false
	}
	pluginConfig := common.GetPluginConfigFromCluster(cluster)
	return checkBackupNameMatchesPluginConfig(backupConfigName, pluginConfig)
}

// BackupConfigIsUsedForRecovery checks whether BackupConfig with specified name
// is used by CNPG Cluster as recovery source
func BackupConfigIsUsedForRecovery(backupConfigName types.NamespacedName, cluster *cnpgv1.Cluster) bool {
	if backupConfigName.Namespace != cluster.Namespace {
		return false
	}
	recoveryPluginConfig := common.GetRecoveryPluginConfigFromCluster(cluster)
	return checkBackupNameMatchesPluginConfig(backupConfigName, recoveryPluginConfig)
}

// GetBackupConfigForCluster returns BackupConfig object used for making backups
// If no BackupConfig reference specified in cluster plugin configuration - it will return (nil, nil)
// If BackupConfig reference specified, but couldn't fetch object - will return (nil, error)
func GetBackupConfigForCluster(ctx context.Context, c client.Client, cluster *cnpgv1.Cluster) (*BackupConfig, error) {
	pluginConfig := common.GetPluginConfigFromCluster(cluster)
	return getBackupConfigFromPluginConfig(ctx, c, pluginConfig, cluster.Namespace)
}

// GetBackupConfigForBackup fetches the BackupConfig for a given Backup
func GetBackupConfigForBackup(ctx context.Context, c client.Client, backup *cnpgv1.Backup) (*BackupConfig, error) {
	if backup == nil {
		return nil, fmt.Errorf("getBackupConfigForBackup: provided backup is nill")
	}

	backupConfigKey := types.NamespacedName{}

	for _, ownerRef := range backup.OwnerReferences {
		if ownerRef.Kind == "BackupConfig" && strings.HasPrefix(ownerRef.APIVersion, GroupVersion.Group) {
			backupConfigKey = types.NamespacedName{
				Namespace: backup.Namespace,
				Name:      ownerRef.Name,
			}
		}
	}

	if backupConfigKey.Name == "" {
		return nil, fmt.Errorf("no BackupConfig owner found for backup %v", client.ObjectKeyFromObject(backup))
	}

	backupConfig := &BackupConfig{}
	if err := c.Get(ctx, backupConfigKey, backupConfig); err != nil {
		return nil, fmt.Errorf(
			"while getting BackupConfig %v for backup %v: %w",
			backupConfigKey, client.ObjectKeyFromObject(backup), err,
		)
	}

	return backupConfig, nil
}

// GetBackupConfigWithSecretsForBackup fetches the BackupConfig with secrets for a given Backup
func GetBackupConfigWithSecretsForBackup(
	ctx context.Context,
	c client.Client,
	backup *cnpgv1.Backup,
) (*BackupConfigWithSecrets, error) {
	backupConfig, err := GetBackupConfigForBackup(ctx, c, backup)
	if err != nil {
		return nil, err
	}

	backupConfigWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("while prefetching backup secrets data: %w", err)
	}
	return backupConfigWithSecrets, nil
}

// GetBackupConfigForCluster returns BackupConfig object used for restoring from backups
// If no BackupConfig reference specified in cluster plugin configuration - it will return (nil, nil)
// If BackupConfig reference specified, but couldn't fetch object - will return (nil, error)
func GetBackupConfigForClusterRecovery(ctx context.Context, c client.Client, cluster *cnpgv1.Cluster) (*BackupConfig, error) {
	pluginConfig := common.GetRecoveryPluginConfigFromCluster(cluster)
	return getBackupConfigFromPluginConfig(ctx, c, pluginConfig, cluster.Namespace)
}

// GetBackupConfigEncryptionSecretName returns secret name
func GetBackupConfigEncryptionSecretName(b *BackupConfig) string {
	secretName := b.Spec.Encryption.ExistingEncryptionSecretName
	if secretName == "" {
		secretName = fmt.Sprintf("%s-encryption", b.Name)
	}
	return secretName
}

func checkBackupNameMatchesPluginConfig(
	backupConfigName types.NamespacedName,
	pluginConfig *cnpgv1.PluginConfiguration,
) bool {
	if pluginConfig == nil || pluginConfig.Parameters == nil {
		return false
	}

	usedConfigName, ok := pluginConfig.Parameters[backupConfigNameParameter]
	return ok && usedConfigName == backupConfigName.Name
}

func getBackupConfigFromPluginConfig(
	ctx context.Context,
	c client.Client,
	pluginConfig *cnpgv1.PluginConfiguration,
	namespace string,
) (*BackupConfig, error) {
	if pluginConfig == nil || pluginConfig.Parameters == nil {
		return nil, nil
	}

	backupConfigName, ok := pluginConfig.Parameters[backupConfigNameParameter]
	if !ok {
		return nil, fmt.Errorf("missing backupConfig parameter with BackupConfig resource name in plugin configuration")
	}

	backupConfig := &BackupConfig{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: backupConfigName}, backupConfig)
	if err != nil {
		return nil, err
	}

	return backupConfig, nil
}
