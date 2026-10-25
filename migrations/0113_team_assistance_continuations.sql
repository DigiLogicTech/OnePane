-- One applied answer per original human wait. A retained receipt establishes
-- provenance only: current execution admission and recovery remain independent.
CREATE UNIQUE INDEX idx_team_assistance_answer_identity ON team_assistance_answers(request_id,id);
-- Required answer context resolves the exact latest committed human wait even
-- after resource continuations and long unrelated Worker histories replace it.
CREATE INDEX idx_agent_worker_human_wait ON agent_worker_steps(run_id,step_number DESC) WHERE step_kind='human' AND status='waiting';
CREATE TABLE team_assistance_continuations (
 request_id TEXT PRIMARY KEY REFERENCES team_assistance_requests(id),
 id TEXT NOT NULL UNIQUE,
 answer_id TEXT NOT NULL UNIQUE,
 actor_principal_id TEXT NOT NULL REFERENCES principals(id),
 continue_step_id TEXT NOT NULL UNIQUE REFERENCES agent_worker_steps(id),
 task_revision INTEGER NOT NULL CHECK(task_revision>0),
 worker_revision INTEGER NOT NULL CHECK(worker_revision>0),
 created_at INTEGER NOT NULL CHECK(created_at>0),
 FOREIGN KEY(request_id,answer_id) REFERENCES team_assistance_answers(request_id,id)
) STRICT;
CREATE TRIGGER team_assistance_continuation_immutable BEFORE UPDATE ON team_assistance_continuations
BEGIN SELECT RAISE(ABORT,'Team assistance continuations are immutable'); END;
CREATE TRIGGER team_assistance_continuation_retained BEFORE DELETE ON team_assistance_continuations
BEGIN SELECT RAISE(ABORT,'Team assistance continuations must be retained'); END;
