package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

type teamAssistanceContinuer interface {
	ContinueAssistance(context.Context, team.ContinueAssistanceCommand) (team.AssistanceContinuation, error)
}

type teamAssistanceContinueInput struct {
	AnswerID               string `json:"answer_id"`
	TaskID                 string `json:"task_id"`
	AttemptID              string `json:"attempt_id"`
	RunID                  string `json:"run_id"`
	WaitStepID             string `json:"wait_step_id"`
	ExpectedTaskRevision   int64  `json:"expected_task_revision"`
	ExpectedWorkerRevision int64  `json:"expected_worker_revision"`
}

func (s *Server) continueTeamAssistance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	sessionID, requestID := r.PathValue("sessionID"), r.PathValue("requestID")
	if !validAssignmentMetadataString(sessionID, 256) || !validAssignmentMetadataString(requestID, 256) {
		writeError(w, 400, "Invalid Team assistance identity")
		return
	}
	if s.team == nil {
		writeError(w, 503, "Team assistance unavailable")
		return
	}
	writer, ready := s.team.(teamAssistanceContinuer)
	verifier, authenticated := s.auth.(WorkspaceTransactionAuthorizer)
	if !ready || !authenticated {
		writeError(w, 503, "Transactional Team continuation unavailable")
		return
	}
	identity, session, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	if identity.PrincipalType != "human" {
		writeError(w, 403, "A human continuation approval is required")
		return
	}
	capabilities := []string{"team.read", "task.read", "team.write", "task.write"}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, capabilities...) {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<10))
	if err != nil || !utf8.Valid(raw) {
		writeError(w, 400, "Invalid bounded continuation request")
		return
	}
	var input teamAssistanceContinueInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		writeError(w, 400, "Invalid continuation request")
		return
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, 400, "Request must contain one continuation")
		return
	}
	var bound team.AssistanceRequest
	out, err := writer.ContinueAssistance(r.Context(), team.ContinueAssistanceCommand{
		WorkspaceID: session.WorkspaceID, SessionID: session.ID, RequestID: requestID, AnswerID: input.AnswerID,
		TaskID: input.TaskID, AttemptID: input.AttemptID, RunID: input.RunID, WaitStepID: input.WaitStepID, ActorPrincipalID: identity.PrincipalID,
		ExpectedTaskRevision: input.ExpectedTaskRevision, ExpectedWorkerRevision: input.ExpectedWorkerRevision,
		AuthorizeInTransaction: func(ctx context.Context, tx storage.Tx, request team.AssistanceRequest) error {
			if !validTeamAssistanceMetadata(request) || request.RequestID != requestID || request.SessionID != session.ID || request.WorkspaceID != session.WorkspaceID || request.ParentTaskID != session.TaskID || session.AcceptedPlanID == nil || request.PlanID != *session.AcceptedPlanID {
				return team.ErrInvalid
			}
			bound = request
			if request.ProjectID != "" {
				capabilities = append(capabilities, "project.read", "project.write")
			}
			return verifier.AuthorizeWorkspaceInTransaction(ctx, tx, r, identity, session.WorkspaceID, capabilities...)
		},
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrUnauthenticated):
			writeError(w, 401, "Authentication expired before continuation")
		case errors.Is(err, ErrForbidden), errors.Is(err, team.ErrHumanRequired):
			writeError(w, 403, "Continuation authority changed")
		case errors.Is(err, team.ErrInvalid), errors.Is(err, teamgate.ErrPaused), errors.Is(err, teamgate.ErrExecution), errors.Is(err, task.ErrHardDependencyUnsatisfied), errors.Is(err, task.ErrRecoveryRequired), errors.Is(err, task.ErrInvalidTransition), errors.Is(err, task.ErrRevisionConflict), errors.Is(err, task.ErrInvalidAttempt), errors.Is(err, task.ErrNoActiveAttempt):
			writeError(w, 409, "Continuation identity, admission or waiting owner changed")
		default:
			respondDomain(w, nil, err, 0)
		}
		return
	}
	// The approval is already durable. If the original credential or scope was
	// revoked after commit, withhold private output; repeating cannot reexecute.
	if bound.ProjectID != "" {
		scope, ok := s.projects.(activeProjectWorkspaceScopeReader)
		if !ok {
			writeError(w, 503, "Project scope unavailable")
			return
		}
		active, err := scope.ActiveProjectWorkspaceScope(r.Context(), session.WorkspaceID, bound.ProjectID, bound.ProjectWorkspaceID)
		if err != nil {
			respondDomain(w, nil, err, 0)
			return
		}
		if !active {
			writeError(w, 403, "Project changed during continuation")
			return
		}
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, capabilities...) {
		return
	}
	body, err := json.Marshal(out)
	if err != nil || len(body) > 8<<10 {
		writeError(w, 503, "Continuation receipt unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
