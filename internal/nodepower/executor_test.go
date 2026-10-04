package nodepower

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeRemoteHolds struct {
	mu                      sync.Mutex
	acquire, renew, release int
}

func (f *fakeRemoteHolds) Acquire(context.Context, string, PowerHoldLease) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquire++
	return nil
}
func (f *fakeRemoteHolds) Renew(context.Context, string, string, time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renew++
	return nil
}
func (f *fakeRemoteHolds) Release(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.release++
	return nil
}

type fakeExecutor struct{ ran bool }

func (f *fakeExecutor) ExecuteOnNode(context.Context, string, string) error { f.ran = true; return nil }

func TestWakeToExecuteAcquiresHoldBeforeDispatch(t *testing.T) {
	now := time.Now().UTC()
	wake := &fakeWake{}
	c := &Coordinator{
		Profiles: fakeStore{profile: Profile{NodeID: "n1", Enabled: true, WakeMethod: WakeMethodWOL, WakeTimeout: time.Second}},
		Attempts: &fakeAttemptStore{}, Probe: fakeProbe{now: now.Add(time.Second)}, Providers: map[WakeMethod]WakeProvider{WakeMethodWOL: wake},
		Now: func() time.Time { return now }, NewID: func() string { return "wake1" },
	}
	holds := &fakeRemoteHolds{}
	exec := &fakeExecutor{}
	r := WakeToExecuteRunner{Coordinator: c, Holds: holds, Executor: exec, LeaseTTL: time.Second, RenewEvery: 250 * time.Millisecond, Now: func() time.Time { return now }, NewLeaseID: func() string { return "lease1" }}
	if err := r.Execute(context.Background(), ExecuteRequest{NodeID: "n1", TaskID: "t1", OriginNodeID: "origin"}); err != nil {
		t.Fatal(err)
	}
	if !exec.ran {
		t.Fatal("task was not dispatched")
	}
	if holds.acquire != 1 || holds.release != 1 {
		t.Fatalf("holds acquire=%d release=%d", holds.acquire, holds.release)
	}
}
