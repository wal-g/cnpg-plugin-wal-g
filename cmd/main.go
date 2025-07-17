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

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/wal-g/cnpg-plugin-wal-g/internal/cmd"
)

func main() {
	cobra.EnableTraverseRunHooks = true

	// logFlags := &log.Flags{}
	rootCmd := &cobra.Command{
		Use: "cnpg-plugin-wal-g [cmd]",
	}

	_ = viper.BindEnv("namespace", "NAMESPACE")       // K8s namespace where instance is running
	_ = viper.BindEnv("cluster-name", "CLUSTER_NAME") // Name of CNPG Cluster resource
	_ = viper.BindEnv("pod-name", "POD_NAME")         // Name of current pod
	_ = viper.BindEnv("pgdata", "PGDATA")             // Path to pgdata folder
	_ = viper.BindEnv("pg_major", "PG_MAJOR")         // Postgresql major version

	_ = viper.BindEnv("zap-devel", "DEBUG")
	_ = viper.BindEnv("zap-log-level", "LOG_LEVEL")
	_ = viper.BindEnv("zap-stacktrace-level", "LOG_STACKTRACE_LEVEL")

	// logFlags.AddFlags(rootrootCmd.PersistentFlags())
	rootCmd.AddCommand(
		cmd.NewInstanceSubcommand(),
		cmd.NewOperatorCmd(),
		cmd.NewDumpConfigCmd(),
	)

	if err := rootCmd.ExecuteContext(ctrl.SetupSignalHandler()); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Println(err)
			os.Exit(1)
		}
	}
}
