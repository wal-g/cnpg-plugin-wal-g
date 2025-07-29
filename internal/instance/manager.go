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
	"flag"
	"os"
	"path"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
	"github.com/wal-g/cnpg-plugin-wal-g/pkg/resourcecachingclient"
	"github.com/wal-g/cnpg-plugin-wal-g/pkg/version"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(cnpgv1.AddToScheme(scheme))
	utilruntime.Must(v1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// Start starts the sidecar informers and CNPG-i server
func Start(ctx context.Context) error {
	opts := zap.Options{}
	if version.IsDevelopment() {
		opts.Development = true
	} else {
		opts.Development = false
		opts.StacktraceLevel = zapcore.DPanicLevel
	}

	opts.BindFlags(flag.CommandLine)
	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	childrenCtx := logr.NewContext(ctx, logger)

	setupLog.Info("Starting CNPG yandex instance plugin")

	podName := viper.GetString("pod-name")
	namespace := viper.GetString("namespace")

	if os.Getenv("PG_MAJOR") == "" {
		panic("Cannot start: PG_MAJOR environment variable is unset")
	}

	controllerOptions := ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				namespace: {},
			},
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.Secret{},
					&v1beta1.BackupConfig{},
					&cnpgv1.Cluster{},
					&cnpgv1.Backup{},
				},
			},
		},
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), controllerOptions)
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		return err
	}

	client, err := resourcecachingclient.CreateClient(
		childrenCtx,
		mgr,
		[]client.Object{
			&corev1.Secret{},
			&v1beta1.BackupConfig{},
			&cnpgv1.Cluster{},
			&cnpgv1.Backup{},
		},
	)
	if err != nil {
		setupLog.Error(err, "unable to create cache client")
		return err
	}

	if err := mgr.Add(&cmd.ZombieProcessReaper{}); err != nil {
		setupLog.Error(err, "unable to create zombie process reaper runnable")
		return err
	}

	if err := mgr.Add(&CNPGI{
		Client:       client,
		InstanceName: podName,
		PGDataPath:   viper.GetString("pgdata"),
		PGWALPath:    path.Join(viper.GetString("pgdata"), "pg_wal"),
		PluginPath:   viper.GetString("plugin-path"),
	}); err != nil {
		setupLog.Error(err, "unable to create CNPGI runnable")
		return err
	}

	if err := mgr.Start(childrenCtx); err != nil {
		return err
	}

	return nil
}
