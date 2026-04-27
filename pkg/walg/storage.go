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
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/wal-g/cnpg-plugin-wal-g/internal/util/cmd"
)

// WALTimelineInfo represents a single timeline entry returned by `wal-g wal-show --detailed-json`
type WALTimelineInfo struct {
	ID               int              `json:"id"`
	ParentID         int              `json:"parent_id"`
	SwitchPointLSN   uint64           `json:"switch_point_lsn"`
	StartSegment     string           `json:"start_segment"`
	EndSegment       string           `json:"end_segment"`
	SegmentsCount    int              `json:"segments_count"`
	MissingSegments  []string         `json:"missing_segments"`
	Backups          []BackupMetadata `json:"backups"`
	SegmentRangeSize int              `json:"segment_range_size"`
	Status           string           `json:"status"`
}

// HasMissingSegments returns true if there are any missing WAL segments in this timeline
func (w *WALTimelineInfo) HasMissingSegments() bool {
	return len(w.MissingSegments) > 0
}

// IsOK returns true if the timeline status is "OK"
func (w *WALTimelineInfo) IsOK() bool {
	return w.Status == "OK"
}

// StorageCheckReadable verifies that the configured storage is accessible for reading
// by running `wal-g st check read`.
func (c *Client) StorageCheckReadable(ctx context.Context) (*cmd.RunResult, error) {
	logger := logr.FromContextOrDiscard(ctx)

	result, err := cmd.New("wal-g", "st", "check", "read").
		WithContext(ctx).
		WithEnv(c.config.ToEnvMap()).
		Run()

	if err != nil {
		logger.Error(
			err, "Error while 'wal-g st check read'",
			"stdout", string(result.Stdout()), "stderr", string(result.Stderr()),
		)
		return result, fmt.Errorf("storage read check failed: %w", err)
	}
	return result, nil
}

// StorageCheckWritable verifies that the configured storage is accessible for writing
// by running `wal-g st check write`.
func (c *Client) StorageCheckWritable(ctx context.Context) (*cmd.RunResult, error) {
	logger := logr.FromContextOrDiscard(ctx)

	result, err := cmd.New("wal-g", "st", "check", "write").
		WithContext(ctx).
		WithEnv(c.config.ToEnvMap()).
		Run()

	if err != nil {
		logger.Error(
			err, "Error while 'wal-g st check write'",
			"stdout", string(result.Stdout()), "stderr", string(result.Stderr()),
		)
		return result, fmt.Errorf("storage write check failed: %w", err)
	}
	return result, nil
}

// WALShow retrieves detailed WAL timeline information by running `wal-g wal-show --detailed-json`.
// Returns a list of WALTimelineInfo entries, each representing a timeline with its segments,
// backups, and integrity status.
func (c *Client) WALShow(ctx context.Context) ([]WALTimelineInfo, error) {
	logger := logr.FromContextOrDiscard(ctx)

	result, err := cmd.New("wal-g", "wal-show", "--detailed-json").
		WithContext(ctx).
		WithEnv(c.config.ToEnvMap()).
		Run()

	if err != nil {
		logger.Error(
			err, "Error while 'wal-g wal-show --detailed-json'",
			"stdout", string(result.Stdout()), "stderr", string(result.Stderr()),
		)
		return nil, fmt.Errorf("wal-show failed: %w", err)
	}

	var timelines []WALTimelineInfo
	if err := json.Unmarshal(result.Stdout(), &timelines); err != nil {
		logger.Error(
			err, "Cannot unmarshal wal-g wal-show output",
			"stdout", string(result.Stdout()),
			"stderr", string(result.Stderr()),
		)
		return nil, fmt.Errorf("cannot unmarshal wal-g wal-show output: %w", err)
	}

	return timelines, nil
}
