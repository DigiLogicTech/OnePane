-- RC11 L5: human-declared target labels for one exact, existing,
-- human-approved TCP probe permit. This DOES NOT prove physical asset
-- identity, Node attestation, or any external device's security.
CREATE TABLE project_goal_tcp_declared_target_bindings (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 permit_id TEXT NOT NULL REFERENCES project_goal_tcp_probe_permits(id) ON DELETE RESTRICT,
 permit_revision INTEGER NOT NULL CHECK(permit_revision>=1),
 plan_hash TEXT NOT NULL CHECK(length(plan_hash)=71 AND substr(plan_hash,1,7)='sha256:'),
 target_ipv4 TEXT NOT NULL,
 target_port INTEGER NOT NULL CHECK(target_port BETWEEN 1 AND 65535),
 display_label TEXT NOT NULL CHECK(length(display_label) BETWEEN 1 AND 64),
 identity_class TEXT NOT NULL DEFAULT 'operator_declared_tcp_endpoint'
  CHECK(identity_class='operator_declared_tcp_endpoint'),
 status TEXT NOT NULL CHECK(status IN ('approved','revoked')),
 revision INTEGER NOT NULL CHECK(revision>=1),
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>approved_at),
 revoked_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 revoked_at INTEGER NOT NULL DEFAULT 0 CHECK(revoked_at>=0),
 CHECK((status='approved' AND revoked_by IS NULL AND revoked_at=0)
  OR (status='revoked' AND revoked_by IS NOT NULL AND revoked_at>=approved_at))
) STRICT;
CREATE UNIQUE INDEX idx_goal_tcp_one_live_declared_target
 ON project_goal_tcp_declared_target_bindings(permit_id) WHERE status='approved';
CREATE INDEX idx_goal_tcp_declared_target_history
 ON project_goal_tcp_declared_target_bindings(project_id,goal_id,approved_at DESC);
CREATE TRIGGER goal_tcp_declared_target_no_delete
 BEFORE DELETE ON project_goal_tcp_declared_target_bindings
 BEGIN SELECT RAISE(ABORT,'declared target audit is immutable'); END;
CREATE TRIGGER goal_tcp_declared_target_immutable
 BEFORE UPDATE ON project_goal_tcp_declared_target_bindings
 WHEN NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id OR
  NEW.goal_id!=OLD.goal_id OR NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.source_id!=OLD.source_id OR NEW.permit_id!=OLD.permit_id OR
  NEW.permit_revision!=OLD.permit_revision OR NEW.plan_hash!=OLD.plan_hash OR
  NEW.target_ipv4!=OLD.target_ipv4 OR NEW.target_port!=OLD.target_port OR
  NEW.display_label!=OLD.display_label OR
  NEW.identity_class!=OLD.identity_class OR
  NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at OR
  NEW.expires_at!=OLD.expires_at OR NEW.revision!=OLD.revision+1 OR
  OLD.status='revoked' OR
  (NEW.status='approved' AND (NEW.revoked_by IS NOT NULL OR NEW.revoked_at!=0)) OR
  (NEW.status='revoked' AND (NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at))
 BEGIN SELECT RAISE(ABORT,'declared TCP target scope cannot be rewritten'); END;
