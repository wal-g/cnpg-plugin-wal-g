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
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
)

// WALPush archives a WAL segment to object storage using wal-g.
func (c *Client) WALPush(ctx context.Context, sourceFileName string) (*cmd.RunResult, error) {
	logger := logr.FromContextOrDiscard(ctx)

	result, err := c.newCmd(ctx, "wal-push", sourceFileName).Run()

	if err != nil {
		logger.Error(
			err, fmt.Sprintf("Error while 'wal-g wal-push %s'", sourceFileName),
			"stdout", string(result.Stdout()), "stderr", string(result.Stderr()),
		)
	}
	return result, err
}

// WALFetch restores a WAL segment from object storage using wal-g.
// Returns the RunResult so callers can inspect the exit code (e.g. 74 = WAL not found).
func (c *Client) WALFetch(ctx context.Context, sourceWalName, destinationFileName string) (*cmd.RunResult, error) {
	logger := logr.FromContextOrDiscard(ctx)

	result, err := c.newCmd(ctx, "wal-fetch", sourceWalName, destinationFileName).Run()

	if err != nil {
		logger.Error(
			err, fmt.Sprintf("Error while 'wal-g wal-fetch %s %s'", sourceWalName, destinationFileName),
			"stdout", string(result.Stdout()), "stderr", string(result.Stderr()),
		)
	}
	return result, err
}
