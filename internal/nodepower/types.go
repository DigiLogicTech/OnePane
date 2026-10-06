package nodepower

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type State string

const (
	StateUnknown       State = "unknown"
	StateOnline        State = "online"
	StateWakeableStale State = "wakeable_stale"
	StateWakeRequested State = "wake_requested"
	StateWaking        State = "waking"
	StateReconnecting  State = "reconnecting"
	StatePreparing     State = "preparing"
	StateReady         State = "ready"
	StateBusy          State = "busy"
	StateDraining      State = "draining"
	StateDormant       State = "dormant"
	StateOffline       State = "offline"
	StateFailed        State = "failed"
)

type WakeMethod string

const (
	WakeMethodWOL     WakeMethod = "wol"
	WakeMethodRelay   WakeMethod = "relay"
	WakeMethodRedfish WakeMethod = "redfish"
	WakeMethodIPMI    WakeMethod = "ipmi"
)

type WakeTarget struct {
	MAC     string `json:"mac,omitempty"`
	Address string `json:"address,omitempty"`
}

type Profile struct {
	NodeID                string        `json:"node_id"`
	Enabled               bool          `json:"enabled"`
	WakeMethod            WakeMethod    `json:"wake_method"`
	WakeTargets           []WakeTarget  `json:"wake_targets,omitempty"`
	WakeTimeout           time.Duration `json:"wake_timeout"`
	ReadyStabilization    time.Duration `json:"ready_stabilization"`
	AllowWakeOnBattery    bool          `json:"allow_wake_on_battery"`
	MinimumBatteryPercent int           `json:"minimum_battery_percent"`
	MaxPowerHoldTTL       time.Duration `json:"max_power_hold_ttl"`
	DefaultPowerHoldTTL   time.Duration `json:"default_power_hold_ttl"`
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.NodeID) == "" {
		return errors.New("nodepower: node id is required")
	}
	if !p.Enabled {
		return nil
	}
	if p.WakeMethod == "" {
		return errors.New("nodepower: wake method is required when wake is enabled")
	}
	if p.WakeTimeout <= 0 {
		return errors.New("nodepower: wake timeout must be positive")
	}
	if p.MinimumBatteryPercent < 0 || p.MinimumBatteryPercent > 100 {
		return errors.New("nodepower: minimum battery percent must be between 0 and 100")
	}
	if p.MaxPowerHoldTTL < 0 || p.DefaultPowerHoldTTL < 0 {
		return errors.New("nodepower: power hold TTLs cannot be negative")
	}
	if p.MaxPowerHoldTTL > 0 && p.DefaultPowerHoldTTL > p.MaxPowerHoldTTL {
		return errors.New("nodepower: default power hold TTL exceeds maximum")
	}
	return nil
}

type PowerTelemetry struct {
	Known          bool      `json:"known"`
	OnBattery      bool      `json:"on_battery"`
	BatteryPercent *int      `json:"battery_percent,omitempty"`
	CapturedAt     time.Time `json:"captured_at"`
}

func (p Profile) AllowsWake(t PowerTelemetry) error {
	if !p.Enabled {
		return errors.New("nodepower: remote wake is disabled")
	}
	if !t.Known {
		return nil
	}
	if t.OnBattery && !p.AllowWakeOnBattery {
		return errors.New("nodepower: wake blocked while target is on battery")
	}
	if t.BatteryPercent != nil && p.MinimumBatteryPercent > 0 && *t.BatteryPercent < p.MinimumBatteryPercent {
		return fmt.Errorf("nodepower: wake blocked at %d%% battery; minimum is %d%%", *t.BatteryPercent, p.MinimumBatteryPercent)
	}
	return nil
}

type Readiness struct {
	NodeID              string    `json:"node_id"`
	Paired              bool      `json:"paired"`
	Authenticated       bool      `json:"authenticated"`
	HeartbeatAt         time.Time `json:"heartbeat_at"`
	CapabilityAt        time.Time `json:"capability_at"`
	RuntimeReady        bool      `json:"runtime_ready"`
	RequiredStoresReady bool      `json:"required_stores_ready"`
	Fresh               bool      `json:"fresh"`
}

func (r Readiness) ReadyAfter(t time.Time) bool {
	return r.Fresh && r.Paired && r.Authenticated && r.RuntimeReady && r.RequiredStoresReady &&
		!r.HeartbeatAt.Before(t) && !r.CapabilityAt.Before(t)
}

type WakeAttemptState string

const (
	WakeAttemptRequested    WakeAttemptState = "requested"
	WakeAttemptSent         WakeAttemptState = "wake_sent"
	WakeAttemptReconnecting WakeAttemptState = "reconnecting"
	WakeAttemptPreparing    WakeAttemptState = "preparing"
	WakeAttemptReady        WakeAttemptState = "ready"
	WakeAttemptFailed       WakeAttemptState = "failed"
	WakeAttemptTimedOut     WakeAttemptState = "timed_out"
)

type WakeAttempt struct {
	ID             string           `json:"id"`
	NodeID         string           `json:"node_id"`
	TaskID         string           `json:"task_id,omitempty"`
	Provider       WakeMethod       `json:"provider"`
	State          WakeAttemptState `json:"state"`
	RequestedAt    time.Time        `json:"requested_at"`
	DeadlineAt     time.Time        `json:"deadline_at"`
	ReadyAt        *time.Time       `json:"ready_at,omitempty"`
	Failure        string           `json:"failure,omitempty"`
	IdempotencyKey string           `json:"idempotency_key"`
}

var (
	ErrNotWakeable = errors.New("nodepower: node is not wakeable")
	ErrWakeTimeout = errors.New("nodepower: wake timed out before authenticated readiness")
)
