package nodepower

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type InhibitHandle interface {
	Release() error
}

type Inhibitor interface {
	Acquire(ctx context.Context, reason string) (InhibitHandle, error)
	Supported() bool
	Name() string
}

var ErrInhibitorUnsupported = errors.New("nodepower: sleep inhibitor unsupported on this host")

type PowerHoldLease struct {
	LeaseID      string    `json:"lease_id"`
	OriginNodeID string    `json:"origin_node_id"`
	TaskID       string    `json:"task_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type HoldManager struct {
	inhibitor Inhibitor
	maxTTL    time.Duration

	mu       sync.Mutex
	platform InhibitHandle
	leases   map[string]*leaseEntry
}

type leaseEntry struct {
	lease PowerHoldLease
	timer *time.Timer
}

func NewHoldManager(inhibitor Inhibitor, maxTTL time.Duration) *HoldManager {
	if maxTTL <= 0 {
		maxTTL = 15 * time.Minute
	}
	return &HoldManager{inhibitor: inhibitor, maxTTL: maxTTL, leases: make(map[string]*leaseEntry)}
}

func (m *HoldManager) Acquire(ctx context.Context, lease PowerHoldLease) error {
	if lease.LeaseID == "" || lease.OriginNodeID == "" || lease.TaskID == "" {
		return errors.New("nodepower: lease id, origin node id and task id are required")
	}
	ttl := time.Until(lease.ExpiresAt)
	if ttl <= 0 {
		return errors.New("nodepower: power hold lease is already expired")
	}
	if ttl > m.maxTTL {
		return fmt.Errorf("nodepower: power hold TTL %s exceeds maximum %s", ttl.Round(time.Second), m.maxTTL)
	}
	if m.inhibitor == nil || !m.inhibitor.Supported() {
		return ErrInhibitorUnsupported
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.leases[lease.LeaseID]; existing != nil {
		if existing.lease.OriginNodeID != lease.OriginNodeID || existing.lease.TaskID != lease.TaskID {
			return errors.New("nodepower: lease id is already bound to another origin/task")
		}
		existing.lease.ExpiresAt = lease.ExpiresAt
		existing.timer.Reset(time.Until(lease.ExpiresAt))
		return nil
	}
	if len(m.leases) == 0 {
		h, err := m.inhibitor.Acquire(context.Background(), "OnePane active remote task")
		if err != nil {
			return err
		}
		m.platform = h
	}
	entry := &leaseEntry{lease: lease}
	entry.timer = time.AfterFunc(time.Until(lease.ExpiresAt), func() { _ = m.Release(lease.LeaseID) })
	m.leases[lease.LeaseID] = entry
	return nil
}

func (m *HoldManager) Renew(leaseID string, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 || ttl > m.maxTTL {
		return errors.New("nodepower: invalid renewal expiry")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.leases[leaseID]
	if entry == nil {
		return errors.New("nodepower: power hold lease not found")
	}
	entry.lease.ExpiresAt = expiresAt
	entry.timer.Reset(ttl)
	return nil
}

func (m *HoldManager) Release(leaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.leases[leaseID]
	if entry == nil {
		return nil
	}
	entry.timer.Stop()
	delete(m.leases, leaseID)
	if len(m.leases) == 0 && m.platform != nil {
		err := m.platform.Release()
		m.platform = nil
		return err
	}
	return nil
}

func (m *HoldManager) Active() []PowerHoldLease {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PowerHoldLease, 0, len(m.leases))
	for _, e := range m.leases {
		out = append(out, e.lease)
	}
	return out
}
