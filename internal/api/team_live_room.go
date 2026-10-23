package api

import (
	"context"
	"net/http"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamLiveReader interface {
	LiveRoom(context.Context, string) (team.LiveRoom, error)
}

func (s *Server) getTeamLiveRoom(w http.ResponseWriter, r *http.Request) {
	if s.team == nil {
		writeError(w, http.StatusServiceUnavailable, "Team unavailable")
		return
	}
	identity, ss, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	if !s.authorize(w, r, identity, ss.WorkspaceID, "task.read") {
		return
	}
	reader, ok := s.team.(teamLiveReader)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Team live room unavailable")
		return
	}
	out, err := reader.LiveRoom(r.Context(), ss.ID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if out.ID != ss.ID || out.WorkspaceID != ss.WorkspaceID || out.TaskID != ss.TaskID {
		writeError(w, http.StatusForbidden, "Team scope mismatch")
		return
	}
	if !s.authorize(w, r, identity, ss.WorkspaceID, "team.read") || !s.authorize(w, r, identity, ss.WorkspaceID, "task.read") {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}
