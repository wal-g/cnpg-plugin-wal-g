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
	"net"
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"

	"github.com/wal-g/cnpg-plugin-wal-g/internal/instance"
)

var _ = Describe("Plugin socket health check", func() {
	var socket string

	BeforeEach(func() {
		// A short directory: unix socket paths are limited to about 100 bytes.
		dir, err := os.MkdirTemp("/tmp", "hc")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)
		socket = filepath.Join(dir, "plugin.sock")
	})

	It("succeeds when the plugin serves its identity on the socket", func() {
		listener, err := net.Listen("unix", socket)
		Expect(err).NotTo(HaveOccurred())
		server := grpc.NewServer()
		identity.RegisterIdentityServer(server, instance.IdentityImplementation{})
		go func() { _ = server.Serve(listener) }()
		DeferCleanup(server.Stop)

		Expect(checkPluginSocket(ctx, socket)).To(Succeed())
	})

	It("fails while nothing listens on the socket", func() {
		Expect(checkPluginSocket(ctx, socket)).NotTo(Succeed())
	})
})
