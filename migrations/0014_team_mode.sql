-- M31 Team Mode: persistent human+agent teams, pre-task deliberation and plan-gated execution.
CREATE TABLE teams (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id),
    name            TEXT NOT NULL,
    purpose         TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL CHECK (status IN ('active','archived')),
    created_by      TEXT NOT NULL REFERENCES principals(id),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_teams_workspace ON teams(workspace_id,status,name);

CREATE TABLE team_members (
    id                  TEXT PRIMARY KEY,
    team_id             TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    principal_id        TEXT REFERENCES principals(id),
    member_kind         TEXT NOT NULL CHECK (member_kind IN ('human','agent','supervisor')),
    display_name        TEXT NOT NULL,
    role_name           TEXT NOT NULL,
    capability_id       TEXT NOT NULL DEFAULT 'inference.general',
    protocol_level      TEXT NOT NULL DEFAULT 'L1' CHECK (protocol_level IN ('L0','L1','L2','L3')),
    route_policy_json   TEXT NOT NULL CHECK (json_valid(route_policy_json)),
    ordinal             INTEGER NOT NULL DEFAULT 0,
    status              TEXT NOT NULL CHECK (status IN ('active','disabled')),
    config_json         TEXT NOT NULL CHECK (json_valid(config_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE(team_id,principal_id)
) STRICT;
CREATE INDEX idx_team_members_team ON team_members(team_id,status,ordinal,id);

CREATE TABLE team_sessions (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    team_id             TEXT NOT NULL REFERENCES teams(id),
    task_id             TEXT NOT NULL UNIQUE REFERENCES tasks(id),
    status              TEXT NOT NULL CHECK (status IN (
        'assembling','deliberating','plan_proposed','plan_accepted','executing',
        'paused_for_deliberation','team_review','completed','cancelled'
    )),
    accepted_plan_id    TEXT,
    gateway_target_id   TEXT REFERENCES gateway_targets(id),
    round_number        INTEGER NOT NULL DEFAULT 0 CHECK (round_number >= 0),
    config_json         TEXT NOT NULL CHECK (json_valid(config_json)),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_by          TEXT NOT NULL REFERENCES principals(id),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_team_sessions_team ON team_sessions(team_id,status,updated_at);
CREATE INDEX idx_team_sessions_workspace ON team_sessions(workspace_id,status,updated_at);

CREATE TABLE task_execution_profiles (
    task_id             TEXT PRIMARY KEY REFERENCES tasks(id),
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    execution_mode      TEXT NOT NULL CHECK (execution_mode IN ('direct','supervisor','team','council')),
    team_id             TEXT REFERENCES teams(id),
    team_session_id     TEXT REFERENCES team_sessions(id),
    config_json         TEXT NOT NULL CHECK (json_valid(config_json)),
    created_by          TEXT NOT NULL REFERENCES principals(id),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    CHECK (execution_mode <> 'team' OR (team_id IS NOT NULL AND team_session_id IS NOT NULL))
) STRICT;
CREATE INDEX idx_task_execution_profiles_mode ON task_execution_profiles(workspace_id,execution_mode);

CREATE TABLE team_messages (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    session_id          TEXT NOT NULL REFERENCES team_sessions(id) ON DELETE CASCADE,
    author_member_id    TEXT REFERENCES team_members(id),
    author_principal_id TEXT REFERENCES principals(id),
    message_kind        TEXT NOT NULL CHECK (message_kind IN ('human','agent','supervisor','system')),
    content_json        TEXT NOT NULL CHECK (json_valid(content_json)),
    reply_to_message_id TEXT REFERENCES team_messages(id),
    round_number        INTEGER NOT NULL CHECK (round_number >= 0),
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_team_messages_session ON team_messages(session_id,round_number,created_at,id);

CREATE TABLE team_plans (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    session_id              TEXT NOT NULL REFERENCES team_sessions(id) ON DELETE CASCADE,
    version                 INTEGER NOT NULL CHECK (version >= 1),
    status                  TEXT NOT NULL CHECK (status IN ('candidate','accepted','rejected','superseded')),
    plan_json               TEXT NOT NULL CHECK (json_valid(plan_json)),
    proposed_by_member_id   TEXT REFERENCES team_members(id),
    proposed_by_principal_id TEXT REFERENCES principals(id),
    accepted_by_principal_id TEXT REFERENCES principals(id),
    created_at              INTEGER NOT NULL,
    accepted_at             INTEGER,
    UNIQUE(session_id,version)
) STRICT;
CREATE INDEX idx_team_plans_session ON team_plans(session_id,status,version);

CREATE TABLE team_objections (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    session_id          TEXT NOT NULL REFERENCES team_sessions(id) ON DELETE CASCADE,
    plan_id             TEXT REFERENCES team_plans(id),
    raised_by_member_id TEXT REFERENCES team_members(id),
    severity            TEXT NOT NULL CHECK (severity IN ('note','concern','blocking','critical')),
    status              TEXT NOT NULL CHECK (status IN ('open','resolved','accepted_risk','dismissed')),
    summary             TEXT NOT NULL,
    detail_json         TEXT NOT NULL CHECK (json_valid(detail_json)),
    resolution_json     TEXT CHECK (resolution_json IS NULL OR json_valid(resolution_json)),
    resolved_by_principal_id TEXT REFERENCES principals(id),
    created_at          INTEGER NOT NULL,
    resolved_at         INTEGER
) STRICT;
CREATE INDEX idx_team_objections_session ON team_objections(session_id,status,severity,created_at);

CREATE TABLE team_decisions (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    session_id          TEXT NOT NULL REFERENCES team_sessions(id) ON DELETE CASCADE,
    decision_type       TEXT NOT NULL,
    decision_json       TEXT NOT NULL CHECK (json_valid(decision_json)),
    decided_by_principal_id TEXT NOT NULL REFERENCES principals(id),
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_team_decisions_session ON team_decisions(session_id,created_at,id);

CREATE TABLE team_turn_requests (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL REFERENCES workspaces(id),
    session_id              TEXT NOT NULL REFERENCES team_sessions(id) ON DELETE CASCADE,
    member_id               TEXT NOT NULL REFERENCES team_members(id),
    trigger_message_id      TEXT REFERENCES team_messages(id),
    status                  TEXT NOT NULL CHECK (status IN ('pending','running','succeeded','failed','blocked','cancelled')),
    selected_candidate_kind TEXT,
    selected_candidate_id   TEXT,
    response_message_id     TEXT REFERENCES team_messages(id),
    budget_reservation_id   TEXT REFERENCES budget_reservations(id),
    error_text              TEXT,
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    completed_at            INTEGER,
    UNIQUE(session_id,member_id,trigger_message_id)
) STRICT;
CREATE INDEX idx_team_turn_requests_due ON team_turn_requests(status,created_at,id);
