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

package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// NewDumpConfigCmd creates a new command for dumping wal-g configuration
func NewDumpConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-config",
		Short: "Dumps wal-g configuration from a BackupConfig to a file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDumpConfig(cmd.Context())
		},
	}

	cmd.Flags().String("backup-config", "", "Name of the BackupConfig resource in format [namespace/]name (defaults to default namespace if not specified)")
	_ = viper.BindPFlag("backup-config", cmd.Flags().Lookup("backup-config"))
	_ = cmd.MarkFlagRequired("backup-config")

	cmd.Flags().String("output", "walg-config.json", "Output file path")
	_ = viper.BindPFlag("output", cmd.Flags().Lookup("output"))

	return cmd
}

func runDumpConfig(ctx context.Context) error {
	backupConfigParam := viper.GetString("backup-config")
	if backupConfigParam == "" {
		return fmt.Errorf("backup-config parameter is required")
	}

	// Parse namespace and name from the backup-config parameter
	namespace := "default"
	backupConfigName := backupConfigParam

	// If the parameter contains a slash, split it into namespace and name
	if strings.Contains(backupConfigParam, "/") {
		parts := strings.SplitN(backupConfigParam, "/", 2)
		if len(parts) == 2 && parts[0] != "" {
			namespace = parts[0]
			backupConfigName = parts[1]
		}
	}

	outputPath := viper.GetString("output")
	if outputPath == "" {
		outputPath = "walg-config.json"
	}

	// Create a Kubernetes client
	k8sConfig, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	k8sClient, err := client.New(k8sConfig, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Get the BackupConfig
	backupConfig := &v1beta1.BackupConfig{}
	err = k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      backupConfigName,
	}, backupConfig)
	if err != nil {
		return fmt.Errorf("failed to get BackupConfig %s/%s: %w", namespace, backupConfigName, err)
	}

	// Get the BackupConfig with secrets
	backupConfigWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("failed to prefetch secrets for BackupConfig %s/%s: %w", namespace, backupConfigName, err)
	}

	// Create a Config object
	config := walg.NewConfigFromBackupConfig(backupConfigWithSecrets)

	// Write the Config to a file
	err = config.ToFile(outputPath)
	if err != nil {
		return fmt.Errorf("failed to write config to file %s: %w", outputPath, err)
	}

	fmt.Printf("Successfully wrote wal-g configuration from BackupConfig %s to %s\n", backupConfigParam, outputPath)
	return nil
}
