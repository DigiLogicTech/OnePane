-- RC11 Level 5: an immutable, evidence-backed TCP expectation drift
-- notice, not a diagnosis of compromise or a remediation authorisation.
CREATE TABLE project_goal_tcp_reachability_alerts (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 baseline_id TEXT NOT NULL REFERENCES project_goal_tcp_baseline_revisions(id) ON DELETE RESTRICT,
 baseline_revision INTEGER NOT NULL CHECK(baseline_revision>0),
 attempt_id TEXT NOT NULL REFERENCES project_goal_tcp_probe_attempts(id) ON DELETE RESTRICT,
 expected_tcp TEXT NOT NULL CHECK(expected_tcp IN ('connected','not_connected')),
 observed_tcp TEXT NOT NULL CHECK(observed_tcp IN ('connected','not_connected')),
 classification TEXT NOT NULL CHECK(classification IN
  ('unexpected_reachable','unexpected_unreachable')),
 status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','acknowledged')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 detected_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 detected_at INTEGER NOT NULL CHECK(detected_at>0),
 acknowledged_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 acknowledged_at INTEGER NOT NULL DEFAULT 0 CHECK(acknowledged_at>=0),
 UNIQUE(attempt_id,baseline_id),
 CHECK(expected_tcp!=observed_tcp),
 CHECK((status='open' AND acknowledged_by IS NULL AND acknowledged_at=0)
  OR (status='acknowledged' AND acknowledged_by IS NOT NULL AND acknowledged_at>=detected_at))
) STRICT;
CREATE INDEX idx_goal_tcp_alert_history
 ON project_goal_tcp_reachability_alerts(project_id,goal_id,detected_at DESC);
CREATE TRIGGER goal_tcp_reachability_alerts_no_delete
 BEFORE DELETE ON project_goal_tcp_reachability_alerts
 BEGIN SELECT RAISE(ABORT,'TCP alert history is retained for audit'); END;
CREATE TRIGGER goal_tcp_reachability_alerts_no_rewrite
 BEFORE UPDATE ON project_goal_tcp_reachability_alerts
 WHEN OLD.status!='open' OR NEW.status!='acknowledged' OR
  NEW.revision!=OLD.revision+1 OR NEW.acknowledged_by IS NULL OR
  NEW.acknowledged_at<OLD.detected_at OR
  NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id OR
  NEW.goal_id!=OLD.goal_id OR NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.source_id!=OLD.source_id OR NEW.baseline_id!=OLD.baseline_id OR
  NEW.baseline_revision!=OLD.baseline_revision OR NEW.attempt_id!=OLD.attempt_id OR
  NEW.expected_tcp!=OLD.expected_tcp OR NEW.observed_tcp!=OLD.observed_tcp OR
  NEW.classification!=OLD.classification OR NEW.detected_by!=OLD.detected_by OR
  NEW.detected_at!=OLD.detected_at
 BEGIN SELECT RAISE(ABORT,'TCP alert provenance is immutable'); END;
