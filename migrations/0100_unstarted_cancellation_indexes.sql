-- Bounded maintenance must probe evidence by Task, never scan full historical
-- grant/queue/publication tables for each candidate. Existing Task-leading
-- indexes cover the remaining evidence tables.
CREATE INDEX idx_tasks_cancel_scan ON tasks(state,id);
CREATE INDEX idx_capability_leases_task ON capability_leases(task_id);
CREATE INDEX idx_resource_leases_task ON resource_leases(task_id);
CREATE INDEX idx_budget_reservations_task ON budget_reservations(task_id);
CREATE INDEX idx_runtime_queue_task ON runtime_execution_queue(task_id);
CREATE INDEX idx_node_power_holds_task ON node_power_holds(task_id);
CREATE INDEX idx_node_wake_attempts_task ON node_wake_attempts(task_id);
CREATE INDEX idx_edit_proposals_task ON edit_proposals(task_id);
CREATE INDEX idx_project_change_proposals_task ON project_change_proposals(task_id);
CREATE INDEX idx_global_resource_leases_task ON global_resource_leases(json_extract(owner_json,'$.task_id'));
-- Avoid scanning unrelated ledger history or earlier cancellation revisions.
CREATE INDEX idx_events_task_execution_evidence
ON events(aggregate_type,aggregate_id,event_type,json_extract(payload_json,'$.revision'));
