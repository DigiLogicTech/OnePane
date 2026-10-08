package api

import (
	"context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/localai"
	"strings"
)

func authorizeHostModelCapability(w http.ResponseWriter, i Identity, capability string) bool {
	if !scopeAllows(i.CapabilityScope, "capabilities", capability) {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func (s *Server) listManagedComponents(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok { return }
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI service unavailable")
		return
	}
	if !authorizeHostModelCapability(w, i, "model.read") { return }
	out, err := s.localAI.ManagedComponents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) manageComponent(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok { return }
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI service unavailable")
		return
	}
	if !authorizeHostModelCapability(w, i, "model.write") { return }
	id := strings.TrimSpace(r.PathValue("componentID"))
	action := strings.TrimSpace(r.PathValue("action"))
	actor := i.PrincipalID
	out, err := s.localAI.RequestComponentAction(r.Context(), id, action, &actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (s *Server) getComponentJob(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok { return }
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI service unavailable")
		return
	}
	if !authorizeHostModelCapability(w, i, "model.read") { return }
	out, err := s.localAI.ComponentJob(r.Context(), strings.TrimSpace(r.PathValue("jobID")))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listRecentComponentJobs(w http.ResponseWriter, r *http.Request) {
 i, ok := s.authenticate(w, r)
 if !ok { return }
 if s.localAI == nil { writeError(w, http.StatusServiceUnavailable, "local AI service unavailable"); return }
 if !authorizeHostModelCapability(w, i, "model.read") { return }
 reader, ok := s.localAI.(interface { RecentComponentJobs(context.Context, int) ([]localai.ComponentJob, error) })
 if !ok { writeError(w, http.StatusServiceUnavailable, "component job history unavailable"); return }
 jobs, err := reader.RecentComponentJobs(r.Context(), 80)
 if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
 type logEntry struct { ID string `json:"id"`; ComponentID string `json:"component_id"`; Action string `json:"action"`; Status string `json:"status"`; Stage string `json:"stage"`; FailureReason string `json:"failure_reason,omitempty"`; UpdatedAt int64 `json:"updated_at"` }
 out := make([]logEntry, 0, len(jobs))
 for _, j := range jobs { item := logEntry{ID:j.ID,ComponentID:j.ComponentID,Action:j.Action,Status:j.Status,Stage:j.Stage,UpdatedAt:j.UpdatedAt};if j.FailureReason!=nil{item.FailureReason=*j.FailureReason};out=append(out,item) }
 writeJSON(w, http.StatusOK, out)
}
