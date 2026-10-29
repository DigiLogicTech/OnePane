package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamAssistanceRequestReader interface {
	AssistanceRequest(context.Context, string, string, string, string) (team.AssistanceRequest, error)
}

type activeProjectWorkspaceScopeReader interface {
	ActiveProjectWorkspaceScope(context.Context, string, string, string) (bool, error)
}

// Reading a waiting contribution's question does not answer or resume it.
func (s *Server) getTeamAssistanceRequest(w http.ResponseWriter, r *http.Request) {
	sessionID, requestID := r.PathValue("sessionID"), r.PathValue("requestID")
	if !validAssignmentMetadataString(sessionID, 256) || !validAssignmentMetadataString(requestID, 256) {
		writeError(w, 400, "Invalid Team assistance identity")
		return
	}
	if s.team == nil {
		writeError(w, 503, "Team assistance unavailable")
		return
	}
	identity, session, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, "team.read", "task.read") {
		return
	}
	reader, ok := s.team.(teamAssistanceRequestReader)
	if !ok {
		writeError(w, 503, "Team assistance reader unavailable")
		return
	}
	request, err := reader.AssistanceRequest(r.Context(), session.WorkspaceID, session.ID, requestID, identity.PrincipalID)
	if err != nil {
		writeTeamAssistanceReadError(w, err)
		return
	}
	if !validTeamAssistanceMetadata(request) || request.RequestID != requestID || request.SessionID != session.ID || request.WorkspaceID != session.WorkspaceID || request.ParentTaskID != session.TaskID || session.AcceptedPlanID == nil || request.PlanID != *session.AcceptedPlanID {
		writeError(w, 403, "Team assistance scope changed")
		return
	}
	capabilities := []string{"team.read", "task.read"}
	if request.ProjectID != "" {
		capabilities = append(capabilities, "project.read")
		if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, capabilities...) {
			return
		}
	}
	// Recollect the exact original owner, accepted graph, sealed criteria and
	// current reader membership. Never publish the first private observation
	// after an intervening change, even when credentials themselves still work.
	current, err := reader.AssistanceRequest(r.Context(), session.WorkspaceID, session.ID, requestID, identity.PrincipalID)
	if err != nil {
		writeTeamAssistanceReadError(w, err)
		return
	}
	if current != request {
		writeError(w, 409, "Team assistance changed during read")
		return
	}
	if request.ProjectID != "" {
		scope, ok := s.projects.(activeProjectWorkspaceScopeReader)
		if !ok {
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
	body, err := json.Marshal(request)
	if err != nil {
		writeError(w, 503, "Team assistance unavailable")
		return
	}
	if len(body) > 16<<10 {
		writeError(w, 409, "Team assistance exceeds the response limit")
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

func writeTeamAssistanceReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, team.ErrInvalid) {
		writeError(w, 409, "Current Team assistance requires reconciliation")
		return
	}
	respondDomain(w, nil, err, 0)
}

func validTeamAssistanceMetadata(request team.AssistanceRequest) bool {
	for _, value := range []string{request.RequestID, request.WorkspaceID, request.SessionID, request.PlanID, request.AssignmentKey, request.MemberID, request.ParentTaskID, request.TaskID, request.AttemptID, request.RunID, request.WaitStepID, request.NoticeMessageID, request.AuthorPrincipalID} {
		if !validAssignmentMetadataString(value, 256) {
			return false
		}
	}
	for _, value := range []string{request.ProjectID, request.ProjectWorkspaceID, request.ArtifactSessionID} {
		if value != "" && !validAssignmentMetadataString(value, 256) {
			return false
		}
	}
	return request.TaskRevision > 0 && request.TaskRevision <= 1<<53-1 &&
		request.WorkerRevision > 0 && request.WorkerRevision <= 1<<53-1 &&
		request.CreatedAt > 0 && request.CreatedAt <= 1<<53-1 &&
		request.AuthorPrincipalID == "system:agent-worker" &&
		(request.ProjectWorkspaceID == "" || request.ProjectID != "") &&
		(request.TaskState == "waiting_approval" || request.TaskState == "paused") &&
		len(request.MemberName) > 0 && len(request.MemberName) <= 256 && utf8.ValidString(request.MemberName) &&
		len(request.Question) <= 512 && utf8.ValidString(request.Question) && strings.TrimSpace(request.Question) != ""
}
