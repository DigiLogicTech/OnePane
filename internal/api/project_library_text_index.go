package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type workspaceLibraryTextService interface {
	ResolveBoundedWorkspaceLibraryVersion(context.Context, string, string, string, int64) (projectworkspace.LibraryVersion, error)
	IndexWorkspaceLibraryText(context.Context, projectworkspace.IndexLibraryTextCommand) (projectworkspace.LibraryTextIndex, error)
	WorkspaceLibraryTextIndex(context.Context, projectworkspace.LibraryTextSelection) (projectworkspace.LibraryTextIndex, error)
	SearchWorkspaceLibraryText(context.Context, projectworkspace.LibraryTextSelection, string) (projectworkspace.LibraryTextSearch, error)
}

// Indexing is an explicit operation on an already managed immutable version.
// No request body, filesystem path, parser command or replacement bytes are
// accepted. Persisting derived text grants no further Library or Task access.
func (s *Server) indexWorkspaceLibraryText(w http.ResponseWriter, r *http.Request) {
	s.workspaceLibraryText(w, r, true, false)
}

func (s *Server) workspaceLibraryTextStatus(w http.ResponseWriter, r *http.Request) {
	s.workspaceLibraryText(w, r, false, false)
}

func (s *Server) searchWorkspaceLibraryText(w http.ResponseWriter, r *http.Request) {
	s.workspaceLibraryText(w, r, false, true)
}

func (s *Server) workspaceLibraryText(w http.ResponseWriter, r *http.Request, index, search bool) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	identity, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	// Text indexing edits derived Library metadata; it does not provision or
	// execute a Workspace runtime (which separately requires project.run).
	p, workspaceID, _, ok := s.workspaceRuntimeContext(w, r, false)
	if !ok {
		return
	}
	text, ready := s.projects.(workspaceLibraryTextService)
	lib, libraryReady := s.projects.(projectLibraryService)
	verifier, authenticated := s.auth.(WorkspaceTransactionAuthorizer)
	if !ready || !libraryReady || !authenticated || s.libraryArtifacts == nil {
		writeError(w, 503, "Transactional Library text indexing unavailable")
		return
	}
	if index {
		body, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
		if readErr != nil || len(body) != 0 {
			writeError(w, 400, "Library indexing accepts no replacement content")
			return
		}
	}
	query := r.URL.Query().Get("q")
	if search && (strings.TrimSpace(query) == "" || len(query) > 256 || !utf8.ValidString(query) || strings.ContainsRune(query, '\x00')) {
		writeError(w, 400, "A bounded literal Library text query is required")
		return
	}
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version < 1 || !validAssignmentMetadataString(r.PathValue("assetID"), 256) {
		writeError(w, 400, "Invalid Library text version")
		return
	}
	capabilities := []string{"project.read"}
	if index {
		capabilities = append(capabilities, "project.write")
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, p.WorkspaceID, capabilities...) {
		return
	}
	selected, err := text.ResolveBoundedWorkspaceLibraryVersion(r.Context(), p.ID, workspaceID, r.PathValue("assetID"), version)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !strings.HasPrefix(selected.StorageURI, "artifact:") {
		writeError(w, 503, "Library text requires a managed artifact")
		return
	}
	artifactID := strings.TrimPrefix(selected.StorageURI, "artifact:")
	// Unsupported/oversized status is metadata, not verified text. Reading a
	// large binary merely to record that outcome would defeat the parser budget.
	if selected.SizeBytes <= projectworkspace.LibraryTextIndexLimit && projectworkspace.LibraryTextMIME(selected.MIMEType) {
		digest, valid := librarySHA256Digest(selected.ContentHash)
		if !valid || s.libraryArtifacts.VerifyExpectedContent(r.Context(), artifactID, "sha256:"+digest, selected.SizeBytes) != nil {
			writeError(w, 503, "Library text source failed integrity verification")
			return
		}
	}
	reader, raw, err := s.libraryArtifacts.Open(r.Context(), artifactID)
	if err != nil {
		writeError(w, 503, "Library text source unavailable")
		return
	}
	defer reader.Close()
	if !libraryArtifactMatchesProject(selected, raw, p.ID, p.WorkspaceID) {
		writeError(w, 403, "Library text provenance mismatch")
		return
	}
	selection := projectworkspace.LibraryTextSelection{
		ProjectID: p.ID, WorkspaceID: workspaceID, AssetID: selected.AssetID,
		Version: selected.Version, ActorPrincipalID: identity.PrincipalID,
		Authorize: func(ctx context.Context, tx storage.Tx) error {
			// Project tenancy and the managed source must still be the exact
			// identities observed before filesystem collection. Membership in
			// another tenant never transfers the original credential's scope.
			var valid bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM projects p JOIN project_workspaces w ON w.project_id=p.id
 JOIN artifacts a ON a.id=?
 WHERE p.id=? AND p.workspace_id=? AND p.status='active'
 AND w.id=? AND w.status='active' AND a.workspace_id=p.workspace_id
 AND a.project_id=p.id AND a.status='active' AND a.content_hash=?
 AND a.size_bytes=? AND a.storage_ref=?)`, raw.ID, p.ID, p.WorkspaceID,
				workspaceID, raw.ContentHash, raw.SizeBytes, raw.StorageRef).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return ErrForbidden
			}
			return verifier.AuthorizeWorkspaceInTransaction(ctx, tx, r, identity, p.WorkspaceID, capabilities...)
		},
	}
	var result any
	if index {
		var content []byte
		if selected.SizeBytes <= projectworkspace.LibraryTextIndexLimit && projectworkspace.LibraryTextMIME(selected.MIMEType) {
			content, err = io.ReadAll(io.LimitReader(reader, projectworkspace.LibraryTextIndexLimit+1))
			if err != nil {
				writeError(w, 503, "Library text source read failed")
				return
			}
		}
		result, err = text.IndexWorkspaceLibraryText(r.Context(), projectworkspace.IndexLibraryTextCommand{Selection: selection, Selected: selected, Content: content})
	} else if search {
		result, err = text.SearchWorkspaceLibraryText(r.Context(), selection, query)
	} else {
		result, err = text.WorkspaceLibraryTextIndex(r.Context(), selection)
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrUnauthenticated):
			writeError(w, 401, "Library text credential expired")
		case errors.Is(err, ErrForbidden):
			writeError(w, 403, "Library text authority changed")
		case errors.Is(err, projectworkspace.ErrLibraryTextNotIndexed):
			writeError(w, 409, "Library version has no ready text index; inspect its indexing status")
		case errors.Is(err, projectworkspace.ErrLibraryTextIndex), errors.Is(err, projectworkspace.ErrLibraryPublicationLimit), errors.Is(err, projectworkspace.ErrLibraryMetadataLimit):
			writeError(w, 409, "Library text identity or index requires reconciliation")
		default:
			respondDomain(w, nil, err, 0)
		}
		return
	}
	// Even an index committed successfully may have no private response after
	// revocation. A later authorized retry observes its immutable retained status.
	currentArtifact, err := s.libraryArtifacts.Get(r.Context(), artifactID)
	if err != nil || !libraryArtifactMatchesProject(selected, currentArtifact, p.ID, p.WorkspaceID) || currentArtifact.StorageRef != raw.StorageRef {
		writeError(w, 403, "Library text source changed during collection")
		return
	}
	if !s.authorizeCurrentLibraryRead(w, r, identity, lib, p, workspaceID, selected) {
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, p.WorkspaceID, capabilities...) {
		return
	}
	body, err := json.Marshal(result)
	if err != nil || len(body) > 1<<20 {
		writeError(w, 503, "Library text result exceeds response limit")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
