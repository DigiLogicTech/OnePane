-- Exact turn correlation for bounded, scoped Team compute observations.
CREATE INDEX idx_inference_team_live_turn ON inference_requests(
 workspace_id,task_id,principal_id,
 json_extract(context_manifest_json,'$.session_id'),
 json_extract(context_manifest_json,'$.turn_id'),
 json_extract(context_manifest_json,'$.member_id'),created_at DESC,id DESC);
CREATE INDEX idx_runtime_queue_inference ON runtime_execution_queue(inference_request_id);
