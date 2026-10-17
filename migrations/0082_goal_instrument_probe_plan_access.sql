-- Metadata-only human export audit for a currently approved exact probe plan.
-- No private IPv4/port, raw certificate, signing key or probe outcome
-- are persisted in this additional ledger.
CREATE TABLE project_goal_instrument_probe_plan_exports(
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 instrument_key_id TEXT NOT NULL REFERENCES project_goal_instrument_keys(id) ON DELETE RESTRICT,
 challenge_id TEXT NOT NULL REFERENCES project_goal_instrument_challenges(id) ON DELETE RESTRICT,
 actor_principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 challenge_sha256 TEXT NOT NULL CHECK(length(challenge_sha256)=71 AND substr(challenge_sha256,1,7)='sha256:'),
 first_exported_at INTEGER NOT NULL CHECK(first_exported_at>0),
 UNIQUE(challenge_id,actor_principal_id)
) STRICT;
CREATE INDEX idx_goal_instrument_plan_export_audit ON project_goal_instrument_probe_plan_exports
 (project_id,goal_id,first_exported_at DESC);
CREATE TRIGGER goal_instrument_probe_plan_no_delete
 BEFORE DELETE ON project_goal_instrument_probe_plan_exports
 BEGIN SELECT RAISE(ABORT,'private endpoint plan export audit is immutable'); END;
CREATE TRIGGER goal_instrument_probe_plan_no_update
 BEFORE UPDATE ON project_goal_instrument_probe_plan_exports
 BEGIN SELECT RAISE(ABORT,'private endpoint plan export audit cannot be rewritten'); END;
