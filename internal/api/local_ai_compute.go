package api

import (
	"net/http"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/localai"
)

func (s *Server) getDeploymentComputePolicy(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok { return }
	ws := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if ws == "" { writeError(w, http.StatusBadRequest, "workspace_id is required"); return }
	if !s.authorize(w, r, i, ws, "model.read") { return }
	if s.localAI == nil { writeError(w, http.StatusServiceUnavailable, "local AI service unavailable"); return }
	out, err := s.localAI.ComputePolicy(r.Context(), r.PathValue("deploymentID"))
	respondDomain(w, out, err, http.StatusOK)
}

func (s *Server) setDeploymentComputePolicy(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok { return }
	var in struct {
		WorkspaceID        string   `json:"workspace_id"`
		Preference         string   `json:"preference"`
		PlacementMode      string   `json:"placement_mode"`
		PreferredDeviceIDs []string `json:"preferred_device_ids"`
		RequiredDeviceIDs  []string `json:"required_device_ids"`
	}
	if !decodeJSON(w, r, &in) { return }
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	if in.WorkspaceID == "" { writeError(w, http.StatusBadRequest, "workspace_id is required"); return }
	if !s.authorize(w, r, i, in.WorkspaceID, "model.write") { return }
	out, err := s.localAI.SetComputePolicy(r.Context(), localai.ComputePolicyCommand{
		DeploymentID: r.PathValue("deploymentID"), Preference: in.Preference, PlacementMode: in.PlacementMode,
		PreferredDeviceIDs: in.PreferredDeviceIDs, RequiredDeviceIDs: in.RequiredDeviceIDs, ActorPrincipalID: i.PrincipalID,
	})
	respondDomain(w, out, err, http.StatusOK)
}
