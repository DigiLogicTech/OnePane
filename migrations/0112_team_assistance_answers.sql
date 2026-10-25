-- A final authored reply is durable input, never permission to resume work.
CREATE TABLE team_assistance_answers (
 request_id TEXT PRIMARY KEY REFERENCES team_assistance_requests(id),
 id TEXT NOT NULL UNIQUE,
 author_principal_id TEXT NOT NULL REFERENCES principals(id),
 idempotency_key TEXT NOT NULL CHECK(length(CAST(idempotency_key AS BLOB)) BETWEEN 1 AND 128),
 answer TEXT NOT NULL CHECK(length(CAST(answer AS BLOB)) BETWEEN 1 AND 4096),
 task_revision INTEGER NOT NULL CHECK(task_revision>0),
 worker_revision INTEGER NOT NULL CHECK(worker_revision>0),
 created_at INTEGER NOT NULL CHECK(created_at>0)
) STRICT;
CREATE TRIGGER team_assistance_answer_immutable BEFORE UPDATE ON team_assistance_answers
BEGIN SELECT RAISE(ABORT,'Team assistance answers are immutable'); END;
CREATE TRIGGER team_assistance_answer_retained BEFORE DELETE ON team_assistance_answers
BEGIN SELECT RAISE(ABORT,'Team assistance answers must be retained'); END;
