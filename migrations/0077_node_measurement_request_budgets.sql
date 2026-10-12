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
CREATE TRIGGER node_measurement_request_budget_no_delete
 BEFORE DELETE ON node_measurement_request_budgets
 BEGIN SELECT RAISE(ABORT,'Node attempt budget cannot be deleted'); END;
CREATE TRIGGER node_measurement_request_budget_monotonic
 BEFORE UPDATE ON node_measurement_request_budgets
 WHEN NEW.peer_node_id!=OLD.peer_node_id OR
  NEW.window_started_at<OLD.window_started_at OR
  (NEW.window_started_at=OLD.window_started_at AND
   NEW.attempts<=OLD.attempts)
 BEGIN SELECT RAISE(ABORT,'Node attempt budget cannot move backward'); END;
