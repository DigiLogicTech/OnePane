-- M19A: managed local runtime supervision and empirical qualification.
-- Additive migration; 0001 remains frozen.

CREATE TABLE local_runtime_instances (
    id                  TEXT PRIMARY KEY,
    node_id             TEXT NOT NULL REFERENCES harness_nodes(id),
    deployment_id       TEXT NOT NULL REFERENCES model_deployments(id),
    runtime_id          TEXT NOT NULL REFERENCES managed_local_runtimes(id),
    managed_model_id    TEXT NOT NULL REFERENCES managed_local_models(id),
    bind_address        TEXT NOT NULL CHECK (bind_address IN ('127.0.0.1','::1')),
    port                INTEGER NOT NULL CHECK (port BETWEEN 1024 AND 65535),
    pid                 INTEGER,
    status              TEXT NOT NULL CHECK (status IN (
        'stopped','starting','healthy','busy','draining','failed','orphaned'
    )),
    process_fingerprint TEXT NOT NULL,
    launch_json         TEXT NOT NULL CHECK (json_valid(launch_json)),
    health_json         TEXT NOT NULL CHECK (json_valid(health_json)),
    started_at          INTEGER,
    last_seen_at        INTEGER,
    stopped_at          INTEGER,
    failure_reason      TEXT,
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE (deployment_id),
    UNIQUE (node_id, port)
) STRICT;
CREATE INDEX idx_local_runtime_instance_status ON local_runtime_instances(node_id,status,updated_at DESC);

CREATE TABLE local_model_qualification_runs (
    id                      TEXT PRIMARY KEY,
    deployment_id           TEXT NOT NULL REFERENCES model_deployments(id),
    hardware_profile_id     TEXT NOT NULL REFERENCES local_hardware_profiles(id),
    runtime_instance_id     TEXT NOT NULL REFERENCES local_runtime_instances(id),
    status                  TEXT NOT NULL CHECK (status IN ('created','running','passed','limited','failed','cancelled')),
    requested_context       INTEGER NOT NULL CHECK (requested_context > 0),
    verified_context        INTEGER CHECK (verified_context IS NULL OR verified_context > 0),
    protocol_level          TEXT CHECK (protocol_level IS NULL OR protocol_level IN ('L0','L1','L2','L3')),
    prompt_tokens_tested    INTEGER NOT NULL DEFAULT 0 CHECK (prompt_tokens_tested >= 0),
    completion_tokens       INTEGER NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
    ttft_ms                 REAL,
    tokens_per_second       REAL,
    peak_memory_bytes       INTEGER,
    metrics_json            TEXT NOT NULL CHECK (json_valid(metrics_json)),
    evidence_json           TEXT NOT NULL CHECK (json_valid(evidence_json)),
    failure_reason          TEXT,
    started_at              INTEGER,
    completed_at            INTEGER,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_local_qualification_deployment ON local_model_qualification_runs(deployment_id,created_at DESC);
