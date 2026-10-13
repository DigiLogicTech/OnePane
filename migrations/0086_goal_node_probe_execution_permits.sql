-- A separate human-approved one-shot probe authority. A telemetry grant
-- is never execution consent. An original challenge may have at most one
-- authorisation in its lifetime, including after revocation.
CREATE TABLE project_goal_node_probe_execution_permits (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 instrument_key_id TEXT NOT NULL REFERENCES project_goal_instrument_keys(id) ON DELETE RESTRICT,
 challenge_id TEXT NOT NULL UNIQUE REFERENCES project_goal_instrument_challenges(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL,
 declared_target_id TEXT NOT NULL,
 tls_pin_id TEXT NOT NULL,
 peer_node_id TEXT NOT NULL REFERENCES harness_nodes(id) ON DELETE RESTRICT,
 peer_tls_fingerprint TEXT NOT NULL CHECK(length(peer_tls_fingerprint)=71 AND substr(peer_tls_fingerprint,1,7)='sha256:'),
 original_challenge_sha256 TEXT NOT NULL CHECK(length(original_challenge_sha256)=71 AND substr(original_challenge_sha256,1,7)='sha256:'),
 instrument_public_key_sha256 TEXT NOT NULL CHECK(length(instrument_public_key_sha256)=71 AND substr(instrument_public_key_sha256,1,7)='sha256:'),
 status TEXT NOT NULL CHECK(status IN ('approved','revoked')),
 revision INTEGER NOT NULL CHECK(revision>=1),
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>approved_at),
 revoked_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 revoked_at INTEGER NOT NULL DEFAULT 0 CHECK(revoked_at>=0),
 CHECK((status='approved' AND revoked_by IS NULL AND revoked_at=0)
   OR(status='revoked' AND revoked_by IS NOT NULL AND revoked_at>=approved_at))
) STRICT;
CREATE INDEX idx_goal_node_probe_permit_inventory
 ON project_goal_node_probe_execution_permits(project_id,goal_id,approved_at DESC);
CREATE TRIGGER goal_node_probe_permit_no_delete
 BEFORE DELETE ON project_goal_node_probe_execution_permits
 BEGIN SELECT RAISE(ABORT,'Node probe authority history immutable'); END;
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
  NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at OR
  NEW.expires_at!=OLD.expires_at OR NEW.revision!=OLD.revision+1 OR
  NEW.status!='revoked' OR OLD.status='revoked' OR
  NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at
 BEGIN SELECT RAISE(ABORT,'Node one-shot permit scope immutable'); END;
