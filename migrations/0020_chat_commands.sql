-- OnePane chat/session slash-command durability.
-- Deliberately avoids a foreign key to the chat-session table because RC8 source
-- integration owns the authoritative session schema/name.

CREATE TABLE IF NOT EXISTS chat_session_controls (
    session_id TEXT PRIMARY KEY,
    busy_mode TEXT NOT NULL DEFAULT 'queue' CHECK (busy_mode IN ('queue','steer','interrupt')),
    approval_mode TEXT NOT NULL DEFAULT 'manual' CHECK (approval_mode IN ('manual','auto_authorized')),
    queue_paused INTEGER NOT NULL DEFAULT 0 CHECK (queue_paused IN (0,1)),
    model_override TEXT,
    agent_override TEXT,
    reasoning_effort TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_prompt_queue (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','delivering','delivered','cancelled','blocked','failed')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_prompt_queue_session_status
    ON chat_prompt_queue(session_id, status, position);
CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_prompt_queue_active_position
    ON chat_prompt_queue(session_id, position)
    WHERE status IN ('pending','delivering');

CREATE TABLE IF NOT EXISTS chat_steering_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    active_turn_id TEXT,
    guidance TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','injected','cancelled','failed')),
    created_at TEXT NOT NULL,
    injected_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_chat_steering_session_status
    ON chat_steering_messages(session_id, status, created_at);

CREATE TABLE IF NOT EXISTS chat_command_events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    command_name TEXT NOT NULL,
    command_args_json TEXT NOT NULL DEFAULT '[]',
    result TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_command_events_session_time
    ON chat_command_events(session_id, created_at);
