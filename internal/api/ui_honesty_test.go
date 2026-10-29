package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/localai"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestClassifyTaskUsesStateWaitAndEventReason(t *testing.T) {
	now := int64(1_000_000)
	cases := []struct {
		name     string
		in       taskStatusInput
		category string
		action   string
		reason   string
	}{
		{name: "running", in: taskStatusInput{State: "running", Now: now, UpdatedAt: now - 5000}, category: "Running", reason: "Work is in progress."},
		{name: "approval", in: taskStatusInput{State: "waiting_approval"}, category: "Needs you", action: "approve", reason: "This task needs your approval before it can continue."},
		{name: "model wait", in: taskStatusInput{State: "waiting_dependency", WaitKind: "model_resources", WaitReason: "No eligible local model"}, category: "Waiting for resources", action: "setup_model", reason: "No eligible local model"},
		{name: "memory wait", in: taskStatusInput{State: "waiting_dependency", WaitKind: "model_resources", WaitReason: "Not enough VRAM"}, category: "Waiting for resources", action: "unload_idle", reason: "Not enough VRAM"},
		{name: "dependency", in: taskStatusInput{State: "waiting_dependency", EventReason: "Predecessor still running"}, category: "Waiting for another task", reason: "Predecessor still running"},
		{name: "blocked", in: taskStatusInput{State: "blocked"}, category: "Blocked/needs fix", action: "inspect"},
		{name: "failed", in: taskStatusInput{State: "failed"}, category: "Failed", action: "inspect"},
		{name: "done", in: taskStatusInput{State: "complete"}, category: "Done", reason: "Finished."},
		{name: "stopped", in: taskStatusInput{State: "cancelled"}, category: "Stopped"},
		{name: "queued", in: taskStatusInput{State: "ready", Now: now, UpdatedAt: now - 1500}, category: "Waiting for resources", reason: "In the queue. No resource hold has been reported."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyTask(tc.in)
			if got.Category != tc.category {
				t.Fatalf("category %q, want %q", got.Category, tc.category)
			}
			if tc.action != "" && got.NextActionID != tc.action {
				t.Fatalf("action %q, want %q", got.NextActionID, tc.action)
			}
			if tc.reason != "" && got.Reason != tc.reason {
				t.Fatalf("reason %q, want %q", got.Reason, tc.reason)
			}
			if tc.in.State == "ready" && got.TimeInStateMS != 1500 {
				t.Fatalf("time in state %d", got.TimeInStateMS)
			}
		})
	}
}

func TestPresentReasonDropsSecrets(t *testing.T) {
	if presentReason("bearer token abc") != "" {
		t.Fatal("secret reason leaked")
	}
	if presentReason("Waiting for the GPU") != "Waiting for the GPU" {
		t.Fatal("plain reason dropped")
	}
}

func TestOrderTaskStatusesPutsHigherPriorityFirst(t *testing.T) {
	rows := []taskStatusInput{
		{ID: "low", Objective: "Background notes", State: "ready", Priority: -20, UpdatedAt: 1},
		{ID: "run", Objective: "Active build", State: "running", Priority: 10, UpdatedAt: 2},
		{ID: "high", Objective: "Fix the menu", State: "waiting_dependency", Priority: 50, UpdatedAt: 3},
	}
	views := []taskStatusView{{TaskID: "low"}, {TaskID: "run"}, {TaskID: "high"}}
	orderTaskStatuses(rows, views)
	if views[1].QueuePosition != nil {
		t.Fatal("running task should not have a queue position")
	}
	if views[2].QueuePosition == nil || *views[2].QueuePosition != 1 {
		t.Fatalf("high priority position %+v", views[2].QueuePosition)
	}
	if views[0].QueuePosition == nil || *views[0].QueuePosition != 2 {
		t.Fatalf("low priority position %+v", views[0].QueuePosition)
	}
	if len(views[2].Ahead) != 1 || views[2].Ahead[0] != "Active build" {
		t.Fatalf("ahead %+v", views[2].Ahead)
	}
	joined := strings.Join(views[0].Ahead, "|")
	if !strings.Contains(joined, "Active build") || !strings.Contains(joined, "Fix the menu") {
		t.Fatalf("low task ahead %+v", views[0].Ahead)
	}
}

func TestSplitMemoryBudgetClampsAndReserves(t *testing.T) {
	const gib = int64(1 << 30)
	total := 16 * gib
	hostFree := gib + gib/2
	got := splitMemoryBudget(total, hostFree, 200<<20, 0, 4*gib, 0, true)
	if !got.available {
		t.Fatal("expected a budget")
	}
	if got.reserve != gib {
		t.Fatalf("reserve %d", got.reserve)
	}
	if got.freeForModels != hostFree-gib {
		t.Fatalf("free %d", got.freeForModels)
	}
	sum := got.otherApps + got.pinned + got.idle + got.inUse + got.reserve + got.freeForModels
	if sum != total {
		t.Fatalf("segments %d != total %d (other %d idle %d reserve %d free %d)", sum, total, got.otherApps, got.idle, got.reserve, got.freeForModels)
	}
	missing := splitMemoryBudget(total, hostFree, 0, 0, 0, 0, false)
	if missing.modelsMeasured || missing.idle != 0 || missing.note == "" {
		t.Fatalf("unmeasured models should not invent a model segment: %+v", missing)
	}
	none := splitMemoryBudget(0, 0, 0, 0, 0, 0, false)
	if none.available {
		t.Fatal("missing host memory reported as available")
	}
}

func TestClassifyResidentModelSkipsStopped(t *testing.T) {
	if _, _, _, ok := classifyResidentModel(localai.ManagedDeploymentSummary{ResidencyState: "stopped", RAMBytes: 100}); ok {
		t.Fatal("stopped model counted")
	}
	p, i, u, ok := classifyResidentModel(localai.ManagedDeploymentSummary{ResidencyState: "resident", RAMBytes: 100, ColibriPinned: true})
	if !ok || p != 100 || i != 0 || u != 0 {
		t.Fatalf("pinned %+v %d %d %d", ok, p, i, u)
	}
	_, i, u, ok = classifyResidentModel(localai.ManagedDeploymentSummary{ResidencyState: "resident", RAMBytes: 80})
	if !ok || i != 80 || u != 0 {
		t.Fatalf("idle %d %d", i, u)
	}
	_, _, u, ok = classifyResidentModel(localai.ManagedDeploymentSummary{ResidencyState: "busy", RAMBytes: 90})
	if !ok || u != 90 {
		t.Fatalf("busy %d", u)
	}
}

func TestTaskStatusSummaryRouteAndShape(t *testing.T) {
	updated := time.Now().Add(-2 * time.Second).UnixMilli()
	fake := honestyTasks{rows: []task.Task{{
		ID: "task-1", WorkspaceID: "ws", Objective: "Write the menu", State: task.StateWaitingDependency,
		Priority: 10, UpdatedAt: updated, Completion: json.RawMessage(`{"ram_bytes":512}`),
	}}}
	s := NewServer(nil, fakeEvents{}, fakeAuth{id: Identity{PrincipalID: "u"}})
	s.SetTasks(fake)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/status-summary?workspace_id=ws", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Tasks  []taskStatusView `json:"tasks"`
		Source string           `json:"source"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tasks) != 1 || body.Tasks[0].Category != "Waiting for another task" || body.Tasks[0].RAMNeededBytes != 512 {
		t.Fatalf("summary %+v", body.Tasks)
	}
	if body.Tasks[0].QueuePosition == nil || *body.Tasks[0].QueuePosition != 1 {
		t.Fatalf("queue %+v", body.Tasks[0].QueuePosition)
	}
	if !strings.Contains(body.Source, "task_state") {
		t.Fatal(body.Source)
	}
}

func TestMemoryBudgetDegradesWithoutHostPanic(t *testing.T) {
	s := NewServer(nil, fakeEvents{}, fakeAuth{id: Identity{PrincipalID: "u"}})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/system/memory-budget?workspace_id=ws", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["admission_wired"] != false {
		t.Fatalf("admission flag %#v", body["admission_wired"])
	}
	if _, ok := body["segments"]; !ok {
		t.Fatal("segments missing")
	}
}

func TestUnloadIdleReportsUnavailableWithoutReaper(t *testing.T) {
	s := NewServer(nil, fakeEvents{}, fakeAuth{id: Identity{PrincipalID: "u"}})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/local-ai/deployments/unload-idle", strings.NewReader(`{"workspace_id":"ws"}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Available bool `json:"available"`
		Unloaded  int  `json:"unloaded"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Available || body.Unloaded != 0 {
		t.Fatalf("%+v", body)
	}
}

type honestyTasks struct{ rows []task.Task }

func (h honestyTasks) Create(context.Context, task.CreateCommand) (task.Task, error) {
	return task.Task{}, nil
}
func (h honestyTasks) List(context.Context, string, int) ([]task.Task, error) { return h.rows, nil }
