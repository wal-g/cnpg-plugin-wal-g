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
	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/wal-g/cnpg-plugin-wal-g/pkg/version"
)

// PluginName is the name of the plugin from the instance manager
// Point-of-view
const PluginName = "cnpg-extensions.yandex.cloud"

// PluginMetadata is the metadata of this plugin.
var PluginMetadata = identity.GetPluginMetadataResponse{
	Name:          PluginName,
	Version:       version.GetVersion(),
	DisplayName:   "CNPGYandexExtensions",
	ProjectUrl:    "https://github.com/wal-g/cnpg-plugin-wal-g",
	RepositoryUrl: "https://github.com/wal-g/cnpg-plugin-wal-g",
	License:       "APACHE 2.0",
	LicenseUrl:    "https://github.com/wal-g/cnpg-plugin-wal-g/LICENSE",
	Maturity:      "beta",
}

const PluginModeNormal = "normal"
const PluginModeRecovery = "recovery"

var AllowedPluginModes = []string{PluginModeNormal, PluginModeRecovery}
