package projectworkspace

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"strings"
	"unicode/utf8"
)

var ErrTaskPublicationLimit = errors.New("Task publications exceed the bounded metadata limit")
var ErrTaskPublicationUnavailable = errors.New("Task publication records require reconciliation")

// Limit raw receipts before joining or applying permissions. A filtered recent
// Workspace list cannot establish a complete association with one exact Task.
// CASE bounds payloads before database/sql materializes any untrusted strings.
const taskLibraryReceiptsSQL = `SELECT
 CASE WHEN length(CAST(relative_path AS BLOB)) BETWEEN 1 AND 4096 THEN relative_path END,
 CASE WHEN length(CAST(content_hash AS BLOB))=64 THEN content_hash END,
 CASE WHEN length(CAST(library_asset_id AS BLOB)) BETWEEN 1 AND 256 THEN library_asset_id END,
 CASE WHEN length(CAST(artifact_id AS BLOB)) BETWEEN 1 AND 256 THEN artifact_id END,
 asset_version,updated_at,
 (project_id=? AND project_workspace_id=? AND status='complete'
  AND (SELECT source_runtime.project_id,source_runtime.project_workspace_id
   FROM project_runtimes source_runtime WHERE source_runtime.id=workspace_file_publications.runtime_id)
   =(workspace_file_publications.project_id,workspace_file_publications.project_workspace_id)
  AND (SELECT source_app.project_runtime_id FROM project_applications source_app
   WHERE source_app.id=workspace_file_publications.application_id)=runtime_id)
 FROM workspace_file_publications WHERE task_id=?
 ORDER BY relative_path,content_hash LIMIT 33`

// TaskLibraryPublications returns all receipts in a deliberately small first
// slice, or no records with an explicit error. Receipts associate bytes with a
// Task, not with its final Attempt: reused versions retain earlier provenance.
// The caller must separately validate the accepted Team completion and current
// credential before publishing this private metadata.
func (s *Service) TaskLibraryPublications(ctx context.Context, projectID, workspaceID, taskID string) ([]WorkspacePublishedOutput, error) {
	for _, value := range []string{projectID, workspaceID, taskID} {
		if value == "" || len(value) > 256 || !validPublicationMetadataText(value) {
			return nil, ErrInvalidCommand
		}
	}
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks t
 JOIN projects p ON p.id=t.project_id AND p.workspace_id=t.workspace_id AND p.status='active'
 JOIN project_workspaces w ON w.id=t.project_workspace_id AND w.project_id=p.id AND w.status='active'
 WHERE t.id=? AND p.id=? AND w.id=? AND t.archived_at IS NULL`, taskID, projectID, workspaceID).Scan(&active)
	if err != nil {
		return nil, err
	}
	if active != 1 {
		return nil, ErrCrossWorkspace
	}
	rows, err := s.db.QueryContext(ctx, taskLibraryReceiptsSQL, projectID, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	type receipt struct {
		path, hash, asset, artifact sql.NullString
		version                     sql.NullInt64
		updated                     int64
		valid                       bool
	}
	receipts := []receipt{}
	for rows.Next() {
		var r receipt
		if err = rows.Scan(&r.path, &r.hash, &r.asset, &r.artifact, &r.version, &r.updated, &r.valid); err != nil {
			break
		}
		receipts = append(receipts, r)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(receipts) > 32 {
		return nil, ErrTaskPublicationLimit
	}
	out := make([]WorkspacePublishedOutput, 0, len(receipts))
	for _, r := range receipts {
		if !r.valid || !r.path.Valid || !r.hash.Valid || !r.asset.Valid || !r.artifact.Valid || !r.version.Valid || r.version.Int64 < 1 || !utf8.ValidString(r.asset.String) || !utf8.ValidString(r.artifact.String) || strings.ContainsAny(r.asset.String+r.artifact.String, "\x00\r\n") || !validRecordedPublicationPath(r.path.String) || !validPublicationDigest(r.hash.String) {
			return nil, ErrTaskPublicationUnavailable
		}
		v, e := s.ResolveBoundedWorkspaceLibraryVersion(ctx, projectID, workspaceID, r.asset.String, r.version.Int64)
		if e != nil {
			return nil, e
		}
		if v.ContentHash != r.hash.String || v.StorageURI != "artifact:"+r.artifact.String || v.SizeBytes < 0 || len(v.MIMEType) > 256 || !validPublicationMetadataText(v.MIMEType) {
			return nil, ErrTaskPublicationUnavailable
		}
		var verified int
		e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artifacts a JOIN projects p ON p.id=a.project_id AND p.workspace_id=a.workspace_id AND p.status='active'
 WHERE a.id=? AND a.project_id=? AND a.status='active' AND a.content_hash=? AND a.size_bytes=?`, r.artifact.String, projectID, "sha256:"+r.hash.String, v.SizeBytes).Scan(&verified)
		if e != nil {
			return nil, e
		}
		if verified != 1 {
			return nil, ErrTaskPublicationUnavailable
		}
		out = append(out, WorkspacePublishedOutput{TaskID: taskID, RelativePath: r.path.String, AssetID: v.AssetID, Version: v.Version, ContentHash: v.ContentHash, SizeBytes: v.SizeBytes, MIMEType: v.MIMEType, PublishedAt: r.updated})
	}
	return out, nil
}

func validPublicationDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validRecordedPublicationPath(s string) bool {
	return validPublicationMetadataText(s) && s != "." && !strings.HasPrefix(s, "/") && !strings.ContainsAny(s, "\\:") && path.Clean(s) == s && s != ".." && !strings.HasPrefix(s, "../")
}

func validPublicationMetadataText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
