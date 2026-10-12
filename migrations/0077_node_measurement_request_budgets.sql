-- Off-by-default paired-Node telemetry: bound *attempts* before JSON parsing.
-- One durable window per currently paired transport Node, across all grants.
CREATE TABLE node_measurement_request_budgets(
 peer_node_id TEXT PRIMARY KEY REFERENCES harness_nodes(id) ON DELETE RESTRICT,
 window_started_at INTEGER NOT NULL CHECK(window_started_at>0),
 attempts INTEGER NOT NULL CHECK(attempts BETWEEN 1 AND 60),
 updated_at INTEGER NOT NULL CHECK(updated_at>=window_started_at)
) STRICT;
CREATE INDEX idx_node_measurement_request_budget_updated
 ON node_measurement_request_budgets(updated_at);
