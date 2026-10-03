package runtimecoord

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
)

// Service is the application-wide model compute scheduler. It deliberately
// schedules compute pools (CPU / individual GPU devices / hybrid claims), not
// Projects or Workspaces. This lets otherwise independent work share hardware
// safely while preserving OnePane's existing per-runtime residency supervisor.
type Service struct {
	db    *sql.DB
	clock clock.Clock

	mu      sync.Mutex
	waiters []*waiter
	active  map[string]*leaseState
}

type waiter struct {
	req      inference.RuntimeScheduleRequest
	claim    Claim
	queuedAt int64
	ready    chan *Lease
	cancel   chan struct{}
}

type leaseState struct {
	ID       string
	Request  inference.RuntimeScheduleRequest
	Claim    Claim
	Acquired int64
}

type Claim struct {
	ComputeMode string   `json:"compute_mode"`
	Backend     string   `json:"backend,omitempty"`
	NodeID      string   `json:"node_id,omitempty"`
	ResourceIDs []string `json:"resource_ids,omitempty"`
	RAMBytes    int64    `json:"ram_bytes,omitempty"`
	VRAMBytes   int64    `json:"vram_bytes,omitempty"`
	Resident    bool     `json:"resident,omitempty"`
	Cached      bool     `json:"cached,omitempty"`
}

type QueueEntry struct {
	ID                 string          `json:"id"`
	WorkspaceID        string          `json:"workspace_id,omitempty"`
	TaskID             *string         `json:"task_id,omitempty"`
	InferenceRequestID string          `json:"inference_request_id,omitempty"`
	DeploymentID       string          `json:"deployment_id"`
	NodeID             string          `json:"node_id,omitempty"`
	Priority           string          `json:"priority"`
	State              string          `json:"state"`
	SafeBoundary       string          `json:"safe_boundary"`
	ResourceClaim      json.RawMessage `json:"resource_claim"`
	QueuedAt           int64           `json:"queued_at"`
	StartedAt          *int64          `json:"started_at,omitempty"`
	CompletedAt        *int64          `json:"completed_at,omitempty"`
	FailureReason      *string         `json:"failure_reason,omitempty"`
}

type Lease struct {
	service *Service
	id      string
	once    sync.Once
}

func NewService(db *sql.DB, clk clock.Clock) *Service {
	return &Service{db: db, clock: clk, active: map[string]*leaseState{}}
}

func (s *Service) Acquire(ctx context.Context, req inference.RuntimeScheduleRequest) (inference.RuntimeScheduleLease, error) {
	if s == nil || s.db == nil || strings.TrimSpace(req.Deployment.ID) == "" {
		return nil, errors.New("runtime coordinator unavailable or deployment missing")
	}
	claim, err := s.claimForDeployment(ctx, req.Deployment)
	if err != nil {
		return nil, err
	}
	if claim.ComputeMode == "cloud" || len(claim.ResourceIDs) == 0 {
		return &Lease{}, nil
	}
	if strings.TrimSpace(req.Priority) == "" {
		req.Priority = "normal"
	}
	w := &waiter{req: req, claim: claim, queuedAt: s.clock.UnixMilli(), ready: make(chan *Lease, 1), cancel: make(chan struct{})}
	if err := s.persistQueued(ctx, w); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.waiters = append(s.waiters, w)
	s.scheduleLocked()
	s.mu.Unlock()

	select {
	case lease := <-w.ready:
		return lease, nil
	case <-ctx.Done():
		s.mu.Lock()
		for i, x := range s.waiters {
			if x == w {
				s.waiters = append(s.waiters[:i], s.waiters[i+1:]...)
				break
			}
		}
		s.scheduleLocked()
		s.mu.Unlock()
		_ = s.persistTerminal(context.Background(), req.InferenceRequestID, "cancelled", ctx.Err().Error())
		return nil, ctx.Err()
	}
}

func (l *Lease) ReportBoundary(ctx context.Context, boundary string) error {
	if l == nil || l.service == nil || l.id == "" {
		return nil
	}
	switch boundary {
	case "unsafe", "safe_turn", "safe_pause", "complete":
	default:
		return fmt.Errorf("invalid execution boundary %q", boundary)
	}
	_, err := l.service.db.ExecContext(ctx, `UPDATE runtime_execution_queue SET safe_boundary=?,revision=revision+1 WHERE id=?`, boundary, l.id)
	return err
}

func (l *Lease) Release(ctx context.Context, failure error) {
	if l == nil || l.service == nil || l.id == "" {
		return
	}
	l.once.Do(func() { l.service.release(ctx, l.id, failure) })
}

func (s *Service) release(ctx context.Context, id string, failure error) {
	s.mu.Lock()
	delete(s.active, id)
	s.scheduleLocked()
	s.mu.Unlock()
	state, reason := "completed", ""
	if failure != nil {
		state, reason = "failed", failure.Error()
	}
	_ = s.persistTerminal(ctx, id, state, reason)
}

func (s *Service) scheduleLocked() {
	if len(s.waiters) == 0 {
		return
	}
	now := s.clock.UnixMilli()
	sort.SliceStable(s.waiters, func(i, j int) bool {
		a, b := s.waiters[i], s.waiters[j]
		pa, pb := priorityScore(a.req.Priority), priorityScore(b.req.Priority)
		pa += int((now - a.queuedAt) / int64(30*time.Second/time.Millisecond))
		pb += int((now - b.queuedAt) / int64(30*time.Second/time.Millisecond))
		if a.claim.Resident {
			pa += 1
		}
		if b.claim.Resident {
			pb += 1
		}
		if pa != pb {
			return pa > pb
		}
		return a.queuedAt < b.queuedAt
	})

	for i := 0; i < len(s.waiters); {
		w := s.waiters[i]
		if !s.resourcesFreeLocked(w.claim.ResourceIDs) {
			i++
			continue
		}
		id := strings.TrimSpace(w.req.InferenceRequestID)
		if id == "" {
			id = fmt.Sprintf("runtime-%d", w.queuedAt)
		}
		st := &leaseState{ID: id, Request: w.req, Claim: w.claim, Acquired: now}
		s.active[id] = st
		s.waiters = append(s.waiters[:i], s.waiters[i+1:]...)
		_ = s.persistRunning(context.Background(), id, now)
		w.ready <- &Lease{service: s, id: id}
	}
}

func (s *Service) resourcesFreeLocked(resources []string) bool {
	for _, active := range s.active {
		for _, a := range active.Claim.ResourceIDs {
			for _, wanted := range resources {
				if a == wanted {
					return false
				}
			}
		}
	}
	return true
}

func priorityScore(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "interactive":
		return 400
	case "high":
		return 300
	case "normal":
		return 200
	case "background":
		return 100
	default:
		return 200
	}
}

func (s *Service) claimForDeployment(ctx context.Context, d inference.ModelDeployment) (Claim, error) {
	claim := Claim{ComputeMode: "cloud"}
	if d.ProviderConnectionID != nil {
		return claim, nil
	}
	if d.NodeID != nil {
		claim.NodeID = *d.NodeID
	}
	var mode, backend, gpuJSON string
	var ram, vram sql.NullInt64
	var cached, resident int
	err := s.db.QueryRowContext(ctx, `SELECT compute_mode,COALESCE(backend,''),COALESCE(ram_bytes,0),gpu_device_ids_json,COALESCE(vram_bytes,0),cached,resident FROM runtime_compute_profiles WHERE deployment_id=?`, d.ID).Scan(&mode, &backend, &ram, &gpuJSON, &vram, &cached, &resident)
	if err == nil {
		claim.ComputeMode, claim.Backend, claim.RAMBytes, claim.VRAMBytes = mode, backend, ram.Int64, vram.Int64
		claim.Cached, claim.Resident = cached != 0, resident != 0
		var devices []string
		_ = json.Unmarshal([]byte(gpuJSON), &devices)
		claim.ResourceIDs = resourceIDs(claim.NodeID, mode, devices)
		return claim, nil
	}
	if err != sql.ErrNoRows {
		return claim, err
	}

	var cfg struct {
		RuntimeBackend string `json:"runtime_backend"`
		Placement      struct {
			Mode    string `json:"mode"`
			Backend string `json:"backend"`
			Devices []struct {
				Kind           string `json:"kind"`
				DeviceID       string `json:"device_id"`
				DeviceIndex    int    `json:"device_index"`
				AllocatedBytes int64  `json:"allocated_bytes"`
			} `json:"devices"`
		} `json:"placement"`
	}
	_ = json.Unmarshal(d.RuntimeConfigJSON, &cfg)
	backend = firstNonEmpty(cfg.Placement.Backend, cfg.RuntimeBackend)
	devices := []string{}
	mode = "cpu"
	for _, dev := range cfg.Placement.Devices {
		if dev.Kind == "accelerator" {
			mode = "gpu"
			id := strings.TrimSpace(dev.DeviceID)
			if id == "" {
				id = fmt.Sprintf("gpu-%d", dev.DeviceIndex)
			}
			devices = append(devices, id)
			claim.VRAMBytes += dev.AllocatedBytes
		}
		if dev.Kind == "cpu" && mode == "gpu" {
			mode = "hybrid"
		}
		if dev.Kind == "cpu" {
			claim.RAMBytes += dev.AllocatedBytes
		}
	}
	if cfg.Placement.Mode == "cpu_offload" {
		mode = "hybrid"
	}
	claim.ComputeMode, claim.Backend = mode, backend
	claim.ResourceIDs = resourceIDs(claim.NodeID, mode, devices)
	return claim, nil
}

func resourceIDs(node, mode string, devices []string) []string {
	if node == "" {
		node = "local"
	}
	out := []string{}
	if mode == "cpu" || mode == "hybrid" {
		out = append(out, "node:"+node+":cpu")
	}
	if mode == "gpu" || mode == "hybrid" {
		if len(devices) == 0 {
			devices = []string{"gpu-0"}
		}
		for _, d := range devices {
			out = append(out, "node:"+node+":gpu:"+d)
		}
	}
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (s *Service) persistQueued(ctx context.Context, w *waiter) error {
	raw, _ := json.Marshal(w.claim)
	id := strings.TrimSpace(w.req.InferenceRequestID)
	if id == "" {
		id = fmt.Sprintf("runtime-%d", w.queuedAt)
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO runtime_execution_queue(id,workspace_id,task_id,inference_request_id,deployment_id,node_id,priority,state,safe_boundary,resource_claim_json,queued_at,revision) VALUES(?,?,?,?,?,?,?,'queued','safe_pause',?,?,1)`, id, w.req.WorkspaceID, w.req.TaskID, nullable(w.req.InferenceRequestID), w.req.Deployment.ID, nullable(w.claim.NodeID), w.req.Priority, string(raw), w.queuedAt)
	return err
}

func (s *Service) persistRunning(ctx context.Context, id string, at int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runtime_execution_queue SET state='running',started_at=?,safe_boundary='unsafe',revision=revision+1 WHERE id=?`, at, id)
	return err
}

func (s *Service) persistTerminal(ctx context.Context, id, state, reason string) error {
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE runtime_execution_queue SET state=?,completed_at=?,safe_boundary='complete',failure_reason=?,revision=revision+1 WHERE id=?`, state, now, nullable(reason), id)
	return err
}

func nullable(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.TrimSpace(v)
}

func (s *Service) Snapshot(ctx context.Context, limit int) ([]QueueEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(workspace_id,''),task_id,inference_request_id,deployment_id,COALESCE(node_id,''),priority,state,safe_boundary,resource_claim_json,queued_at,started_at,completed_at,failure_reason FROM runtime_execution_queue ORDER BY queued_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueEntry{}
	for rows.Next() {
		var x QueueEntry
		var task, inf, failure sql.NullString
		var started, completed sql.NullInt64
		var raw string
		if err := rows.Scan(&x.ID, &x.WorkspaceID, &task, &inf, &x.DeploymentID, &x.NodeID, &x.Priority, &x.State, &x.SafeBoundary, &raw, &x.QueuedAt, &started, &completed, &failure); err != nil {
			return nil, err
		}
		if task.Valid {
			v := task.String
			x.TaskID = &v
		}
		if inf.Valid {
			x.InferenceRequestID = inf.String
		}
		if failure.Valid {
			v := failure.String
			x.FailureReason = &v
		}
		if started.Valid {
			v := started.Int64
			x.StartedAt = &v
		}
		if completed.Valid {
			v := completed.Int64
			x.CompletedAt = &v
		}
		x.ResourceClaim = json.RawMessage(raw)
		out = append(out, x)
	}
	return out, rows.Err()
}
