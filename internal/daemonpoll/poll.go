package daemonpoll

import (
	"context"
	"sync"
	"time"
)

// Signal wakes idle background loops without a fixed short poll.
// Nudge coalesces: a loop that is already awake does not queue work.
type Signal struct {
	mu sync.Mutex
	ch chan struct{}
}

func NewSignal() *Signal {
	return &Signal{ch: make(chan struct{}, 1)}
}

func (s *Signal) Nudge() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case s.ch <- struct{}{}:
	default:
	}
}

func (s *Signal) C() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.ch
}

// NextWait doubles an idle interval up to max and resets it when work was found.
func NextWait(current, base, max time.Duration, busy bool) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if max < base {
		max = base
	}
	if busy || current <= 0 {
		return base
	}
	next := current * 2
	if next > max || next < current {
		return max
	}
	return next
}

// Run calls work immediately, then waits. Idle rounds back off. A wake or
// new work returns the interval to base. The callback reports whether it
// found something to do.
func Run(ctx context.Context, base, max time.Duration, wake <-chan struct{}, work func(context.Context) (bool, error)) {
	wait := base
	for {
		if ctx.Err() != nil {
			return
		}
		busy, err := work(ctx)
		if ctx.Err() != nil {
			return
		}
		wait = NextWait(wait, base, max, busy || err != nil)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
			wait = base
		case <-timer.C:
		}
	}
}
