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

// ensureCachedFile ensures that a file at filePath exists and holds data, writing it only when the
// cached value for cacheKey differs from data. It returns filePath on success, or an empty string if
// data is empty (nothing to write).
func ensureCachedFile(cacheMap map[string]string, mu *sync.RWMutex, filePath, cacheKey, data string) (string, error) {
	if data == "" {
		return "", nil
	}

	mu.RLock()
	cachedValue, exists := cacheMap[cacheKey]
	mu.RUnlock()

	// If the data hasn't changed, we don't need to update the file
	if exists && cachedValue == data {
		return filePath, nil
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(filePath, []byte(data), 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	mu.Lock()
	cacheMap[cacheKey] = data
	mu.Unlock()

	return filePath, nil
}
