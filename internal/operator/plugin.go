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

package operator

import (
	"context"
	"path/filepath"

	cnpgihttp "github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/http"
	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	"google.golang.org/grpc"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CNPGI is the implementation of the CNPG-i server for operator
type CNPGI struct {
	Client         client.Client
	ServerAddress  string
	PluginPath     string
	ServerCertPath string
	ServerCertName string
	ServerCertKey  string
	ClientCertPath string
	ClientCertName string
}

// Start starts the GRPC server
// of the operator plugin
func (c *CNPGI) Start(ctx context.Context) error {
	enrich := func(server *grpc.Server) error {
		lifecycle.RegisterOperatorLifecycleServer(server, LifecycleImplementation{
			Client: c.Client,
		})
		reconciler.RegisterReconcilerHooksServer(server, ReconcilerImplementation{
			Client: c.Client,
		})
		return nil
	}

	srv := cnpgihttp.Server{
		IdentityImpl:  IdentityImplementation{},
		Enrichers:     []cnpgihttp.ServerEnricher{enrich},
		PluginPath:    c.PluginPath,
		ServerAddress: c.ServerAddress,
	}

	if len(c.ServerCertPath) > 0 {
		srv.ServerCertPath = filepath.Join(c.ServerCertPath, c.ServerCertName)
		srv.ServerKeyPath = filepath.Join(c.ServerCertPath, c.ServerCertKey)
	}

	if len(c.ClientCertPath) > 0 {
		srv.ClientCertPath = filepath.Join(c.ClientCertPath, c.ClientCertName)
	}

	return srv.Start(ctx)
}
