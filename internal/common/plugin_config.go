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

package common

import (
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
)

func GetRecoveryPluginConfigFromCluster(cluster *cnpgv1.Cluster) *cnpgv1.PluginConfiguration {
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil {
		return nil
	}

	clusterRecoverySourceName := cluster.Spec.Bootstrap.Recovery.Source
	if clusterRecoverySourceName == "" {
		return nil
	}

	clusterRecoverySource, ok := lo.Find(cluster.Spec.ExternalClusters, func(c cnpgv1.ExternalCluster) bool {
		return c.Name == clusterRecoverySourceName
	})
	if !ok {
		return nil
	}

	return clusterRecoverySource.PluginConfiguration
}

func GetPluginConfigFromCluster(cluster *cnpgv1.Cluster) *cnpgv1.PluginConfiguration {
	if cluster.Spec.Plugins == nil {
		return nil
	}

	pluginConfig, ok := lo.Find(cluster.Spec.Plugins, func(c cnpgv1.PluginConfiguration) bool {
		return c.Name == PluginName
	})

	if !ok {
		return nil
	}

	return &pluginConfig
}

// GetInitContainerRestartPolicy returns the restart policy configuration for init containers.
// Returns the policy pointer, or nil if no restart policy should be set.
// Supports "Always" (default) and "unset" values.
func GetInitContainerRestartPolicy(cluster *cnpgv1.Cluster) *corev1.ContainerRestartPolicy {
	pluginConfig := GetPluginConfigFromCluster(cluster)
	if pluginConfig == nil || pluginConfig.Parameters == nil {
		// Default: Always
		policy := corev1.ContainerRestartPolicyAlways
		return &policy
	}

	restartPolicyParam, exists := pluginConfig.Parameters["sidecarRestartPolicy"]
	if !exists {
		// Default: Always
		policy := corev1.ContainerRestartPolicyAlways
		return &policy
	}

	switch restartPolicyParam {
	case "Always":
		policy := corev1.ContainerRestartPolicyAlways
		return &policy
	case "unset":
		// Don't set restart policy (traditional init container behavior)
		return nil
	default:
		// Invalid value, use default (Always)
		policy := corev1.ContainerRestartPolicyAlways
		return &policy
	}
}
