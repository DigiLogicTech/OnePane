-- M27 budget enforcement hardening.
-- Reservations may bind to either a model inference request or an external
-- agent-runtime invocation. Only one active reservation may back each dispatch.
ALTER TABLE budget_reservations
ADD COLUMN agent_runtime_invocation_id TEXT REFERENCES agent_runtime_invocations(id);

CREATE UNIQUE INDEX IF NOT EXISTS ux_budget_reservations_active_inference
ON budget_reservations(inference_request_id)
WHERE inference_request_id IS NOT NULL AND status='reserved';

CREATE UNIQUE INDEX IF NOT EXISTS ux_budget_reservations_active_runtime
ON budget_reservations(agent_runtime_invocation_id)
WHERE agent_runtime_invocation_id IS NOT NULL AND status='reserved';
