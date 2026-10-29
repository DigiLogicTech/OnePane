-- Bounded Project-local keyset paging must not sort all assets or walk global
-- text-index history. This adds no read, execution or recovery authority.
CREATE INDEX idx_library_assets_project_identity
 ON project_library_assets(project_id,archived,id);
