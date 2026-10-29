package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

// taskStatusView is a read-only explanation of a task. It does not admit,
// resume, or reorder work. Queue position is the visible priority order,
// not the scheduler cursor owned by the model-admission branch.
type taskStatusView struct {
	TaskID          string   `json:"task_id"`
	Category        string   `json:"category"`
	Reason          string   `json:"reason"`
	NextAction      string   `json:"next_action,omitempty"`
	NextActionID    string   `json:"next_action_id,omitempty"`
	TimeInStateMS   int64    `json:"time_in_state_ms"`
	QueuePosition   *int     `json:"queue_position,omitempty"`
	Ahead           []string `json:"ahead,omitempty"`
	RAMNeededBytes  int64    `json:"ram_needed_bytes,omitempty"`
	VRAMNeededBytes int64    `json:"vram_needed_bytes,omitempty"`
	RAMFreeBytes    *int64   `json:"ram_free_bytes,omitempty"`
	VRAMFreeBytes   *int64   `json:"vram_free_bytes,omitempty"`
}

type taskStatusInput struct {
	ID          string
	Objective   string
	State       string
	Priority    int
	UpdatedAt   int64
	Now         int64
	WaitKind    string
	WaitReason  string
	EventReason string
	RAMNeeded   int64
	VRAMNeeded  int64
}

func presentReason(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 180 || strings.ContainsAny(s, "\r\n") {
		return ""
	}
	low := strings.ToLower(s)
	for _, bad := range []string{"password", "secret", "token", "bearer", "api_key", "authorization", "cookie"} {
		if strings.Contains(low, bad) {
			return ""
		}
	}
	return s
}

func resourceWait(kind, reason string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "model_resources", "toolchain_resources":
		return true
	}
	low := strings.ToLower(reason)
	return strings.Contains(low, "memory") || strings.Contains(low, "vram") || strings.Contains(low, "ram") || strings.Contains(low, "model")
}

func memoryWait(reason string) bool {
	low := strings.ToLower(reason)
	return strings.Contains(low, "memory") || strings.Contains(low, "vram") || strings.Contains(low, "ram") || strings.Contains(low, "unload")
}

// classifyTask explains one task from state plus an optional worker wait and
// the latest ledger reason. Missing pieces stay as plain statements.
func classifyTask(in taskStatusInput) taskStatusView {
	state := strings.ToLower(strings.TrimSpace(in.State))
	reason := presentReason(in.WaitReason)
	if reason == "" {
		reason = presentReason(in.EventReason)
	}
	out := taskStatusView{TaskID: in.ID, RAMNeededBytes: in.RAMNeeded, VRAMNeededBytes: in.VRAMNeeded}
	if in.Now > 0 && in.UpdatedAt > 0 && in.Now >= in.UpdatedAt {
		out.TimeInStateMS = in.Now - in.UpdatedAt
	}
	set := func(category, fallback, action, actionID string) {
		out.Category = category
		if reason == "" {
			reason = fallback
		}
		out.Reason = reason
		out.NextAction = action
		out.NextActionID = actionID
	}
	switch state {
	case string(task.StateRunning), string(task.StateVerifying), string(task.StateCompletionRequested):
		set("Running", "Work is in progress.", "", "")
	case string(task.StateWaitingApproval):
		set("Needs you", "This task needs your approval before it can continue.", "Approve", "approve")
	case string(task.StatePaused):
		set("Needs you", "Paused. It will not continue until you resume it.", "Inspect", "inspect")
	case string(task.StateWaitingDependency):
		if resourceWait(in.WaitKind, reason) {
			if memoryWait(reason) {
				set("Waiting for resources", "Waiting for memory to free up.", "Unload idle model", "unload_idle")
			} else {
				set("Waiting for resources", "Waiting for a model or for memory to free up.", "Set up a model", "setup_model")
			}
		} else {
			set("Waiting for another task", "Waiting for an earlier task to finish.", "", "")
		}
	case string(task.StateBlocked):
		set("Blocked/needs fix", "Blocked. Something needs a fix before this can continue.", "Review the blocker", "inspect")
	case string(task.StateFailed):
		set("Failed", "Failed. The last attempt did not finish.", "Review the failure", "inspect")
	case string(task.StateComplete):
		set("Done", "Finished.", "", "")
	case string(task.StateCancelled), string(task.StateCancelling), string(task.StateCancelRequested), string(task.StateCompensating):
		set("Stopped", "Cancelled. No further work will run.", "", "")
	case string(task.StateCreated), string(task.StateReady), "":
		set("Waiting for resources", "In the queue. No resource hold has been reported.", "", "")
	default:
		set("Waiting for resources", "This state has no clearer explanation yet.", "", "")
	}
	return out
}

func queuedState(state string) bool {
	switch strings.ToLower(state) {
	case string(task.StateCreated), string(task.StateReady), string(task.StateWaitingDependency), string(task.StateWaitingApproval), string(task.StatePaused):
		return true
	default:
		return false
	}
}

func runningState(state string) bool {
	switch strings.ToLower(state) {
	case string(task.StateRunning), string(task.StateVerifying), string(task.StateCompletionRequested):
		return true
	default:
		return false
	}
}

func shortObjective(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

// orderTaskStatuses fills queue position and what is ahead. Higher priority
// numbers run first. This is a display order for the tasks already loaded.
func orderTaskStatuses(rows []taskStatusInput, views []taskStatusView) {
	type item struct {
		i        int
		priority int
		updated  int64
	}
	var running []int
	var queued []item
	for i, row := range rows {
		if runningState(row.State) {
			running = append(running, i)
		} else if queuedState(row.State) {
			queued = append(queued, item{i: i, priority: row.Priority, updated: row.UpdatedAt})
		}
	}
	sort.SliceStable(queued, func(a, b int) bool {
		if queued[a].priority != queued[b].priority {
			return queued[a].priority > queued[b].priority
		}
		if queued[a].updated != queued[b].updated {
			return queued[a].updated < queued[b].updated
		}
		return rows[queued[a].i].ID < rows[queued[b].i].ID
	})
	runningNames := make([]string, 0, len(running))
	for _, i := range running {
		if name := shortObjective(rows[i].Objective); name != "" {
			runningNames = append(runningNames, name)
		}
	}
	for pos, item := range queued {
		n := pos + 1
		views[item.i].QueuePosition = &n
		ahead := append([]string{}, runningNames...)
		for _, earlier := range queued[:pos] {
			if name := shortObjective(rows[earlier.i].Objective); name != "" {
				ahead = append(ahead, name)
			}
		}
		if len(ahead) > 4 {
			ahead = ahead[:4]
		}
		if len(ahead) > 0 {
			views[item.i].Ahead = ahead
		}
	}
}

func taskResourceNeeds(raw json.RawMessage) (ram, vram int64) {
	if len(raw) == 0 || !json.Valid(raw) {
		return 0, 0
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return 0, 0
	}
	var walk func(map[string]any)
	walk = func(m map[string]any) {
		for k, v := range m {
			key := strings.ToLower(k)
			switch n := v.(type) {
			case float64:
				if n <= 0 || n > 1<<42 {
					continue
				}
				switch key {
				case "ram_bytes", "memory_required_bytes", "ram_needed_bytes":
					if ram == 0 {
						ram = int64(n)
					}
				case "vram_bytes", "vram_required_bytes", "vram_needed_bytes":
					if vram == 0 {
						vram = int64(n)
					}
				}
			case map[string]any:
				walk(n)
			}
		}
	}
	walk(doc)
	return ram, vram
}

func loadLatestTaskReasons(ctx context.Context, db *sql.DB, workspace string, ids []string) map[string]string {
	out := map[string]string{}
	if db == nil || workspace == "" || len(ids) == 0 {
		return out
	}
	if len(ids) > 200 {
		ids = ids[:200]
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, workspace)
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	for _, id := range ids {
		args = append(args, id)
	}
	q := `SELECT aggregate_id, payload_json FROM events
WHERE workspace_id=? AND aggregate_type='task' AND aggregate_id IN (` + marks + `)
ORDER BY sequence DESC LIMIT 400`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, payload string
		if rows.Scan(&id, &payload) != nil {
			continue
		}
		if _, seen := out[id]; seen {
			continue
		}
		var doc map[string]any
		if json.Unmarshal([]byte(payload), &doc) != nil {
			continue
		}
		reason, _ := doc["reason"].(string)
		if clean := presentReason(reason); clean != "" {
			out[id] = clean
		}
	}
	return out
}

func (s *Server) taskStatusSummary(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "task service unavailable")
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, identity, workspaceID, "task.read") {
		return
	}
	rows, err := s.tasks.List(r.Context(), workspaceID, 250)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "task status unavailable")
		return
	}
	waits, err := loadTaskModelWaits(r.Context(), s.attentionDB, workspaceID, rows)
	if err != nil {
		waits = map[string]taskModelWait{}
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	reasons := loadLatestTaskReasons(r.Context(), s.attentionDB, workspaceID, ids)
	now := time.Now().UnixMilli()
	inputs := make([]taskStatusInput, len(rows))
	views := make([]taskStatusView, len(rows))
	var ramFree, vramFree *int64
	if snap := s.readMemorySnapshot(r.Context(), workspaceID); snap.available {
		ram := snap.freeForModels
		ramFree = &ram
		if snap.vramKnown {
			v := snap.vramFree
			vramFree = &v
		}
	}
	for i, row := range rows {
		wait := waits[row.ID]
		ram, vram := taskResourceNeeds(row.Completion)
		inputs[i] = taskStatusInput{
			ID: row.ID, Objective: row.Objective, State: string(row.State), Priority: row.Priority,
			UpdatedAt: row.UpdatedAt, Now: now, WaitKind: wait.Kind, WaitReason: wait.Reason,
			EventReason: reasons[row.ID], RAMNeeded: ram, VRAMNeeded: vram,
		}
		views[i] = classifyTask(inputs[i])
		views[i].RAMFreeBytes = ramFree
		views[i].VRAMFreeBytes = vramFree
	}
	orderTaskStatuses(inputs, views)
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks":        views,
		"generated_at": now,
		"source":       "task_state_worker_wait_and_latest_event_reason",
		"integration":  "Queue position is display order by priority. Wire it to the model-admission branch's live scheduler cursor when that field is published. RAM and VRAM free come from the memory-budget estimate, not from admission.",
	})
}
