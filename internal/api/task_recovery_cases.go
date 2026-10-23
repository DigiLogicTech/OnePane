package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

type taskRecoveryReader interface {
	taskDetailReader
	ListRecoveryCases(context.Context, string, string, string) (task.RecoveryCasePage, error)
	GetRecoveryCase(context.Context, string, string, string) (task.RecoveryCaseMetadata, error)
}

func (s *Server) readTaskRecoveryCases(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	reader, ok := s.tasks.(taskRecoveryReader)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Task recovery intake unavailable")
		return
	}
	t, err := reader.Get(r.Context(), r.PathValue("taskID"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Task unavailable")
		return
	}
	if !s.authorize(w, r, i, t.WorkspaceID, "task.read") {
		return
	}
	var result any
	if caseID := r.PathValue("caseID"); caseID != "" {
		result, err = reader.GetRecoveryCase(r.Context(), t.WorkspaceID, t.ID, caseID)
	} else {
		after := r.URL.Query().Get("after")
		if len(after) > 160 {
			writeError(w, http.StatusBadRequest, "Invalid recovery case cursor")
			return
		}
		result, err = reader.ListRecoveryCases(r.Context(), t.WorkspaceID, t.ID, after)
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Recovery case not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Recovery intake unavailable")
		return
	}
	// Live revocation during the database read denies the returned metadata.
	fresh, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if fresh.PrincipalID != i.PrincipalID || fresh.CredentialID != i.CredentialID || fresh.PrincipalType != i.PrincipalType {
		writeError(w, http.StatusForbidden, "Identity changed during recovery read")
		return
	}
	if !s.authorize(w, r, fresh, t.WorkspaceID, "task.read") {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}
