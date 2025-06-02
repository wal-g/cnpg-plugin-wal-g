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
	"sync"
	"syscall"
)

var (
	subscribers   map[chan ProcessInfo]bool
	subscribersMx sync.Mutex
)

func subscribeOnProcessExits(ch chan ProcessInfo) {
	subscribersMx.Lock()
	defer subscribersMx.Unlock()

	// Initialize map on first access
	if subscribers == nil {
		subscribers = make(map[chan ProcessInfo]bool)
	}
	subscribers[ch] = true
}

func unsubscribeFromProcessExits(ch chan ProcessInfo) {
	subscribersMx.Lock()
	defer subscribersMx.Unlock()

	delete(subscribers, ch)
}

func notifyAllSubscribers(pid int, wstatus syscall.WaitStatus) {
	subscribersMx.Lock()
	subscribersSafeCopy := make([]chan ProcessInfo, 0, len(subscribers))
	for subscriber := range subscribers {
		subscribersSafeCopy = append(subscribersSafeCopy, subscriber)
	}
	defer subscribersMx.Unlock()

	for _, subscriber := range subscribersSafeCopy {
		subscriber <- ProcessInfo{Pid: pid, Status: wstatus}
	}
}
