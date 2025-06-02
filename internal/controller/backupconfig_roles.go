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

package controller

import (
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	"github.com/wal-g/cnpg-plugin-wal-g/api/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildRole builds the Role object for this cluster
// It collects all BackupConfig objects used by cluster and grants read/watch permissions on them only
// It also finds all secrets referenced by these BackupConfig and grants read permission on them
func BuildRoleForBackupConfigs(
	cluster *cnpgv1.Cluster,
	backupConfigs []v1beta1.BackupConfig,
) *rbacv1.Role {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      GetRoleBindingNameForBackupConfig(cluster.Name),
		},

		Rules: []rbacv1.PolicyRule{},
	}

	backupConfigNames := stringset.New()
	secretsNames := stringset.New()

	for _, backupConfig := range backupConfigs {
		backupConfigNames.Put(backupConfig.Name)
		if backupConfig.Spec.Storage.StorageType == v1beta1.StorageTypeS3 {
			secretsNames.Put(backupConfig.Spec.Storage.S3.AccessKeyIDRef.Name)
			secretsNames.Put(backupConfig.Spec.Storage.S3.AccessKeySecretRef.Name)
		}
	}

	role.Rules = append(
		role.Rules,
		rbacv1.PolicyRule{
			APIGroups:     []string{"cnpg-extensions.yandex.cloud"},
			Verbs:         []string{"watch", "get"},
			Resources:     []string{"backupconfigs"},
			ResourceNames: backupConfigNames.ToSortedList(),
		},
		rbacv1.PolicyRule{
			APIGroups:     []string{"cnpg-extensions.yandex.cloud"},
			Verbs:         []string{"update"},
			Resources:     []string{"backupconfigs/status"},
			ResourceNames: backupConfigNames.ToSortedList(),
		},
		rbacv1.PolicyRule{
			APIGroups:     []string{""},
			Verbs:         []string{"watch", "get"},
			Resources:     []string{"secrets"},
			ResourceNames: secretsNames.ToSortedList(),
		},
	)
	return role
}

// BuildRoleBinding builds the role binding object for this cluster
func BuildRoleBindingForBackupConfig(
	cluster *cnpgv1.Cluster,
) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      GetRoleBindingNameForBackupConfig(cluster.Name),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				APIGroup:  "",
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     GetRoleBindingNameForBackupConfig(cluster.Name),
		},
	}
}

// GetRBACName returns the name of the RBAC entities for the
// barman cloud plugin
func GetRoleBindingNameForBackupConfig(clusterName string) string {
	return fmt.Sprintf("%s-backups", clusterName)
}
