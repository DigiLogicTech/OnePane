-- Harness v0.1 initial durable domain schema.
-- Timestamps are UTC Unix milliseconds. JSON is canonical application data;
-- authoritative state remains in typed relational columns wherever practical.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version         INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    checksum        TEXT NOT NULL,
    applied_at      INTEGER NOT NULL
) STRICT;

CREATE TABLE system_state (
    singleton_id    INTEGER PRIMARY KEY CHECK (singleton_id = 1),
    mode            TEXT NOT NULL CHECK (mode IN (
        'uninitialized','bootstrap','commissioning','recovery','normal',
        'safe','read_only','maintenance','degraded'
    )),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    reason          TEXT,
    updated_at      INTEGER NOT NULL
) STRICT;

CREATE TABLE harness_nodes (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    local                   INTEGER NOT NULL CHECK (local IN (0,1)),
    identity_fingerprint    TEXT NOT NULL UNIQUE,
    trust_state             TEXT NOT NULL CHECK (trust_state IN (
        'local','discovered','pairing','paired','revoked','unavailable'
    )),
    trust_zone              TEXT,
    endpoint_json           TEXT CHECK (endpoint_json IS NULL OR json_valid(endpoint_json)),
    protocol_json           TEXT NOT NULL CHECK (json_valid(protocol_json)),
    capabilities_json       TEXT NOT NULL CHECK (json_valid(capabilities_json)),
    last_seen_at            INTEGER,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX idx_harness_nodes_one_local ON harness_nodes(local) WHERE local = 1;

CREATE TABLE workspaces (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('active','suspended','archived')),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;

CREATE TABLE principals (
    id              TEXT PRIMARY KEY,
    principal_type  TEXT NOT NULL CHECK (principal_type IN (
        'human','agent','service','system','recovery','watchdog'
    )),
    display_name    TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('active','disabled','revoked')),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;

CREATE TABLE roles (
    id              TEXT PRIMARY KEY,
    role_class      TEXT NOT NULL CHECK (role_class IN ('human','agent','system')),
    name            TEXT NOT NULL UNIQUE,
    definition_json TEXT NOT NULL CHECK (json_valid(definition_json)),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;

CREATE TABLE workspace_memberships (
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id),
    principal_id    TEXT NOT NULL REFERENCES principals(id),
    status          TEXT NOT NULL CHECK (status IN ('active','suspended','revoked')),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, principal_id)
) STRICT;

CREATE TABLE principal_roles (
    principal_id    TEXT NOT NULL REFERENCES principals(id),
    role_id         TEXT NOT NULL REFERENCES roles(id),
    workspace_id    TEXT REFERENCES workspaces(id),
    created_at      INTEGER NOT NULL,
    PRIMARY KEY (principal_id, role_id, workspace_id)
) STRICT;

CREATE UNIQUE INDEX idx_principal_roles_scope
ON principal_roles(principal_id, role_id, COALESCE(workspace_id, ''));

CREATE TABLE auth_identities (
    id                  TEXT PRIMARY KEY,
    principal_id        TEXT NOT NULL REFERENCES principals(id),
    provider            TEXT NOT NULL,
    issuer              TEXT,
    subject             TEXT,
    credential_json     TEXT NOT NULL CHECK (json_valid(credential_json)),
    status              TEXT NOT NULL CHECK (status IN ('active','disabled','revoked')),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX idx_auth_identity_external ON auth_identities(provider, issuer, subject)
WHERE issuer IS NOT NULL AND subject IS NOT NULL;

CREATE TABLE auth_sessions (
    id                      TEXT PRIMARY KEY,
    principal_id            TEXT NOT NULL REFERENCES principals(id),
    token_hash              TEXT NOT NULL UNIQUE,
    auth_method             TEXT NOT NULL,
    auth_strength           TEXT NOT NULL,
    created_at              INTEGER NOT NULL,
    last_seen_at            INTEGER NOT NULL,
    idle_expires_at         INTEGER,
    expires_at              INTEGER NOT NULL,
    reauthenticated_at      INTEGER,
    revoked_at              INTEGER,
    metadata_json           TEXT NOT NULL CHECK (json_valid(metadata_json))
) STRICT;

CREATE TABLE api_credentials (
    id                      TEXT PRIMARY KEY,
    principal_id            TEXT NOT NULL REFERENCES principals(id),
    name                    TEXT NOT NULL,
    credential_hash         TEXT NOT NULL UNIQUE,
    workspace_scope_json    TEXT NOT NULL CHECK (json_valid(workspace_scope_json)),
    capability_scope_json   TEXT NOT NULL CHECK (json_valid(capability_scope_json)),
    expires_at              INTEGER,
    last_used_at            INTEGER,
    revoked_at              INTEGER,
    created_at              INTEGER NOT NULL,
    CHECK (expires_at IS NULL OR expires_at > created_at)
) STRICT;

CREATE TABLE secret_records (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    logical_name        TEXT NOT NULL,
    provider_type       TEXT NOT NULL,
    version             INTEGER NOT NULL CHECK (version >= 1),
    status              TEXT NOT NULL CHECK (status IN ('active','retired','revoked')),
    metadata_json       TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_by          TEXT NOT NULL REFERENCES principals(id),
    created_at          INTEGER NOT NULL,
    retired_at          INTEGER,
    UNIQUE (workspace_id, logical_name, version)
) STRICT;
CREATE UNIQUE INDEX idx_secret_active_version ON secret_records(workspace_id, logical_name)
WHERE status = 'active';

CREATE TABLE builtin_vault_items (
    secret_record_id    TEXT PRIMARY KEY REFERENCES secret_records(id),
    ciphertext          BLOB NOT NULL,
    wrapped_dek         BLOB NOT NULL,
    nonce               BLOB NOT NULL,
    algorithm           TEXT NOT NULL,
    key_version         INTEGER NOT NULL CHECK (key_version >= 1),
    created_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE policies (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    logical_name        TEXT NOT NULL,
    version             INTEGER NOT NULL CHECK (version >= 1),
    status              TEXT NOT NULL CHECK (status IN ('candidate','active','superseded','disabled')),
    policy_json         TEXT NOT NULL CHECK (json_valid(policy_json)),
    created_by          TEXT NOT NULL REFERENCES principals(id),
    created_at          INTEGER NOT NULL,
    activated_at        INTEGER,
    UNIQUE (workspace_id, logical_name, version)
) STRICT;
CREATE UNIQUE INDEX idx_policy_one_active ON policies(workspace_id, logical_name)
WHERE status = 'active';

CREATE TABLE instruction_templates (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    logical_name        TEXT NOT NULL,
    version             INTEGER NOT NULL CHECK (version >= 1),
    status              TEXT NOT NULL CHECK (status IN ('candidate','active','superseded','disabled')),
    content             TEXT NOT NULL,
    content_hash        TEXT NOT NULL,
    metadata_json       TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    UNIQUE (workspace_id, logical_name, version)
) STRICT;

CREATE TABLE projects (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    name                    TEXT NOT NULL,
    description             TEXT,
    status                  TEXT NOT NULL CHECK (status IN ('active','archived','suspended')),
    project_policy_json     TEXT NOT NULL CHECK (json_valid(project_policy_json)),
    indexing_config_json    TEXT NOT NULL CHECK (json_valid(indexing_config_json)),
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_by              TEXT NOT NULL REFERENCES principals(id),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_projects_workspace ON projects(workspace_id, status);

CREATE TABLE repository_connections (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES projects(id),
    provider            TEXT NOT NULL,
    repository_ref      TEXT NOT NULL,
    default_branch      TEXT,
    auth_secret_ref     TEXT,
    connection_json     TEXT NOT NULL CHECK (json_valid(connection_json)),
    status              TEXT NOT NULL CHECK (status IN (
        'connected','degraded','reauth_required','disconnected'
    )),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_repository_connections_project ON repository_connections(project_id);

CREATE TABLE artifacts (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    project_id          TEXT REFERENCES projects(id),
    content_hash        TEXT NOT NULL,
    media_type          TEXT NOT NULL,
    size_bytes          INTEGER NOT NULL CHECK (size_bytes >= 0),
    storage_ref         TEXT NOT NULL,
    confidentiality     TEXT NOT NULL CHECK (confidentiality IN (
        'public','internal','confidential','secret'
    )),
    residency           TEXT NOT NULL CHECK (residency IN ('any','trusted_nodes','origin_node')),
    trust               TEXT NOT NULL CHECK (trust IN (
        'trusted_control','trusted_procedure','authoritative_data','user_instruction',
        'untrusted_content','unverified_derived','verified_derived'
    )),
    origin_node_id      TEXT REFERENCES harness_nodes(id),
    status              TEXT NOT NULL CHECK (status IN ('active','quarantined','corrupted','archived')),
    metadata_json       TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_by          TEXT REFERENCES principals(id),
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_artifacts_workspace ON artifacts(workspace_id, created_at);
CREATE INDEX idx_artifacts_hash ON artifacts(content_hash);

CREATE TABLE artifact_sessions (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    project_id          TEXT REFERENCES projects(id),
    title               TEXT NOT NULL,
    format              TEXT NOT NULL,
    source_artifact_id  TEXT REFERENCES artifacts(id),
    current_revision_id TEXT REFERENCES artifact_revisions(id),
    status              TEXT NOT NULL CHECK (status IN ('active','published','archived')),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_by          TEXT NOT NULL REFERENCES principals(id),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE artifact_revisions (
    id                  TEXT PRIMARY KEY,
    session_id          TEXT NOT NULL REFERENCES artifact_sessions(id),
    parent_revision_id  TEXT REFERENCES artifact_revisions(id),
    artifact_id         TEXT NOT NULL REFERENCES artifacts(id),
    content_hash        TEXT NOT NULL,
    author_principal_id TEXT NOT NULL REFERENCES principals(id),
    source_type         TEXT NOT NULL CHECK (source_type IN ('human','agent','import','merge','publish')),
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_artifact_revisions_session ON artifact_revisions(session_id, created_at);

CREATE TABLE intent_revisions (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    root_intent_id          TEXT NOT NULL,
    revision_number         INTEGER NOT NULL CHECK (revision_number >= 1),
    objective               TEXT NOT NULL,
    acceptance_json         TEXT NOT NULL CHECK (json_valid(acceptance_json)),
    constraints_json        TEXT NOT NULL CHECK (json_valid(constraints_json)),
    status                  TEXT NOT NULL CHECK (status IN ('current','superseded','cancelled')),
    created_by              TEXT NOT NULL REFERENCES principals(id),
    created_at              INTEGER NOT NULL,
    UNIQUE (root_intent_id, revision_number)
) STRICT;
CREATE UNIQUE INDEX idx_intent_current ON intent_revisions(root_intent_id) WHERE status = 'current';

CREATE TABLE plans (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    intent_revision_id      TEXT NOT NULL REFERENCES intent_revisions(id),
    supersedes_plan_id      TEXT REFERENCES plans(id),
    status                  TEXT NOT NULL CHECK (status IN (
        'candidate','validating','active','stale','superseded','completed','cancelled','failed'
    )),
    plan_json               TEXT NOT NULL CHECK (json_valid(plan_json)),
    risk_class              TEXT NOT NULL CHECK (risk_class IN ('low','medium','high','critical')),
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_by              TEXT NOT NULL REFERENCES principals(id),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;

CREATE TABLE tasks (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    project_id              TEXT REFERENCES projects(id),
    artifact_session_id     TEXT REFERENCES artifact_sessions(id),
    plan_id                 TEXT REFERENCES plans(id),
    parent_task_id          TEXT REFERENCES tasks(id),
    objective               TEXT NOT NULL,
    state                   TEXT NOT NULL CHECK (state IN (
        'created','ready','running','waiting_dependency','waiting_approval','paused',
        'completion_requested','verifying','blocked','failed','complete',
        'cancel_requested','cancelling','compensating','cancelled'
    )),
    scheduling_class        TEXT NOT NULL DEFAULT 'normal_task' CHECK (scheduling_class IN (
        'urgent_recovery','user_interactive','approval_continuation','normal_task',
        'background_routine','maintenance','best_effort'
    )),
    priority                INTEGER NOT NULL DEFAULT 0,
    completion_json         TEXT NOT NULL CHECK (json_valid(completion_json)),
    result_json             TEXT CHECK (result_json IS NULL OR json_valid(result_json)),
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    ready_at                INTEGER,
    cancel_requested_at     INTEGER,
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_tasks_workspace_state ON tasks(workspace_id, state, scheduling_class, priority, ready_at);
CREATE INDEX idx_tasks_plan ON tasks(plan_id);
CREATE INDEX idx_tasks_parent ON tasks(parent_task_id);

CREATE TABLE task_dependencies (
    task_id             TEXT NOT NULL REFERENCES tasks(id),
    depends_on_task_id  TEXT NOT NULL REFERENCES tasks(id),
    dependency_type     TEXT NOT NULL CHECK (dependency_type IN ('hard','soft')),
    dependency_mode     TEXT NOT NULL CHECK (dependency_mode IN ('all','any','quorum')),
    metadata_json       TEXT NOT NULL CHECK (json_valid(metadata_json)),
    PRIMARY KEY (task_id, depends_on_task_id)
) STRICT;

CREATE TABLE task_attempts (
    id                  TEXT PRIMARY KEY,
    task_id             TEXT NOT NULL REFERENCES tasks(id),
    attempt_number      INTEGER NOT NULL CHECK (attempt_number >= 1),
    worker_principal_id TEXT REFERENCES principals(id),
    status              TEXT NOT NULL CHECK (status IN (
        'created','queued','running','waiting','succeeded','failed','cancelled','interrupted'
    )),
    recovery_snapshot_id TEXT REFERENCES recovery_snapshots(id),
    started_at          INTEGER,
    ended_at            INTEGER,
    metadata_json       TEXT NOT NULL CHECK (json_valid(metadata_json)),
    UNIQUE (task_id, attempt_number)
) STRICT;
CREATE UNIQUE INDEX idx_task_one_active_attempt
ON task_attempts(task_id)
WHERE status IN ('created','queued','running','waiting');

CREATE TABLE edit_proposals (
    id                  TEXT PRIMARY KEY,
    session_id          TEXT NOT NULL REFERENCES artifact_sessions(id),
    base_revision_id    TEXT NOT NULL REFERENCES artifact_revisions(id),
    author_principal_id TEXT NOT NULL REFERENCES principals(id),
    task_id             TEXT REFERENCES tasks(id),
    operations_json     TEXT NOT NULL CHECK (json_valid(operations_json)),
    summary             TEXT,
    status              TEXT NOT NULL CHECK (status IN (
        'proposed','validating','accepted','rejected','superseded','conflict'
    )),
    created_at          INTEGER NOT NULL,
    resolved_at         INTEGER
) STRICT;
CREATE INDEX idx_edit_proposals_session ON edit_proposals(session_id, status);

CREATE TABLE capability_leases (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    principal_id        TEXT NOT NULL REFERENCES principals(id),
    task_id             TEXT REFERENCES tasks(id),
    capability_id       TEXT NOT NULL,
    scope_json          TEXT NOT NULL CHECK (json_valid(scope_json)),
    status              TEXT NOT NULL CHECK (status IN ('active','expired','revoked','exhausted','invalidated')),
    issued_by           TEXT NOT NULL REFERENCES principals(id),
    issued_at           INTEGER NOT NULL,
    expires_at          INTEGER NOT NULL,
    usage_limit         INTEGER,
    usage_count         INTEGER NOT NULL DEFAULT 0 CHECK (usage_count >= 0),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
) STRICT;
CREATE INDEX idx_capability_leases_active ON capability_leases(principal_id, task_id, status, expires_at);

CREATE TABLE resource_leases (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    task_id             TEXT REFERENCES tasks(id),
    resource_ref        TEXT NOT NULL,
    lease_mode          TEXT NOT NULL CHECK (lease_mode IN ('exclusive_mutation')),
    status              TEXT NOT NULL CHECK (status IN ('active','released','expired','revoked')),
    acquired_at         INTEGER NOT NULL,
    expires_at          INTEGER NOT NULL,
    released_at         INTEGER,
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
) STRICT;
CREATE UNIQUE INDEX idx_resource_one_active_writer
ON resource_leases(workspace_id, resource_ref)
WHERE status = 'active' AND lease_mode = 'exclusive_mutation';

CREATE TABLE approvals (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    operation_id            TEXT REFERENCES operations(id),
    requested_by            TEXT NOT NULL REFERENCES principals(id),
    approved_by             TEXT REFERENCES principals(id),
    status                  TEXT NOT NULL CHECK (status IN (
        'pending','approved','denied','expired','invalidated','revoked','consumed'
    )),
    operation_hash          TEXT NOT NULL,
    requirement_json        TEXT NOT NULL CHECK (json_valid(requirement_json)),
    policy_revision         INTEGER NOT NULL,
    created_at              INTEGER NOT NULL,
    expires_at              INTEGER,
    resolved_at             INTEGER,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
) STRICT;

CREATE TABLE operations (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    task_id                 TEXT REFERENCES tasks(id),
    attempt_id              TEXT REFERENCES task_attempts(id),
    principal_id            TEXT NOT NULL REFERENCES principals(id),
    compensates_operation_id TEXT REFERENCES operations(id),
    idempotency_key         TEXT NOT NULL,
    state                   TEXT NOT NULL CHECK (state IN (
        'proposed','authorized','prepared','executing','observing','verified','committed',
        'denied','failed','aborted','unknown_outcome','blocked_unknown_outcome',
        'compensating','compensated','compensation_failed'
    )),
    capability_id           TEXT NOT NULL,
    resource_ref            TEXT NOT NULL,
    tool_id                 TEXT NOT NULL,
    tool_version            TEXT NOT NULL,
    adapter_id              TEXT NOT NULL,
    adapter_version         TEXT NOT NULL,
    desired_state_json      TEXT NOT NULL CHECK (json_valid(desired_state_json)),
    precondition_json       TEXT NOT NULL CHECK (json_valid(precondition_json)),
    reconciliation_json     TEXT NOT NULL CHECK (json_valid(reconciliation_json)),
    compensation_json       TEXT CHECK (compensation_json IS NULL OR json_valid(compensation_json)),
    policy_revision         INTEGER NOT NULL,
    input_hash              TEXT NOT NULL,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    UNIQUE (workspace_id, idempotency_key)
) STRICT;
CREATE INDEX idx_operations_recovery ON operations(state, updated_at);
CREATE INDEX idx_operations_task ON operations(task_id, created_at);

CREATE TABLE operation_receipts (
    id                  TEXT PRIMARY KEY,
    operation_id        TEXT NOT NULL REFERENCES operations(id),
    receipt_type        TEXT NOT NULL,
    external_request_id TEXT,
    receipt_json        TEXT NOT NULL CHECK (json_valid(receipt_json)),
    integrity_hash      TEXT NOT NULL,
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_operation_receipts_operation ON operation_receipts(operation_id, created_at);

CREATE TABLE tool_invocations (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    task_id             TEXT REFERENCES tasks(id),
    attempt_id          TEXT REFERENCES task_attempts(id),
    principal_id        TEXT NOT NULL REFERENCES principals(id),
    operation_id        TEXT REFERENCES operations(id),
    tool_id             TEXT NOT NULL,
    tool_version        TEXT NOT NULL,
    adapter_id          TEXT NOT NULL,
    adapter_version     TEXT NOT NULL,
    mode                TEXT NOT NULL CHECK (mode IN (
        'observe','read','execute_sandboxed','mutate','external_send'
    )),
    resource_ref        TEXT,
    input_hash          TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN (
        'created','authorized','running','succeeded','failed','timed_out','cancelled','interrupted'
    )),
    summary             TEXT,
    result_json         TEXT CHECK (result_json IS NULL OR json_valid(result_json)),
    error_code          TEXT,
    started_at          INTEGER,
    ended_at            INTEGER,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_tool_invocations_task ON tool_invocations(task_id, created_at);
CREATE INDEX idx_tool_invocations_operation ON tool_invocations(operation_id);
CREATE INDEX idx_tool_invocations_status ON tool_invocations(status);

CREATE TABLE observations (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    subject_ref             TEXT NOT NULL,
    observation_type        TEXT NOT NULL,
    probe_tool_id           TEXT NOT NULL,
    probe_tool_version      TEXT NOT NULL,
    source_principal_id     TEXT REFERENCES principals(id),
    adapter_id              TEXT,
    adapter_version         TEXT,
    value_json              TEXT NOT NULL CHECK (json_valid(value_json)),
    confidentiality         TEXT NOT NULL CHECK (confidentiality IN (
        'public','internal','confidential','secret'
    )),
    residency               TEXT NOT NULL CHECK (residency IN ('any','trusted_nodes','origin_node')),
    trust                   TEXT NOT NULL CHECK (trust IN (
        'trusted_control','trusted_procedure','authoritative_data','user_instruction',
        'untrusted_content','unverified_derived','verified_derived'
    )),
    origin_node_id          TEXT REFERENCES harness_nodes(id),
    integrity_hash          TEXT NOT NULL,
    observed_at             INTEGER NOT NULL,
    created_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_observations_subject ON observations(workspace_id, subject_ref, observed_at DESC);
CREATE INDEX idx_observations_probe ON observations(probe_tool_id, observed_at DESC);

CREATE TABLE verifications (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    task_id             TEXT REFERENCES tasks(id),
    operation_id        TEXT REFERENCES operations(id),
    subject_ref         TEXT NOT NULL,
    required_level      TEXT NOT NULL CHECK (required_level IN ('V0','V1','V2','V3','V4','V5')),
    achieved_level      TEXT CHECK (achieved_level IS NULL OR achieved_level IN ('V0','V1','V2','V3','V4','V5')),
    status              TEXT NOT NULL CHECK (status IN ('pending','pass','fail','inconclusive','stale')),
    spec_json           TEXT NOT NULL CHECK (json_valid(spec_json)),
    result_json         TEXT CHECK (result_json IS NULL OR json_valid(result_json)),
    verified_by         TEXT REFERENCES principals(id),
    started_at          INTEGER NOT NULL,
    completed_at        INTEGER,
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
) STRICT;
CREATE INDEX idx_verifications_task ON verifications(task_id, status);

CREATE TABLE checkpoints (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    task_id             TEXT NOT NULL REFERENCES tasks(id),
    verification_id     TEXT NOT NULL REFERENCES verifications(id),
    state_json          TEXT NOT NULL CHECK (json_valid(state_json)),
    status              TEXT NOT NULL CHECK (status IN ('valid','stale','superseded')),
    created_at          INTEGER NOT NULL,
    invalidated_at      INTEGER
) STRICT;
CREATE INDEX idx_checkpoints_task ON checkpoints(task_id, created_at DESC);

CREATE TABLE recovery_snapshots (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    task_id             TEXT NOT NULL REFERENCES tasks(id),
    attempt_id          TEXT REFERENCES task_attempts(id),
    snapshot_json       TEXT NOT NULL CHECK (json_valid(snapshot_json)),
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_recovery_snapshots_task ON recovery_snapshots(task_id, created_at DESC);

CREATE TABLE dependency_refs (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    owner_type          TEXT NOT NULL,
    owner_id            TEXT NOT NULL,
    target_type         TEXT NOT NULL,
    target_ref          TEXT NOT NULL,
    target_revision     INTEGER,
    dependency_kind     TEXT NOT NULL,
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_dependency_target ON dependency_refs(workspace_id, target_type, target_ref);
CREATE INDEX idx_dependency_owner ON dependency_refs(owner_type, owner_id);

CREATE TABLE provider_connections (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    provider            TEXT NOT NULL,
    display_name        TEXT NOT NULL,
    auth_type           TEXT NOT NULL,
    secret_ref          TEXT,
    status              TEXT NOT NULL CHECK (status IN (
        'connected','degraded','rate_limited','expired','reauth_required','revoked','unavailable'
    )),
    connection_json     TEXT NOT NULL CHECK (json_valid(connection_json)),
    retry_after         INTEGER,
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE entitlements (
    id                      TEXT PRIMARY KEY,
    provider_connection_id  TEXT NOT NULL REFERENCES provider_connections(id),
    snapshot_json           TEXT NOT NULL CHECK (json_valid(snapshot_json)),
    observed_at             INTEGER NOT NULL,
    expires_at              INTEGER
) STRICT;
CREATE INDEX idx_entitlements_connection ON entitlements(provider_connection_id, observed_at DESC);

CREATE TABLE models (
    id                  TEXT PRIMARY KEY,
    provider_name       TEXT,
    model_ref           TEXT NOT NULL,
    architecture        TEXT,
    revision_ref        TEXT,
    weights_hash        TEXT,
    quantization        TEXT,
    modalities_json     TEXT NOT NULL CHECK (json_valid(modalities_json)),
    static_metadata_json TEXT NOT NULL CHECK (json_valid(static_metadata_json)),
    trust_state         TEXT NOT NULL CHECK (trust_state IN (
        'trusted','user_trusted','quarantined','revoked'
    )),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX idx_models_identity ON models(model_ref, COALESCE(revision_ref,''), COALESCE(weights_hash,''), COALESCE(quantization,''));

CREATE TABLE model_deployments (
    id                      TEXT PRIMARY KEY,
    model_id                TEXT NOT NULL REFERENCES models(id),
    node_id                 TEXT REFERENCES harness_nodes(id),
    provider_connection_id  TEXT REFERENCES provider_connections(id),
    runtime_name            TEXT,
    runtime_version         TEXT,
    runtime_config_json     TEXT NOT NULL CHECK (json_valid(runtime_config_json)),
    status                  TEXT NOT NULL CHECK (status IN (
        'discovered','qualifying','ready','degraded','draining','unavailable','disabled'
    )),
    residency_state         TEXT CHECK (residency_state IS NULL OR residency_state IN (
        'stopped','loading','resident','busy','draining','failed'
    )),
    context_max_reported    INTEGER,
    context_max_verified    INTEGER,
    deployment_fingerprint  TEXT NOT NULL,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    discovered_at           INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    CHECK (node_id IS NOT NULL OR provider_connection_id IS NOT NULL)
) STRICT;
CREATE INDEX idx_model_deployments_model ON model_deployments(model_id);
CREATE INDEX idx_model_deployments_node ON model_deployments(node_id);
CREATE INDEX idx_model_deployments_status ON model_deployments(status);

CREATE TABLE compatibility_profiles (
    id                      TEXT PRIMARY KEY,
    deployment_id           TEXT NOT NULL REFERENCES model_deployments(id),
    role_name               TEXT,
    capability_id           TEXT NOT NULL,
    protocol_level          TEXT NOT NULL CHECK (protocol_level IN ('L0','L1','L2','L3')),
    context_min             INTEGER NOT NULL DEFAULT 0 CHECK (context_min >= 0),
    context_max_verified    INTEGER,
    context_max_supported   INTEGER,
    risk_class              TEXT NOT NULL CHECK (risk_class IN ('low','medium','high','critical')),
    qualification           TEXT NOT NULL CHECK (qualification IN (
        'untested','incompatible','limited','mediated','supported','verified'
    )),
    mediation_json          TEXT NOT NULL CHECK (json_valid(mediation_json)),
    evidence_json           TEXT NOT NULL CHECK (json_valid(evidence_json)),
    profile_fingerprint     TEXT NOT NULL,
    verified_at             INTEGER,
    stale_at                INTEGER,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_compatibility_route ON compatibility_profiles(capability_id, qualification, deployment_id);

CREATE TABLE budget_accounts (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    parent_account_id   TEXT REFERENCES budget_accounts(id),
    name                TEXT NOT NULL,
    unit                TEXT NOT NULL,
    limit_amount        INTEGER NOT NULL CHECK (limit_amount >= 0),
    committed_amount    INTEGER NOT NULL DEFAULT 0 CHECK (committed_amount >= 0),
    reserved_amount     INTEGER NOT NULL DEFAULT 0 CHECK (reserved_amount >= 0),
    period_json         TEXT NOT NULL CHECK (json_valid(period_json)),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    CHECK (committed_amount + reserved_amount <= limit_amount)
) STRICT;

CREATE TABLE budget_reservations (
    id                  TEXT PRIMARY KEY,
    budget_account_id   TEXT NOT NULL REFERENCES budget_accounts(id),
    task_id             TEXT REFERENCES tasks(id),
    inference_request_id TEXT REFERENCES inference_requests(id),
    amount              INTEGER NOT NULL CHECK (amount >= 0),
    status              TEXT NOT NULL CHECK (status IN ('reserved','committed','released','expired')),
    actual_amount       INTEGER,
    reserved_at         INTEGER NOT NULL,
    expires_at          INTEGER NOT NULL,
    closed_at           INTEGER
) STRICT;
CREATE INDEX idx_budget_reservations_account ON budget_reservations(budget_account_id, status, expires_at);

CREATE TABLE inference_sessions (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    task_id             TEXT REFERENCES tasks(id),
    principal_id        TEXT NOT NULL REFERENCES principals(id),
    deployment_id       TEXT REFERENCES model_deployments(id),
    status              TEXT NOT NULL CHECK (status IN (
        'created','connecting','ready','active','context_pressure','draining','closed','degraded','failed'
    )),
    parent_session_id   TEXT REFERENCES inference_sessions(id),
    context_manifest_json TEXT NOT NULL CHECK (json_valid(context_manifest_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    closed_at           INTEGER
) STRICT;
CREATE INDEX idx_inference_sessions_task ON inference_sessions(task_id, status);

CREATE TABLE inference_requests (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    task_id                 TEXT REFERENCES tasks(id),
    session_id              TEXT REFERENCES inference_sessions(id),
    principal_id            TEXT NOT NULL REFERENCES principals(id),
    deployment_id           TEXT REFERENCES model_deployments(id),
    provider_connection_id  TEXT REFERENCES provider_connections(id),
    origin_node_id          TEXT REFERENCES harness_nodes(id),
    execution_node_id       TEXT REFERENCES harness_nodes(id),
    status                  TEXT NOT NULL CHECK (status IN (
        'created','routed','budget_reserved','dispatched','executing','succeeded',
        'rate_limited','failed','cancelled','unknown'
    )),
    capability_json         TEXT NOT NULL CHECK (json_valid(capability_json)),
    context_manifest_json   TEXT NOT NULL CHECK (json_valid(context_manifest_json)),
    classification_json     TEXT NOT NULL CHECK (json_valid(classification_json)),
    request_json            TEXT NOT NULL CHECK (json_valid(request_json)),
    response_artifact_id    TEXT REFERENCES artifacts(id),
    usage_json              TEXT CHECK (usage_json IS NULL OR json_valid(usage_json)),
    error_code              TEXT,
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    completed_at            INTEGER
) STRICT;
CREATE INDEX idx_inference_requests_task ON inference_requests(task_id, created_at);
CREATE INDEX idx_inference_requests_recovery ON inference_requests(status, updated_at);

CREATE TABLE continuation_capsules (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    task_id             TEXT NOT NULL REFERENCES tasks(id),
    from_session_id     TEXT REFERENCES inference_sessions(id),
    checkpoint_id       TEXT REFERENCES checkpoints(id),
    capsule_json        TEXT NOT NULL CHECK (json_valid(capsule_json)),
    content_hash        TEXT NOT NULL,
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_continuation_task ON continuation_capsules(task_id, created_at DESC);

CREATE TABLE environment_resources (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    node_id             TEXT REFERENCES harness_nodes(id),
    resource_ref        TEXT NOT NULL,
    resource_type       TEXT NOT NULL,
    current_state_json  TEXT NOT NULL CHECK (json_valid(current_state_json)),
    confidentiality     TEXT NOT NULL CHECK (confidentiality IN ('public','internal','confidential','secret')),
    residency           TEXT NOT NULL CHECK (residency IN ('any','trusted_nodes','origin_node')),
    source_observation_id TEXT REFERENCES observations(id),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    observed_at         INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE (workspace_id, resource_ref)
) STRICT;

CREATE TABLE memory_records (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    project_id          TEXT REFERENCES projects(id),
    statement           TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('active','stale','disputed','superseded','suspended','archived')),
    evidence_status     TEXT NOT NULL CHECK (evidence_status IN (
        'observed_once','repeated','corroborated','user_asserted','inferred','disputed'
    )),
    applicability_json  TEXT NOT NULL CHECK (json_valid(applicability_json)),
    provenance_json     TEXT NOT NULL CHECK (json_valid(provenance_json)),
    confidentiality     TEXT NOT NULL CHECK (confidentiality IN ('public','internal','confidential','secret')),
    residency           TEXT NOT NULL CHECK (residency IN ('any','trusted_nodes','origin_node')),
    trust               TEXT NOT NULL,
    supersedes_id       TEXT REFERENCES memory_records(id),
    last_verified_at    INTEGER,
    valid_until         INTEGER,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_memory_active ON memory_records(workspace_id, project_id, status, updated_at);

CREATE TABLE skills (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    logical_id          TEXT NOT NULL,
    version             INTEGER NOT NULL CHECK (version >= 1),
    skill_type          TEXT NOT NULL CHECK (skill_type IN ('operational','inspection','remediation')),
    lifecycle_state     TEXT NOT NULL CHECK (lifecycle_state IN (
        'candidate','quarantined','testing','verified','active','suspended','deprecated','archived','rejected'
    )),
    manifest_json       TEXT NOT NULL CHECK (json_valid(manifest_json)),
    content_hash        TEXT NOT NULL,
    qualification_json  TEXT NOT NULL CHECK (json_valid(qualification_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE (workspace_id, logical_id, version)
) STRICT;
CREATE UNIQUE INDEX idx_skill_active ON skills(workspace_id, logical_id) WHERE lifecycle_state = 'active';

CREATE TABLE workflows (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    logical_id          TEXT NOT NULL,
    version             INTEGER NOT NULL CHECK (version >= 1),
    lifecycle_state     TEXT NOT NULL CHECK (lifecycle_state IN (
        'candidate','quarantined','testing','verified','active','suspended','deprecated','archived','rejected'
    )),
    definition_json     TEXT NOT NULL CHECK (json_valid(definition_json)),
    content_hash        TEXT NOT NULL,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE (workspace_id, logical_id, version)
) STRICT;
CREATE UNIQUE INDEX idx_workflow_active ON workflows(workspace_id, logical_id) WHERE lifecycle_state = 'active';

CREATE TABLE knowledge_candidates (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    candidate_type      TEXT NOT NULL CHECK (candidate_type IN (
        'skill','workflow','inspection','remediation','memory','environment_update'
    )),
    state               TEXT NOT NULL CHECK (state IN (
        'proposed','quarantined','generalising','testing','validating','approved','published',
        'rejected','superseded','manual_review'
    )),
    evidence_json       TEXT NOT NULL CHECK (json_valid(evidence_json)),
    candidate_json      TEXT NOT NULL CHECK (json_valid(candidate_json)),
    taint_json          TEXT NOT NULL CHECK (json_valid(taint_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE routines (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    name                TEXT NOT NULL,
    definition_version  INTEGER NOT NULL CHECK (definition_version >= 1),
    status              TEXT NOT NULL CHECK (status IN ('active','paused','disabled','archived')),
    trigger_json        TEXT NOT NULL CHECK (json_valid(trigger_json)),
    policy_json         TEXT NOT NULL CHECK (json_valid(policy_json)),
    timezone            TEXT NOT NULL,
    created_by          TEXT NOT NULL REFERENCES principals(id),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE routine_occurrences (
    id                  TEXT PRIMARY KEY,
    routine_id          TEXT NOT NULL REFERENCES routines(id),
    definition_version  INTEGER NOT NULL,
    occurrence_key      TEXT NOT NULL,
    task_id             TEXT REFERENCES tasks(id),
    state               TEXT NOT NULL CHECK (state IN (
        'expected','instantiated','running','verifying','succeeded','retrying','failed',
        'skipped','cancelled','missed','superseded'
    )),
    intended_local_time TEXT,
    trigger_time_utc    INTEGER NOT NULL,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE (routine_id, occurrence_key)
) STRICT;

CREATE TABLE incidents (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    task_id             TEXT REFERENCES tasks(id),
    operation_id        TEXT REFERENCES operations(id),
    severity            TEXT NOT NULL CHECK (severity IN ('info','warning','high','critical')),
    state               TEXT NOT NULL CHECK (state IN (
        'open','classified','remediating','verifying','resolved','blocked','escalated','suppressed','recurring'
    )),
    incident_type       TEXT NOT NULL,
    summary             TEXT NOT NULL,
    details_json        TEXT NOT NULL CHECK (json_valid(details_json)),
    opened_at           INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    resolved_at         INTEGER
) STRICT;
CREATE INDEX idx_incidents_open ON incidents(state, severity, opened_at);

CREATE TABLE watchdog_states (
    id                  TEXT PRIMARY KEY,
    watchdog_name       TEXT NOT NULL UNIQUE,
    state_json          TEXT NOT NULL CHECK (json_valid(state_json)),
    heartbeat_at        INTEGER NOT NULL,
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE deliberations (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    task_id             TEXT REFERENCES tasks(id),
    state               TEXT NOT NULL CHECK (state IN ('created','proposing','critiquing','synthesizing','completed','cancelled','failed')),
    config_json         TEXT NOT NULL CHECK (json_valid(config_json)),
    budget_json         TEXT NOT NULL CHECK (json_valid(budget_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE plan_proposals (
    id                  TEXT PRIMARY KEY,
    deliberation_id     TEXT REFERENCES deliberations(id),
    task_id             TEXT REFERENCES tasks(id),
    principal_id        TEXT NOT NULL REFERENCES principals(id),
    proposal_json       TEXT NOT NULL CHECK (json_valid(proposal_json)),
    status              TEXT NOT NULL CHECK (status IN ('submitted','critiqued','selected','rejected','superseded')),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE notifications (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    principal_id        TEXT REFERENCES principals(id),
    notification_type   TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('pending','delivered','failed','dismissed')),
    payload_json        TEXT NOT NULL CHECK (json_valid(payload_json)),
    created_at          INTEGER NOT NULL,
    delivered_at        INTEGER
) STRICT;

CREATE TABLE events (
    sequence            INTEGER PRIMARY KEY AUTOINCREMENT,
    id                  TEXT NOT NULL UNIQUE,
    workspace_id        TEXT REFERENCES workspaces(id),
    event_type          TEXT NOT NULL,
    aggregate_type      TEXT NOT NULL,
    aggregate_id        TEXT NOT NULL,
    actor_principal_id  TEXT REFERENCES principals(id),
    request_id          TEXT,
    trace_id            TEXT,
    payload_json        TEXT NOT NULL CHECK (json_valid(payload_json)),
    occurred_at         INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_events_workspace_sequence ON events(workspace_id, sequence);
CREATE INDEX idx_events_aggregate ON events(aggregate_type, aggregate_id, sequence);
CREATE INDEX idx_events_type ON events(event_type, sequence);

CREATE TRIGGER events_no_update
BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'event ledger is append-only');
END;

CREATE TRIGGER events_no_delete
BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'event ledger is append-only');
END;

CREATE TRIGGER observations_no_update
BEFORE UPDATE ON observations
BEGIN
    SELECT RAISE(ABORT, 'observations are immutable');
END;

CREATE TRIGGER observations_no_delete
BEFORE DELETE ON observations
BEGIN
    SELECT RAISE(ABORT, 'observations are immutable');
END;

CREATE TRIGGER artifact_revisions_no_update
BEFORE UPDATE ON artifact_revisions
BEGIN
    SELECT RAISE(ABORT, 'artifact revisions are immutable');
END;

CREATE TRIGGER artifact_revisions_no_delete
BEFORE DELETE ON artifact_revisions
BEGIN
    SELECT RAISE(ABORT, 'artifact revisions are immutable');
END;

CREATE TABLE outbox_jobs (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    job_type            TEXT NOT NULL,
    payload_json        TEXT NOT NULL CHECK (json_valid(payload_json)),
    status              TEXT NOT NULL CHECK (status IN ('pending','claimed','succeeded','retry','failed')),
    available_at        INTEGER NOT NULL,
    claimed_by          TEXT,
    claim_expires_at    INTEGER,
    attempts            INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts        INTEGER NOT NULL DEFAULT 10 CHECK (max_attempts >= 1),
    last_error          TEXT,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_outbox_available ON outbox_jobs(status, available_at, claim_expires_at);

CREATE TABLE api_idempotency (
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    principal_id        TEXT NOT NULL REFERENCES principals(id),
    idempotency_key     TEXT NOT NULL,
    request_hash        TEXT NOT NULL,
    response_status     INTEGER,
    response_json       TEXT CHECK (response_json IS NULL OR json_valid(response_json)),
    created_at          INTEGER NOT NULL,
    expires_at          INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, principal_id, idempotency_key)
) STRICT;

-- Backfill circular FK-style reference is enforced in the domain layer because
-- SQLite cannot add a named FK to artifact_sessions.current_revision_id without
-- rebuilding the table. The referenced revision must belong to the same session.

