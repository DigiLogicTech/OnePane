-- Cancellation evidence follows either side of each durable association.
-- Do not let corrupt/missing Task ownership hide live request/accounting rows.
CREATE INDEX idx_inference_cancel_session ON inference_requests(json_extract(context_manifest_json,'$.session_id'));
CREATE INDEX idx_inference_cancel_turn ON inference_requests(json_extract(context_manifest_json,'$.turn_id'));
CREATE INDEX idx_budget_reservations_inference ON budget_reservations(inference_request_id);
CREATE INDEX idx_agent_runtime_cancel_session ON agent_runtime_invocations(json_extract(request_json,'$.context_manifest.session_id'));
CREATE INDEX idx_agent_runtime_cancel_turn ON agent_runtime_invocations(json_extract(request_json,'$.context_manifest.turn_id'));
