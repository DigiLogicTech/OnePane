package projectworkspace

import (
	"context"
	"database/sql"
	"errors"
)

// ErrLibraryPublicationLimit requires an explicit direct grant or a smaller
// publication set. A truncated directional inventory never establishes access.
var ErrLibraryPublicationLimit = errors.New("Library version exceeds the bounded directional publication limit")

// ErrLibraryMetadataLimit identifies stored metadata that cannot safely enter
// this bounded projection. It never returns a partially materialized version.
var ErrLibraryMetadataLimit = errors.New("Library version exceeds bounded metadata limits")

// boundedWorkspaceLibraryVersionSQL keeps the complete permission decision in
// one SQLite snapshot. The materialized candidate set is capped BEFORE link
// filtering, so neither an absent grant nor many disabled/foreign links can
// cause an unbounded walk. The exact-version index also supplies link ordering.
// A current direct grant avoids the directional candidate set entirely.
const boundedWorkspaceLibraryVersionSQL = `WITH selected AS MATERIALIZED (
 SELECT v.asset_id,v.version,
  CASE WHEN length(CAST(v.content_hash AS BLOB))<=71 THEN v.content_hash ELSE '' END AS content_hash,
  COALESCE(v.size_bytes,0) AS size_bytes,
  CASE WHEN length(CAST(COALESCE(v.mime_type,'') AS BLOB))<=256 THEN COALESCE(v.mime_type,'') ELSE '' END AS mime_type,
  CASE WHEN length(CAST(v.storage_uri AS BLOB))<=265 THEN v.storage_uri ELSE '' END AS storage_uri,
  v.created_at,a.project_id,
  (length(CAST(v.content_hash AS BLOB))>71 OR length(CAST(COALESCE(v.mime_type,'') AS BLOB))>256
   OR length(CAST(v.storage_uri AS BLOB))>265 OR COALESCE(v.size_bytes,0)<0) AS invalid_metadata,
  EXISTS(SELECT 1 FROM workspace_library_grants g
   WHERE g.asset_id=a.id AND g.project_workspace_id=? AND g.enabled=1
    AND json_extract(g.permissions_json,'$.read')=1
    AND ((g.version_policy='latest' AND v.version=a.current_version)
     OR (g.version_policy='pinned' AND g.pinned_version=v.version))) AS direct_read
 FROM project_library_asset_versions v
 JOIN project_library_assets a ON a.id=v.asset_id
 JOIN project_workspaces w ON w.id=? AND w.project_id=a.project_id AND w.status='active'
 WHERE a.project_id=? AND a.id=? AND a.archived=0 AND v.version=?
  AND (SELECT p.status FROM projects p WHERE p.id=a.project_id)='active'
), candidates AS MATERIALIZED (
 SELECT CASE WHEN length(CAST(pub.link_id AS BLOB))<=256 THEN pub.link_id ELSE '' END AS link_id,
  length(CAST(pub.link_id AS BLOB))>256 AS invalid_metadata
 FROM project_workspace_publications pub
 WHERE pub.asset_id=(SELECT asset_id FROM selected)
  AND pub.asset_version=(SELECT version FROM selected)
  AND pub.content_hash=(SELECT content_hash FROM selected)
  AND (SELECT direct_read FROM selected)=0
  AND (SELECT invalid_metadata FROM selected)=0
 ORDER BY pub.link_id LIMIT 129
)
SELECT s.asset_id,s.version,s.content_hash,s.size_bytes,s.mime_type,s.storage_uri,s.created_at,
 CASE WHEN s.invalid_metadata=1 THEN -2
  WHEN s.direct_read=1 THEN 1
  WHEN (SELECT COUNT(*) FROM candidates)>128 THEN -1
  WHEN EXISTS(SELECT 1 FROM candidates WHERE invalid_metadata=1) THEN -2
  WHEN EXISTS(SELECT 1 FROM candidates c CROSS JOIN project_workspace_links l ON l.id=c.link_id
   WHERE l.project_id=s.project_id AND l.target_workspace_id=? AND l.enabled=1
    AND (l.expires_at_ms IS NULL OR l.expires_at_ms>?)
    AND EXISTS(SELECT 1 FROM project_workspaces source
     WHERE source.id=l.source_workspace_id AND source.project_id=l.project_id AND source.status='active')
    AND EXISTS(SELECT 1 FROM project_workspaces target
     WHERE target.id=l.target_workspace_id AND target.project_id=l.project_id AND target.status='active'))
  THEN 1 ELSE 0 END
FROM selected s`

// ResolveBoundedWorkspaceLibraryVersion resolves an immutable version for a
// bounded metadata projection. It preserves direct latest/pinned grants and
// exact enabled directional grants, while explicitly deferring directional
// decisions with more than 128 raw same-version/hash publications. The older
// content resolver remains available with its existing semantics.
func (s *Service) ResolveBoundedWorkspaceLibraryVersion(ctx context.Context, projectID, workspaceID, assetID string, version int64) (LibraryVersion, error) {
	if projectID == "" || workspaceID == "" || assetID == "" || version < 1 || len(projectID) > 256 || len(workspaceID) > 256 || len(assetID) > 256 {
		return LibraryVersion{}, ErrInvalidCommand
	}
	var selected LibraryVersion
	var permission int
	err := s.db.QueryRowContext(ctx, boundedWorkspaceLibraryVersionSQL,
		workspaceID, workspaceID, projectID, assetID, version, workspaceID, s.clock.UnixMilli()).Scan(
		&selected.AssetID, &selected.Version, &selected.ContentHash, &selected.SizeBytes,
		&selected.MIMEType, &selected.StorageURI, &selected.CreatedAt, &permission)
	if err == sql.ErrNoRows {
		return LibraryVersion{}, ErrCrossWorkspace
	}
	if err != nil {
		return LibraryVersion{}, err
	}
	if permission == -1 {
		return LibraryVersion{}, ErrLibraryPublicationLimit
	}
	if permission == -2 {
		return LibraryVersion{}, ErrLibraryMetadataLimit
	}
	if permission != 1 {
		return LibraryVersion{}, ErrCrossWorkspace
	}
	return selected, nil
}
