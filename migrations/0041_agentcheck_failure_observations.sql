-- RC11: append-only, typed Agent Check failure-stage evidence.
-- No raw HTTP errors, user prompts, notes, model responses or provider details.
-- Additive migration: existing sessions are not relabelled or inferred failed.
CREATE TABLE model_agentcheck_failure_observations (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id       TEXT NOT NULL REFERENCES model_testbed_sessions(id),
    deployment_id    TEXT NOT NULL REFERENCES model_deployments(id),
    stage            TEXT NOT NULL CHECK (stage IN (
        'runtime_acquire','deployment_read','model_read','inference_dispatch',
        'response_validation','turn_store','completion_validation',
        'completion_persist','runtime_release','runtime_unload','session_abort'
    )),
    category         TEXT NOT NULL CHECK (category IN (
        'cancelled','deadline_exceeded','not_found','invalid_response',
        'storage_failure','abort_requested','execution_failure','unknown'
    )),
    observed_at      INTEGER NOT NULL CHECK (observed_at > 0)
) STRICT;
CREATE INDEX idx_agentcheck_failure_session
    ON model_agentcheck_failure_observations(session_id, observed_at DESC, id DESC);
CREATE INDEX idx_agentcheck_failure_deployment
    ON model_agentcheck_failure_observations(deployment_id, observed_at DESC);
CREATE TRIGGER model_agentcheck_failure_no_update
BEFORE UPDATE ON model_agentcheck_failure_observations
BEGIN
    SELECT RAISE(ABORT, 'Agent Check failure observations are immutable');
END;
CREATE TRIGGER model_agentcheck_failure_no_delete
BEFORE DELETE ON model_agentcheck_failure_observations
BEGIN
    SELECT RAISE(ABORT, 'Agent Check failure observations are immutable');
END;
