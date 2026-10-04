-- M11 durable invocation records for external agent harnesses/runtimes.
-- These are deliberately distinct from inference_requests: an external harness
-- may orchestrate one or many models and is not itself a ModelDeployment.

CREATE TABLE agent_runtime_invocations (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    task_id                 TEXT REFERENCES tasks(id),
    attempt_id              TEXT REFERENCES task_attempts(id),
    principal_id            TEXT NOT NULL REFERENCES principals(id),
    connection_id           TEXT NOT NULL REFERENCES agent_runtime_connections(id),
    status                  TEXT NOT NULL CHECK (status IN (
        'created','authorized','dispatched','executing','succeeded','failed','unknown','cancelled'
    )),
    request_json            TEXT NOT NULL CHECK (json_valid(request_json)),
    response_artifact_id    TEXT REFERENCES artifacts(id),
    error_code              TEXT,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    completed_at            INTEGER
) STRICT;

CREATE INDEX idx_agent_runtime_invocations_task
ON agent_runtime_invocations(task_id, created_at);

CREATE INDEX idx_agent_runtime_invocations_connection
ON agent_runtime_invocations(connection_id, status, updated_at);
