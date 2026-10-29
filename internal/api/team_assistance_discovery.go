package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamAssistanceRequestsReader interface {
	AssistanceRequests(context.Context, string, string, string) ([]team.AssistanceRequest, error)
}

type assistanceDiscoveryTaskReader interface {
	Get(context.Context, string) (task.Task, error)
}

type assistanceDiscoveryScope struct {
	ProjectID, ProjectWorkspaceID, ArtifactSessionID string
	Revision                                         int64
	State                                            task.State
}

func discoveryTaskScope(root task.Task, workspace, taskID string) (assistanceDiscoveryScope, bool) {
	var scope assistanceDiscoveryScope
	if root.ID != taskID || root.WorkspaceID != workspace || root.ArchivedAt != nil || root.Revision < 1 || root.Revision > 1<<53-1 {
		return scope, false
	}
	values := []*string{root.ProjectID, root.ProjectWorkspaceID, root.ArtifactSessionID}
	for _, value := range values {
		if value != nil && !validAssignmentMetadataString(*value, 256) {
			return scope, false
		}
	}
	if root.ProjectID != nil {
		scope.ProjectID = *root.ProjectID
	}
	if root.ProjectWorkspaceID != nil {
		scope.ProjectWorkspaceID = *root.ProjectWorkspaceID
	}
	if root.ArtifactSessionID != nil {
		scope.ArtifactSessionID = *root.ArtifactSessionID
	}
	scope.Revision, scope.State = root.Revision, root.State
	return scope, scope.ProjectWorkspaceID == "" || scope.ProjectID != ""
}

// Discovery observes only the bounded current accepted contributions. It never
// treats an empty list as a reason to skip the root Task's Project permission.
func (s *Server) listTeamAssistanceRequests(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !validAssignmentMetadataString(r.PathValue("sessionID"), 256) {
		writeError(w, 400, "Invalid Team assistance session")
		return
	}
	if s.team == nil || s.tasks == nil {
		writeError(w, 503, "Team assistance discovery unavailable")
		return
	}
	reader, ready := s.team.(teamAssistanceRequestsReader)
	tasks, taskReady := s.tasks.(assistanceDiscoveryTaskReader)
	if !ready || !taskReady {
		writeError(w, 503, "Team assistance discovery unavailable")
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
	root, err := tasks.Get(r.Context(), session.TaskID)
	if err != nil {
		writeTeamAssistanceReadError(w, err)
		return
	}
	scope, ok := discoveryTaskScope(root, session.WorkspaceID, session.TaskID)
	if !ok {
		writeError(w, 403, "Team assistance root scope changed")
		return
	}
	if scope.ProjectID != "" {
		capabilities = append(capabilities, "project.read")
		if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, capabilities...) {
			return
		}
	}
	requests, err := reader.AssistanceRequests(r.Context(), session.WorkspaceID, session.ID, identity.PrincipalID)
	if err != nil {
		writeTeamAssistanceReadError(w, err)
		return
	}
	if len(requests) > 8 {
		writeError(w, 409, "Team assistance exceeds the request limit")
		return
	}
	seen := make(map[string]bool, len(requests))
	for _, request := range requests {
		if !validTeamAssistanceMetadata(request) || request.SessionID != session.ID || request.WorkspaceID != session.WorkspaceID || request.ParentTaskID != session.TaskID || session.AcceptedPlanID == nil || request.PlanID != *session.AcceptedPlanID || request.ProjectID != scope.ProjectID || request.ProjectWorkspaceID != scope.ProjectWorkspaceID || request.ArtifactSessionID != scope.ArtifactSessionID || seen[request.RequestID] {
			writeError(w, 403, "Team assistance request scope changed")
			return
		}
		seen[request.RequestID] = true
	}
	if requests == nil {
		requests = []team.AssistanceRequest{}
	}
	body, err := json.Marshal(struct {
		Requests []team.AssistanceRequest `json:"requests"`
	}{requests})
	if err != nil || len(body) > 128<<10 {
		writeError(w, 409, "Team assistance exceeds the response limit")
		return
	}
	current, err := reader.AssistanceRequests(r.Context(), session.WorkspaceID, session.ID, identity.PrincipalID)
	if err != nil {
		writeTeamAssistanceReadError(w, err)
		return
	}
	if !slices.Equal(current, requests) {
		writeError(w, 409, "Team assistance changed during discovery")
		return
	}
	root, err = tasks.Get(r.Context(), session.TaskID)
	if err != nil {
		writeTeamAssistanceReadError(w, err)
		return
	}
	currentScope, ok := discoveryTaskScope(root, session.WorkspaceID, session.TaskID)
	if !ok || currentScope != scope {
		writeError(w, 409, "Team assistance root changed during discovery")
		return
	}
	if scope.ProjectID != "" {
		activeScope, ok := s.projects.(activeProjectWorkspaceScopeReader)
		if !ok {
			writeError(w, 503, "Project scope unavailable")
			return
		}
		active, err := activeScope.ActiveProjectWorkspaceScope(r.Context(), session.WorkspaceID, scope.ProjectID, scope.ProjectWorkspaceID)
		if err != nil {
			respondDomain(w, nil, err, 0)
			return
		}
		if !active {
			writeError(w, 403, "Project changed during discovery")
			return
		}
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, session.WorkspaceID, capabilities...) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
