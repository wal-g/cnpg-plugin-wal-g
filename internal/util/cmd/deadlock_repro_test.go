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
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestReaperDeadlockUnderConcurrentExits reproduces a deadlock in the SIGCHLD
// reaper that happens when several subprocesses exit concurrently.
//
// notifyAllSubscribers broadcasts every exit to every subscriber with a
// blocking send, while still holding subscribersMx (the Unlock is deferred, so
// it only runs on return). A subscriber is registered but not reading during
// two windows in Builder.Run:
//
//  1. between subscribeOnProcessExits() and wait(), which spans cmd.Start();
//  2. between wait() matching its own pid and the deferred unsubscribe.
//
// If more than the channel buffer worth of exits land in such a window, the
// reaper blocks on the send while holding the mutex, so
// unsubscribeFromProcessExits() blocks on the same mutex. Nothing can drain or
// remove the channel that is blocking the reaper: Wait4 is never called again
// and every wait() hangs forever.
//
// Without the fix this stalls after a handful of executions; with per-pid
// delivery it completes all of them in a few seconds.
func TestReaperDeadlockUnderConcurrentExits(t *testing.T) {
	const (
		workers   = 32
		perWorker = 400
		stallFor  = 20 * time.Second
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reaper := &ZombieProcessReaper{}
	go func() { _ = reaper.Start(ctx) }()
	time.Sleep(200 * time.Millisecond) // let the reaper install its SIGCHLD handler

	var done int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				if ctx.Err() != nil {
					return
				}
				_, _ = New("/bin/true").WithContext(ctx).Run()
				atomic.AddInt64(&done, 1)
			}
		}()
	}

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()

	// Watchdog: if the completion counter stops advancing, we are deadlocked.
	last := int64(-1)
	lastChange := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-finished:
			t.Logf("no deadlock: %d executions completed", atomic.LoadInt64(&done))
			return
		case <-tick.C:
			cur := atomic.LoadInt64(&done)
			if cur != last {
				last = cur
				lastChange = time.Now()
				t.Logf("progress: %d/%d", cur, workers*perWorker)
				continue
			}
			if time.Since(lastChange) >= stallFor {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				fmt.Printf("\n===== DEADLOCK: no progress for %s after %d executions =====\n", stallFor, cur)
				fmt.Printf("%s\n", buf[:n])
				t.Fatalf("deadlock reproduced: stalled at %d/%d executions", cur, workers*perWorker)
			}
		}
	}
}
