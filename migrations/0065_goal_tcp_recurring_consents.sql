-- RC11: explicit, separate recurring NETWORK consent. Existing manual
-- reachability permits alone NEVER authorise an unattended socket.
-- This only grants a short-lived, exact-target capability; it does not
-- activate the goal's verified security health or permit remediation.
CREATE TABLE project_goal_tcp_recurring_consents (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 permit_id TEXT NOT NULL REFERENCES project_goal_tcp_probe_permits(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 plan_hash TEXT NOT NULL CHECK(length(plan_hash)=71 AND substr(plan_hash,1,7)='sha256:'),
 target_ipv4 TEXT NOT NULL CHECK(length(target_ipv4) BETWEEN 7 AND 15),
 target_port INTEGER NOT NULL CHECK(target_port BETWEEN 1 AND 65535),
 timeout_ms INTEGER NOT NULL CHECK(timeout_ms BETWEEN 100 AND 1500),
 cadence_minutes INTEGER NOT NULL CHECK(cadence_minutes BETWEEN 15 AND 10080),
 effect_class TEXT NOT NULL DEFAULT 'network_tcp_connect_recurring'
   CHECK(effect_class='network_tcp_connect_recurring'),
 status TEXT NOT NULL DEFAULT 'approved' CHECK(status IN ('approved','revoked')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 revoked_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>approved_at),
 revoked_at INTEGER NOT NULL DEFAULT 0 CHECK(revoked_at>=0),
 CHECK((status='approved' AND revoked_by IS NULL AND revoked_at=0)
   OR (status='revoked' AND revoked_by IS NOT NULL AND revoked_at>=approved_at))
) STRICT;
CREATE UNIQUE INDEX idx_goal_tcp_recurring_source_active
 ON project_goal_tcp_recurring_consents(goal_id,source_id)
 WHERE status='approved';
CREATE INDEX idx_goal_tcp_recurring_due
 ON project_goal_tcp_recurring_consents(status,expires_at,id);
CREATE TRIGGER goal_tcp_recurring_consents_no_delete
 BEFORE DELETE ON project_goal_tcp_recurring_consents
 BEGIN SELECT RAISE(ABORT,'recurring network consent is retained'); END;
CREATE TRIGGER goal_tcp_recurring_consents_scope_immutable
 BEFORE UPDATE ON project_goal_tcp_recurring_consents
 WHEN NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id
 OR NEW.goal_id!=OLD.goal_id
 OR NEW.project_workspace_id!=OLD.project_workspace_id
 OR NEW.permit_id!=OLD.permit_id
 OR NEW.source_id!=OLD.source_id OR NEW.plan_hash!=OLD.plan_hash
 OR NEW.target_ipv4!=OLD.target_ipv4 OR NEW.target_port!=OLD.target_port
 OR NEW.timeout_ms!=OLD.timeout_ms OR NEW.cadence_minutes!=OLD.cadence_minutes
 OR NEW.effect_class!=OLD.effect_class
 OR NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at
 OR NEW.expires_at!=OLD.expires_at
 OR OLD.status!='approved' OR NEW.status!='revoked'
 OR NEW.revision!=OLD.revision+1
 OR NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at
 BEGIN SELECT RAISE(ABORT,'recurring TCP consent cannot be broadened'); END;
