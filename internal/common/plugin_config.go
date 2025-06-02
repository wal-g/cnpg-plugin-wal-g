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
)

func GetRecoveryPluginConfigFromCluster(cluster cnpgv1.Cluster) *cnpgv1.PluginConfiguration {
	if cluster.Spec.Bootstrap.Recovery == nil {
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

func GetPluginConfigFromCluster(cluster cnpgv1.Cluster) *cnpgv1.PluginConfiguration {
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
