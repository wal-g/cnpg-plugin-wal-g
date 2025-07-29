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

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wal-g/cnpg-plugin-wal-g/pkg/version"
)

// NewVersionCmd creates a new command for printing plugin version
func NewVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Prints cnpg-plugin-wal-g version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf(
				"CNPG Plugin WAL-G\nVersion: %s\nGitCommit: %s\nBuildDate: %s\n",
				version.GetVersionNumber(),
				version.GetCommitHash(),
				version.GetBuildDate(),
			)
		},
	}

	return cmd
}
