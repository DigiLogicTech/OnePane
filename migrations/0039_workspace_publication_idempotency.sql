-- Additive crash-safe idempotency ledger for autonomous Workspace file
-- publications. Never infer that a partially recorded external side effect
-- did not occur: ambiguous rows remain in_progress for explicit recovery.
-- Project/model/credential data is not touched.
CREATE TABLE workspace_file_publications (
 task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 runtime_id TEXT NOT NULL REFERENCES project_runtimes(id) ON DELETE RESTRICT,
 application_id TEXT NOT NULL REFERENCES project_applications(id) ON DELETE RESTRICT,
 relative_path TEXT NOT NULL,
 content_hash TEXT NOT NULL CHECK(length(content_hash)=64),
 status TEXT NOT NULL CHECK(status IN ('in_progress','complete')),
 artifact_id TEXT,
 library_asset_id TEXT,
 asset_version INTEGER,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(task_id,relative_path,content_hash),
 CHECK(
   (status='in_progress' AND library_asset_id IS NULL AND asset_version IS NULL)
   OR
   (status='complete' AND artifact_id IS NOT NULL AND library_asset_id IS NOT NULL AND asset_version>=1)
 )
) STRICT;
CREATE INDEX idx_workspace_file_publications_project
 ON workspace_file_publications(project_id,project_workspace_id,updated_at DESC);
