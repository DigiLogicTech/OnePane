-- Explicit human-mediated Research Council Chair proposals and approvals.
-- Chair turns are distinct from research seats; they do not affect round
-- completion counts or permit inference/task execution.
CREATE TABLE manual_web_council_chair_turns (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES team_sessions(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  member_id TEXT NOT NULL REFERENCES team_members(id),
  provider_id TEXT NOT NULL,
  model_label TEXT NOT NULL,
  stage TEXT NOT NULL CHECK(stage IN ('agenda','review')),
  after_round INTEGER NOT NULL CHECK(after_round>=0),
  prompt_text TEXT NOT NULL,
  prompt_sha256 TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('awaiting_input','awaiting_approval','approved')),
  response_text TEXT,
  response_sha256 TEXT,
  submitted_by TEXT REFERENCES principals(id),
  approved_text TEXT,
  approved_sha256 TEXT,
  approved_by TEXT REFERENCES principals(id),
  created_at INTEGER NOT NULL,
  submitted_at INTEGER,
  approved_at INTEGER,
  UNIQUE(session_id,stage,after_round),
  CHECK(length(prompt_text)>0),
  CHECK((status='awaiting_input' AND response_text IS NULL)
    OR (status IN ('awaiting_approval','approved') AND response_text IS NOT NULL)),
  CHECK((status!='approved' AND approved_text IS NULL)
    OR (status='approved' AND approved_text IS NOT NULL AND approved_by IS NOT NULL))
) STRICT;
CREATE INDEX idx_manual_web_chair_workspace
 ON manual_web_council_chair_turns(workspace_id,status,created_at);
