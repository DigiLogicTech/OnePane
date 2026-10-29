package daemonpoll

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestNextWaitBacksOffOnlyWhileIdle(t *testing.T) {
	base := 2 * time.Second
	max := 20 * time.Second
	if got := NextWait(0, base, max, false); got != base {
		t.Fatalf("first wait %s", got)
	}
	if got := NextWait(base, base, max, false); got != 4*time.Second {
		t.Fatalf("idle wait %s", got)
	}
	if got := NextWait(16*time.Second, base, max, false); got != max {
		t.Fatalf("capped wait %s", got)
	}
	if got := NextWait(max, base, max, true); got != base {
		t.Fatalf("busy wait %s", got)
	}
}

func TestRunWakesImmediatelyAndBacksOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signal := NewSignal()
	var calls atomic.Int32
	started := make(chan struct{})
	go Run(ctx, 40*time.Millisecond, 200*time.Millisecond, signal.C(), func(context.Context) (bool, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		return false, nil
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("loop did not start")
	}
	time.Sleep(350 * time.Millisecond)
	idle := calls.Load()
	if idle > 5 {
		t.Fatalf("idle loop polled %d times in 350ms; backoff did not engage", idle)
	}
	signal.Nudge()
	signal.Nudge()
	deadline := time.Now().Add(300 * time.Millisecond)
	for calls.Load() == idle && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() == idle {
		t.Fatal("wake did not run the idle loop")
	}
	cancel()
}
