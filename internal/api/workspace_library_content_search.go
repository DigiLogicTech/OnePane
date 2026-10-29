package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type libraryContentSearchPosition struct {
	Text                   projectworkspace.LibraryTextSearchPosition
	Document               projectworkspace.LibraryDocumentSearchPosition
	TextDone, DocumentDone bool
}

// Combined navigation retains no excerpt, source receipt or transferable grant.
type libraryContentCursorBinding struct {
	tenant, project, workspace, principal, credential, principalType, method string
	scopes, query                                                            [32]byte
}
type libraryContentCursorEntry struct {
	binding       libraryContentCursorBinding
	position      libraryContentSearchPosition
	expires, used time.Time
}
type libraryContentSearchCursorRegistry struct {
	mu      sync.Mutex
	entries map[string]libraryContentCursorEntry
	now     func() time.Time
}

func (c *libraryContentSearchCursorRegistry) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
func (c *libraryContentSearchCursorRegistry) get(token string, binding libraryContentCursorBinding) (libraryContentSearchPosition, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	entry, ok := c.entries[token]
	if !ok || !now.Before(entry.expires) || entry.binding != binding {
		if ok && !now.Before(entry.expires) {
			delete(c.entries, token)
		}
		return libraryContentSearchPosition{}, false
	}
	entry.used = now
	c.entries[token] = entry
	return entry.position, true
}
func (c *libraryContentSearchCursorRegistry) put(binding libraryContentCursorBinding, position libraryContentSearchPosition) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	if c.entries == nil {
		c.entries = make(map[string]libraryContentCursorEntry)
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
	c.entries[token] = libraryContentCursorEntry{binding: binding, position: position, expires: now.Add(5 * time.Minute), used: now}
	return token, nil
}

type workspaceIndexedContentResponse struct {
	Query       string                                       `json:"query"`
	TextMatches []projectworkspace.LibraryWorkspaceTextMatch `json:"text_matches"`
	WordSources []projectworkspace.LibraryDocumentIndex      `json:"word_sources"`
	WordMatches []workspaceDocumentSearchMatch               `json:"word_matches"`
	HasMore     bool                                         `json:"has_more"`
	NextCursor  string                                       `json:"next_cursor"`
}

// The bounded union preserves each format's native coordinates, scan limits
// and independent source verification. It is a live forward scan, not global
// ranking, semantic retrieval or a cross-request frozen snapshot.
func (s *Server) searchWorkspaceIndexedContent(w http.ResponseWriter, r *http.Request) {
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
	reader, ready := s.projects.(interface {
		workspaceIndexedTextService
		workspaceIndexedDocumentService
	})
	verifier, authenticated := s.auth.(WorkspaceTransactionAuthorizer)
	if !ready || !authenticated || s.libraryArtifacts == nil {
		writeError(w, 503, "Transactional Workspace content search unavailable")
		return
	}
	query, token := r.URL.Query().Get("q"), r.URL.Query().Get("cursor")
	if strings.TrimSpace(query) == "" || len(query) > 256 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) || len(token) > 128 {
		writeError(w, 400, "Invalid bounded literal content search")
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, p.WorkspaceID, "project.read") {
		return
	}
	scopeJSON, _ := json.Marshal([]json.RawMessage{identity.WorkspaceScope, identity.CapabilityScope})
	binding := libraryContentCursorBinding{tenant: p.WorkspaceID, project: p.ID, workspace: workspace, principal: identity.PrincipalID, credential: identity.CredentialID, principalType: identity.PrincipalType, method: identity.AuthMethod, scopes: sha256.Sum256(scopeJSON), query: sha256.Sum256([]byte(query))}
	position := libraryContentSearchPosition{}
	if token != "" {
		var found bool
		position, found = s.libraryContentSearchCursors.get(token, binding)
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
	text := projectworkspace.LibraryWorkspaceTextSearch{Query: query, Matches: []projectworkspace.LibraryWorkspaceTextMatch{}}
	word := projectworkspace.LibraryWorkspaceDocumentSearch{Query: query, Matches: []projectworkspace.LibraryWorkspaceDocumentMatch{}}
	finalizeText, finalizeWord := func() bool { return true }, func() bool { return true }
	if !position.TextDone {
		var verified bool
		text, finalizeText, verified = s.collectVerifiedWorkspaceTextSearch(w, r, p, workspace, reader, selection, query, position.Text)
		if !verified {
			return
		}
	}
	if !position.DocumentDone {
		var verified bool
		word, finalizeWord, verified = s.collectVerifiedWorkspaceDocumentsSearch(w, r, p, workspace, reader, selection, query, position.Document)
		if !verified {
			return
		}
	}
	// Both comparable collections and both complete source-identity checks occur
	// after ALL format-specific artifact I/O. No independently fetched stale
	// excerpt or partial success is published if either format fails.
	if !finalizeText() || !finalizeWord() {
		return
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
	out := workspaceIndexedContentResponse{Query: query, TextMatches: text.Matches, WordSources: []projectworkspace.LibraryDocumentIndex{}, WordMatches: []workspaceDocumentSearchMatch{}, HasMore: text.HasMore || word.HasMore}
	type versionKey struct {
		asset   string
		version int64
	}
	sources := map[versionKey]int{}
	for _, match := range word.Matches {
		key := versionKey{match.Index.AssetID, match.Index.Version}
		index, exists := sources[key]
		if !exists {
			index = len(out.WordSources)
			sources[key] = index
			out.WordSources = append(out.WordSources, match.Index)
		}
		out.WordMatches = append(out.WordMatches, workspaceDocumentSearchMatch{Source: index, Unit: match.Unit})
	}
	// The union admits each existing bounded page (8 raw lines + 8 paragraphs).
	// A format that has finished is never restarted on the same forward cursor.
	next := libraryContentSearchPosition{Text: text.NextPosition, Document: word.NextPosition, TextDone: position.TextDone || !text.HasMore, DocumentDone: position.DocumentDone || !word.HasMore}
	// Check the complete bounded response before allocating navigation state.
	body, err := json.Marshal(out)
	if err != nil || len(body) > (2<<20)-256 {
		writeError(w, 503, "Content search output exceeds response limit")
		return
	}
	if out.HasMore {
		out.NextCursor, err = s.libraryContentSearchCursors.put(binding, next)
		if err != nil {
			writeError(w, 503, "Search continuation unavailable")
			return
		}
	}
	body, err = json.Marshal(out)
	if err != nil || len(body) > 2<<20 {
		writeError(w, 503, "Content search output exceeds response limit")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}
