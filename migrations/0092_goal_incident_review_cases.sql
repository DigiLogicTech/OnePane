-- RC11: operator-reviewed case for a known TCP reachability drift notice.
-- A case is not a confirmed security incident, device attestation, or
-- authorisation to take any network/host action.
CREATE TABLE project_goal_incident_reviews (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 tcp_alert_id TEXT NOT NULL UNIQUE REFERENCES project_goal_tcp_reachability_alerts(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 status TEXT NOT NULL DEFAULT 'pending_review'
  CHECK(status IN ('pending_review','reviewed')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 decision TEXT NOT NULL DEFAULT 'unassessed' CHECK(decision IN
  ('unassessed','needs_investigation','insufficient_evidence','referred_out_of_band')),
 opened_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 opened_at INTEGER NOT NULL CHECK(opened_at>0),
 reviewed_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 reviewed_at INTEGER NOT NULL DEFAULT 0 CHECK(reviewed_at>=0),
 CHECK((status='pending_review' AND decision='unassessed'
  AND reviewed_by IS NULL AND reviewed_at=0)
  OR (status='reviewed' AND decision!='unassessed'
  AND reviewed_by IS NOT NULL AND reviewed_at>=opened_at))
) STRICT;
CREATE INDEX idx_goal_incident_review_history
 ON project_goal_incident_reviews(project_id,goal_id,opened_at DESC);
CREATE TRIGGER goal_incident_reviews_no_delete
 BEFORE DELETE ON project_goal_incident_reviews
 BEGIN SELECT RAISE(ABORT,'incident review history is retained for audit'); END;
CREATE TRIGGER goal_incident_reviews_no_rewrite
 BEFORE UPDATE ON project_goal_incident_reviews
 WHEN OLD.status!='pending_review' OR NEW.status!='reviewed'
  OR NEW.revision!=OLD.revision+1 OR NEW.reviewed_by IS NULL
  OR NEW.reviewed_at<OLD.opened_at OR NEW.decision='unassessed'
  OR NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id
  OR NEW.goal_id!=OLD.goal_id
  OR NEW.project_workspace_id!=OLD.project_workspace_id
  OR NEW.tcp_alert_id!=OLD.tcp_alert_id
  OR NEW.source_id!=OLD.source_id
  OR NEW.opened_by!=OLD.opened_by OR NEW.opened_at!=OLD.opened_at
 BEGIN SELECT RAISE(ABORT,'incident review provenance is immutable'); END;
