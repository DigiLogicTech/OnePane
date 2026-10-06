-- M32 Bot Runtime: provider/harness-native conversational bot surfaces.
-- Bot chat is intentionally separate from autonomous Task execution. A Bot may
-- retain its provider/harness-native tools and authority; OnePane records the
-- conversation but does not claim those side effects passed ToolGateway.
CREATE TABLE bot_connections (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id),
    preset_id       TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    source_kind     TEXT NOT NULL CHECK (source_kind IN ('harness_native','provider_hosted','relay')),
    access_mode     TEXT NOT NULL CHECK (access_mode IN ('harness_api','hosted_surface','relay_api')),
    source_ref      TEXT,
    base_url        TEXT,
    credential_ref  TEXT,
    config_json     TEXT NOT NULL CHECK (json_valid(config_json)),
    status          TEXT NOT NULL CHECK (status IN ('active','disabled','error')),
    created_by      TEXT NOT NULL REFERENCES principals(id),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_bot_connections_workspace ON bot_connections(workspace_id,status,preset_id);

CREATE TABLE bot_profiles (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    connection_id       TEXT NOT NULL REFERENCES bot_connections(id) ON DELETE CASCADE,
    remote_bot_id       TEXT NOT NULL,
    display_name        TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    avatar_url          TEXT,
    launch_url          TEXT,
    capabilities_json   TEXT NOT NULL CHECK (json_valid(capabilities_json)),
    metadata_json       TEXT NOT NULL CHECK (json_valid(metadata_json)),
    status              TEXT NOT NULL CHECK (status IN ('active','hidden','disabled')),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE(connection_id,remote_bot_id)
) STRICT;
CREATE INDEX idx_bot_profiles_workspace ON bot_profiles(workspace_id,status,display_name);

CREATE TABLE bot_sessions (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    bot_id              TEXT NOT NULL REFERENCES bot_profiles(id) ON DELETE CASCADE,
    remote_session_id   TEXT,
    title               TEXT NOT NULL DEFAULT '',
    session_kind        TEXT NOT NULL CHECK (session_kind IN ('canonical','scratch')),
    status              TEXT NOT NULL CHECK (status IN ('active','archived','error')),
    created_by          TEXT NOT NULL REFERENCES principals(id),
    metadata_json       TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    last_message_at     INTEGER
) STRICT;
CREATE UNIQUE INDEX uq_bot_canonical_session ON bot_sessions(bot_id,created_by) WHERE session_kind='canonical' AND status='active';
CREATE INDEX idx_bot_sessions_workspace ON bot_sessions(workspace_id,status,updated_at);

CREATE TABLE bot_messages (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    session_id          TEXT NOT NULL REFERENCES bot_sessions(id) ON DELETE CASCADE,
    role                TEXT NOT NULL CHECK (role IN ('user','assistant','system','tool')),
    content_json        TEXT NOT NULL CHECK (json_valid(content_json)),
    remote_message_id   TEXT,
    delivery_status     TEXT NOT NULL CHECK (delivery_status IN ('local','pending','sent','received','failed','unknown')),
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_bot_messages_session ON bot_messages(session_id,created_at,id);

CREATE TABLE bot_turns (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    session_id          TEXT NOT NULL REFERENCES bot_sessions(id) ON DELETE CASCADE,
    user_message_id     TEXT NOT NULL REFERENCES bot_messages(id),
    response_message_id TEXT REFERENCES bot_messages(id),
    status              TEXT NOT NULL CHECK (status IN ('pending','sending','succeeded','failed','unknown','cancelled')),
    idempotency_key     TEXT NOT NULL,
    remote_run_id       TEXT,
    receipt_json        TEXT CHECK (receipt_json IS NULL OR json_valid(receipt_json)),
    error_text          TEXT,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    completed_at        INTEGER,
    UNIQUE(workspace_id,idempotency_key)
) STRICT;
CREATE INDEX idx_bot_turns_status ON bot_turns(status,created_at,id);
