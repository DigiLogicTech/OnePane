package api

import (
	"net/http"
	"strings"
)

func (s *Server) listManagedComponents(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok { return }
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI service unavailable")
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "model.read") { return }
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
	var in struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if !decodeJSON(w, r, &in) { return }
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	if in.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "model.write") { return }
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
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "model.read") { return }
	out, err := s.localAI.ComponentJob(r.Context(), strings.TrimSpace(r.PathValue("jobID")))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
