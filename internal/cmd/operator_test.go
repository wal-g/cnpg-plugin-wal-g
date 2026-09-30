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
	"io"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
)

var _ = Describe("Operator status interval", func() {
	BeforeEach(func() {
		viper.Reset()
	})

	AfterEach(func() {
		viper.Reset()
	})

	DescribeTable("binds the interval to the operator configuration",
		func(args []string, expected time.Duration) {
			cmd := NewOperatorCmd()
			Expect(cmd.ParseFlags(args)).To(Succeed())
			Expect(viper.GetDuration("backup-config-status-interval")).To(Equal(expected))
		},
		Entry("defaults to two minutes", []string{}, 2*time.Minute),
		Entry("accepts minutes", []string{"--backup-config-status-interval=10m"}, 10*time.Minute),
		Entry("accepts hours", []string{"--backup-config-status-interval=1h"}, time.Hour),
		Entry("accepts compound durations", []string{"--backup-config-status-interval=1m30s"}, 90*time.Second),
	)

	It("defaults archive checks to five minutes independently of availability checks", func() {
		cmd := NewOperatorCmd()
		Expect(cmd.ParseFlags([]string{"--backup-config-status-interval=1m"})).To(Succeed())
		Expect(viper.GetDuration("backup-config-status-archive-interval")).To(Equal(5 * time.Minute))
		Expect(cmd.ParseFlags([]string{"--backup-config-status-archive-interval=10m"})).To(Succeed())
		Expect(viper.GetDuration("backup-config-status-archive-interval")).To(Equal(10 * time.Minute))
		Expect(viper.GetDuration("backup-config-status-interval")).To(Equal(time.Minute))
	})

	DescribeTable("rejects invalid intervals before starting the operator",
		func(flag, value, message string) {
			cmd := NewOperatorCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--" + flag + "=" + value})
			Expect(cmd.Execute()).To(MatchError(ContainSubstring(message)))
		},
		Entry("zero", "backup-config-status-interval", "0", "backup-config-status-interval must be a positive duration"),
		Entry("negative", "backup-config-status-interval", "-1m", "backup-config-status-interval must be a positive duration"),
		Entry("malformed", "backup-config-status-interval", "invalid", "invalid duration"),
		Entry("missing unit", "backup-config-status-interval", "10", "missing unit"),
		Entry("zero archive interval", "backup-config-status-archive-interval", "0", "backup-config-status-archive-interval must be a positive duration"),
		Entry("negative archive interval", "backup-config-status-archive-interval", "-1m", "backup-config-status-archive-interval must be a positive duration"),
		Entry("malformed archive interval", "backup-config-status-archive-interval", "invalid", "invalid duration"),
		Entry("archive interval missing unit", "backup-config-status-archive-interval", "10", "missing unit"),
	)
})
