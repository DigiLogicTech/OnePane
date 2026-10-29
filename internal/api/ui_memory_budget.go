package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/hostmetrics"
	"github.com/DigiLogicTech/OnePane/internal/localai"
)

// memorySnapshot is a presentation estimate. It does not admit or evict models.
// The model-admission branch should replace the segments with its live
// free-memory decision when that snapshot exists.
type memorySnapshot struct {
	available      bool
	total          int64
	hostFree       int64
	reserve        int64
	freeForModels  int64
	otherApps      int64
	pinned         int64
	idle           int64
	inUse          int64
	process        int64
	vramTotal      int64
	vramFree       int64
	vramKnown      bool
	modelsMeasured bool
	note           string
}

type memorySegment struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Bytes int64  `json:"bytes"`
}

func admissionReserve(total int64) int64 {
	if total <= 0 {
		return 0
	}
	reserve := total / 10
	const cap int64 = 1 << 30
	if reserve > cap {
		reserve = cap
	}
	return reserve
}

func splitMemoryBudget(total, hostFree, process, pinned, idle, inUse int64, modelsMeasured bool) memorySnapshot {
	out := memorySnapshot{total: total, hostFree: hostFree, process: process, pinned: pinned, idle: idle, inUse: inUse, modelsMeasured: modelsMeasured}
	if total <= 0 {
		out.note = "Host memory was not reported."
		return out
	}
	out.available = true
	if hostFree < 0 {
		hostFree = 0
	}
	if hostFree > total {
		hostFree = total
	}
	out.hostFree = hostFree
	used := total - hostFree
	models := pinned + idle + inUse
	if models < 0 {
		models = 0
	}
	if !modelsMeasured {
		models = 0
		out.pinned, out.idle, out.inUse = 0, 0, 0
		out.note = "Model memory is not measured yet. The bar shows host free memory only."
	} else if models > used {
		// Install-plan estimates can exceed the host's used figure. Clamp
		// rather than invent negative "other apps".
		scale := float64(used) / float64(models)
		out.pinned = int64(float64(pinned) * scale)
		out.idle = int64(float64(idle) * scale)
		out.inUse = used - out.pinned - out.idle
		if out.inUse < 0 {
			out.inUse = 0
		}
		models = out.pinned + out.idle + out.inUse
		out.note = "Model sizes are install-plan estimates and were clamped to memory currently in use."
	}
	other := used - models
	if other < 0 {
		other = 0
	}
	out.otherApps = other
	out.reserve = admissionReserve(total)
	if out.reserve > hostFree {
		out.reserve = hostFree
	}
	out.freeForModels = hostFree - out.reserve
	if out.freeForModels < 0 {
		out.freeForModels = 0
	}
	return out
}

func classifyResidentModel(row localai.ManagedDeploymentSummary) (pinned, idle, inUse int64, measured bool) {
	if row.RAMBytes <= 0 {
		return 0, 0, 0, false
	}
	state := strings.ToLower(strings.TrimSpace(row.ResidencyState))
	switch state {
	case "", "stopped", "absent", "removed":
		return 0, 0, 0, false
	}
	if row.ColibriPinned || state == "busy" || state == "starting" || state == "draining" {
		if row.ColibriPinned && state != "busy" && state != "starting" && state != "draining" {
			return row.RAMBytes, 0, 0, true
		}
		return 0, 0, row.RAMBytes, true
	}
	return 0, row.RAMBytes, 0, true
}

func (s *Server) readMemorySnapshot(ctx context.Context, workspaceID string) memorySnapshot {
	metrics := hostmetrics.CollectWithPaths(nil)
	var pinned, idle, inUse int64
	measured := false
	if s != nil && s.localAI != nil {
		rows, err := s.localAI.ManagedDeployments(ctx, workspaceID)
		if err == nil {
			for _, row := range rows {
				p, i, u, ok := classifyResidentModel(row)
				if !ok {
					continue
				}
				measured = true
				pinned += p
				idle += i
				inUse += u
			}
		}
	}
	out := splitMemoryBudget(int64(metrics.MemoryTotalBytes), int64(metrics.MemoryAvailableBytes), int64(metrics.ProcessRSSBytes), pinned, idle, inUse, measured)
	var vramTotal, vramUsed uint64
	for _, gpu := range metrics.GPUs {
		if gpu.VRAMTotalBytes == 0 {
			continue
		}
		out.vramKnown = true
		vramTotal += gpu.VRAMTotalBytes
		vramUsed += gpu.VRAMUsedBytes
	}
	if out.vramKnown {
		out.vramTotal = int64(vramTotal)
		free := int64(vramTotal - vramUsed)
		if free < 0 {
			free = 0
		}
		out.vramFree = free
	}
	return out
}

func (s *Server) memoryBudget(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, identity, workspaceID, "model.read") {
		return
	}
	snap := s.readMemorySnapshot(r.Context(), workspaceID)
	segments := []memorySegment{}
	if snap.available {
		segments = []memorySegment{
			{ID: "other_apps", Label: "Other apps", Bytes: snap.otherApps},
			{ID: "models_in_use", Label: "Models in use", Bytes: snap.inUse},
			{ID: "models_pinned", Label: "Pinned models", Bytes: snap.pinned},
			{ID: "models_idle", Label: "Idle models", Bytes: snap.idle},
			{ID: "reserve", Label: "Reserve", Bytes: snap.reserve},
			{ID: "free", Label: "Free", Bytes: snap.freeForModels},
		}
	}
	var nextFits any
	var nextReason string
	if s.tasks != nil {
		if rows, err := s.tasks.List(r.Context(), workspaceID, 100); err == nil {
			var need int64
			for _, row := range rows {
				if !queuedState(string(row.State)) {
					continue
				}
				ram, _ := taskResourceNeeds(row.Completion)
				if ram > need {
					need = ram
				}
			}
			if need > 0 && snap.available {
				fits := need <= snap.freeForModels || (snap.idle > 0 && need <= snap.freeForModels+snap.idle)
				nextFits = fits
				if !fits {
					nextReason = "The next queued task that reported a memory need does not fit in free memory."
				} else if need > snap.freeForModels {
					nextReason = "The next queued task fits only after an idle model is unloaded."
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":            snap.available,
		"total_bytes":          snap.total,
		"host_available_bytes": snap.hostFree,
		"reserve_bytes":        snap.reserve,
		"free_bytes":           snap.freeForModels,
		"other_apps_bytes":     snap.otherApps,
		"pinned_model_bytes":   snap.pinned,
		"idle_model_bytes":     snap.idle,
		"in_use_model_bytes":   snap.inUse,
		"process_bytes":        snap.process,
		"vram_total_bytes":     snap.vramTotal,
		"vram_free_bytes":      snap.vramFree,
		"vram_known":           snap.vramKnown,
		"models_measured":      snap.modelsMeasured,
		"segments":             segments,
		"next_task_fits":       nextFits,
		"next_task_reason":     nextReason,
		"note":                 snap.note,
		"generated_at":         time.Now().UnixMilli(),
		"source":               "hostmetrics_and_managed_deployment_estimates",
		"admission_wired":      false,
		"integration":          "Replace these segments with the model-admission branch's live free-memory decision when that snapshot is published. Reserve matches the existing one-tenth host reserve, capped at 1 GiB, and is not a new admission rule.",
	})
}

type idleModelReaper interface {
	ReapIdleManagedRuntimes(context.Context, time.Duration, int) (int, error)
}

func (s *Server) unloadIdleModels(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" && r.Body != nil {
		var body struct {
			WorkspaceID string `json:"workspace_id"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
		workspaceID = strings.TrimSpace(body.WorkspaceID)
	}
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, identity, workspaceID, "model.write") {
		return
	}
	reaper, ok := s.localAI.(idleModelReaper)
	if !ok || s.localAI == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"unloaded":  0,
			"reason":    "Idle unload is not available on this build.",
		})
		return
	}
	n, err := reaper.ReapIdleManagedRuntimes(r.Context(), time.Second, 4)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": true,
			"unloaded":  0,
			"reason":    "Idle models could not be unloaded.",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"unloaded":  n,
		"reason":    "Idle models that were unused for at least a second were asked to stop. Pinned and busy models stay loaded.",
	})
}
