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

type workspaceLibraryDocumentService interface {
	ResolveBoundedWorkspaceLibraryVersion(context.Context, string, string, string, int64) (projectworkspace.LibraryVersion, error)
	IndexWorkspaceLibraryDocument(context.Context, projectworkspace.IndexLibraryDocumentCommand) (projectworkspace.LibraryDocumentIndex, error)
	WorkspaceLibraryDocumentIndex(context.Context, projectworkspace.LibraryTextSelection) (projectworkspace.LibraryDocumentIndex, error)
	ReadWorkspaceLibraryDocument(context.Context, projectworkspace.LibraryTextSelection, string, int) (projectworkspace.LibraryDocumentPage, error)
}

func (s *Server) indexWorkspaceLibraryDocument(w http.ResponseWriter, r *http.Request) {
	s.workspaceLibraryDocument(w, r, true, false)
}
func (s *Server) workspaceLibraryDocumentStatus(w http.ResponseWriter, r *http.Request) {
	s.workspaceLibraryDocument(w, r, false, false)
}
func (s *Server) readWorkspaceLibraryDocument(w http.ResponseWriter, r *http.Request) {
	s.workspaceLibraryDocument(w, r, false, true)
}

// Document extraction retains a distinct source/derived receipt. It creates no
// new Library asset/grant and never interprets ZIP byte offsets as text pages.
func (s *Server) workspaceLibraryDocument(w http.ResponseWriter, r *http.Request, index, read bool) {
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
	documents, ready := s.projects.(workspaceLibraryDocumentService)
	verifier, authenticated := s.auth.(WorkspaceTransactionAuthorizer)
	if !ready || !authenticated || s.libraryArtifacts == nil {
		writeError(w, 503, "Transactional document extraction unavailable")
		return
	}
	if index {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
		if err != nil || len(body) != 0 {
			writeError(w, 400, "Document extraction accepts no replacement content")
			return
		}
	}
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version < 1 || !validAssignmentMetadataString(r.PathValue("assetID"), 256) {
		writeError(w, 400, "Invalid document version")
		return
	}
	query := r.URL.Query().Get("q")
	after := 0
	if read {
		if len(query) > 256 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) || (query != "" && strings.TrimSpace(query) == "") {
			writeError(w, 400, "Invalid document literal query")
			return
		}
		if value := r.URL.Query().Get("after"); value != "" {
			after, err = strconv.Atoi(value)
			if err != nil || after < 0 || after > 8192 {
				writeError(w, 400, "Invalid document paragraph position")
				return
			}
		}
	}
	capabilities := []string{"project.read"}
	if index {
		capabilities = append(capabilities, "project.write")
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, p.WorkspaceID, capabilities...) {
		return
	}
	selected, err := documents.ResolveBoundedWorkspaceLibraryVersion(r.Context(), p.ID, workspace, r.PathValue("assetID"), version)
	if err != nil {
		s.workspaceLibraryDocumentError(w, err)
		return
	}
	if !strings.HasPrefix(selected.StorageURI, "artifact:") {
		writeError(w, 503, "Document requires a managed source artifact")
		return
	}
	artifactID := strings.TrimPrefix(selected.StorageURI, "artifact:")
	eligible := selected.SizeBytes <= projectworkspace.LibraryDocumentIndexLimit && selected.MIMEType == projectworkspace.LibraryDocumentMIME
	if eligible {
		digest, valid := librarySHA256Digest(selected.ContentHash)
		if !valid || s.libraryArtifacts.VerifyExpectedContent(r.Context(), artifactID, "sha256:"+digest, selected.SizeBytes) != nil {
			writeError(w, 503, "Document source failed integrity verification")
			return
		}
	}
	reader, raw, err := s.libraryArtifacts.Open(r.Context(), artifactID)
	if err != nil {
		writeError(w, 503, "Document source unavailable")
		return
	}
	defer reader.Close()
	if !libraryArtifactMatchesProject(selected, raw, p.ID, p.WorkspaceID) {
		writeError(w, 403, "Document source provenance changed")
		return
	}
	selection := projectworkspace.LibraryTextSelection{ProjectID: p.ID, WorkspaceID: workspace, AssetID: selected.AssetID, Version: selected.Version, ActorPrincipalID: identity.PrincipalID, Authorize: func(ctx context.Context, tx storage.Tx) error {
		var valid bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects p JOIN project_workspaces w ON w.project_id=p.id JOIN artifacts a ON a.id=? WHERE p.id=? AND p.workspace_id=? AND p.status='active' AND w.id=? AND w.status='active' AND a.workspace_id=p.workspace_id AND a.project_id=p.id AND a.status='active' AND a.content_hash=? AND a.size_bytes=? AND a.storage_ref=?)`, raw.ID, p.ID, p.WorkspaceID, workspace, raw.ContentHash, raw.SizeBytes, raw.StorageRef).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return ErrForbidden
		}
		return verifier.AuthorizeWorkspaceInTransaction(ctx, tx, r, identity, p.WorkspaceID, capabilities...)
	}}
	var content []byte
	if eligible {
		content, err = io.ReadAll(io.LimitReader(reader, projectworkspace.LibraryDocumentIndexLimit+1))
		if err != nil {
			writeError(w, 503, "Document source read failed")
			return
		}
	}
	var result any
	var receipt projectworkspace.LibraryDocumentIndex
	if index {
		receipt, err = documents.IndexWorkspaceLibraryDocument(r.Context(), projectworkspace.IndexLibraryDocumentCommand{Selection: selection, Selected: selected, Content: content})
		result = receipt
	} else if read {
		var page projectworkspace.LibraryDocumentPage
		page, err = documents.ReadWorkspaceLibraryDocument(r.Context(), selection, query, after)
		receipt = page.Index
		result = page
	} else {
		receipt, err = documents.WorkspaceLibraryDocumentIndex(r.Context(), selection)
		result = receipt
	}
	if err != nil {
		s.workspaceLibraryDocumentError(w, err)
		return
	}
	// A derived text hash cannot reconstruct a ZIP source hash. Reparse the
	// independently verified original bytes before disclosing any retained
	// content/part receipt, so self-consistent forged derived rows still deny.
	if !index && eligible {
		if err = projectworkspace.VerifyLibraryDocumentContent(receipt, content); err != nil {
			s.workspaceLibraryDocumentError(w, err)
			return
		}
	}
	currentArtifact, err := s.libraryArtifacts.Get(r.Context(), artifactID)
	if err != nil || currentArtifact.StorageRef != raw.StorageRef || !libraryArtifactMatchesProject(selected, currentArtifact, p.ID, p.WorkspaceID) {
		writeError(w, 403, "Document source changed before publication")
		return
	}
	// Keep the final grant decision on the bounded exact-version resolver too;
	// a later publication-history change cannot reintroduce an unbounded scan.
	currentVersion, err := documents.ResolveBoundedWorkspaceLibraryVersion(r.Context(), p.ID, workspace, selected.AssetID, selected.Version)
	if err != nil {
		s.workspaceLibraryDocumentError(w, err)
		return
	}
	currentProject, err := s.projects.Project(r.Context(), p.ID)
	if err != nil || currentProject.ID != p.ID || currentProject.WorkspaceID != p.WorkspaceID || currentProject.Status != "active" || currentVersion != selected {
		writeError(w, 403, "Document scope or version changed before publication")
		return
	}
	if !s.authorizeCurrentWorkspaceRead(w, r, identity, p.WorkspaceID, capabilities...) {
		return
	}
	body, err := json.Marshal(result)
	if err != nil || len(body) > 1<<20 {
		writeError(w, 503, "Document result exceeds response limit")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

func (s *Server) workspaceLibraryDocumentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		writeError(w, 401, "Document credential expired")
	case errors.Is(err, ErrForbidden):
		writeError(w, 403, "Document authority changed")
	case errors.Is(err, projectworkspace.ErrLibraryDocumentNotIndexed):
		writeError(w, 409, "Document has no ready extraction; inspect its status")
	case errors.Is(err, projectworkspace.ErrLibraryDocumentIndex), errors.Is(err, projectworkspace.ErrLibraryPublicationLimit), errors.Is(err, projectworkspace.ErrLibraryMetadataLimit):
		writeError(w, 409, "Document identity or extraction requires reconciliation")
	default:
		respondDomain(w, nil, err, 0)
	}
}
