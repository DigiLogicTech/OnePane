-- Alpha 3.2 reliability: immutable Team/Council run provenance.

CREATE TABLE team_session_manifests (
    session_id       TEXT PRIMARY KEY REFERENCES team_sessions(id) ON DELETE CASCADE,
    workspace_id     TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    team_id          TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    execution_mode   TEXT NOT NULL CHECK (execution_mode IN ('team','council')),
    research_mode    INTEGER NOT NULL DEFAULT 0 CHECK (research_mode IN (0,1)),
    snapshot_json    TEXT NOT NULL CHECK (json_valid(snapshot_json)),
    snapshot_sha256  TEXT NOT NULL CHECK (length(snapshot_sha256)=64),
    created_at       INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_team_session_manifests_workspace ON team_session_manifests(workspace_id,created_at);

CREATE TABLE team_session_seat_bindings (
    session_id              TEXT NOT NULL REFERENCES team_sessions(id) ON DELETE CASCADE,
    member_id               TEXT NOT NULL REFERENCES team_members(id) ON DELETE CASCADE,
    candidate_kind          TEXT NOT NULL CHECK (candidate_kind IN ('model_deployment','agent_runtime')),
    candidate_id            TEXT NOT NULL,
    candidate_snapshot_json TEXT NOT NULL CHECK (json_valid(candidate_snapshot_json)),
    created_at              INTEGER NOT NULL,
    PRIMARY KEY(session_id,member_id)
) STRICT;
CREATE INDEX idx_team_session_seat_bindings_candidate ON team_session_seat_bindings(candidate_kind,candidate_id);
