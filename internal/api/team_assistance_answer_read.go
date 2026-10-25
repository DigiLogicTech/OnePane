package api

import (
	"context"
	"encoding/json"
	"net/http"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamAssistanceAnswerReader interface {
	AssistanceAnswer(context.Context, string, string, string, string) (team.AssistanceAnswerObservation, error)
}

func validTeamAssistanceAnswerObservation(observation team.AssistanceAnswerObservation) bool {
	answer := observation.Answer
	return validTeamAssistanceMetadata(observation.Request) &&
		answer.RequestID == observation.Request.RequestID &&
		validAssignmentMetadataString(answer.AnswerID, 256) &&
		validAssignmentMetadataString(answer.AuthorPrincipalID, 256) &&
		validAssignmentMetadataString(answer.IdempotencyKey, 128) &&
		len(answer.Text) > 0 && len(answer.Text) <= 4096 && utf8.ValidString(answer.Text) &&
		answer.TaskRevision > 0 && answer.TaskRevision <= 1<<53-1 &&
		answer.WorkerRevision > 0 && answer.WorkerRevision <= 1<<53-1 &&
		answer.CreatedAt > 0 && answer.CreatedAt <= 1<<53-1
}

// Observe the retained authored reply without treating it as permission to
// continue. This route remains bound to the exact original waiting request.
func (s *Server) getTeamAssistanceAnswer(w http.ResponseWriter, r *http.Request) {
	sessionID, requestID := r.PathValue("sessionID"), r.PathValue("requestID")
	if !validAssignmentMetadataString(sessionID, 256) || !validAssignmentMetadataString(requestID, 256) {
		writeError(w, 400, "Invalid Team assistance identity")
		return
	}
	if s.team == nil {
		writeError(w, 503, "Team assistance unavailable")
		return
	}
	reader, ready := s.team.(teamAssistanceAnswerReader)
	if !ready {
		writeError(w, 503, "Team answer observation unavailable")
		return
	}
	identity, session, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	capabilities := []string{"team.read", "task.read"}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, capabilities...) {
		return
	}
	observation, err := reader.AssistanceAnswer(r.Context(), session.WorkspaceID, session.ID, requestID, identity.PrincipalID)
	if err != nil {
		writeTeamAssistanceReadError(w, err)
		return
	}
	request := observation.Request
	if !validTeamAssistanceAnswerObservation(observation) || request.RequestID != requestID || request.SessionID != session.ID || request.WorkspaceID != session.WorkspaceID || request.ParentTaskID != session.TaskID || session.AcceptedPlanID == nil || request.PlanID != *session.AcceptedPlanID {
		writeError(w, 403, "Team answer scope changed")
		return
	}
	if request.ProjectID != "" {
		capabilities = append(capabilities, "project.read")
		if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, capabilities...) {
			return
		}
	}
	current, err := reader.AssistanceAnswer(r.Context(), session.WorkspaceID, session.ID, requestID, identity.PrincipalID)
	if err != nil {
		writeTeamAssistanceReadError(w, err)
		return
	}
	if current != observation {
		writeError(w, 409, "Team answer changed during read")
		return
	}
	if request.ProjectID != "" {
		scope, ready := s.projects.(activeProjectWorkspaceScopeReader)
		if !ready {
			writeError(w, 503, "Project scope unavailable")
			return
		}
		active, err := scope.ActiveProjectWorkspaceScope(r.Context(), session.WorkspaceID, request.ProjectID, request.ProjectWorkspaceID)
		if err != nil {
			respondDomain(w, nil, err, 0)
			return
		}
		if !active {
			writeError(w, 403, "Project changed during read")
			return
		}
	}
	body, err := json.Marshal(observation)
	if err != nil || len(body) > 48<<10 {
		writeError(w, 409, "Team answer exceeds the response limit")
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, capabilities...) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
