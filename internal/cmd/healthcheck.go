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
	"path"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
)

// pluginSocketDir is the directory the operator mounts into the instance pod,
// where the instance plugin serves CNPG-I on a socket named after the plugin.
const pluginSocketDir = "/plugins"

// NewHealthcheckCmd creates the command run by the instance sidecar's startup probe
func NewHealthcheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Checks whether the instance plugin is serving",
	}

	socket := path.Join(pluginSocketDir, common.PluginMetadata.Name)
	cmd.AddCommand(&cobra.Command{
		Use:   "unix",
		Short: fmt.Sprintf("Checks that the instance plugin answers on unix://%s", socket),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return checkPluginSocket(cmd.Context(), socket)
		},
	})

	return cmd
}

// checkPluginSocket returns nil once the plugin's gRPC server on the socket
// answers the identity service.
func checkPluginSocket(ctx context.Context, socket string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	conn, err := grpc.NewClient("unix://"+socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("while creating a client for %s: %w", socket, err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := identity.NewIdentityClient(conn).GetPluginMetadata(ctx, &identity.GetPluginMetadataRequest{}); err != nil {
		return fmt.Errorf("the plugin on %s does not answer: %w", socket, err)
	}
	return nil
}
