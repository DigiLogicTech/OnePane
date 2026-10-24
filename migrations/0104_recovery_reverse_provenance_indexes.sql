-- Recovery attribution must follow persisted reverse provenance independently of
-- nullable or inconsistent direct Task/Attempt columns. These are lookup indexes,
-- not effect clearance, inventory completeness or authority to resume a Task.
-- Do not filter by tenant/status: a foreign or historical row is still evidence.
CREATE INDEX idx_runtime_recovery_request_task ON agent_runtime_invocations(json_extract(request_json,'$.task_id'));
CREATE INDEX idx_runtime_recovery_request_attempt ON agent_runtime_invocations(json_extract(request_json,'$.attempt_id'));

-- Creation of an artifact/version may commit before its caller saves a forward
-- link. Orphan reverse provenance must remain independently discoverable.
CREATE INDEX idx_artifacts_recovery_task ON artifacts(json_extract(metadata_json,'$.task_id'));
CREATE INDEX idx_artifacts_recovery_attempt ON artifacts(json_extract(metadata_json,'$.attempt_id'));
CREATE INDEX idx_artifacts_recovery_inference ON artifacts(json_extract(metadata_json,'$.inference_request_id'));
CREATE INDEX idx_artifacts_recovery_runtime ON artifacts(json_extract(metadata_json,'$.agent_runtime_invocation_id'));
CREATE INDEX idx_artifacts_recovery_agent_request ON artifacts(json_extract(metadata_json,'$.agent_request_id'));
CREATE INDEX idx_library_versions_recovery_task ON project_library_asset_versions(json_extract(provenance_json,'$.task_id'));
CREATE INDEX idx_library_versions_recovery_attempt ON project_library_asset_versions(json_extract(provenance_json,'$.attempt_id'));
CREATE INDEX idx_library_versions_recovery_artifact ON project_library_asset_versions(json_extract(provenance_json,'$.artifact_id'));

CREATE INDEX idx_assistant_turns_recovery_task ON assistant_turns(json_extract(provenance_json,'$.task_id'));
CREATE INDEX idx_orchestrator_turns_recovery_task ON project_orchestrator_turns(json_extract(provenance_json,'$.task_id'));
CREATE INDEX idx_notifications_recovery_source_id ON notification_deliveries(source_event_id);
