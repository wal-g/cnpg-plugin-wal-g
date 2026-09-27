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
	"os/exec"
	"sync"
	"syscall"
)

var (
	subscribers   map[int]chan syscall.WaitStatus
	subscribersMx sync.Mutex
)

// startAndSubscribe starts the command and registers a channel for its exit status.
func startAndSubscribe(command *exec.Cmd) (chan syscall.WaitStatus, error) {
	subscribersMx.Lock()
	defer subscribersMx.Unlock()

	if err := command.Start(); err != nil {
		return nil, err
	}

	if subscribers == nil {
		subscribers = make(map[int]chan syscall.WaitStatus)
	}
	ch := make(chan syscall.WaitStatus, 1)
	subscribers[command.Process.Pid] = ch
	return ch, nil
}

func unsubscribeFromProcessExits(pid int, ch chan syscall.WaitStatus) {
	subscribersMx.Lock()
	defer subscribersMx.Unlock()

	// Do not remove a newer subscription for a reused PID.
	if subscribers[pid] == ch {
		delete(subscribers, pid)
	}
}

// Hold subscribersMx from Wait4 through delivery. One send per buffered channel cannot block.
func notifyProcessExitLocked(pid int, wstatus syscall.WaitStatus) {
	if ch, ok := subscribers[pid]; ok {
		delete(subscribers, pid)
		ch <- wstatus
	}
}
