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
	"crypto/tls"
	"flag"
	"os"
	"path/filepath"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/spf13/viper"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	// +kubebuilder:scaffold:imports

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/controller"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
	webhookv1 "github.com/wal-g/cnpg-plugin-wal-g/internal/webhook/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/pkg/version"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	// WebhookSecretName is the name of the secret where the certificates
	// for the webhook server are stored
	WebhookSecretName = "cnpg-plugin-wal-g-webhook-cert" // #nosec

	// WebhookServiceName is the name of the service where the webhook server
	// is reachable
	WebhookServiceName = "cnpg-plugin-wal-g-webhook-service" // #nosec

	// MutatingWebhookConfigurationName is the name of the mutating webhook configuration
	MutatingWebhookConfigurationName = "cnpg-plugin-wal-g-mutating-webhook-configuration"

	// ValidatingWebhookConfigurationName is the name of the validating webhook configuration
	ValidatingWebhookConfigurationName = "cnpg-plugin-wal-g-validating-webhook-configuration"

	// The name of the directory containing the TLS certificates
	defaultWebhookCertDir = "/run/secrets/cnpg-plugin-wal-g/webhook"

	// CaSecretName is the name of the secret which is hosting the Operator CA
	CaSecretName = "cnpg-plugin-wal-g-ca-secret" // #nosec

	// OperatorDeploymentLabelSelector is the labelSelector to be used to get the operators deployment
	OperatorDeploymentLabelSelector = "app.kubernetes.io/name=cnpg-plugin-wal-g"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(cnpgv1.AddToScheme(scheme))
	utilruntime.Must(v1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func Start(ctx context.Context) error {
	var tlsOpts []func(*tls.Config)

	opts := zap.Options{}
	if version.IsDevelopment() {
		opts.Development = true
	} else {
		opts.Development = false
		opts.StacktraceLevel = zapcore.DPanicLevel
	}

	opts.BindFlags(flag.CommandLine)

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !viper.GetBool("enable-http2") {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	webhookCertPath := viper.GetString("webhook-cert-path")
	webhookCertName := viper.GetString("webhook-cert-name")
	webhookCertKey := viper.GetString("webhook-cert-key")

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
		CertDir: defaultWebhookCertDir,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   viper.GetString("metrics-bind-address"),
		SecureServing: viper.GetBool("metrics-secure"),
		TLSOpts:       tlsOpts,
	}

	if viper.GetBool("metrics-secure") {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	metricsCertPath := viper.GetString("metrics-cert-path")
	metricsCertName := viper.GetString("metrics-cert-name")
	metricsCertKey := viper.GetString("metrics-cert-key")

	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: viper.GetString("health-probe-bind-address"),
		LeaderElection:         viper.GetBool("leader-elect"),
		LeaderElectionID:       "f522231e.cnpg-extensions.yandex.cloud",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	if err := mgr.Add(&cmd.ZombieProcessReaper{}); err != nil {
		setupLog.Error(err, "unable to create zombie process reaper runnable")
		return err
	}

	backupDeletionController := controller.NewBackupDeletionController(mgr.GetClient())
	if err := mgr.Add(backupDeletionController); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BackupDeletionController")
		return err
	}

	if err = (&controller.BackupConfigReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BackupConfigReconciler")
		return err
	}

	// Create and add the RetentionController
	retentionController := controller.NewRetentionController(
		mgr.GetClient(),
		10*time.Minute, // Run retention check each 10 minutes
	)
	if err := mgr.Add(retentionController); err != nil {
		setupLog.Error(err, "unable to add controller", "controller", "RetentionController")
		return err
	}

	// Register the BackupReconciler
	if err = (&controller.BackupReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		DeletionController: backupDeletionController,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BackupReconciler")
		return err
	}

	// nolint:goconst
	kubeClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create Kubernetes client")
		os.Exit(1)
	}
	if err := ensurePKI(ctx, kubeClient); err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	if err = webhookv1.SetupBackupConfigWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "PostgresqlCluster")
		os.Exit(1)
	}

	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			return err
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			return err
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	cnpgiConfig := CNPGI{
		Client:         mgr.GetClient(),
		ServerAddress:  viper.GetString("cnpg-i-bind-address"),
		PluginPath:     viper.GetString("cnpg-i-socket-path"),
		ServerCertPath: viper.GetString("cnpg-i-cert-path"),
		ServerCertName: viper.GetString("cnpg-i-cert-name"),
		ServerCertKey:  viper.GetString("cnpg-i-cert-key"),
		ClientCertPath: viper.GetString("cnpg-i-client-cert-path"),
		ClientCertName: viper.GetString("cnpg-i-client-cert-name"),
	}

	if err := mgr.Add(&cnpgiConfig); err != nil {
		setupLog.Error(err, "unable to create CNPGI runnable")
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}

// ensurePKI sets up the necessary Public Key Infrastructure (PKI) for the operator,
// including generating and installing certificates into the webhook configuration.
func ensurePKI(
	ctx context.Context,
	kubeClient client.Client,
) error {
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)
	operatorNamespace, _, err := kubeConfig.Namespace()
	if err != nil {
		return err
	}
	// We need to self-manage required PKI infrastructure and install the certificates into
	// the webhooks configuration
	pkiConfig := certs.PublicKeyInfrastructure{
		CaSecretName:                       CaSecretName,
		CertDir:                            defaultWebhookCertDir,
		SecretName:                         WebhookSecretName,
		ServiceName:                        WebhookServiceName,
		OperatorNamespace:                  operatorNamespace,
		MutatingWebhookConfigurationName:   MutatingWebhookConfigurationName,
		ValidatingWebhookConfigurationName: ValidatingWebhookConfigurationName,
		OperatorDeploymentLabelSelector:    OperatorDeploymentLabelSelector,
	}
	err = pkiConfig.Setup(ctx, kubeClient)
	if err != nil {
		setupLog.Error(err, "unable to setup PKI infrastructure")
	}
	return err
}
