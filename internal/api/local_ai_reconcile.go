package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/localai"
)

func (s *Server) reconcileManagedLocalDeployments(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok { return }
	var in struct { WorkspaceID string `json:"workspace_id"` }
	if !decodeJSON(w, r, &in) { return }
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	if !s.authorize(w, r, i, in.WorkspaceID, "model.write") { return }
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return
	}
	provider, ok := s.localAI.(interface{ ReconcileManagedDeployments(context.Context,string,string)(localai.ManagedDeploymentReconcileReport,error) })
	if !ok {
		writeError(w, http.StatusNotImplemented, "managed deployment reconciliation unavailable")
		return
	}
	out, err := provider.ReconcileManagedDeployments(r.Context(), in.WorkspaceID, i.PrincipalID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
