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
	"encoding/json"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// CnpgClusterFromJSON decodes a JSON representation of a CNPG cluster.
func CnpgClusterFromJSON(clusterJSON []byte) (cnpgv1.Cluster, error) {
	result := cnpgv1.Cluster{}
	if err := json.Unmarshal(clusterJSON, &result); err != nil {
		return result, fmt.Errorf("error unmarshalling object JSON: %w", err)
	}

	return result, nil
}
