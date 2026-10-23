package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

type teamMessagePager interface {
	PageMessages(context.Context, string, team.MessagePageOptions) (team.MessagePage, error)
}

func (s *Server) pageTeamMessages(w http.ResponseWriter, r *http.Request) {
	if s.team == nil {
		writeError(w, http.StatusServiceUnavailable, "Team unavailable")
		return
	}
	identity, ss, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	pager, ok := s.team.(teamMessagePager)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Team history paging unavailable")
		return
	}
	query := r.URL.Query()
	limit := 0
	if query.Has("limit") {
		v, err := strconv.Atoi(query.Get("limit"))
		if err != nil || v < 1 || v > 50 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 50")
			return
		}
		limit = v
	}
	out, err := pager.PageMessages(r.Context(), ss.ID, team.MessagePageOptions{BeforeID: query.Get("before"), AfterID: query.Get("after"), Limit: limit})
	if errors.Is(err, team.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "invalid Team history cursor or page")
		return
	}
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	// Every poll/page is newly authenticated. Recheck after collection as well,
	// so a revocation observed during the read prevents publishing the payload.
	if !s.authorize(w, r, identity, ss.WorkspaceID, "team.read") {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}
