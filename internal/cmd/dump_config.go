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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/walg"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// NewDumpConfigCmd creates a new command for dumping wal-g configuration
func NewDumpConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-config [namespace/]BackupConfigName <pgVersion>",
		Short: "Dumps BackupConfig wal-g configuration to file. Namespace is optional, using default if not specified",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("provide 2 arguments - BackupConfig resource name and postgresql major version number")
			}

			viper.Set("backup-config", args[0])
			viper.Set("pgversion", args[1])

			if viper.GetInt("pgversion") < 10 {
				return fmt.Errorf(
					"unsupported pg version passed: %d, should be single number",
					viper.GetInt("pgversion"),
				)
			}

			scheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(scheme))
			utilruntime.Must(cnpgv1.AddToScheme(scheme))
			utilruntime.Must(v1beta1.AddToScheme(scheme))

			// Create a Kubernetes client
			k8sConfig, err := config.GetConfig()
			if err != nil {
				return fmt.Errorf("failed to get Kubernetes config: %w", err)
			}

			k8sClient, err := client.New(k8sConfig, client.Options{
				Scheme: scheme,
			})
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			return runDumpConfig(cmd.Context(), k8sClient)
		},
	}

	cmd.Flags().StringP("output", "o", "walg.json", "Output file path")
	_ = viper.BindPFlag("output", cmd.Flags().Lookup("output"))

	return cmd
}

func runDumpConfig(ctx context.Context, client client.Client) error {
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
		outputPath = "walg.json"
	}

	// Get the BackupConfig
	backupConfig := &v1beta1.BackupConfig{}
	err := client.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      backupConfigName,
	}, backupConfig)
	if err != nil {
		return fmt.Errorf("failed to get BackupConfig %s/%s: %w", namespace, backupConfigName, err)
	}

	// Get the BackupConfig with secrets
	backupConfigWithSecrets, err := backupConfig.PrefetchSecretsData(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to prefetch secrets for BackupConfig %s/%s: %w", namespace, backupConfigName, err)
	}

	// Create a Config object
	config := walg.NewConfigFromBackupConfig(backupConfigWithSecrets, viper.GetInt("pgversion"))

	// Write the Config to a file
	err = config.ToFile(outputPath)
	if err != nil {
		return fmt.Errorf("failed to write config to file %s: %w", outputPath, err)
	}

	fmt.Printf(
		"WAL-G configuration from BackupConfig '%s/%s' written to file %s\n",
		namespace, backupConfigName, outputPath,
	)
	fmt.Printf("Use it like this: 'wal-g --config %s backup-list'\n", outputPath)
	return nil
}
