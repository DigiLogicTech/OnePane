-- RC11 P0-AF. Monotonic Goal plan generations prevent historical
-- operator Node execution grants reviving after plan edits and rollback.
-- Existing exports made before this migration deliberately fail closed.
ALTER TABLE project_goal_node_probe_execution_permits
 ADD COLUMN plan_sha256 TEXT NOT NULL DEFAULT '' CHECK(plan_sha256='' OR
   (length(plan_sha256)=71 AND substr(plan_sha256,1,7)='sha256:'));
ALTER TABLE project_goal_node_probe_execution_permits
 ADD COLUMN plan_generation INTEGER NOT NULL DEFAULT 0 CHECK(plan_generation>=0);

DROP TRIGGER goal_node_probe_permit_scope_immutable;
CREATE TRIGGER goal_node_probe_permit_scope_immutable
 BEFORE UPDATE ON project_goal_node_probe_execution_permits
 WHEN NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id OR
  NEW.goal_id!=OLD.goal_id OR NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.instrument_key_id!=OLD.instrument_key_id OR NEW.challenge_id!=OLD.challenge_id OR
  NEW.source_id!=OLD.source_id OR NEW.declared_target_id!=OLD.declared_target_id OR
  NEW.tls_pin_id!=OLD.tls_pin_id OR NEW.peer_node_id!=OLD.peer_node_id OR
  NEW.peer_tls_fingerprint!=OLD.peer_tls_fingerprint OR
  NEW.original_challenge_sha256!=OLD.original_challenge_sha256 OR
  NEW.instrument_public_key_sha256!=OLD.instrument_public_key_sha256 OR
  NEW.plan_sha256!=OLD.plan_sha256 OR
  NEW.plan_generation!=OLD.plan_generation OR
  NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at OR
  NEW.expires_at!=OLD.expires_at OR NEW.revision!=OLD.revision+1 OR
  NEW.status!='revoked' OR OLD.status='revoked' OR
  NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at
 BEGIN SELECT RAISE(ABORT,'Node one-shot permit scope immutable'); END;
