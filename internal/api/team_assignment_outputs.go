package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamAssignmentCompletionReader interface {
	AssignmentCompletion(context.Context, string, string) (team.AssignmentCompletion, error)
}
type taskLibraryPublicationReader interface {
	TaskLibraryPublications(context.Context, string, string, string) ([]projectworkspace.WorkspacePublishedOutput, error)
}
type assignmentPublicationsResponse struct {
	Completion team.AssignmentCompletion                   `json:"completion"`
	Outputs    []projectworkspace.WorkspacePublishedOutput `json:"outputs"`
}

// Viewing a recorded contribution never executes work. Each content link still
// passes the independent Library read checks when it is subsequently opened.
func (s *Server) getTeamAssignmentOutputs(w http.ResponseWriter, r *http.Request) {
	if !validAssignmentMetadataString(r.PathValue("sessionID"), 256) || !validAssignmentMetadataString(r.PathValue("taskID"), 256) {
		writeError(w, 400, "Invalid Team assignment identity")
		return
	}
	if s.team == nil || s.projects == nil {
		writeError(w, 503, "Team publications unavailable")
		return
	}
	identity, ss, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, ss.WorkspaceID, "team.read", "task.read", "project.read") {
		return
	}
	reader, ok := s.team.(teamAssignmentCompletionReader)
	if !ok {
		writeError(w, 503, "Team completion reader unavailable")
		return
	}
	library, ok := s.projects.(taskLibraryPublicationReader)
	if !ok {
		writeError(w, 503, "Task publication reader unavailable")
		return
	}
	completion, err := reader.AssignmentCompletion(r.Context(), ss.ID, r.PathValue("taskID"))
	if err != nil {
		writeAssignmentOutputsError(w, err)
		return
	}
	if !validAssignmentCompletionMetadata(completion) || completion.SessionID != ss.ID || completion.WorkspaceID != ss.WorkspaceID || completion.ParentTaskID != ss.TaskID || completion.TaskID != r.PathValue("taskID") || ss.AcceptedPlanID == nil || completion.PlanID != *ss.AcceptedPlanID {
		writeError(w, 403, "Team assignment scope changed")
		return
	}
	p, err := s.projects.Project(r.Context(), completion.ProjectID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if p.ID != completion.ProjectID || p.WorkspaceID != ss.WorkspaceID || p.Status != "active" {
		writeError(w, 403, "Project changed during read")
		return
	}
	outputs, err := library.TaskLibraryPublications(r.Context(), completion.ProjectID, completion.ProjectWorkspaceID, completion.TaskID)
	if err != nil {
		writeAssignmentOutputsError(w, err)
		return
	}
	if len(outputs) > 32 {
		writeError(w, 409, "Task publications exceed the bounded metadata limit")
		return
	}
	for _, item := range outputs {
		if !validAssignmentPublicationMetadata(item) || item.TaskID != completion.TaskID {
			writeError(w, 403, "Task publication scope mismatch")
			return
		}
	}
	// Recollect current grants/receipts after storage work. No partial inventory
	// is returned if records or permissions changed during the read.
	currentOutputs, err := library.TaskLibraryPublications(r.Context(), completion.ProjectID, completion.ProjectWorkspaceID, completion.TaskID)
	if err != nil {
		writeAssignmentOutputsError(w, err)
		return
	}
	if !reflect.DeepEqual(outputs, currentOutputs) {
		writeError(w, 409, "Task publications changed during read")
		return
	}
	current, err := reader.AssignmentCompletion(r.Context(), ss.ID, completion.TaskID)
	if err != nil {
		writeAssignmentOutputsError(w, err)
		return
	}
	if current != completion {
		writeError(w, 409, "Team completion changed during read")
		return
	}
	p, err = s.projects.Project(r.Context(), completion.ProjectID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if p.ID != completion.ProjectID || p.WorkspaceID != ss.WorkspaceID || p.Status != "active" {
		writeError(w, 403, "Project changed during read")
		return
	}
	body, err := json.Marshal(assignmentPublicationsResponse{Completion: completion, Outputs: outputs})
	if err != nil {
		writeError(w, 503, "Task publication metadata unavailable")
		return
	}
	if len(body) > 256<<10 {
		writeError(w, 409, "Task publications exceed the bounded response limit")
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, ss.WorkspaceID, "team.read", "task.read", "project.read") {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func writeAssignmentOutputsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projectworkspace.ErrLibraryPublicationLimit), errors.Is(err, projectworkspace.ErrLibraryMetadataLimit), errors.Is(err, projectworkspace.ErrTaskPublicationLimit), errors.Is(err, projectworkspace.ErrTaskPublicationUnavailable):
		writeError(w, 409, "Recorded publications require reconciliation or a smaller selection")
	case errors.Is(err, team.ErrInvalid):
		writeError(w, 409, "Current accepted completion evidence is unavailable")
	default:
		respondDomain(w, nil, err, 0)
	}
}

func validAssignmentMetadataString(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validAssignmentCompletionMetadata(c team.AssignmentCompletion) bool {
	for _, value := range []string{c.SessionID, c.WorkspaceID, c.ParentTaskID, c.TaskID, c.PlanID, c.AssignmentKey, c.MemberID, c.ProjectID, c.ProjectWorkspaceID, c.CheckpointID, c.VerificationID} {
		if !validAssignmentMetadataString(value, 256) {
			return false
		}
	}
	return (c.ArtifactSessionID == "" || validAssignmentMetadataString(c.ArtifactSessionID, 256)) && c.TaskRevision > 0 && c.TaskRevision <= 1<<53-1 && len(c.VerificationLevel) == 2 && c.VerificationLevel[0] == 'V' && c.VerificationLevel[1] >= '0' && c.VerificationLevel[1] <= '5'
}

func validAssignmentPublicationMetadata(p projectworkspace.WorkspacePublishedOutput) bool {
	digest, ok := librarySHA256Digest(p.ContentHash)
	return validAssignmentMetadataString(p.TaskID, 256) && validAssignmentMetadataString(p.AssetID, 256) && validAssignmentMetadataString(p.RelativePath, 4096) && (p.MIMEType == "" || validAssignmentMetadataString(p.MIMEType, 256)) && ok && digest == p.ContentHash && p.Version > 0 && p.Version <= 1<<53-1 && p.SizeBytes >= 0 && p.SizeBytes <= 1<<53-1 && p.PublishedAt >= 0 && p.PublishedAt <= 1<<53-1
}
