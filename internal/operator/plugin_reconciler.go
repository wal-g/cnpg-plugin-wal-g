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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/object"
	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/common"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/controller"
)

// ReconcilerImplementation implements the Reconciler capability
type ReconcilerImplementation struct {
	Client client.Client
	reconciler.UnimplementedReconcilerHooksServer
}

// GetCapabilities implements the Reconciler interface
func (r ReconcilerImplementation) GetCapabilities(
	_ context.Context,
	_ *reconciler.ReconcilerHooksCapabilitiesRequest,
) (*reconciler.ReconcilerHooksCapabilitiesResult, error) {
	return &reconciler.ReconcilerHooksCapabilitiesResult{
		ReconcilerCapabilities: []*reconciler.ReconcilerHooksCapability{
			{
				Kind: reconciler.ReconcilerHooksCapability_KIND_CLUSTER,
			},
			{
				Kind: reconciler.ReconcilerHooksCapability_KIND_BACKUP,
			},
		},
	}, nil
}

// Pre implements the reconciler interface
func (r ReconcilerImplementation) Pre(
	ctx context.Context,
	request *reconciler.ReconcilerHooksRequest,
) (*reconciler.ReconcilerHooksResult, error) {
	logger := logr.FromContextOrDiscard(ctx).WithName("plugin_reconciler").WithValues("method", "Pre")
	logger.Info("Pre hook reconciliation start")

	// Checking that reconciling is performed with Cluster resource
	reconciledKind, err := object.GetKind(request.GetResourceDefinition())
	if err != nil {
		return nil, err
	}
	if reconciledKind != "Cluster" {
		return &reconciler.ReconcilerHooksResult{
			Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
		}, nil
	}

	cluster, err := common.CnpgClusterFromJSON(request.ClusterDefinition)
	if err != nil {
		logger.Error(err, "Error while unmarshalling Cluster object. Should requeue")
		return &reconciler.ReconcilerHooksResult{
			Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE,
		}, nil
	}

	logger = logger.WithValues("name", cluster.Name, "namespace", cluster.Namespace)
	ctx = logr.NewContext(ctx, logger)

	backupConfigs := make([]v1beta1.BackupConfig, 0)

	archiveBackupConfig, err := v1beta1.GetBackupConfigForCluster(ctx, r.Client, cluster)
	if err != nil {
		logger.Error(err, "Error while fetching cluster backup config. Should requeue")
		return &reconciler.ReconcilerHooksResult{
			Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE,
		}, nil
	}
	if archiveBackupConfig != nil {
		backupConfigs = append(backupConfigs, *archiveBackupConfig)
	}

	recoveryBackupConfig, err := v1beta1.GetBackupConfigForClusterRecovery(ctx, r.Client, cluster)
	if err != nil {
		logger.Error(err, "Error while fetching cluster backup config. Should requeue")
		return &reconciler.ReconcilerHooksResult{
			Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE,
		}, nil
	}
	if recoveryBackupConfig != nil {
		backupConfigs = append(backupConfigs, *recoveryBackupConfig)
	}

	if err := r.ensureRole(ctx, &cluster, backupConfigs); err != nil {
		return nil, err
	}

	if err := r.ensureRoleBinding(ctx, &cluster); err != nil {
		return nil, err
	}

	logger.Info("Pre hook reconciliation completed")
	return &reconciler.ReconcilerHooksResult{
		Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
	}, nil
}

// Post implements the reconciler interface
func (r ReconcilerImplementation) Post(
	_ context.Context,
	_ *reconciler.ReconcilerHooksRequest,
) (*reconciler.ReconcilerHooksResult, error) {
	return &reconciler.ReconcilerHooksResult{
		Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
	}, nil
}

func (r ReconcilerImplementation) ensureRole(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	backupConfigs []v1beta1.BackupConfig,
) error {
	contextLogger := log.FromContext(ctx)

	var role rbacv1.Role
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      controller.GetRoleNameForBackupConfig(cluster.Name),
	}, &role); err != nil {
		if !apierrs.IsNotFound(err) {
			return err
		}

		newRole := rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cluster.Namespace,
				Name:      controller.GetRoleNameForBackupConfig(cluster.Name),
			},

			Rules: []rbacv1.PolicyRule{},
		}

		controller.BuildRoleForBackupConfigs(&newRole, cluster, backupConfigs)
		contextLogger.Info(
			"Creating role",
			"name", newRole.Name,
			"namespace", newRole.Namespace,
		)

		if err := setOwnerReference(cluster, &newRole); err != nil {
			return err
		}

		return r.Client.Create(ctx, &newRole)
	}

	// Role already exists, need to calculate changes and update
	oldRole := role.DeepCopy()

	controller.BuildRoleForBackupConfigs(&role, cluster, backupConfigs)
	if equality.Semantic.DeepEqual(role, oldRole) {
		// There's no need to hit the API server again
		return nil
	}

	contextLogger.Info(
		"Patching role",
		"role_name", role.Name,
		"role_namespace", role.Namespace,
		"role_rules", role.Rules,
	)

	return r.Client.Patch(ctx, &role, client.MergeFrom(&role))
}

func (r ReconcilerImplementation) ensureRoleBinding(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
) error {
	var role rbacv1.RoleBinding
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      controller.GetRoleNameForBackupConfig(cluster.Name),
	}, &role); err != nil {
		if apierrs.IsNotFound(err) {
			return r.createRoleBinding(ctx, cluster)
		}
		return err
	}
	return nil
}

func (r ReconcilerImplementation) createRoleBinding(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
) error {
	roleBinding := controller.BuildRoleBindingForBackupConfig(cluster)
	if err := setOwnerReference(cluster, roleBinding); err != nil {
		return err
	}
	return r.Client.Create(ctx, roleBinding)
}
