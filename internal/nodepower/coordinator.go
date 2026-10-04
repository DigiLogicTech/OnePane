package nodepower

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type ProfileStore interface {
	Profile(ctx context.Context, nodeID string) (Profile, error)
	LastPowerTelemetry(ctx context.Context, nodeID string) (PowerTelemetry, error)
}

type AttemptStore interface {
	BeginWakeAttempt(ctx context.Context, attempt WakeAttempt) error
	UpdateWakeAttempt(ctx context.Context, attempt WakeAttempt) error
}

type ReadyProbe interface {
	Snapshot(ctx context.Context, nodeID string) (Readiness, error)
	WaitForReady(ctx context.Context, nodeID string, notBefore time.Time) (Readiness, error)
}

type WakeProvider interface {
	Method() WakeMethod
	Wake(ctx context.Context, profile Profile) error
}

type Coordinator struct {
	Profiles  ProfileStore
	Attempts  AttemptStore
	Probe     ReadyProbe
	Providers map[WakeMethod]WakeProvider
	Now       func() time.Time
	NewID     func() string

	mu       sync.Mutex
	inflight map[string]*wakeCall
}

type wakeCall struct {
	done      chan struct{}
	readiness Readiness
	err       error
}

type EnsureReadyRequest struct {
	NodeID         string
	TaskID         string
	IdempotencyKey string
}

func (c *Coordinator) EnsureReady(ctx context.Context, req EnsureReadyRequest) (Readiness, error) {
	if req.NodeID == "" {
		return Readiness{}, errors.New("nodepower: node id is required")
	}
	if c.Profiles == nil || c.Attempts == nil || c.Probe == nil {
		return Readiness{}, errors.New("nodepower: coordinator dependencies are incomplete")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.NewID == nil {
		c.NewID = func() string { return fmt.Sprintf("wake-%d", c.Now().UnixNano()) }
	}

	snap, err := c.Probe.Snapshot(ctx, req.NodeID)
	if err == nil && snap.Fresh && snap.Paired && snap.Authenticated && snap.RuntimeReady && snap.RequiredStoresReady {
		return snap, nil
	}

	c.mu.Lock()
	if c.inflight == nil {
		c.inflight = make(map[string]*wakeCall)
	}
	if existing := c.inflight[req.NodeID]; existing != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return Readiness{}, ctx.Err()
		case <-existing.done:
			return existing.readiness, existing.err
		}
	}
	call := &wakeCall{done: make(chan struct{})}
	c.inflight[req.NodeID] = call
	c.mu.Unlock()

	readiness, runErr := c.ensureReadyLeader(ctx, req)

	c.mu.Lock()
	call.readiness, call.err = readiness, runErr
	close(call.done)
	delete(c.inflight, req.NodeID)
	c.mu.Unlock()
	return readiness, runErr
}

func (c *Coordinator) ensureReadyLeader(ctx context.Context, req EnsureReadyRequest) (Readiness, error) {
	profile, err := c.Profiles.Profile(ctx, req.NodeID)
	if err != nil {
		return Readiness{}, err
	}
	if err := profile.Validate(); err != nil {
		return Readiness{}, err
	}
	if !profile.Enabled {
		return Readiness{}, ErrNotWakeable
	}
	telemetry, err := c.Profiles.LastPowerTelemetry(ctx, req.NodeID)
	if err == nil {
		if err := profile.AllowsWake(telemetry); err != nil {
			return Readiness{}, err
		}
	}
	provider := c.Providers[profile.WakeMethod]
	if provider == nil {
		return Readiness{}, fmt.Errorf("%w: provider %q is not configured", ErrNotWakeable, profile.WakeMethod)
	}

	requestedAt := c.Now().UTC()
	timeout := profile.WakeTimeout
	attempt := WakeAttempt{
		ID: c.NewID(), NodeID: req.NodeID, TaskID: req.TaskID, Provider: profile.WakeMethod,
		State: WakeAttemptRequested, RequestedAt: requestedAt, DeadlineAt: requestedAt.Add(timeout),
		IdempotencyKey: req.IdempotencyKey,
	}
	if err := c.Attempts.BeginWakeAttempt(ctx, attempt); err != nil {
		return Readiness{}, err
	}
	if err := provider.Wake(ctx, profile); err != nil {
		attempt.State = WakeAttemptFailed
		attempt.Failure = err.Error()
		_ = c.Attempts.UpdateWakeAttempt(context.WithoutCancel(ctx), attempt)
		return Readiness{}, err
	}
	attempt.State = WakeAttemptSent
	_ = c.Attempts.UpdateWakeAttempt(context.WithoutCancel(ctx), attempt)

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	attempt.State = WakeAttemptReconnecting
	_ = c.Attempts.UpdateWakeAttempt(context.WithoutCancel(ctx), attempt)

	ready, err := c.Probe.WaitForReady(waitCtx, req.NodeID, requestedAt)
	if err != nil {
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			attempt.State = WakeAttemptTimedOut
			attempt.Failure = ErrWakeTimeout.Error()
			_ = c.Attempts.UpdateWakeAttempt(context.WithoutCancel(ctx), attempt)
			return Readiness{}, ErrWakeTimeout
		}
		attempt.State = WakeAttemptFailed
		attempt.Failure = err.Error()
		_ = c.Attempts.UpdateWakeAttempt(context.WithoutCancel(ctx), attempt)
		return Readiness{}, err
	}
	if !ready.ReadyAfter(requestedAt) {
		attempt.State = WakeAttemptFailed
		attempt.Failure = "fresh authenticated heartbeat/capability readiness was not established"
		_ = c.Attempts.UpdateWakeAttempt(context.WithoutCancel(ctx), attempt)
		return Readiness{}, errors.New("nodepower: target reappeared without fresh authenticated readiness")
	}
	if profile.ReadyStabilization > 0 {
		attempt.State = WakeAttemptPreparing
		_ = c.Attempts.UpdateWakeAttempt(context.WithoutCancel(ctx), attempt)
		timer := time.NewTimer(profile.ReadyStabilization)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Readiness{}, ctx.Err()
		case <-timer.C:
		}
	}
	now := c.Now().UTC()
	attempt.State = WakeAttemptReady
	attempt.ReadyAt = &now
	if err := c.Attempts.UpdateWakeAttempt(context.WithoutCancel(ctx), attempt); err != nil {
		return Readiness{}, err
	}
	return ready, nil
}
