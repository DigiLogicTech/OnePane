-- Prevent a revoke/reapprove cycle from multiplying TCP probes
-- against the same source within the goal's cadence window. A new permit ID
-- is not a new network-rate budget. This index also serializes competing
-- HTTP callers and survives process restarts.
CREATE UNIQUE INDEX idx_goal_tcp_attempt_source_cadence
 ON project_goal_tcp_probe_attempts(goal_id,source_id,cadence_window);
