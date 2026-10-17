-- Bound historical per-target/Source incident review lookups, even when
-- one long-lived Project accumulates many independently signed instrument
-- self-reports across multiple targets. No sensor, monitoring, mutation,
-- new permission or loss of the immutable signed claim history.
CREATE INDEX IF NOT EXISTS idx_goal_incident_measurement_lookup
ON project_goal_instrument_measurement_claims(
 project_id,goal_id,declared_target_id,source_id,accepted_at DESC);
