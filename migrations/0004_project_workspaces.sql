-- M13-adjacent Project Workspace / Project Runtime foundation.
-- Runtime intent is durable here; actual sandbox execution remains owned by sandbox-runner.

CREATE TABLE project_runtimes (
    id                      TEXT PRIMARY KEY,
    project_id              TEXT NOT NULL UNIQUE REFERENCES projects(id),
    node_id                 TEXT REFERENCES harness_nodes(id),
    isolation_mode          TEXT NOT NULL CHECK (isolation_mode IN ('sandboxed_container','microvm')),
    backend                 TEXT NOT NULL CHECK (backend IN ('sandbox_runner')),
    desired_state           TEXT NOT NULL CHECK (desired_state IN ('stopped','running','suspended')),
    status                  TEXT NOT NULL CHECK (status IN (
        'defined','provisioning','stopped','starting','running','degraded','failed','suspended','deleting'
    )),
    runtime_spec_json       TEXT NOT NULL CHECK (json_valid(runtime_spec_json)),
    resource_limits_json    TEXT NOT NULL CHECK (json_valid(resource_limits_json)),
    network_policy_json     TEXT NOT NULL CHECK (json_valid(network_policy_json)),
    filesystem_policy_json  TEXT NOT NULL CHECK (json_valid(filesystem_policy_json)),
    environment_bindings_json TEXT NOT NULL CHECK (json_valid(environment_bindings_json)),
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_by              TEXT NOT NULL REFERENCES principals(id),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_project_runtimes_status ON project_runtimes(status, desired_state, updated_at);

CREATE TABLE project_applications (
    id                      TEXT PRIMARY KEY,
    project_runtime_id      TEXT NOT NULL REFERENCES project_runtimes(id),
    name                    TEXT NOT NULL,
    source_kind             TEXT NOT NULL CHECK (source_kind IN (
        'oci_image','git','package','artifact','compose'
    )),
    source_ref              TEXT NOT NULL,
    version_ref             TEXT,
    install_spec_json       TEXT NOT NULL CHECK (json_valid(install_spec_json)),
    runtime_spec_json       TEXT NOT NULL CHECK (json_valid(runtime_spec_json)),
    environment_bindings_json TEXT NOT NULL CHECK (json_valid(environment_bindings_json)),
    desired_state           TEXT NOT NULL CHECK (desired_state IN ('installed','running','stopped','removed')),
    status                  TEXT NOT NULL CHECK (status IN (
        'declared','installing','installed','starting','running','stopped','degraded','failed','removing','removed'
    )),
    trust                   TEXT NOT NULL CHECK (trust IN ('untrusted_content','user_instruction','trusted_procedure')),
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_by              TEXT NOT NULL REFERENCES principals(id),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    UNIQUE (project_runtime_id, name)
) STRICT;
CREATE INDEX idx_project_apps_runtime ON project_applications(project_runtime_id, status);

CREATE TABLE project_runtime_endpoints (
    id                      TEXT PRIMARY KEY,
    project_runtime_id      TEXT NOT NULL REFERENCES project_runtimes(id),
    application_id          TEXT REFERENCES project_applications(id),
    name                    TEXT NOT NULL,
    protocol                TEXT NOT NULL CHECK (protocol IN ('http','https','tcp')),
    internal_port           INTEGER NOT NULL CHECK (internal_port >= 1 AND internal_port <= 65535),
    exposure                TEXT NOT NULL CHECK (exposure IN ('preview','workspace','private','public')),
    path_prefix             TEXT,
    desired_state           TEXT NOT NULL CHECK (desired_state IN ('enabled','disabled')),
    status                  TEXT NOT NULL CHECK (status IN ('declared','provisioning','ready','degraded','failed','disabled')),
    external_url            TEXT,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_by              TEXT NOT NULL REFERENCES principals(id),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    UNIQUE (project_runtime_id, name)
) STRICT;
CREATE INDEX idx_project_endpoints_runtime ON project_runtime_endpoints(project_runtime_id, status);

CREATE TABLE project_change_proposals (
    id                      TEXT PRIMARY KEY,
    project_id              TEXT NOT NULL REFERENCES projects(id),
    project_runtime_id      TEXT REFERENCES project_runtimes(id),
    task_id                 TEXT REFERENCES tasks(id),
    proposal_kind           TEXT NOT NULL CHECK (proposal_kind IN (
        'code','content','runtime_config','application_install','application_update','application_remove','endpoint','routine_binding'
    )),
    status                  TEXT NOT NULL CHECK (status IN (
        'draft','proposed','reviewing','approved','rejected','applied','superseded','cancelled'
    )),
    summary                 TEXT NOT NULL,
    base_revision           INTEGER CHECK (base_revision IS NULL OR base_revision >= 1),
    patch_artifact_id       TEXT REFERENCES artifacts(id),
    metadata_json           TEXT NOT NULL CHECK (json_valid(metadata_json)),
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    proposed_by             TEXT NOT NULL REFERENCES principals(id),
    reviewed_by             TEXT REFERENCES principals(id),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    reviewed_at             INTEGER
) STRICT;
CREATE INDEX idx_project_change_review ON project_change_proposals(project_id, status, created_at);

CREATE TABLE project_routine_bindings (
    id                      TEXT PRIMARY KEY,
    project_id              TEXT NOT NULL REFERENCES projects(id),
    project_runtime_id      TEXT NOT NULL REFERENCES project_runtimes(id),
    application_id          TEXT REFERENCES project_applications(id),
    routine_id              TEXT NOT NULL REFERENCES routines(id),
    action_kind             TEXT NOT NULL CHECK (action_kind IN ('app_command','http_request','tool_capability')),
    action_ref              TEXT NOT NULL,
    action_spec_json        TEXT NOT NULL CHECK (json_valid(action_spec_json)),
    status                  TEXT NOT NULL CHECK (status IN ('active','paused','disabled')),
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_by              TEXT NOT NULL REFERENCES principals(id),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    UNIQUE (routine_id, project_runtime_id, action_ref)
) STRICT;
CREATE INDEX idx_project_routine_runtime ON project_routine_bindings(project_runtime_id, status);
