-- M17A: local AI bootstrap / hardware-aware model provisioning.
-- Additive migration; 0001 remains frozen.

CREATE TABLE local_hardware_profiles (
    id                  TEXT PRIMARY KEY,
    node_id             TEXT NOT NULL REFERENCES harness_nodes(id),
    fingerprint         TEXT NOT NULL,
    os_name             TEXT NOT NULL,
    os_version          TEXT,
    architecture        TEXT NOT NULL,
    kernel_version      TEXT,
    cpu_json            TEXT NOT NULL CHECK (json_valid(cpu_json)),
    memory_json         TEXT NOT NULL CHECK (json_valid(memory_json)),
    accelerators_json   TEXT NOT NULL CHECK (json_valid(accelerators_json)),
    runtimes_json       TEXT NOT NULL CHECK (json_valid(runtimes_json)),
    storage_json        TEXT NOT NULL CHECK (json_valid(storage_json)),
    detected_at         INTEGER NOT NULL,
    UNIQUE (node_id, fingerprint)
) STRICT;
CREATE INDEX idx_local_hardware_node ON local_hardware_profiles(node_id, detected_at DESC);

CREATE TABLE local_model_install_plans (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT REFERENCES workspaces(id),
    node_id                 TEXT NOT NULL REFERENCES harness_nodes(id),
    hardware_profile_id     TEXT NOT NULL REFERENCES local_hardware_profiles(id),
    role_name               TEXT NOT NULL,
    use_case                TEXT NOT NULL,
    model_ref               TEXT NOT NULL,
    source_ref              TEXT NOT NULL,
    runtime_name            TEXT NOT NULL,
    quantization            TEXT NOT NULL,
    context_tokens          INTEGER NOT NULL CHECK (context_tokens > 0),
    fit_level               TEXT NOT NULL CHECK (fit_level IN ('perfect','good','marginal')),
    run_mode                TEXT NOT NULL CHECK (run_mode IN ('gpu','moe','cpu_gpu','cpu')),
    estimated_tps           REAL,
    memory_required_bytes   INTEGER NOT NULL CHECK (memory_required_bytes > 0),
    disk_required_bytes     INTEGER NOT NULL CHECK (disk_required_bytes > 0),
    download_scratch_bytes  INTEGER NOT NULL CHECK (download_scratch_bytes >= 0),
    plan_json               TEXT NOT NULL CHECK (json_valid(plan_json)),
    status                  TEXT NOT NULL CHECK (status IN (
        'proposed','approved','installing_runtime','downloading_model','qualifying',
        'ready','failed','cancelled','superseded'
    )),
    created_by              TEXT REFERENCES principals(id),
    approved_by             TEXT REFERENCES principals(id),
    failure_reason          TEXT,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_local_model_plan_node ON local_model_install_plans(node_id, status, updated_at DESC);

CREATE TABLE managed_local_runtimes (
    id                  TEXT PRIMARY KEY,
    node_id             TEXT NOT NULL REFERENCES harness_nodes(id),
    runtime_name        TEXT NOT NULL,
    runtime_version     TEXT NOT NULL,
    install_root        TEXT NOT NULL,
    executable_path     TEXT NOT NULL,
    source_url          TEXT NOT NULL,
    source_sha256       TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('installing','ready','failed','disabled')),
    managed_by_harness  INTEGER NOT NULL CHECK (managed_by_harness IN (0,1)),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    installed_at        INTEGER,
    updated_at          INTEGER NOT NULL,
    UNIQUE (node_id, runtime_name)
) STRICT;

CREATE TABLE managed_local_models (
    id                      TEXT PRIMARY KEY,
    node_id                 TEXT NOT NULL REFERENCES harness_nodes(id),
    plan_id                 TEXT NOT NULL REFERENCES local_model_install_plans(id),
    model_id                TEXT REFERENCES models(id),
    deployment_id           TEXT REFERENCES model_deployments(id),
    runtime_id              TEXT REFERENCES managed_local_runtimes(id),
    model_ref               TEXT NOT NULL,
    source_ref              TEXT NOT NULL,
    local_path              TEXT NOT NULL,
    expected_sha256         TEXT,
    observed_sha256         TEXT,
    size_bytes              INTEGER,
    status                  TEXT NOT NULL CHECK (status IN (
        'downloading','downloaded','verifying','qualifying','ready','failed','removed'
    )),
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    installed_at            INTEGER,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_managed_local_models_node ON managed_local_models(node_id, status, updated_at DESC);
