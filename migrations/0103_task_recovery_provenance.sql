-- Historical interrupted Attempts can outlive their outbox jobs. Keep the
-- actual intake provenance rather than inventing an outbox identifier.
DROP TRIGGER task_recovery_cases_identity_immutable;
DROP TRIGGER task_recovery_cases_no_delete;
DROP INDEX idx_task_recovery_cases_workspace;
DROP INDEX idx_task_recovery_cases_task;
ALTER TABLE task_recovery_cases RENAME TO task_recovery_cases_v102;
CREATE TABLE task_recovery_cases (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    attempt_id TEXT NOT NULL UNIQUE REFERENCES task_attempts(id),
    interruption_event_id TEXT NOT NULL REFERENCES events(id),
    source_job_id TEXT,
    source_kind TEXT NOT NULL DEFAULT 'outbox' CHECK (source_kind IN ('outbox','interruption_ledger')),
    phase TEXT NOT NULL DEFAULT 'awaiting_inventory' CHECK (phase='awaiting_inventory'),
    task_revision_at_capture INTEGER NOT NULL CHECK (task_revision_at_capture>=1),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision>=1),
    created_at INTEGER NOT NULL,
    CHECK ((source_kind='outbox' AND source_job_id IS NOT NULL AND length(source_job_id)>0)
        OR (source_kind='interruption_ledger' AND source_job_id IS NULL))
) STRICT;
INSERT INTO task_recovery_cases(id,workspace_id,task_id,attempt_id,interruption_event_id,source_job_id,phase,task_revision_at_capture,revision,created_at)
SELECT id,workspace_id,task_id,attempt_id,interruption_event_id,source_job_id,phase,task_revision_at_capture,revision,created_at FROM task_recovery_cases_v102;
DROP TABLE task_recovery_cases_v102;
CREATE INDEX idx_task_recovery_cases_workspace ON task_recovery_cases(workspace_id,created_at,id);
CREATE INDEX idx_task_recovery_cases_task ON task_recovery_cases(task_id,attempt_id);
CREATE INDEX idx_task_recovery_interrupted_attempts ON task_attempts(status,id,task_id);
CREATE TRIGGER task_recovery_cases_identity_immutable
BEFORE UPDATE OF id,workspace_id,task_id,attempt_id,interruption_event_id,source_job_id,source_kind,task_revision_at_capture,created_at ON task_recovery_cases
BEGIN
    SELECT RAISE(ABORT, 'recovery case identity is immutable');
END;
CREATE TRIGGER task_recovery_cases_no_delete
BEFORE DELETE ON task_recovery_cases
BEGIN
    SELECT RAISE(ABORT, 'recovery cases require durable disposition');
END;
