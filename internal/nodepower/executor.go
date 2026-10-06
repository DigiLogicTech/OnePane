package nodepower

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrPowerHoldLost = errors.New("nodepower: remote power hold was lost during execution")

type RemoteHoldClient interface {
	Acquire(ctx context.Context, nodeID string, lease PowerHoldLease) error
	Renew(ctx context.Context, nodeID, leaseID string, expiresAt time.Time) error
	Release(ctx context.Context, nodeID, leaseID string) error
}

type TaskExecutor interface {
	ExecuteOnNode(ctx context.Context, nodeID, taskID string) error
}

type WakeToExecuteRunner struct {
	Coordinator *Coordinator
	Holds       RemoteHoldClient
	Executor    TaskExecutor
	LeaseTTL    time.Duration
	RenewEvery  time.Duration
	Now         func() time.Time
	NewLeaseID  func() string
}

type ExecuteRequest struct {
	NodeID         string
	TaskID         string
	OriginNodeID   string
	IdempotencyKey string
}

// Execute wakes a dormant/wakeable node if necessary, proves fresh M33 readiness,
// acquires a task-bound keep-awake lease on the target, and only then dispatches
// the Task. The target is permitted to return to its native power policy only
// after the lease is released or expires.
func (r *WakeToExecuteRunner) Execute(ctx context.Context, req ExecuteRequest) error {
	if r.Coordinator == nil || r.Holds == nil || r.Executor == nil {
		return errors.New("nodepower: wake-to-execute dependencies are incomplete")
	}
	if req.NodeID == "" || req.TaskID == "" || req.OriginNodeID == "" {
		return errors.New("nodepower: node id, task id and origin node id are required")
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.NewLeaseID == nil {
		r.NewLeaseID = func() string { return fmt.Sprintf("power-%d", r.Now().UnixNano()) }
	}
	ttl := r.LeaseTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	renew := r.RenewEvery
	if renew <= 0 {
		renew = ttl / 3
	}
	if renew <= 0 || renew >= ttl {
		return errors.New("nodepower: renew interval must be positive and shorter than lease TTL")
	}

	if _, err := r.Coordinator.EnsureReady(ctx, EnsureReadyRequest{
		NodeID: req.NodeID, TaskID: req.TaskID, IdempotencyKey: req.IdempotencyKey,
	}); err != nil {
		return err
	}

	leaseID := r.NewLeaseID()
	lease := PowerHoldLease{
		LeaseID: leaseID, OriginNodeID: req.OriginNodeID, TaskID: req.TaskID,
		ExpiresAt: r.Now().Add(ttl),
	}
	if err := r.Holds.Acquire(ctx, req.NodeID, lease); err != nil {
		return fmt.Errorf("nodepower: target became ready but keep-awake lease failed: %w", err)
	}
	defer func() { _ = r.Holds.Release(context.WithoutCancel(ctx), req.NodeID, leaseID) }()

	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	renewErr := make(chan error, 1)
	done := make(chan struct{})
	var once sync.Once
	stopRenew := func() { once.Do(func() { close(done) }) }
	defer stopRenew()

	go func() {
		ticker := time.NewTicker(renew)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-execCtx.Done():
				return
			case <-ticker.C:
				expires := r.Now().Add(ttl)
				renewCtx, renewCancel := context.WithTimeout(context.Background(), minDuration(renew, 10*time.Second))
				err := r.Holds.Renew(renewCtx, req.NodeID, leaseID, expires)
				renewCancel()
				if err != nil {
					select {
					case renewErr <- err:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()

	execErr := r.Executor.ExecuteOnNode(execCtx, req.NodeID, req.TaskID)
	stopRenew()
	select {
	case err := <-renewErr:
		// The existing OnePane recovery/verification path must decide whether an
		// interrupted side-effecting task is safe to retry. Do not auto-replay here.
		return fmt.Errorf("%w: %v", ErrPowerHoldLost, err)
	default:
	}
	return execErr
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
