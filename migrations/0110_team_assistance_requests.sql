-- A recorded human wait is a request for help, never authority to resume.
CREATE TABLE team_assistance_requests (
 id TEXT PRIMARY KEY,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id),
 session_id TEXT NOT NULL REFERENCES team_sessions(id),
 plan_id TEXT NOT NULL REFERENCES team_plans(id),
 assignment_key TEXT NOT NULL,
 member_id TEXT NOT NULL REFERENCES team_members(id),
 task_id TEXT NOT NULL REFERENCES tasks(id),
 attempt_id TEXT NOT NULL REFERENCES task_attempts(id),
 run_id TEXT NOT NULL REFERENCES agent_worker_runs(id),
 wait_step_id TEXT NOT NULL UNIQUE REFERENCES agent_worker_steps(id),
 notice_message_id TEXT NOT NULL UNIQUE REFERENCES team_messages(id),
 author_principal_id TEXT NOT NULL REFERENCES principals(id),
 question TEXT NOT NULL CHECK(length(CAST(question AS BLOB)) BETWEEN 1 AND 512),
 created_at INTEGER NOT NULL,
 FOREIGN KEY(session_id,assignment_key) REFERENCES team_execution_assignments(session_id,assignment_key)
) STRICT;
CREATE INDEX idx_team_assistance_session ON team_assistance_requests(workspace_id,session_id,created_at,id);
-- Reverse ownership cannot be inferred from a shared session, nor require a
-- tenant filter to discover a historical inconsistent association.
CREATE INDEX idx_team_assistance_task ON team_assistance_requests(task_id,id);
CREATE INDEX idx_team_assistance_attempt ON team_assistance_requests(attempt_id,id);
CREATE INDEX idx_team_assistance_run ON team_assistance_requests(run_id,id);
CREATE TRIGGER team_assistance_request_immutable BEFORE UPDATE ON team_assistance_requests
BEGIN SELECT RAISE(ABORT,'Team assistance request identity and text are immutable'); END;
CREATE TRIGGER team_assistance_request_retained BEFORE DELETE ON team_assistance_requests
BEGIN SELECT RAISE(ABORT,'Team assistance requests must be retained'); END;
