-- A bounded historical reachability-return lookup joins the immutable
-- original attempt and checks the exact approved baseline revision.
-- This index creates no probes, approvals or monitoring activation.
CREATE INDEX IF NOT EXISTS idx_goal_tcp_return_evidence
ON project_goal_tcp_probe_attempts(
 project_id,goal_id,source_id,baseline_id_at_claim,claimed_at);
