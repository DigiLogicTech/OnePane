package api

import (
    "net/http"
    "strings"

    "github.com/DigiLogicTech/OnePane/internal/localai"
)

// listColibriPoolModels exposes eligible model directories from the configured
// managed model pool only. Paths cannot be supplied by the caller.
func (s *Server) listColibriPoolModels(w http.ResponseWriter, r *http.Request) {
    identity, ok := s.authenticate(w, r)
    if !ok { return }
    workspace := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
    if workspace == "" {
        writeError(w, http.StatusBadRequest, "workspace_id is required")
        return
    }
    if !s.authorize(w, r, identity, workspace, "model.read") { return }
    if s.localAI == nil {
        writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
        return
    }
    entries, err := s.localAI.ColibriPoolModels(r.Context())
    if err != nil {
        respondDomain(w, nil, err, http.StatusInternalServerError)
        return
    }
    if entries == nil { entries = []localai.ColibriPoolModel{} }
    writeJSON(w, http.StatusOK, entries)
}
