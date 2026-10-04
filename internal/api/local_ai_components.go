package api

import (
	"net/http"
	"strings"
)

func (s *Server) listManagedComponents(w http.ResponseWriter, r *http.Request) {
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI service unavailable")
		return
	}
	out, err := s.localAI.ManagedComponents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) manageComponent(w http.ResponseWriter, r *http.Request) {
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI service unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("componentID"))
	action := strings.TrimSpace(r.PathValue("action"))
	out, err := s.localAI.ManageComponent(r.Context(), id, action)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
