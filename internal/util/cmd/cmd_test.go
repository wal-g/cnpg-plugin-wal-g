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
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func startTestReaper() {
	GinkgoHelper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (&ZombieProcessReaper{}).Start(ctx) }()
	DeferCleanup(func() {
		cancel()
		Eventually(done, 5*time.Second).Should(Receive(BeNil()))
	})
}

func subscriberCount() int {
	subscribersMx.Lock()
	defer subscribersMx.Unlock()
	return len(subscribers)
}

// Wait4 and the subscription registry are process-wide.
var _ = Describe("Command execution", Serial, func() {
	BeforeEach(func() {
		startTestReaper()
	})

	AfterEach(func() {
		Expect(subscriberCount()).To(BeZero())
	})

	DescribeTable("command results", func(code int) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := New("/bin/sh", "-c", `printf '%s' "$CMD_TEST_VALUE"; printf 'diagnostic' >&2; exit "$1"`, "sh", fmt.Sprint(code)).
			WithContext(ctx).
			WithEnv(map[string]string{"CMD_TEST_VALUE": "command output"}).
			Run()

		if code == 0 {
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(err).To(HaveOccurred())
		}
		Expect(result.State()).NotTo(BeNil())
		Expect(result.State().Pid()).To(BeNumerically(">", 0))
		Expect(result.State().ExitCode()).To(Equal(code))
		Expect(result.State().Success()).To(Equal(code == 0))
		Expect(result.State().Exited()).To(BeTrue())
		Expect(string(result.Stdout())).To(Equal("command output"))
		Expect(string(result.Stderr())).To(Equal("diagnostic"))
	},
		Entry("should preserve successful results", 0),
		Entry("should preserve non-zero exit codes", 74),
	)

	It("should not retain a subscription when starting fails", func() {
		result, err := New(filepath.Join(GinkgoT().TempDir(), "missing-executable")).Run()

		Expect(err).To(MatchError(ContainSubstring("subprocess cmd.Start() error")))
		Expect(result).NotTo(BeNil())
		Expect(result.State()).To(BeNil())
	})

	It("should report a process terminated by a signal", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := New("/bin/sh", "-c", "kill -TERM $$").WithContext(ctx).Run()

		Expect(err).To(HaveOccurred())
		Expect(result.State()).NotTo(BeNil())
		Expect(result.State().ExitCode()).To(Equal(-1))
		Expect(result.State().Exited()).To(BeFalse())
	})

	It("should cancel a running command and continue handling new commands", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ready := filepath.Join(GinkgoT().TempDir(), "ready")
		done := make(chan error, 1)
		go func() {
			_, err := New("/bin/sh", "-c", `printf ready > "$1"; exec sleep 60`, "sh", ready).WithContext(ctx).Run()
			done <- err
		}()

		Eventually(func() error {
			_, err := os.Stat(ready)
			return err
		}, 5*time.Second, time.Millisecond).Should(Succeed())

		cancel()
		var err error
		Eventually(done, 5*time.Second).Should(Receive(&err))
		Expect(err).To(HaveOccurred())
		Expect(subscriberCount()).To(BeZero())

		next, nextCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer nextCancel()
		_, err = New("/bin/sh", "-c", "exit 0").WithContext(next).Run()
		Expect(err).NotTo(HaveOccurred())
	})

	It("should reap a burst of exits while all subscribers are unread", func() {
		const count = 32
		commands := make([]*exec.Cmd, 0, count)
		subscriptions := make([]chan syscall.WaitStatus, 0, count)
		DeferCleanup(func() {
			for i, command := range commands {
				_ = command.Wait()
				unsubscribeFromProcessExits(command.Process.Pid, subscriptions[i])
			}
		})

		for i := 0; i < count; i++ {
			command := exec.Command("/bin/sh", "-c", `exit "$1"`, "sh", fmt.Sprint(i))
			ch, err := startAndSubscribe(command)
			Expect(err).NotTo(HaveOccurred())
			commands = append(commands, command)
			subscriptions = append(subscriptions, ch)
		}

		// Drain only after every exit has been delivered.
		Eventually(subscriberCount, 5*time.Second, time.Millisecond).Should(BeZero())
		for i, ch := range subscriptions {
			var got syscall.WaitStatus
			Expect(ch).To(Receive(&got))
			Expect(got.Exited()).To(BeTrue())
			Expect(got.ExitStatus()).To(Equal(i))
		}
	})

	It("should preserve results under concurrent exits", func() {
		const workers, iterations = 32, 40
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var wg sync.WaitGroup

		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer GinkgoRecover()
				for i := 0; i < iterations; i++ {
					code := 0
					if i%2 != 0 {
						code = 74
					}
					result, err := New("/bin/sh", "-c", `printf output; printf error >&2; exit "$1"`, "sh", fmt.Sprint(code)).WithContext(ctx).Run()

					if code == 0 {
						Expect(err).NotTo(HaveOccurred())
					} else {
						Expect(err).To(HaveOccurred())
					}
					Expect(result.State()).NotTo(BeNil())
					Expect(result.State().ExitCode()).To(Equal(code))
					Expect(string(result.Stdout())).To(Equal("output"))
					Expect(string(result.Stderr())).To(Equal("error"))
				}
			}()
		}

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		Eventually(done, 30*time.Second).Should(BeClosed())
	})
})
