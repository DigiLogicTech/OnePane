-- RC11 provenance: bind each unattended TCP attempt to the exact human
-- recurring consent that authorized it. A manual TCP test has NO recurring
-- grant. This metadata cannot be used to retroactively widen authority.
ALTER TABLE project_goal_tcp_probe_attempts
 ADD COLUMN recurring_grant_id TEXT REFERENCES project_goal_tcp_recurring_grants(id)
 ON DELETE RESTRICT;
CREATE INDEX idx_goal_tcp_attempt_recurring_provenance
 ON project_goal_tcp_probe_attempts(recurring_grant_id,claimed_at DESC);
CREATE TRIGGER goal_tcp_attempts_authority_source
 BEFORE INSERT ON project_goal_tcp_probe_attempts
 WHEN (NEW.requested_by='system:tcp-recurring-observer' AND NEW.recurring_grant_id IS NULL)
  OR (NEW.requested_by!='system:tcp-recurring-observer' AND NEW.recurring_grant_id IS NOT NULL)
 BEGIN SELECT RAISE(ABORT,'TCP attempt lacks exact recurring consent provenance'); END;
-- Preserve original immutable receipt columns and also forbid rebinding
-- the recurring consent after a claim has been committed.
DROP TRIGGER goal_tcp_probe_attempts_no_rewrite;
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
  NEW.requested_by!=OLD.requested_by OR NEW.claimed_at!=OLD.claimed_at OR
  NEW.recurring_grant_id IS NOT OLD.recurring_grant_id
 BEGIN SELECT RAISE(ABORT,'TCP probe provenance is immutable'); END;
