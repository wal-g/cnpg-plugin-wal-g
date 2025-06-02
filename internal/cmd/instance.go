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
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/instance"
)

func NewInstanceSubcommand() *cobra.Command {
	// logFlags := &log.Flags{}
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Starts the CNPG Yandex Extentions instance-plugin acting as a sidecar for PostgreSQL instances",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return instance.Start(cmd.Context())
		},
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			mode := viper.GetString("mode")
			if !slices.Contains(common.AllowedPluginModes, mode) {
				return fmt.Errorf("incorrect mode parameter: %s, should be one of 'normal', 'recovery'", mode)
			}
			return nil
		},
	}

	cmd.Flags().String("mode", "normal", "instance plugin mode: 'normal' (use standard settings for backups and WAL fetch/store) or 'recovery' (use recovery settings for backups and WAL fetching)")
	_ = viper.BindPFlag("mode", cmd.Flags().Lookup("mode"))

	cmd.Flags().String("cnpg-i-socket-path", "/plugins/cnpg-extensions.yandex.cloud",
		"CNPG-I GRPC server socket path (default /plugins/cnpg-extensions.yandex.cloud)")
	_ = viper.BindPFlag("cnpg-i-socket-path", cmd.Flags().Lookup("cnpg-i-socket-path"))

	return cmd
}
