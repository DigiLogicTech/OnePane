package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

type taskRecoveryJournalReader interface {
	taskRecoveryReader
	ObserveRecoveryJournal(context.Context, string, string, string) (task.RecoveryJournalView, error)
}

func (s *Server) readTaskRecoveryJournal(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	reader, ok := s.tasks.(taskRecoveryJournalReader)
	if !ok {
		writeError(w, 503, "Stored recovery journal unavailable")
		return
	}
	initial, err := reader.Get(r.Context(), r.PathValue("taskID"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "Task not found")
		return
	}
	if err != nil {
		writeError(w, 500, "Task unavailable")
		return
	}
	if !s.authorizeCurrentTaskRead(w, r, identity, initial.WorkspaceID) {
		return
	}
	caseID := r.PathValue("caseID")
	if caseID == "" || len(caseID) > 160 {
		writeError(w, 400, "Invalid recovery record")
		return
	}
	before, err := reader.GetRecoveryCase(r.Context(), initial.WorkspaceID, initial.ID, caseID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "Recovery record not found")
		return
	}
	if err != nil {
		writeError(w, 503, "Recovery record unavailable")
		return
	}
	if before.ID != caseID || before.TaskID != initial.ID || before.Phase != "awaiting_inventory" {
		writeError(w, 403, "Recovery record changed")
		return
	}
	if !s.authorizeCurrentTaskRead(w, r, identity, initial.WorkspaceID) {
		return
	}
	view, err := reader.ObserveRecoveryJournal(r.Context(), initial.WorkspaceID, initial.ID, caseID)
	if err != nil {
		writeError(w, 503, "Stored journal unavailable or exceeds inspection limits")
		return
	}
	current, err := reader.GetRecoveryCase(r.Context(), initial.WorkspaceID, initial.ID, caseID)
	if err != nil || current != before || view.CaseID != caseID || view.TaskID != initial.ID || view.AttemptID != before.AttemptID || view.Phase != before.Phase {
		writeError(w, 403, "Recovery record changed during inspection")
		return
	}
	after, err := reader.Get(r.Context(), initial.ID)
	sameOptional := func(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
	if err != nil || after.ID != initial.ID || after.WorkspaceID != initial.WorkspaceID || after.Revision != view.TaskRevision || !sameOptional(after.ProjectID, initial.ProjectID) || !sameOptional(after.ProjectWorkspaceID, initial.ProjectWorkspaceID) || !sameOptional(after.ArtifactSessionID, initial.ArtifactSessionID) {
		writeError(w, 403, "Task scope or journal revision changed")
		return
	}
	if !s.authorizeCurrentTaskRead(w, r, identity, initial.WorkspaceID) {
		return
	}
	body, err := json.Marshal(view)
	if err != nil || len(body) > 128<<10 {
		writeError(w, 503, "Journal inspection exceeds response limits")
		return
	}
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}
