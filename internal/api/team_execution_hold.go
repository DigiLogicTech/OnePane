package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamExecutionHolder interface {
	PauseExecution(context.Context, team.ExecutionControlCommand) (team.Session, error)
	ResumeExecution(context.Context, team.ExecutionControlCommand) (team.Session, error)
}

func (s *Server) holdTeamExecution(w http.ResponseWriter, r *http.Request) {
	if s.team == nil {
		writeError(w, http.StatusServiceUnavailable, "Team unavailable")
		return
	}
	action := r.PathValue("action")
	if action != "pause" && action != "resume" {
		writeError(w, http.StatusNotFound, "Unknown Team control")
		return
	}
	i, ss, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	if !s.authorize(w, r, i, ss.WorkspaceID, "task.write") {
		return
	}
	svc, ok := s.team.(teamExecutionHolder)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Team pause control unavailable")
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
	c := team.ExecutionControlCommand{SessionID: ss.ID, PlanID: in.PlanID, ExpectedTaskRevision: in.ExpectedTaskRevision, ActorPrincipalID: i.PrincipalID, Reason: in.Reason}
	var out team.Session
	var err error
	if action == "pause" {
		out, err = svc.PauseExecution(r.Context(), c)
	} else {
		out, err = svc.ResumeExecution(r.Context(), c)
	}
	if errors.Is(err, task.ErrRevisionConflict) {
		writeError(w, http.StatusConflict, "Task changed; refresh before changing Team execution")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondDomain(w, out, err, http.StatusOK)
}
