-- M9 additive schema for bring-your-own external agent harnesses/runtimes.
-- The frozen 0001 schema remains unchanged. External runtimes are deliberately
-- separate from Model/ModelDeployment: a runtime such as Hermes may orchestrate
-- models and agents, but it is not itself inference compute.

CREATE TABLE agent_runtime_connections (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT REFERENCES workspaces(id),
    node_id             TEXT REFERENCES harness_nodes(id),
    runtime_kind        TEXT NOT NULL,
    display_name        TEXT NOT NULL,
    adapter_name        TEXT NOT NULL,
    adapter_version     TEXT NOT NULL,
    endpoint_json       TEXT NOT NULL CHECK (json_valid(endpoint_json)),
    auth_type           TEXT NOT NULL,
    secret_ref          TEXT,
    status              TEXT NOT NULL CHECK (status IN (
        'registered','connected','degraded','reauth_required','unavailable','disabled','revoked'
    )),
    trust_state         TEXT NOT NULL CHECK (trust_state IN (
        'untrusted','user_trusted','trusted_adapter','revoked'
    )),
    operating_mode      TEXT NOT NULL CHECK (operating_mode IN (
        'proposal_only','gateway_mediated','unmanaged'
    )),
    protocol_json       TEXT NOT NULL CHECK (json_valid(protocol_json)),
    capabilities_json   TEXT NOT NULL CHECK (json_valid(capabilities_json)),
    data_policy_json    TEXT NOT NULL CHECK (json_valid(data_policy_json)),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE INDEX idx_agent_runtime_workspace
ON agent_runtime_connections(workspace_id, status);

CREATE INDEX idx_agent_runtime_adapter
ON agent_runtime_connections(adapter_name, adapter_version, status);
