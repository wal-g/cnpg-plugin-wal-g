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

package walg

import (
	"fmt"
	"sync"
)

// caCertCache stores CA certificate data for each BackupConfig to track changes
var caCertCache = make(map[string]string)
var caCertCacheMutex sync.RWMutex

// getCACertFilePath returns the path to the CA certificate file for a BackupConfig
func getCACertFilePath(namespace, name string) string {
	return fmt.Sprintf("/tmp/custom-ca/%s-%s-ca.crt", namespace, name)
}

// ensureCACertFile ensures that the CA certificate file exists and has the correct content
// It returns the path to the CA certificate file if successful, or an empty string if no CA cert is provided
func ensureCACertFile(namespace, name, caCertData string) (string, error) {
	cacheKey := fmt.Sprintf("%s/%s", namespace, name)
	return ensureCachedFile(caCertCache, &caCertCacheMutex, getCACertFilePath(namespace, name), cacheKey, caCertData)
}
