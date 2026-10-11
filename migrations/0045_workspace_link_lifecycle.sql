-- RC11: directional Workspace artifact links may expire without deleting
-- immutable versions or their audit history. Existing links remain perpetual.
ALTER TABLE project_workspace_links ADD COLUMN expires_at_ms INTEGER
 CHECK(expires_at_ms IS NULL OR expires_at_ms>0);
CREATE INDEX idx_project_workspace_links_expiry
 ON project_workspace_links(project_id,enabled,expires_at_ms);
