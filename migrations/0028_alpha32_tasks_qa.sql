-- Alpha 3.2 Tasks QA: durable task scope and archive lifecycle.
ALTER TABLE tasks ADD COLUMN project_workspace_id TEXT;
ALTER TABLE tasks ADD COLUMN archived_at INTEGER;
CREATE INDEX IF NOT EXISTS idx_tasks_workspace_archive_updated
ON tasks(workspace_id, archived_at, updated_at DESC);
