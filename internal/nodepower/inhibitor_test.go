package nodepower

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeInhibitor struct {
	mu                 sync.Mutex
	acquired, released int
}

func (f *fakeInhibitor) Supported() bool { return true }
func (f *fakeInhibitor) Name() string    { return "fake" }
func (f *fakeInhibitor) Acquire(context.Context, string) (InhibitHandle, error) {
	f.mu.Lock()
	f.acquired++
	f.mu.Unlock()
	return fakeHandle{f: f}, nil
}

type fakeHandle struct{ f *fakeInhibitor }

func (h fakeHandle) Release() error { h.f.mu.Lock(); h.f.released++; h.f.mu.Unlock(); return nil }

func TestHoldManagerReferenceCountsAndExpires(t *testing.T) {
	f := &fakeInhibitor{}
	m := NewHoldManager(f, 2*time.Second)
	ctx := context.Background()
	if err := m.Acquire(ctx, PowerHoldLease{LeaseID: "a", OriginNodeID: "origin", TaskID: "task1", ExpiresAt: time.Now().Add(500 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if err := m.Acquire(ctx, PowerHoldLease{LeaseID: "b", OriginNodeID: "origin", TaskID: "task2", ExpiresAt: time.Now().Add(500 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if f.acquired != 1 {
		t.Fatalf("acquired=%d want 1", f.acquired)
	}
	if err := m.Release("a"); err != nil {
		t.Fatal(err)
	}
	if f.released != 0 {
		t.Fatalf("released early=%d", f.released)
	}
	if err := m.Release("b"); err != nil {
		t.Fatal(err)
	}
	if f.released != 1 {
		t.Fatalf("released=%d want 1", f.released)
	}
}
