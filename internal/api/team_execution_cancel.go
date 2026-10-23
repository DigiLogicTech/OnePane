package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamExecutionCanceller interface {
	RequestExecutionCancellation(context.Context, team.CancelExecutionCommand) (team.Session, error)
}

func (s *Server) cancelTeamExecution(w http.ResponseWriter, r *http.Request) {
	if s.team == nil {
		writeError(w, http.StatusServiceUnavailable, "Team unavailable")
		return
	}
	i, ss, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	if !s.authorize(w, r, i, ss.WorkspaceID, "task.write") {
		return
	}
	svc, ok := s.team.(teamExecutionCanceller)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Team cancellation unavailable")
		return
	}
	var in struct {
		PlanID               string `json:"plan_id"`
		ExpectedTaskRevision int64  `json:"expected_task_revision"`
		Reason               string `json:"reason"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := svc.RequestExecutionCancellation(r.Context(), team.CancelExecutionCommand{SessionID: ss.ID, PlanID: in.PlanID, ExpectedTaskRevision: in.ExpectedTaskRevision, ActorPrincipalID: i.PrincipalID, Reason: in.Reason})
	if errors.Is(err, task.ErrRevisionConflict) {
		writeError(w, http.StatusConflict, "Task changed; refresh before requesting cancellation")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondDomain(w, out, err, http.StatusAccepted)
}
