-- M28 independent V1-V5 assurance worker.
CREATE TABLE assurance_runs (
    id                  TEXT PRIMARY KEY,
    verification_id     TEXT NOT NULL UNIQUE REFERENCES verifications(id),
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    task_id             TEXT REFERENCES tasks(id),
    operation_id        TEXT REFERENCES operations(id),
    worker_run_id       TEXT REFERENCES agent_worker_runs(id),
    status              TEXT NOT NULL CHECK (status IN (
        'queued','running','waiting_evidence','waiting_human','passed','failed','inconclusive','interrupted'
    )),
    required_level      TEXT NOT NULL CHECK (required_level IN ('V1','V2','V3','V4','V5')),
    achieved_level      TEXT CHECK (achieved_level IS NULL OR achieved_level IN ('V0','V1','V2','V3','V4','V5')),
    evidence_hash       TEXT,
    result_json         TEXT NOT NULL CHECK (json_valid(result_json)),
    failure_reason      TEXT,
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    started_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    completed_at        INTEGER
) STRICT;
CREATE INDEX idx_assurance_runs_status ON assurance_runs(status, updated_at);
CREATE INDEX idx_assurance_runs_task ON assurance_runs(task_id, started_at);

CREATE TABLE verification_acceptances (
    id                  TEXT PRIMARY KEY,
    verification_id     TEXT NOT NULL REFERENCES verifications(id),
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    principal_id        TEXT NOT NULL REFERENCES principals(id),
    decision            TEXT NOT NULL CHECK (decision IN ('accepted','rejected')),
    evidence_hash       TEXT NOT NULL,
    note                TEXT NOT NULL,
    created_at          INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX ux_verification_acceptance_once
ON verification_acceptances(verification_id);
CREATE INDEX idx_verification_acceptances_verification
ON verification_acceptances(verification_id, created_at);
