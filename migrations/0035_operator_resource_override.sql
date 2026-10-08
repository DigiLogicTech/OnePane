-- Alpha 3.3: retain honest too-tight estimates when operator explicitly overrides
-- RAM/VRAM admission. SQLite cannot ALTER a named CHECK constraint; rebuild
-- the plan table transactionally. The migration runner creates a durable
-- pre-migration backup and runs PRAGMA quick_check on completion.
-- Defer child-table foreign keys until the original table name is restored.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE local_model_install_plans_rebuilt (
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
    fit_level               TEXT NOT NULL CHECK (fit_level IN ('perfect','good','marginal','too_tight')),
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

INSERT INTO local_model_install_plans_rebuilt SELECT * FROM local_model_install_plans;
DROP TABLE local_model_install_plans;
ALTER TABLE local_model_install_plans_rebuilt RENAME TO local_model_install_plans;
CREATE INDEX idx_local_model_plan_node ON local_model_install_plans(node_id, status, updated_at DESC);
