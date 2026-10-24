-- Resolve exact immutable versions before following directional links. This
-- prevents unrelated Project publication history from governing read cost.
-- It does not bound fan-out of publications of the same exact version/hash.
CREATE INDEX idx_workspace_publications_asset_version_hash
ON project_workspace_publications(asset_id,asset_version,content_hash,link_id);
