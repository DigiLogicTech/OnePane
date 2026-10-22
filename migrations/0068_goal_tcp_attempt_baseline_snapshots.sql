-- RC11: an operator changing expected reachability must NOT retroactively
-- reinterpret a historical TCP handshake as evidence for the new baseline.
-- Capture the exact baseline ID and revision at TCP attempt admission.
-- Legacy attempts remain revision 0 / unknown; do not backfill by guessing.
ALTER TABLE project_goal_tcp_probe_attempts
 ADD COLUMN baseline_id_at_claim TEXT NOT NULL DEFAULT '';
ALTER TABLE project_goal_tcp_probe_attempts
 ADD COLUMN baseline_revision_at_claim INTEGER NOT NULL DEFAULT 0
 CHECK(baseline_revision_at_claim>=0);
CREATE TRIGGER goal_tcp_attempts_baseline_snapshot_immutable
 BEFORE UPDATE ON project_goal_tcp_probe_attempts
 WHEN NEW.baseline_id_at_claim!=OLD.baseline_id_at_claim OR
 NEW.baseline_revision_at_claim!=OLD.baseline_revision_at_claim
 BEGIN SELECT RAISE(ABORT,'TCP baseline admission snapshot is immutable'); END;
CREATE INDEX idx_goal_tcp_attempt_baseline_snapshot
 ON project_goal_tcp_probe_attempts(goal_id,source_id,baseline_id_at_claim);
