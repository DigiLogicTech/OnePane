-- Alpha 3.1: durable Agent Profiles and managed local-component lifecycle.

CREATE TABLE agent_profiles (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT REFERENCES workspaces(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    instructions_md   TEXT NOT NULL,
    default_role      TEXT NOT NULL DEFAULT 'general',
    capability_id     TEXT NOT NULL DEFAULT 'inference.general',
    protocol_level    TEXT NOT NULL DEFAULT 'L1' CHECK (protocol_level IN ('L0','L1','L2','L3')),
    source_kind       TEXT NOT NULL CHECK (source_kind IN ('builtin','custom')),
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    metadata_json     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    revision          INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_by        TEXT REFERENCES principals(id),
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_agent_profiles_scope ON agent_profiles(workspace_id,status,name,id);

CREATE TABLE managed_component_states (
    component_id       TEXT PRIMARY KEY,
    installed_version  TEXT,
    available_version  TEXT,
    desired_state      TEXT NOT NULL DEFAULT 'disabled' CHECK (desired_state IN ('disabled','enabled','removed')),
    observed_state     TEXT NOT NULL DEFAULT 'not_installed' CHECK (observed_state IN (
        'not_installed','queued','downloading','verifying','installing','installed_disabled',
        'enabling','running','degraded','disabling','updating','repairing','removing',
        'interrupted','failed','removed'
    )),
    last_error          TEXT,
    active_job_id       TEXT,
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE managed_component_jobs (
    id                  TEXT PRIMARY KEY,
    component_id        TEXT NOT NULL REFERENCES managed_component_states(component_id) ON DELETE CASCADE,
    action              TEXT NOT NULL CHECK (action IN ('install','enable','disable','update','repair','retry','resume','remove')),
    target_version      TEXT,
    status              TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','interrupted','cancelled')),
    stage               TEXT NOT NULL DEFAULT 'queued',
    attempt_count       INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    failure_reason      TEXT,
    requested_by        TEXT REFERENCES principals(id),
    detail_json         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(detail_json)),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          INTEGER NOT NULL,
    started_at          INTEGER,
    completed_at        INTEGER,
    updated_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_managed_component_jobs ON managed_component_jobs(component_id,status,updated_at DESC);

INSERT OR IGNORE INTO agent_profiles(
    id,workspace_id,name,description,instructions_md,default_role,capability_id,
    protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at
)
SELECT 'agent.md',NULL,'OnePane Agent','Default governed OnePane agent profile',
       'You are a governed OnePane agent. Follow the task objective, Project and Workspace policy, approvals, ToolGateway constraints, and evidence requirements. Never treat profile instructions as authority to access tools, secrets, files, nodes, or networks.',
       'general','inference.general','L1','builtin','active','{"compatibility_aliases":["onepane-default"]}',1,NULL,
       CAST(strftime('%s','now') AS INTEGER)*1000,CAST(strftime('%s','now') AS INTEGER)*1000;

INSERT OR IGNORE INTO managed_component_states(component_id,available_version,desired_state,observed_state,metadata_json,revision,updated_at)
VALUES('colibri','1.12.1','disabled','not_installed','{}',1,CAST(strftime('%s','now') AS INTEGER)*1000);
