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
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wal-g/cnpg-plugin-wal-g/internal/operator"
)

func NewOperatorCmd() *cobra.Command {
	cobra.EnableTraverseRunHooks = true

	// logFlags := &log.Flags{}
	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Starts the CNPG Yandex Extensions operator reconciler and the CNPG-i plugin",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return operator.Start(cmd.Context())
		},
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}

	cmd.Flags().String("metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	_ = viper.BindPFlag("metrics-bind-address", cmd.Flags().Lookup("metrics-bind-address"))

	cmd.Flags().String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	_ = viper.BindPFlag("health-probe-bind-address", cmd.Flags().Lookup("health-probe-bind-address"))

	cmd.Flags().Bool("leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	_ = viper.BindPFlag("leader-elect", cmd.Flags().Lookup("leader-elect"))

	cmd.Flags().Bool("metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	_ = viper.BindPFlag("metrics-secure", cmd.Flags().Lookup("metrics-secure"))

	cmd.Flags().String("webhook-cert-path", "", "The directory that contains the webhook certificate.")
	_ = viper.BindPFlag("webhook-cert-path", cmd.Flags().Lookup("webhook-cert-path"))

	cmd.Flags().String("webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	_ = viper.BindPFlag("webhook-cert-name", cmd.Flags().Lookup("webhook-cert-path"))

	cmd.Flags().String("webhook-cert-key", "tls.key", "The name of the webhook key file.")
	_ = viper.BindPFlag("webhook-cert-key", cmd.Flags().Lookup("webhook-cert-path"))

	cmd.Flags().String("metrics-cert-path", "", "The directory that contains the metrics server certificate.")
	_ = viper.BindPFlag("metrics-cert-path", cmd.Flags().Lookup("webhook-cert-path"))

	cmd.Flags().String("metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	_ = viper.BindPFlag("metrics-cert-name", cmd.Flags().Lookup("webhook-cert-path"))

	cmd.Flags().String("metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	_ = viper.BindPFlag("metrics-cert-key", cmd.Flags().Lookup("webhook-cert-path"))

	cmd.Flags().Bool("enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	_ = viper.BindPFlag("enable-http2", cmd.Flags().Lookup("enable-http2"))

	cmd.Flags().String("cnpg-i-bind-address", ":9090", "The address the CNPG-I GRPC server binds to")
	_ = viper.BindPFlag("cnpg-i-bind-address", cmd.Flags().Lookup("cnpg-i-bind-address"))

	cmd.Flags().String("cnpg-i-pg-sidecar-image", "cnpg-plugin-wal-g-instance-plugin:latest",
		"Instance plugin image (with tag) to inject into postgres pod spec as a sidecar)")
	_ = viper.BindPFlag("cnpg-i-pg-sidecar-image", cmd.Flags().Lookup("cnpg-i-pg-sidecar-image"))

	cmd.Flags().String("cnpg-i-cert-path", "", "The directory that contains the CNPG-I GRPC server certificate.")
	_ = viper.BindPFlag("cnpg-i-cert-path", cmd.Flags().Lookup("cnpg-i-cert-path"))

	cmd.Flags().String("cnpg-i-cert-name", "tls.crt", "The name of the CNPG-I GRPC server certificate file.")
	_ = viper.BindPFlag("cnpg-i-cert-name", cmd.Flags().Lookup("cnpg-i-cert-name"))

	cmd.Flags().String("cnpg-i-cert-key", "tls.key", "The name of the CNPG-I GRPC server key file.")
	_ = viper.BindPFlag("cnpg-i-cert-key", cmd.Flags().Lookup("cnpg-i-cert-key"))

	cmd.Flags().String("cnpg-i-client-cert-path", "",
		"The directory that contains the CNPG-I GRPC client certificate.")
	_ = viper.BindPFlag("cnpg-i-client-cert-path", cmd.Flags().Lookup("cnpg-i-client-cert-path"))

	cmd.Flags().String("cnpg-i-client-cert-name", "tls.crt",
		"The name of the CNPG-I GRPC client certificate file.")
	_ = viper.BindPFlag("cnpg-i-client-cert-name", cmd.Flags().Lookup("cnpg-i-client-cert-name"))

	return cmd
}
