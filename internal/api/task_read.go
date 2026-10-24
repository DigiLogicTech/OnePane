package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

type taskDetailReader interface {
	Get(context.Context, string) (task.Task, error)
}

// Task inspection uses the same public metadata contract as the Task list.
// Completion criteria, model context, credentials and raw result JSON are not
// added to this surface. Artifact/result access keeps its independent gates.
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	reader, available := s.tasks.(taskDetailReader)
	if !available {
		writeError(w, http.StatusServiceUnavailable, "Task service unavailable")
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
	if !s.authorizeCurrentTaskRead(w, r, i, t.WorkspaceID) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, taskResponse(t))
}

func (s *Server) authorizeCurrentTaskRead(w http.ResponseWriter, r *http.Request, original Identity, workspace string) bool {
	// The credential may have been revoked or replaced while storage was read.
	// Authorizing an earlier Identity alone does not revalidate its credential.
	return s.authorizeCurrentWorkspaceRead(w, r, original, workspace, "task.read")
}
