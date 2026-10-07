package api

import (
	"net/http"
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
