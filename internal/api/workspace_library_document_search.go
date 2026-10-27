package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
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

type workspaceIndexedDocumentService interface {
	workspaceLibraryDocumentService
	SearchWorkspaceIndexedLibraryDocuments(context.Context, projectworkspace.LibraryTextSelection, string, projectworkspace.LibraryDocumentSearchPosition) (projectworkspace.LibraryWorkspaceDocumentSearch, error)
}

// Cursor records contain navigation only, never text or a transferable grant.
// Fixed expiry, global and per-credential LRU limits bound process memory. A
// restart/eviction expires navigation honestly; every page reauthorizes reads.
type libraryDocumentCursorBinding struct {
	tenant, project, workspace, principal, credential, principalType, method string
	scopes, query                                                            [32]byte
}
type libraryDocumentCursorEntry struct {
	binding       libraryDocumentCursorBinding
	position      projectworkspace.LibraryDocumentSearchPosition
	expires, used time.Time
}
type libraryDocumentSearchCursorRegistry struct {
	mu      sync.Mutex
	entries map[string]libraryDocumentCursorEntry
	now     func() time.Time
}

func (c *libraryDocumentSearchCursorRegistry) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
func (c *libraryDocumentSearchCursorRegistry) get(token string, binding libraryDocumentCursorBinding) (projectworkspace.LibraryDocumentSearchPosition, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	entry, ok := c.entries[token]
	if !ok || !now.Before(entry.expires) || entry.binding != binding {
		if ok && !now.Before(entry.expires) {
			delete(c.entries, token)
		}
		return projectworkspace.LibraryDocumentSearchPosition{}, false
	}
	entry.used = now
	c.entries[token] = entry
	return entry.position, true
}
func (c *libraryDocumentSearchCursorRegistry) put(binding libraryDocumentCursorBinding, position projectworkspace.LibraryDocumentSearchPosition) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	if c.entries == nil {
		c.entries = make(map[string]libraryDocumentCursorEntry)
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
	c.entries[token] = libraryDocumentCursorEntry{binding: binding, position: position, expires: now.Add(5 * time.Minute), used: now}
	return token, nil
}

type workspaceIndexedDocumentResponse struct {
	Query      string                                  `json:"query"`
	Sources    []projectworkspace.LibraryDocumentIndex `json:"sources"`
	Matches    []workspaceDocumentSearchMatch          `json:"matches"`
	HasMore    bool                                    `json:"has_more"`
	NextCursor string                                  `json:"next_cursor"`
}
type verifiedLibraryDocumentVersion struct {
	selected projectworkspace.LibraryVersion
	raw      artifact.Artifact
	index    projectworkspace.LibraryDocumentIndex
}

// Emit each source receipt once so eight escaped paragraphs plus two 64-part
// receipts remain below the response budget.
type workspaceDocumentSearchMatch struct {
	Source int                                  `json:"source"`
	Unit   projectworkspace.LibraryDocumentUnit `json:"unit"`
}

func (s *Server) searchWorkspaceIndexedDocuments(w http.ResponseWriter, r *http.Request) {
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
	reader, ready := s.projects.(workspaceIndexedDocumentService)
	verifier, authenticated := s.auth.(WorkspaceTransactionAuthorizer)
	if !ready || !authenticated || s.libraryArtifacts == nil {
		writeError(w, 503, "Transactional Workspace document search unavailable")
		return
	}
	query, token := r.URL.Query().Get("q"), r.URL.Query().Get("cursor")
	if strings.TrimSpace(query) == "" || len(query) > 256 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) || len(token) > 128 {
		writeError(w, 400, "Invalid bounded literal document search")
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, p.WorkspaceID, "project.read") {
		return
	}
	scopeJSON, _ := json.Marshal([]json.RawMessage{identity.WorkspaceScope, identity.CapabilityScope})
	binding := libraryDocumentCursorBinding{tenant: p.WorkspaceID, project: p.ID, workspace: workspace, principal: identity.PrincipalID, credential: identity.CredentialID, principalType: identity.PrincipalType, method: identity.AuthMethod, scopes: sha256.Sum256(scopeJSON), query: sha256.Sum256([]byte(query))}
	position := projectworkspace.LibraryDocumentSearchPosition{}
	if token != "" {
		var found bool
		position, found = s.libraryDocumentSearchCursors.get(token, binding)
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
	page, finalize, verified := s.collectVerifiedWorkspaceDocumentsSearch(w, r, p, workspace, reader, selection, query, position)
	if !verified || !finalize() {
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
	out := workspaceIndexedDocumentResponse{Query: query, Sources: []projectworkspace.LibraryDocumentIndex{}, Matches: []workspaceDocumentSearchMatch{}, HasMore: page.HasMore}
	type versionKey struct {
		asset   string
		version int64
	}
	sourcePositions := map[versionKey]int{}
	for _, match := range page.Matches {
		key := versionKey{match.Index.AssetID, match.Index.Version}
		position, exists := sourcePositions[key]
		if !exists {
			position = len(out.Sources)
			sourcePositions[key] = position
			out.Sources = append(out.Sources, match.Index)
		}
		out.Matches = append(out.Matches, workspaceDocumentSearchMatch{Source: position, Unit: match.Unit})
	}
	if page.HasMore {
		out.NextCursor, err = s.libraryDocumentSearchCursors.put(binding, page.NextPosition)
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

func (s *Server) collectVerifiedWorkspaceDocumentsSearch(w http.ResponseWriter, r *http.Request, p projectworkspace.Project, workspace string, reader workspaceIndexedDocumentService, selection projectworkspace.LibraryTextSelection, query string, position projectworkspace.LibraryDocumentSearchPosition) (projectworkspace.LibraryWorkspaceDocumentSearch, func() bool, bool) {
	page, err := reader.SearchWorkspaceIndexedLibraryDocuments(r.Context(), selection, query, position)
	if err != nil {
		s.workspaceIndexedDocumentError(w, err)
		return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
	}
	if len(page.Matches) > 8 || len(page.Candidates) > 2 || page.Query != query || page.HasMore && page.NextPosition.AssetID == "" {
		writeError(w, 503, "Invalid bounded search result")
		return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
	}
	type versionKey struct {
		asset   string
		version int64
	}
	verified := make(map[versionKey]verifiedLibraryDocumentVersion)
	for _, candidate := range page.Candidates {
		key := versionKey{candidate.Index.AssetID, candidate.Index.Version}
		if _, exists := verified[key]; exists {
			writeError(w, 503, "Duplicate document verification candidate")
			return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
		}
		selected, err := reader.ResolveBoundedWorkspaceLibraryVersion(r.Context(), p.ID, workspace, key.asset, key.version)
		if err != nil {
			s.workspaceIndexedDocumentError(w, err)
			return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
		}
		digest, valid := librarySHA256Digest(selected.ContentHash)
		if !valid || selected != candidate.Version || candidate.Index.RawSourceSHA != "sha256:"+digest || candidate.Index.RawSourceSize != selected.SizeBytes || candidate.Index.ParserID != projectworkspace.LibraryDocumentParserID || candidate.Index.Status == "not_indexed" || !strings.HasPrefix(selected.StorageURI, "artifact:") {
			writeError(w, 409, "Indexed source identity changed")
			return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
		}
		id := strings.TrimPrefix(selected.StorageURI, "artifact:")
		raw, err := s.libraryArtifacts.Get(r.Context(), id)
		if err != nil || !libraryArtifactMatchesProject(selected, raw, p.ID, p.WorkspaceID) {
			writeError(w, 403, "Indexed source provenance changed")
			return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
		}
		eligible := selected.MIMEType == projectworkspace.LibraryDocumentMIME && selected.SizeBytes <= projectworkspace.LibraryDocumentIndexLimit
		if eligible {
			if err = s.libraryArtifacts.VerifyExpectedContent(r.Context(), id, "sha256:"+digest, selected.SizeBytes); err != nil {
				writeError(w, 503, "Indexed document source failed integrity verification")
				return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
			}
			stream, opened, openErr := s.libraryArtifacts.Open(r.Context(), id)
			if openErr != nil {
				writeError(w, 503, "Indexed document source unavailable")
				return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
			}
			content, readErr := io.ReadAll(io.LimitReader(stream, selected.SizeBytes+1))
			closeErr := stream.Close()
			if readErr != nil || closeErr != nil || opened.StorageRef != raw.StorageRef || !libraryArtifactMatchesProject(selected, opened, p.ID, p.WorkspaceID) {
				writeError(w, 503, "Indexed document read or provenance changed")
				return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
			}
			if err = projectworkspace.VerifyLibraryDocumentContent(candidate.Index, content); err != nil {
				s.workspaceIndexedDocumentError(w, err)
				return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
			}
		}
		verified[key] = verifiedLibraryDocumentVersion{selected: selected, raw: raw, index: candidate.Index}
	}
	for _, match := range page.Matches {
		key := versionKey{match.Index.AssetID, match.Index.Version}
		entry, ok := verified[key]
		if !ok || !reflect.DeepEqual(entry.index, match.Index) {
			writeError(w, 503, "Document match has no independently verified source")
			return projectworkspace.LibraryWorkspaceDocumentSearch{}, nil, false
		}
	}
	return page, func() bool {
		// Repeat the same bounded read after all potentially slow artifact I/O. Do
		// not expose a partial result or allocate a cursor from changed collections.
		current, err := reader.SearchWorkspaceIndexedLibraryDocuments(r.Context(), selection, query, position)
		if err != nil {
			s.workspaceIndexedDocumentError(w, err)
			return false
		}
		if !reflect.DeepEqual(page, current) {
			writeError(w, 409, "Search sources changed; retry the page")
			return false
		}
		for key, entry := range verified {
			selected, err := reader.ResolveBoundedWorkspaceLibraryVersion(r.Context(), p.ID, workspace, key.asset, key.version)
			if err != nil {
				s.workspaceIndexedDocumentError(w, err)
				return false
			}
			raw, err := s.libraryArtifacts.Get(r.Context(), entry.raw.ID)
			if err != nil || selected != entry.selected || raw.StorageRef != entry.raw.StorageRef || !libraryArtifactMatchesProject(selected, raw, p.ID, p.WorkspaceID) {
				writeError(w, 403, "Indexed source changed before publication")
				return false
			}
		}

		return true
	}, true
}

func (s *Server) workspaceIndexedDocumentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		writeError(w, 401, "Search credential expired")
	case errors.Is(err, ErrForbidden):
		writeError(w, 403, "Search authority changed")
	case errors.Is(err, projectworkspace.ErrLibraryDocumentIndex), errors.Is(err, projectworkspace.ErrLibraryPublicationLimit), errors.Is(err, projectworkspace.ErrLibraryMetadataLimit):
		writeError(w, 409, "Indexed search requires reconciliation")
	default:
		respondDomain(w, nil, err, 0)
	}
}
