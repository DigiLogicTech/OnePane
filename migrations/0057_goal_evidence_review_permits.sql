-- RC11 Level 5: explicit, time-limited grants to review ALREADY ATTESTED
-- observation receipt metadata. This never permits a probe, network request,
-- raw sensor-data read, remediation, host effect, or LLM dispatch.
-- No existing goal is activated or granted a permit by this migration.
CREATE TABLE project_goal_evidence_review_permits (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 plan_hash TEXT NOT NULL CHECK(length(plan_hash)=71 AND substr(plan_hash,1,7)='sha256:'),
 effect_class TEXT NOT NULL DEFAULT 'existing_receipt_metadata'
  CHECK(effect_class='existing_receipt_metadata'),
 status TEXT NOT NULL DEFAULT 'approved'
  CHECK(status IN ('approved','revoked')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 revoked_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>approved_at),
 revoked_at INTEGER NOT NULL DEFAULT 0 CHECK(revoked_at>=0),
 CHECK((status='approved' AND revoked_at=0 AND revoked_by IS NULL)
    OR (status='revoked' AND revoked_at>=approved_at AND revoked_by IS NOT NULL))
) STRICT;
CREATE UNIQUE INDEX idx_goal_evidence_review_active_source
 ON project_goal_evidence_review_permits(goal_id,source_id)
 WHERE status='approved';
CREATE INDEX idx_goal_evidence_review_scope
 ON project_goal_evidence_review_permits(project_id,goal_id,approved_at DESC);
CREATE TRIGGER goal_evidence_review_no_delete
 BEFORE DELETE ON project_goal_evidence_review_permits
 BEGIN SELECT RAISE(ABORT,'evidence review permits are retained for audit'); END;
