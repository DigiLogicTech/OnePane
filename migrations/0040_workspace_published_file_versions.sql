-- Additive durable mapping: one named Project Workspace file path maps to
-- one stable Project Library asset. Each verified changed build appends an
-- immutable version; Library grants retain their existing pinned/latest policy.
-- No existing imported files, models, Project data or grants are rewritten.
CREATE TABLE workspace_published_file_assets (
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 relative_path TEXT NOT NULL,
 asset_id TEXT NOT NULL UNIQUE REFERENCES project_library_assets(id) ON DELETE RESTRICT,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(project_workspace_id,relative_path)
) STRICT;
CREATE INDEX idx_workspace_published_file_assets_asset
 ON workspace_published_file_assets(asset_id);
