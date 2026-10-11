-- Additive performance index for authenticated per-Project Workspace Task
-- inventory. Limit is applied after tenancy/Project/Workspace filtering.
-- Only active (non-archived) Task rows need this index. Historic Task IDs,
-- completion JSON, child attempts, and state transitions are not altered.
CREATE INDEX IF NOT EXISTS idx_tasks_project_workspace_active_updated
 ON tasks(workspace_id,project_id,project_workspace_id,updated_at DESC,id DESC)
 WHERE archived_at IS NULL;
