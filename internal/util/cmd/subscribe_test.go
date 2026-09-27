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
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Process exit subscriptions", Serial, func() {
	It("should deliver other exits when a subscriber stops reading", func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer GinkgoRecover()
			subscribersMx.Lock()
			defer subscribersMx.Unlock()

			slow := make(chan syscall.WaitStatus, 1)
			waiting := make(chan syscall.WaitStatus, 1)
			subscribers = map[int]chan syscall.WaitStatus{1: slow, 2: waiting}
			notifyProcessExitLocked(1, 0)
			for pid := 3; pid <= 64; pid++ {
				ch := make(chan syscall.WaitStatus, 1)
				subscribers[pid] = ch
				status := syscall.WaitStatus(74 << 8)
				notifyProcessExitLocked(pid, status)
				Expect(ch).To(Receive(Equal(status)))
			}

			Expect(slow).To(HaveLen(1))
			Expect(waiting).To(BeEmpty())
			notifyProcessExitLocked(2, 0)
			Expect(waiting).To(Receive(Equal(syscall.WaitStatus(0))))
			Expect(subscribers).To(BeEmpty())
		}()

		Eventually(done, 5*time.Second).Should(BeClosed())
	})

	It("should ignore unknown and duplicate exits", func() {
		subscribersMx.Lock()
		defer subscribersMx.Unlock()
		const pid = 123
		ch := make(chan syscall.WaitStatus, 1)
		subscribers = map[int]chan syscall.WaitStatus{pid: ch}

		notifyProcessExitLocked(pid+1, syscall.WaitStatus(74<<8))
		Expect(ch).To(BeEmpty())
		Expect(subscribers).To(HaveKeyWithValue(pid, ch))

		notifyProcessExitLocked(pid, 0)
		notifyProcessExitLocked(pid, syscall.WaitStatus(1<<8))
		Expect(ch).To(HaveLen(1))
		Expect(ch).To(Receive(Equal(syscall.WaitStatus(0))))
		Expect(subscribers).To(BeEmpty())
	})

	It("should preserve the new subscription after PID reuse", func() {
		const pid = 123
		old := make(chan syscall.WaitStatus, 1)
		current := make(chan syscall.WaitStatus, 1)
		subscribersMx.Lock()
		subscribers = map[int]chan syscall.WaitStatus{pid: old}
		notifyProcessExitLocked(pid, 0)
		subscribers[pid] = current
		subscribersMx.Unlock()

		unsubscribeFromProcessExits(pid, old)

		subscribersMx.Lock()
		defer subscribersMx.Unlock()
		Expect(subscribers[pid]).To(Equal(current))
		status := syscall.WaitStatus(74 << 8)
		notifyProcessExitLocked(pid, status)
		Expect(current).To(Receive(Equal(status)))
	})
})
