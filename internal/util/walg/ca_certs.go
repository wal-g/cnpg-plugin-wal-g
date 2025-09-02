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
	"os"
	"path/filepath"
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
	if caCertData == "" {
		return "", nil
	}

	cacheKey := fmt.Sprintf("%s/%s", namespace, name)
	filePath := getCACertFilePath(namespace, name)

	caCertCacheMutex.RLock()
	cachedCert, exists := caCertCache[cacheKey]
	caCertCacheMutex.RUnlock()

	// If the certificate hasn't changed, we don't need to update the file
	if exists && cachedCert == caCertData {
		return filePath, nil
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory for CA certificate: %w", err)
	}

	// Write the certificate to the file
	if err := os.WriteFile(filePath, []byte(caCertData), 0644); err != nil {
		return "", fmt.Errorf("failed to write CA certificate to file: %w", err)
	}

	// Update the cache
	caCertCacheMutex.Lock()
	caCertCache[cacheKey] = caCertData
	caCertCacheMutex.Unlock()

	return filePath, nil
}
