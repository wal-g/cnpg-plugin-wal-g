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
	subscribers   map[int]chan ProcessInfo
	pendingExits  map[int]syscall.WaitStatus
	subscribersMx sync.Mutex
)

func subscribeOnProcessExits(pid int, ch chan ProcessInfo) {
	subscribersMx.Lock()
	defer subscribersMx.Unlock()

	if subscribers == nil {
		subscribers = make(map[int]chan ProcessInfo)
	}
	if pendingExits == nil {
		pendingExits = make(map[int]syscall.WaitStatus)
	}
	// the process may have exited between cmd.Start() and subscribing
	if st, ok := pendingExits[pid]; ok {
		delete(pendingExits, pid)
		ch <- ProcessInfo{Pid: pid, Status: st} // buffer >= 1: never blocks
		return
	}
	subscribers[pid] = ch
}

func unsubscribeFromProcessExits(pid int) {
	subscribersMx.Lock()
	defer subscribersMx.Unlock()

	delete(subscribers, pid)
	delete(pendingExits, pid)
}

func notifyAllSubscribers(pid int, wstatus syscall.WaitStatus) {
	subscribersMx.Lock()
	defer subscribersMx.Unlock()

	if ch, ok := subscribers[pid]; ok {
		select {
		case ch <- ProcessInfo{Pid: pid, Status: wstatus}:
		default: // buffer of 1 per pid; only reachable on duplicate delivery
		}
		delete(subscribers, pid)
		return
	}
	if pendingExits == nil {
		pendingExits = make(map[int]syscall.WaitStatus)
	}
	pendingExits[pid] = wstatus
}
