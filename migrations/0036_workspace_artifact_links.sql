-- Explicit intra-Project exchange, not shared sandbox storage.
-- Revisions are immutable references to Project Library assets, never file mounts.
CREATE TABLE project_workspace_links (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  source_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE CASCADE,
  target_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'artifacts' CHECK(kind IN ('artifacts')),
  enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
  revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>=1),
  created_by TEXT NOT NULL REFERENCES principals(id),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  CHECK (source_workspace_id<>target_workspace_id),
  UNIQUE(project_id,source_workspace_id,target_workspace_id,kind)
) STRICT;
CREATE INDEX idx_project_workspace_links_project
 ON project_workspace_links(project_id,enabled,updated_at DESC);

CREATE TABLE project_workspace_publications (
  id TEXT PRIMARY KEY,
  link_id TEXT NOT NULL REFERENCES project_workspace_links(id) ON DELETE CASCADE,
  asset_id TEXT NOT NULL REFERENCES project_library_assets(id) ON DELETE RESTRICT,
  asset_version INTEGER NOT NULL,
  content_hash TEXT NOT NULL,
  published_by TEXT NOT NULL REFERENCES principals(id),
  published_at INTEGER NOT NULL,
  FOREIGN KEY(asset_id,asset_version)
    REFERENCES project_library_asset_versions(asset_id,version),
  UNIQUE(link_id,asset_id,asset_version)
) STRICT;
CREATE INDEX idx_project_workspace_publications_link
 ON project_workspace_publications(link_id,published_at DESC);
