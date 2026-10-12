-- RC11 manual TCP reachability. One bounded, audited TCP handshake check per
-- approved source/cadence window; the claimed record survives crashes.
-- No migration grants authority or activates background TCP scheduling.
CREATE TABLE project_goal_tcp_probe_attempts (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 permit_id TEXT NOT NULL REFERENCES project_goal_tcp_probe_permits(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 target_ipv4 TEXT NOT NULL,
 target_port INTEGER NOT NULL CHECK(target_port BETWEEN 1 AND 65535),
 timeout_ms INTEGER NOT NULL CHECK(timeout_ms BETWEEN 100 AND 1500),
 cadence_window INTEGER NOT NULL CHECK(cadence_window>=0),
 goal_revision INTEGER NOT NULL CHECK(goal_revision>0),
 permit_revision INTEGER NOT NULL CHECK(permit_revision>0),
 requested_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 status TEXT NOT NULL CHECK(status IN
  ('claimed','connected','not_connected','timed_out','cancelled')),
 claimed_at INTEGER NOT NULL CHECK(claimed_at>0),
 completed_at INTEGER NOT NULL DEFAULT 0 CHECK(completed_at>=0),
 CHECK((status='claimed' AND completed_at=0)
  OR (status!='claimed' AND completed_at>=claimed_at)),
 UNIQUE(permit_id,cadence_window)
) STRICT;
CREATE INDEX idx_goal_tcp_attempt_history
 ON project_goal_tcp_probe_attempts(project_id,goal_id,claimed_at DESC);
CREATE TRIGGER goal_tcp_probe_attempts_no_delete
 BEFORE DELETE ON project_goal_tcp_probe_attempts
 BEGIN SELECT RAISE(ABORT,'TCP probe attempt audit cannot be deleted'); END;
CREATE TRIGGER goal_tcp_probe_attempts_no_rewrite
 BEFORE UPDATE ON project_goal_tcp_probe_attempts
 WHEN OLD.status!='claimed' OR NEW.id!=OLD.id OR
  NEW.project_id!=OLD.project_id OR NEW.goal_id!=OLD.goal_id OR
  NEW.permit_id!=OLD.permit_id OR NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.source_id!=OLD.source_id OR NEW.target_ipv4!=OLD.target_ipv4 OR
  NEW.target_port!=OLD.target_port OR NEW.timeout_ms!=OLD.timeout_ms OR
  NEW.cadence_window!=OLD.cadence_window OR
  NEW.goal_revision!=OLD.goal_revision OR
  NEW.permit_revision!=OLD.permit_revision OR
  NEW.requested_by!=OLD.requested_by OR NEW.claimed_at!=OLD.claimed_at
 BEGIN SELECT RAISE(ABORT,'TCP probe provenance is immutable'); END;
