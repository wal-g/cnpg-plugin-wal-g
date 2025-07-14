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
	"crypto/rand"
	"encoding/hex"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/object"
	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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

	// Check and create encryption secrets if needed
	for i := range backupConfigs {
		if err := r.ensureEncryptionSecret(ctx, &backupConfigs[i]); err != nil {
			logger.Error(err, "Error while ensuring encryption secret. Should requeue")
			return &reconciler.ReconcilerHooksResult{
				Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE,
			}, nil
		}
	}

	if err := r.ensureRole(ctx, cluster, backupConfigs); err != nil {
		return &reconciler.ReconcilerHooksResult{
			Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE,
		}, err
	}

	if err := r.ensureRoleBinding(ctx, cluster); err != nil {
		return &reconciler.ReconcilerHooksResult{
			Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE,
		}, err
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
	logger := logr.FromContextOrDiscard(ctx)

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
		logger.Info("Creating role", "role", newRole.TypeMeta)

		if err := setOwnerReference(cluster, &newRole); err != nil {
			return err
		}

		return r.Client.Create(ctx, &newRole)
	}

	// Role already exists, need to calculate changes and update
	oldRole := role.DeepCopy()

	controller.BuildRoleForBackupConfigs(&role, cluster, backupConfigs)
	if equality.Semantic.DeepEqual(&role, oldRole) {
		// There's no need to hit the API server again
		return nil
	}

	patchData, _ := client.MergeFrom(oldRole).Data(&role)
	logger.Info("Patching role", "role", role.TypeMeta, "patch", string(patchData))

	return r.Client.Patch(ctx, &role, client.MergeFrom(oldRole))
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

// ensureEncryptionSecret checks if encryption is enabled and creates the secret if needed
func (r ReconcilerImplementation) ensureEncryptionSecret(
	ctx context.Context,
	backupConfig *v1beta1.BackupConfig,
) error {
	logger := logr.FromContextOrDiscard(ctx).WithValues(
		"backupConfig", backupConfig.Name,
		"namespace", backupConfig.Namespace,
	)

	// Check if encryption is enabled and configured to use libsodium
	if backupConfig.Spec.Encryption.Method != "libsodium" {
		return nil
	}

	// Check if user specified secret - we don't need to create our own one
	if backupConfig.Spec.Encryption.ExistingEncryptionSecretName != "" {
		return nil
	}

	secretName := v1beta1.GetBackupConfigEncryptionSecretName(backupConfig)
	// Check if the secret already exists
	secret := &corev1.Secret{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: backupConfig.Namespace,
		Name:      secretName,
	}, secret)

	// Secret exists, nothing to do
	if err == nil {
		logger.V(1).Info("Encryption key secret already exists", "secretName", secretName)
		return nil
	}

	if !apierrs.IsNotFound(err) {
		return fmt.Errorf("failed to check if encryption key secret exists: %w", err)
	}

	// Secret doesn't exist, create it
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: backupConfig.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{},
	}

	switch backupConfig.Spec.Encryption.Method {
	case "libsodium":
		{
			secretKey := "libsodiumKey"
			// Creating 32-byte random key
			key, err := generateRandomHexString(32)
			if err != nil {
				return fmt.Errorf("failed to generate random data for secret: %w", err)
			}
			newSecret.Data[secretKey] = []byte(key)
		}
	}

	// Set owner reference to the backup config
	if err := setOwnerReference(backupConfig, newSecret); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	if err := r.Client.Create(ctx, newSecret); err != nil {
		return fmt.Errorf("failed to create encryption key secret: %w", err)
	}

	logger.Info("Created new encryption secret with random key", "secretName", secretName)
	return nil
}

// generateRandomHexString generates a random key, encodes with HEX and return via bytes slice.
func generateRandomHexString(bytesNum int) (string, error) {
	// Generate a random key of bytesNum length
	keyBytes := make([]byte, bytesNum)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	// Convert to hex string (64 characters)
	return hex.EncodeToString(keyBytes), nil
}
