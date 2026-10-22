-- RC11 Level 5: immutable, per-cadence review of already-attached
-- observation receipt METADATA, NOT an external probe or health attestation.
-- No scheduler is activated by this additive migration.
CREATE TABLE project_goal_evidence_review_runs (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 permit_id TEXT NOT NULL REFERENCES project_goal_evidence_review_permits(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 cadence_window INTEGER NOT NULL CHECK(cadence_window>=0),
 evidence_state TEXT NOT NULL CHECK(evidence_state IN
  ('missing','stale_attached','recent_attached')),
 last_observed_at INTEGER NOT NULL DEFAULT 0 CHECK(last_observed_at>=0),
 goal_revision INTEGER NOT NULL CHECK(goal_revision>0),
 permit_revision INTEGER NOT NULL CHECK(permit_revision>0),
 reviewed_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 reviewed_at INTEGER NOT NULL CHECK(reviewed_at>0),
 UNIQUE(permit_id,cadence_window)
) STRICT;
CREATE INDEX idx_goal_review_run_history
 ON project_goal_evidence_review_runs(project_id,goal_id,reviewed_at DESC);
CREATE TRIGGER goal_review_run_no_update
 BEFORE UPDATE ON project_goal_evidence_review_runs
 BEGIN SELECT RAISE(ABORT,'goal review evidence runs are immutable'); END;
CREATE TRIGGER goal_review_run_no_delete
 BEFORE DELETE ON project_goal_evidence_review_runs
 BEGIN SELECT RAISE(ABORT,'goal review evidence runs are immutable'); END;
