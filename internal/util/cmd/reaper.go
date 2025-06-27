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

// Package cmd same as exec module but with reaper support.
package cmd

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-logr/logr"
)

type ProcessInfo struct {
	Pid    int
	Status syscall.WaitStatus
}

type ZombieProcessReaper struct {
}

func (r *ZombieProcessReaper) Start(ctx context.Context) error {
	logger := logr.FromContextOrDiscard(ctx).WithName("ZombieProcessReaper")
	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, syscall.SIGCHLD)
	defer signal.Stop(sigCh)

	logger.Info("Starting zombie process reaper")

	for {
		// wait for SIGCHLD
		select {
		case <-sigCh:
		case <-ctx.Done():
			return nil
		}

		// reap all the zombies
		r.doReaping(ctx)
	}
}

func (r *ZombieProcessReaper) doReaping(ctx context.Context) {
	logger := logr.FromContextOrDiscard(ctx)
	var wstatus syscall.WaitStatus
	for {
		pid, err := syscall.Wait4(-1, &wstatus, syscall.WNOHANG, nil)
		if errors.Is(err, syscall.EINTR) {
			continue // Need to retry later
		}

		if errors.Is(err, syscall.ECHILD) || pid == 0 {
			return // No more zombies left, can return and wait for another SIGCHLD
		}

		if err != nil {
			logger.Error(err, "zombie reaper error in wait4 sigcall")
			return
		}

		logger.V(1).Info("Handled process finish", "pid", pid)
		notifyAllSubscribers(pid, wstatus)
	}
}
