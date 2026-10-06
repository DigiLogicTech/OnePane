package nodepower

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	profile Profile
	telem   PowerTelemetry
}

func (f fakeStore) Profile(context.Context, string) (Profile, error) { return f.profile, nil }
func (f fakeStore) LastPowerTelemetry(context.Context, string) (PowerTelemetry, error) {
	return f.telem, nil
}

type fakeAttemptStore struct {
	mu     sync.Mutex
	states []WakeAttemptState
}

func (f *fakeAttemptStore) BeginWakeAttempt(_ context.Context, a WakeAttempt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, a.State)
	return nil
}
func (f *fakeAttemptStore) UpdateWakeAttempt(_ context.Context, a WakeAttempt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, a.State)
	return nil
}

type fakeWake struct{ calls int }

func (f *fakeWake) Method() WakeMethod                  { return WakeMethodWOL }
func (f *fakeWake) Wake(context.Context, Profile) error { f.calls++; return nil }

type fakeProbe struct {
	now time.Time
	bad bool
}

func (f fakeProbe) Snapshot(context.Context, string) (Readiness, error) {
	return Readiness{}, errors.New("stale")
}
func (f fakeProbe) WaitForReady(_ context.Context, node string, notBefore time.Time) (Readiness, error) {
	if f.bad {
		return Readiness{NodeID: node, Paired: true, Authenticated: false, HeartbeatAt: f.now, CapabilityAt: f.now, RuntimeReady: true, RequiredStoresReady: true, Fresh: true}, nil
	}
	at := f.now
	if at.Before(notBefore) {
		at = notBefore.Add(time.Millisecond)
	}
	return Readiness{NodeID: node, Paired: true, Authenticated: true, HeartbeatAt: at, CapabilityAt: at, RuntimeReady: true, RequiredStoresReady: true, Fresh: true}, nil
}

func TestEnsureReadyRequiresFreshAuthenticatedFederationReadiness(t *testing.T) {
	now := time.Now().UTC()
	wake := &fakeWake{}
	attempts := &fakeAttemptStore{}
	c := Coordinator{
		Profiles: fakeStore{profile: Profile{NodeID: "n1", Enabled: true, WakeMethod: WakeMethodWOL, WakeTimeout: time.Second}},
		Attempts: attempts, Probe: fakeProbe{now: now.Add(time.Second)}, Providers: map[WakeMethod]WakeProvider{WakeMethodWOL: wake},
		Now: func() time.Time { return now }, NewID: func() string { return "w1" },
	}
	got, err := c.EnsureReady(context.Background(), EnsureReadyRequest{NodeID: "n1", TaskID: "t1", IdempotencyKey: "t1:n1"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Authenticated || wake.calls != 1 {
		t.Fatalf("got=%+v wake.calls=%d", got, wake.calls)
	}
}

func TestEnsureReadyRejectsUnauthenticatedReturn(t *testing.T) {
	now := time.Now().UTC()
	c := Coordinator{
		Profiles: fakeStore{profile: Profile{NodeID: "n1", Enabled: true, WakeMethod: WakeMethodWOL, WakeTimeout: time.Second}},
		Attempts: &fakeAttemptStore{}, Probe: fakeProbe{now: now.Add(time.Second), bad: true}, Providers: map[WakeMethod]WakeProvider{WakeMethodWOL: &fakeWake{}},
		Now: func() time.Time { return now }, NewID: func() string { return "w1" },
	}
	_, err := c.EnsureReady(context.Background(), EnsureReadyRequest{NodeID: "n1", TaskID: "t1"})
	if err == nil {
		t.Fatal("expected readiness rejection")
	}
}
