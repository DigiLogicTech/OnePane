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
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamAssistanceAnswerWriter interface {
	AnswerAssistance(context.Context, team.AnswerAssistanceCommand) (team.AssistanceAnswer, error)
}

type teamAssistanceAnswerInput struct {
	TaskID                 string `json:"task_id"`
	AttemptID              string `json:"attempt_id"`
	RunID                  string `json:"run_id"`
	WaitStepID             string `json:"wait_step_id"`
	ExpectedTaskRevision   int64  `json:"expected_task_revision"`
	ExpectedWorkerRevision int64  `json:"expected_worker_revision"`
	IdempotencyKey         string `json:"idempotency_key"`
	Text                   string `json:"text"`
}

// A reply retains human-authored input. Continuing the original owner remains
// a separate explicit action with Task write authority and ordinary admission.
func (s *Server) answerTeamAssistanceRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	sessionID, requestID := r.PathValue("sessionID"), r.PathValue("requestID")
	if !validAssignmentMetadataString(sessionID, 256) || !validAssignmentMetadataString(requestID, 256) {
		writeError(w, 400, "Invalid Team assistance identity")
		return
	}
	if s.team == nil {
		writeError(w, 503, "Team assistance unavailable")
		return
	}
	writer, ready := s.team.(teamAssistanceAnswerWriter)
	verifier, authenticated := s.auth.(WorkspaceTransactionAuthorizer)
	if !ready || !authenticated {
		writeError(w, 503, "Transactional Team assistance unavailable")
		return
	}
	identity, session, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	if identity.PrincipalType != "human" {
		writeError(w, 403, "A human answer is required")
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, "team.read", "task.read", "team.write") {
		return
	}
	// Accept enough escaped JSON for 4KiB of authored text, then validate its
	// UTF-8 before encoding/json can replace invalid input bytes.
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil || !utf8.Valid(raw) {
		writeError(w, 400, "Invalid bounded answer request")
		return
	}
	var input teamAssistanceAnswerInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		writeError(w, 400, "Invalid answer request")
		return
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, 400, "Request must contain one answer")
		return
	}
	capabilities := []string{"team.read", "task.read", "team.write"}
	out, err := writer.AnswerAssistance(r.Context(), team.AnswerAssistanceCommand{
		WorkspaceID: session.WorkspaceID, SessionID: session.ID, RequestID: requestID,
		TaskID: input.TaskID, AttemptID: input.AttemptID, RunID: input.RunID, WaitStepID: input.WaitStepID,
		ActorPrincipalID: identity.PrincipalID, IdempotencyKey: input.IdempotencyKey, Text: input.Text,
		ExpectedTaskRevision: input.ExpectedTaskRevision, ExpectedWorkerRevision: input.ExpectedWorkerRevision,
		AuthorizeInTransaction: func(ctx context.Context, tx storage.Tx, request team.AssistanceRequest) error {
			if !validTeamAssistanceMetadata(request) || request.RequestID != requestID || request.SessionID != session.ID || request.WorkspaceID != session.WorkspaceID || request.ParentTaskID != session.TaskID || session.AcceptedPlanID == nil || request.PlanID != *session.AcceptedPlanID {
				return team.ErrInvalid
			}
			if request.ProjectID != "" {
				capabilities = append(capabilities, "project.read")
			}
			return verifier.AuthorizeWorkspaceInTransaction(ctx, tx, r, identity, session.WorkspaceID, capabilities...)
		},
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrUnauthenticated):
			writeError(w, 401, "Authentication expired before answer")
		case errors.Is(err, ErrForbidden), errors.Is(err, team.ErrHumanRequired):
			writeError(w, 403, "Answer authority changed")
		case errors.Is(err, team.ErrInvalid):
			writeError(w, 409, "Answer identity or waiting contribution changed")
		default:
			respondDomain(w, nil, err, 0)
		}
		return
	}
	// A credential may expire after a committed answer. Withhold the private
	// response then; the stable submission key allows a later authorized retry.
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, capabilities...) {
		return
	}
	body, err := json.Marshal(out)
	if err != nil || len(body) > 32<<10 {
		writeError(w, 503, "Answer response unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
