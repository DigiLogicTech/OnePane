package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type workspaceIndexedTextService interface {
	workspaceLibraryTextService
	SearchWorkspaceIndexedLibraryText(context.Context, projectworkspace.LibraryTextSelection, string, projectworkspace.LibraryTextSearchPosition) (projectworkspace.LibraryWorkspaceTextSearch, error)
}

// Cursor records contain navigation only, never text or a transferable grant.
// Fixed expiry, global and per-credential LRU limits bound process memory. A
// restart/eviction expires navigation honestly; every page reauthorizes reads.
type libraryTextCursorBinding struct {
	tenant, project, workspace, principal, credential, principalType, method string
	scopes, query                                                            [32]byte
}
type libraryTextCursorEntry struct {
	binding       libraryTextCursorBinding
	position      projectworkspace.LibraryTextSearchPosition
	expires, used time.Time
}
type libraryTextSearchCursorRegistry struct {
	mu      sync.Mutex
	entries map[string]libraryTextCursorEntry
	now     func() time.Time
}

func (c *libraryTextSearchCursorRegistry) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
func (c *libraryTextSearchCursorRegistry) get(token string, binding libraryTextCursorBinding) (projectworkspace.LibraryTextSearchPosition, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	entry, ok := c.entries[token]
	if !ok || !now.Before(entry.expires) || entry.binding != binding {
		if ok && !now.Before(entry.expires) {
			delete(c.entries, token)
		}
		return projectworkspace.LibraryTextSearchPosition{}, false
	}
	entry.used = now
	c.entries[token] = entry
	return entry.position, true
}
func (c *libraryTextSearchCursorRegistry) put(binding libraryTextCursorBinding, position projectworkspace.LibraryTextSearchPosition) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	if c.entries == nil {
		c.entries = make(map[string]libraryTextCursorEntry)
	}
	count := 0
	oldest, credentialOldest := "", ""
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
			continue
		}
		if oldest == "" || entry.used.Before(c.entries[oldest].used) {
			oldest = key
		}
		if entry.binding.principal == binding.principal && entry.binding.credential == binding.credential {
			count++
			if credentialOldest == "" || entry.used.Before(c.entries[credentialOldest].used) {
				credentialOldest = key
			}
		}
	}
	if count >= 32 {
		delete(c.entries, credentialOldest)
	}
	if len(c.entries) >= 256 {
		delete(c.entries, oldest)
	}
	c.entries[token] = libraryTextCursorEntry{binding: binding, position: position, expires: now.Add(5 * time.Minute), used: now}
	return token, nil
}

type workspaceIndexedTextResponse struct {
	Query      string                                       `json:"query"`
	Matches    []projectworkspace.LibraryWorkspaceTextMatch `json:"matches"`
	HasMore    bool                                         `json:"has_more"`
	NextCursor string                                       `json:"next_cursor"`
}
type verifiedLibraryTextVersion struct {
	selected projectworkspace.LibraryVersion
	raw      artifact.Artifact
}

func (s *Server) searchWorkspaceIndexedText(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	identity, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	p, workspace, _, ok := s.workspaceRuntimeContext(w, r, false)
	if !ok {
		return
	}
	reader, ready := s.projects.(workspaceIndexedTextService)
	verifier, authenticated := s.auth.(WorkspaceTransactionAuthorizer)
	if !ready || !authenticated || s.libraryArtifacts == nil {
		writeError(w, 503, "Transactional Workspace text search unavailable")
		return
	}
	query, token := r.URL.Query().Get("q"), r.URL.Query().Get("cursor")
	if strings.TrimSpace(query) == "" || len(query) > 256 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) || len(token) > 128 {
		writeError(w, 400, "Invalid bounded literal text search")
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, p.WorkspaceID, "project.read") {
		return
	}
	scopeJSON, _ := json.Marshal([]json.RawMessage{identity.WorkspaceScope, identity.CapabilityScope})
	binding := libraryTextCursorBinding{tenant: p.WorkspaceID, project: p.ID, workspace: workspace, principal: identity.PrincipalID, credential: identity.CredentialID, principalType: identity.PrincipalType, method: identity.AuthMethod, scopes: sha256.Sum256(scopeJSON), query: sha256.Sum256([]byte(query))}
	position := projectworkspace.LibraryTextSearchPosition{}
	if token != "" {
		var found bool
		position, found = s.libraryTextSearchCursors.get(token, binding)
		if !found {
			writeError(w, 409, "Search navigation expired or changed; start a new search")
			return
		}
	}
	selection := projectworkspace.LibraryTextSelection{ProjectID: p.ID, WorkspaceID: workspace, ActorPrincipalID: identity.PrincipalID, Authorize: func(ctx context.Context, tx storage.Tx) error {
		var valid bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects p JOIN project_workspaces w ON w.project_id=p.id WHERE p.id=? AND p.workspace_id=? AND p.status='active' AND w.id=? AND w.status='active')`, p.ID, p.WorkspaceID, workspace).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return ErrForbidden
		}
		return verifier.AuthorizeWorkspaceInTransaction(ctx, tx, r, identity, p.WorkspaceID, "project.read")
	}}
	page, err := reader.SearchWorkspaceIndexedLibraryText(r.Context(), selection, query, position)
	if err != nil {
		s.workspaceIndexedTextError(w, err)
		return
	}
	if len(page.Matches) > 8 || page.Query != query || page.HasMore && page.NextPosition.AssetID == "" {
		writeError(w, 503, "Invalid bounded search result")
		return
	}
	type versionKey struct {
		asset   string
		version int64
	}
	verified := make(map[versionKey]verifiedLibraryTextVersion)
	for _, match := range page.Matches {
		key := versionKey{match.Index.AssetID, match.Index.Version}
		if _, exists := verified[key]; exists {
			continue
		}
		selected, err := reader.ResolveBoundedWorkspaceLibraryVersion(r.Context(), p.ID, workspace, key.asset, key.version)
		if err != nil {
			s.workspaceIndexedTextError(w, err)
			return
		}
		digest, valid := librarySHA256Digest(selected.ContentHash)
		if !valid || match.Index.ContentHash != "sha256:"+digest || match.Index.SizeBytes != selected.SizeBytes || selected.SizeBytes > projectworkspace.LibraryTextIndexLimit || !strings.HasPrefix(selected.StorageURI, "artifact:") {
			writeError(w, 409, "Indexed source identity changed")
			return
		}
		id := strings.TrimPrefix(selected.StorageURI, "artifact:")
		if err = s.libraryArtifacts.VerifyContent(r.Context(), id); err != nil {
			writeError(w, 503, "Indexed source failed integrity verification")
			return
		}
		stream, raw, err := s.libraryArtifacts.Open(r.Context(), id)
		if err != nil {
			writeError(w, 503, "Indexed source unavailable")
			return
		}
		closeErr := stream.Close()
		if closeErr != nil || !libraryArtifactMatchesProject(selected, raw, p.ID, p.WorkspaceID) {
			writeError(w, 403, "Indexed source provenance changed")
			return
		}
		verified[key] = verifiedLibraryTextVersion{selected: selected, raw: raw}
	}
	// Repeat the same bounded read after all potentially slow artifact I/O. Do
	// not expose a partial result or allocate a cursor from changed collections.
	current, err := reader.SearchWorkspaceIndexedLibraryText(r.Context(), selection, query, position)
	if err != nil {
		s.workspaceIndexedTextError(w, err)
		return
	}
	if !reflect.DeepEqual(page, current) {
		writeError(w, 409, "Search sources changed; retry the page")
		return
	}
	for key, entry := range verified {
		selected, err := reader.ResolveBoundedWorkspaceLibraryVersion(r.Context(), p.ID, workspace, key.asset, key.version)
		if err != nil {
			s.workspaceIndexedTextError(w, err)
			return
		}
		raw, err := s.libraryArtifacts.Get(r.Context(), entry.raw.ID)
		if err != nil || selected != entry.selected || raw.StorageRef != entry.raw.StorageRef || !libraryArtifactMatchesProject(selected, raw, p.ID, p.WorkspaceID) {
			writeError(w, 403, "Indexed source changed before publication")
			return
		}
	}
	project, err := s.projects.Project(r.Context(), p.ID)
	if err != nil || project.ID != p.ID || project.WorkspaceID != p.WorkspaceID || project.Status != "active" {
		writeError(w, 403, "Search Project changed")
		return
	}
	views, ok := s.projects.(projectWorkspaceViewReader)
	if !ok {
		writeError(w, 503, "Workspace search scope unavailable")
		return
	}
	view, err := views.WorkspaceView(r.Context(), workspace)
	if err != nil || view.ProjectID != p.ID || view.Status != "active" {
		writeError(w, 403, "Search Workspace changed")
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, p.WorkspaceID, "project.read") {
		return
	}
	out := workspaceIndexedTextResponse{Query: query, Matches: page.Matches, HasMore: page.HasMore}
	if page.HasMore {
		out.NextCursor, err = s.libraryTextSearchCursors.put(binding, page.NextPosition)
		if err != nil {
			writeError(w, 503, "Search continuation unavailable")
			return
		}
	}
	body, err := json.Marshal(out)
	if err != nil || len(body) > 1<<20 {
		writeError(w, 503, "Search output exceeds response limit")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

func (s *Server) workspaceIndexedTextError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		writeError(w, 401, "Search credential expired")
	case errors.Is(err, ErrForbidden):
		writeError(w, 403, "Search authority changed")
	case errors.Is(err, projectworkspace.ErrLibraryTextIndex), errors.Is(err, projectworkspace.ErrLibraryPublicationLimit), errors.Is(err, projectworkspace.ErrLibraryMetadataLimit):
		writeError(w, 409, "Indexed search requires reconciliation")
	default:
		respondDomain(w, nil, err, 0)
	}
}
