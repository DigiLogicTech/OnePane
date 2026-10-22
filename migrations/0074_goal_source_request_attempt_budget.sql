-- L5: bounded request attempts, including rejected signatures/JSON.
-- One durable credential-wide window is shared by staging + proof admission.
-- The protocol remains off-by-default until a human mints a source token.
CREATE TABLE project_goal_source_request_budgets (
 credential_id TEXT PRIMARY KEY REFERENCES api_credentials(id) ON DELETE RESTRICT,
 window_started_at INTEGER NOT NULL CHECK(window_started_at>0),
 attempts INTEGER NOT NULL CHECK(attempts BETWEEN 1 AND 60),
 updated_at INTEGER NOT NULL CHECK(updated_at>=window_started_at)
) STRICT;
CREATE INDEX idx_goal_source_request_budgets_updated
 ON project_goal_source_request_budgets(updated_at);
