-- M26: durable autonomous agent worker runs and ordered step journal.
-- Additive migration; 0001 remains frozen.

CREATE TABLE agent_worker_runs (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    task_id                 TEXT NOT NULL REFERENCES tasks(id),
    attempt_id              TEXT NOT NULL UNIQUE REFERENCES task_attempts(id),
    worker_principal_id     TEXT NOT NULL REFERENCES principals(id),
    status                  TEXT NOT NULL CHECK (status IN (
        'running','waiting','blocked','succeeded','failed','interrupted'
    )),
    role_name               TEXT NOT NULL,
    capability_id           TEXT NOT NULL,
    protocol_level          TEXT NOT NULL CHECK (protocol_level IN ('L0','L1','L2','L3')),
    max_steps               INTEGER NOT NULL CHECK (max_steps BETWEEN 1 AND 256),
    step_count              INTEGER NOT NULL DEFAULT 0 CHECK (step_count >= 0),
    max_replans             INTEGER NOT NULL CHECK (max_replans BETWEEN 0 AND 32),
    replan_count            INTEGER NOT NULL DEFAULT 0 CHECK (replan_count >= 0),
    max_escalations         INTEGER NOT NULL CHECK (max_escalations BETWEEN 0 AND 32),
    escalation_count        INTEGER NOT NULL DEFAULT 0 CHECK (escalation_count >= 0),
    route_policy_json       TEXT NOT NULL CHECK (json_valid(route_policy_json)),
    continuation_json       TEXT NOT NULL CHECK (json_valid(continuation_json)),
    last_candidate_kind     TEXT CHECK (last_candidate_kind IS NULL OR last_candidate_kind IN ('model_deployment','agent_runtime')),
    last_candidate_id       TEXT,
    last_error              TEXT,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    started_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    completed_at            INTEGER
) STRICT;
CREATE INDEX idx_agent_worker_runs_status ON agent_worker_runs(status, updated_at);
CREATE INDEX idx_agent_worker_runs_task ON agent_worker_runs(task_id, started_at);

CREATE TABLE agent_worker_steps (
    id                      TEXT PRIMARY KEY,
    run_id                  TEXT NOT NULL REFERENCES agent_worker_runs(id),
    step_number             INTEGER NOT NULL CHECK (step_number >= 1),
    step_kind               TEXT NOT NULL CHECK (step_kind IN (
        'route','model','agent_runtime','tool','operation','delegate','replan',
        'complete','human','escalate','wait','fail','verification'
    )),
    status                  TEXT NOT NULL CHECK (status IN ('started','succeeded','failed','waiting','unknown')),
    candidate_kind          TEXT CHECK (candidate_kind IS NULL OR candidate_kind IN ('model_deployment','agent_runtime')),
    candidate_id            TEXT,
    proposal_type           TEXT CHECK (proposal_type IS NULL OR proposal_type IN ('tool','delegate','replan','complete','human','escalate','wait','fail')),
    request_ref             TEXT,
    result_ref              TEXT,
    detail_json             TEXT NOT NULL CHECK (json_valid(detail_json)),
    started_at              INTEGER NOT NULL,
    completed_at            INTEGER,
    UNIQUE (run_id, step_number)
) STRICT;
CREATE INDEX idx_agent_worker_steps_run ON agent_worker_steps(run_id, step_number);
