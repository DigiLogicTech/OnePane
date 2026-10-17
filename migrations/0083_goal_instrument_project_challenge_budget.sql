-- Bound total short-lived instrument challenge issuance across ALL
-- enrolled/rotated keys for one Project. Per-key budgets alone can be
-- evaded by repeated human key rotation and exhaust immutable SQLite
-- challenge/audit storage on low-resource local-first installations.
CREATE INDEX IF NOT EXISTS idx_goal_instrument_challenge_project_issued
 ON project_goal_instrument_challenges(project_id,issued_at DESC);
