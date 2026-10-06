-- M30 messaging gateway + durable notification delivery.
CREATE TABLE gateway_connections (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id),
    preset_id       TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    delivery_mode   TEXT NOT NULL CHECK (delivery_mode IN ('native_http','smtp','relay')),
    credential_ref  TEXT,
    config_json     TEXT NOT NULL CHECK (json_valid(config_json)),
    status          TEXT NOT NULL CHECK (status IN ('active','disabled','error')),
    created_by      TEXT NOT NULL REFERENCES principals(id),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_gateway_connections_workspace ON gateway_connections(workspace_id,status,preset_id);

CREATE TABLE gateway_targets (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id),
    connection_id   TEXT NOT NULL REFERENCES gateway_connections(id),
    name            TEXT NOT NULL,
    address         TEXT NOT NULL,
    thread_ref      TEXT,
    config_json     TEXT NOT NULL CHECK (json_valid(config_json)),
    status          TEXT NOT NULL CHECK (status IN ('active','disabled')),
    revision        INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_gateway_targets_connection ON gateway_targets(connection_id,status);

CREATE TABLE notification_rules (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    name                TEXT NOT NULL,
    event_type          TEXT NOT NULL,
    aggregate_type      TEXT,
    aggregate_id        TEXT,
    routine_id          TEXT REFERENCES routines(id),
    target_id           TEXT NOT NULL REFERENCES gateway_targets(id),
    template_json       TEXT NOT NULL CHECK (json_valid(template_json)),
    status              TEXT NOT NULL CHECK (status IN ('active','paused','disabled')),
    created_by          TEXT NOT NULL REFERENCES principals(id),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_notification_rules_match ON notification_rules(workspace_id,status,event_type);

CREATE TABLE notification_deliveries (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id),
    rule_id             TEXT REFERENCES notification_rules(id),
    target_id           TEXT NOT NULL REFERENCES gateway_targets(id),
    source_event_id     TEXT REFERENCES events(id),
    source_event_seq    INTEGER REFERENCES events(sequence),
    idempotency_key     TEXT NOT NULL,
    message_json        TEXT NOT NULL CHECK (json_valid(message_json)),
    status              TEXT NOT NULL CHECK (status IN ('pending','sending','accepted','failed','unknown','cancelled')),
    attempts            INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts        INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts >= 1),
    next_attempt_at     INTEGER NOT NULL,
    operation_id        TEXT REFERENCES operations(id),
    verification_id     TEXT REFERENCES verifications(id),
    receipt_json        TEXT CHECK (receipt_json IS NULL OR json_valid(receipt_json)),
    last_error          TEXT,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE(workspace_id,idempotency_key)
) STRICT;
CREATE INDEX idx_notification_deliveries_due ON notification_deliveries(status,next_attempt_at,created_at);
CREATE INDEX idx_notification_deliveries_source ON notification_deliveries(source_event_seq,target_id);

CREATE TABLE gateway_event_cursor (
    id              INTEGER PRIMARY KEY CHECK (id=1),
    last_sequence   INTEGER NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    updated_at      INTEGER NOT NULL
) STRICT;
INSERT INTO gateway_event_cursor(id,last_sequence,updated_at) VALUES(1,0,0);
