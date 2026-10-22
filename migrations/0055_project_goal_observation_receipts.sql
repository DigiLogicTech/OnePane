-- RC11 OPM-04: human-attested links to existing immutable observations.
-- No observation payloads are copied, and no monitoring is activated.
CREATE TABLE project_goal_observation_receipts (
 id TEXT PRIMARY KEY,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 observation_id TEXT NOT NULL REFERENCES observations(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 integrity_hash TEXT NOT NULL CHECK(length(integrity_hash)=71 AND substr(integrity_hash,1,7)='sha256:'),
 observed_at INTEGER NOT NULL CHECK(observed_at>0),
 attached_at INTEGER NOT NULL CHECK(attached_at>0),
 attached_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 UNIQUE(goal_id,observation_id)
) STRICT;
CREATE INDEX idx_project_goal_observation_receipts
 ON project_goal_observation_receipts(goal_id,attached_at DESC);
CREATE TRIGGER project_goal_observation_receipts_no_update
 BEFORE UPDATE ON project_goal_observation_receipts
 BEGIN SELECT RAISE(ABORT,'goal observation receipts are immutable'); END;
CREATE TRIGGER project_goal_observation_receipts_no_delete
 BEFORE DELETE ON project_goal_observation_receipts
 BEGIN SELECT RAISE(ABORT,'goal observation receipts are immutable'); END;
