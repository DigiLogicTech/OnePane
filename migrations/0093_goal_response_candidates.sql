-- RC11: non-executable, human-authored Level-5 response intent.
-- No existing goal, review, or installation gains mutation authority.
CREATE TABLE project_goal_response_candidates (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 incident_review_id TEXT NOT NULL REFERENCES project_goal_incident_reviews(id) ON DELETE RESTRICT,
 tcp_alert_id TEXT NOT NULL REFERENCES project_goal_tcp_reachability_alerts(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 goal_plan_hash TEXT NOT NULL CHECK(length(goal_plan_hash)=71 AND substr(goal_plan_hash,1,7)='sha256:'),
 goal_plan_generation INTEGER NOT NULL CHECK(goal_plan_generation>0),
 incident_review_revision INTEGER NOT NULL CHECK(incident_review_revision>0),
 action_code TEXT NOT NULL CHECK(action_code='restart_local_observation_worker'),
 effect_class TEXT NOT NULL DEFAULT 'host_process_control' CHECK(effect_class='host_process_control'),
 risk_class TEXT NOT NULL DEFAULT 'requires_separate_approval' CHECK(risk_class='requires_separate_approval'),
 execution_authorized INTEGER NOT NULL DEFAULT 0 CHECK(execution_authorized=0),
 status TEXT NOT NULL CHECK(status IN ('proposed','withdrawn')),
 revision INTEGER NOT NULL CHECK(revision IN (1,2)),
 proposed_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 proposed_at INTEGER NOT NULL CHECK(proposed_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>proposed_at),
 withdrawn_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 withdrawn_at INTEGER NOT NULL DEFAULT 0 CHECK(withdrawn_at>=0),
 CHECK((status='proposed' AND revision=1 AND withdrawn_by IS NULL AND withdrawn_at=0)
  OR (status='withdrawn' AND revision=2 AND withdrawn_by IS NOT NULL AND withdrawn_at>=proposed_at))
) STRICT;
CREATE UNIQUE INDEX idx_goal_response_one_open_candidate
 ON project_goal_response_candidates(incident_review_id) WHERE status='proposed';
CREATE INDEX idx_goal_response_history
 ON project_goal_response_candidates(project_id,goal_id,proposed_at DESC);
CREATE TRIGGER goal_response_candidate_no_delete BEFORE DELETE ON project_goal_response_candidates
 BEGIN SELECT RAISE(ABORT,'response intent history is retained'); END;
CREATE TRIGGER goal_response_candidate_no_rewrite BEFORE UPDATE ON project_goal_response_candidates
 WHEN OLD.status!='proposed' OR NEW.status!='withdrawn' OR NEW.revision!=2
  OR NEW.withdrawn_by IS NULL OR NEW.withdrawn_at<OLD.proposed_at
  OR NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id
  OR NEW.goal_id!=OLD.goal_id OR NEW.project_workspace_id!=OLD.project_workspace_id
  OR NEW.incident_review_id!=OLD.incident_review_id OR NEW.tcp_alert_id!=OLD.tcp_alert_id
  OR NEW.source_id!=OLD.source_id OR NEW.goal_plan_hash!=OLD.goal_plan_hash
  OR NEW.goal_plan_generation!=OLD.goal_plan_generation
  OR NEW.incident_review_revision!=OLD.incident_review_revision
  OR NEW.action_code!=OLD.action_code OR NEW.effect_class!=OLD.effect_class
  OR NEW.risk_class!=OLD.risk_class OR NEW.execution_authorized!=0
  OR NEW.proposed_by!=OLD.proposed_by OR NEW.proposed_at!=OLD.proposed_at
  OR NEW.expires_at!=OLD.expires_at
 BEGIN SELECT RAISE(ABORT,'response intent scope cannot be rewritten'); END;
