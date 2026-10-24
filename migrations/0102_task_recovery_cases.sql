-- Capturing an interrupted incarnation does not authorize retry or disposition.
CREATE TABLE task_recovery_cases (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    attempt_id TEXT NOT NULL UNIQUE REFERENCES task_attempts(id),
    interruption_event_id TEXT NOT NULL REFERENCES events(id),
    source_job_id TEXT NOT NULL,
    phase TEXT NOT NULL DEFAULT 'awaiting_inventory' CHECK (phase='awaiting_inventory'),
    task_revision_at_capture INTEGER NOT NULL CHECK (task_revision_at_capture>=1),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision>=1),
    created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_task_recovery_cases_workspace ON task_recovery_cases(workspace_id,created_at,id);
CREATE INDEX idx_task_recovery_cases_task ON task_recovery_cases(task_id,attempt_id);
CREATE INDEX idx_task_recovery_jobs ON outbox_jobs(job_type,status,id,available_at);

CREATE TRIGGER task_recovery_cases_identity_immutable
BEFORE UPDATE OF id,workspace_id,task_id,attempt_id,interruption_event_id,source_job_id,task_revision_at_capture,created_at ON task_recovery_cases
BEGIN
    SELECT RAISE(ABORT, 'recovery case identity is immutable');
END;

CREATE TRIGGER task_recovery_cases_no_delete
BEFORE DELETE ON task_recovery_cases
BEGIN
    SELECT RAISE(ABORT, 'recovery cases require durable disposition');
END;
