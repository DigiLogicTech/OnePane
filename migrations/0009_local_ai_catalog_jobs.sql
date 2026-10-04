-- M25: trusted local-AI artifact catalog and durable one-click install jobs.
-- Additive migration; 0001 remains frozen.

CREATE TABLE local_ai_catalogs (
    id                  TEXT PRIMARY KEY,
    catalog_version     TEXT NOT NULL,
    key_id              TEXT NOT NULL,
    source_url          TEXT,
    payload_json        TEXT NOT NULL CHECK (json_valid(payload_json)),
    payload_sha256      TEXT NOT NULL CHECK (length(payload_sha256) = 64),
    signature_b64       TEXT NOT NULL,
    generated_at        INTEGER NOT NULL,
    expires_at          INTEGER NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('active','superseded','rejected')),
    imported_at         INTEGER NOT NULL,
    UNIQUE (catalog_version, payload_sha256)
) STRICT;
CREATE UNIQUE INDEX idx_local_ai_catalog_one_active ON local_ai_catalogs(status) WHERE status='active';

CREATE TABLE local_ai_install_jobs (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT REFERENCES workspaces(id),
    plan_id                 TEXT NOT NULL REFERENCES local_model_install_plans(id),
    catalog_id              TEXT REFERENCES local_ai_catalogs(id),
    deployment_id           TEXT REFERENCES model_deployments(id),
    qualification_run_id    TEXT REFERENCES local_model_qualification_runs(id),
    status                  TEXT NOT NULL CHECK (status IN (
        'queued','resolving','provisioning','starting','qualifying',
        'ready','failed','cancelled','interrupted'
    )),
    attempt_count           INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    failure_reason          TEXT,
    requested_by            TEXT REFERENCES principals(id),
    started_at              INTEGER,
    completed_at            INTEGER,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_local_ai_jobs_status ON local_ai_install_jobs(status, created_at);
CREATE UNIQUE INDEX idx_local_ai_one_active_job_per_plan
ON local_ai_install_jobs(plan_id)
WHERE status IN ('queued','resolving','provisioning','starting','qualifying');

-- A recommendation/install plan owns at most one managed model artifact.
CREATE UNIQUE INDEX idx_managed_local_models_plan ON managed_local_models(plan_id);
