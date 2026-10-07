package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/localai"
)

func (s *Server) listLocalAIInstallJobs(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok { return }
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "model.read") { return }
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return
	}
	provider, ok := s.localAI.(interface{ ActiveInstallJobs(context.Context,string)([]localai.InstallJob,error) })
	if !ok {
		writeError(w, http.StatusNotImplemented, "active install job listing unavailable")
		return
	}
	rows, err := provider.ActiveInstallJobs(r.Context(), workspaceID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}
