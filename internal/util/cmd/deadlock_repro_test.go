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

// Reproduz o deadlock do reaper de SIGCHLD sob saidas concorrentes de subprocesso.
//
// Hipotese: notifyAllSubscribers faz envio BLOQUEANTE para todos os assinantes
// SEGURANDO subscribersMx (o defer Unlock so roda no return). Um assinante que
// esteja numa "janela surda" -- entre subscribe e wait(), durante cmd.Start(),
// ou entre o match e o defer unsubscribe -- nao le do canal. Com >8 saidas nessa
// janela o canal enche, o reaper trava com o mutex na mao, e o unsubscribe de
// qualquer goroutine trava no mesmo mutex. Deadlock auto-reforcante.
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
	time.Sleep(200 * time.Millisecond) // deixa o reaper registrar o handler de SIGCHLD

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

	// watchdog: se o contador parar de avancar, e deadlock
	last := int64(-1)
	lastChange := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-finished:
			t.Logf("NAO REPRODUZIU: %d execucoes concluidas sem travar", atomic.LoadInt64(&done))
			return
		case <-tick.C:
			cur := atomic.LoadInt64(&done)
			if cur != last {
				last = cur
				lastChange = time.Now()
				t.Logf("progresso: %d/%d", cur, workers*perWorker)
				continue
			}
			if time.Since(lastChange) >= stallFor {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				fmt.Printf("\n===== DEADLOCK: sem progresso por %s em %d execucoes =====\n", stallFor, cur)
				fmt.Printf("%s\n", buf[:n])
				t.Fatalf("DEADLOCK REPRODUZIDO: travou em %d/%d execucoes", cur, workers*perWorker)
			}
		}
	}
}
