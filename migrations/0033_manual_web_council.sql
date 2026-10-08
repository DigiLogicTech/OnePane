-- Optional operator-mediated ChatGPT Web seat: no browser automation or API credentials.
-- A blocked team_turn_request remains pending until a human submits a response.
CREATE TABLE manual_web_council_turns (
    turn_id TEXT PRIMARY KEY REFERENCES team_turn_requests(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES team_sessions(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    member_id TEXT NOT NULL REFERENCES team_members(id),
    provider_id TEXT NOT NULL,
    model_label TEXT NOT NULL,
    conversation_generation INTEGER NOT NULL DEFAULT 1 CHECK(conversation_generation >= 1),
    prompt_text TEXT NOT NULL,
    prompt_sha256 TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('awaiting_input','submitted')),
    response_text TEXT,
    response_sha256 TEXT,
    submitted_by TEXT REFERENCES principals(id),
    created_at INTEGER NOT NULL,
    submitted_at INTEGER,
    CHECK(length(prompt_text)>0),
    CHECK((status='awaiting_input' AND response_text IS NULL)
       OR (status='submitted' AND response_text IS NOT NULL AND submitted_by IS NOT NULL))
) STRICT;
CREATE INDEX idx_manual_web_council_pending
ON manual_web_council_turns(workspace_id,status,created_at,turn_id);
