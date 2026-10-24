-- Complete bounded direct Task/Attempt lookup coverage reviewed for recovery.
-- Task and Attempt paths must be queried independently: corrupt or nullable
-- Task ownership must not hide a row referencing the original Attempt.
-- Keep all statuses/tenants, including closed history and released holds.
CREATE INDEX idx_runtime_recovery_attempt ON agent_runtime_invocations(attempt_id);
CREATE INDEX idx_operations_recovery_attempt ON operations(attempt_id);
CREATE INDEX idx_tools_recovery_attempt ON tool_invocations(attempt_id);
CREATE INDEX idx_snapshots_recovery_attempt ON recovery_snapshots(attempt_id);
CREATE INDEX idx_assistant_recovery_task ON assistant_turns(task_id);
CREATE INDEX idx_handoffs_recovery_task ON assistant_project_handoffs(task_id);
CREATE INDEX idx_orchestrator_recovery_task ON project_orchestrator_turns(task_id);
CREATE INDEX idx_deliberations_recovery_task ON deliberations(task_id);
CREATE INDEX idx_incidents_recovery_task ON incidents(task_id);
CREATE INDEX idx_plan_proposals_recovery_task ON plan_proposals(task_id);
CREATE INDEX idx_routine_occurrences_recovery_task ON routine_occurrences(task_id);
CREATE INDEX idx_dependencies_recovery_inverse ON task_dependencies(depends_on_task_id);
CREATE INDEX idx_assignments_recovery_parent ON team_execution_assignments(parent_task_id);
CREATE INDEX idx_holds_recovery_root ON team_execution_holds(root_task_id);
CREATE INDEX idx_held_tasks_recovery_task ON team_execution_held_tasks(task_id);
