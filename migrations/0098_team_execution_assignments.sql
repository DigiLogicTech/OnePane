-- Assignment identity and reviewed dependency ownership are immutable. Task,
-- Attempt, Worker, gateway and verification records own execution lifecycle.
CREATE TABLE team_assignment_roots (
 session_id TEXT PRIMARY KEY REFERENCES team_sessions(id),
 plan_id TEXT NOT NULL REFERENCES team_plans(id),
 task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
 completion_sha256 TEXT NOT NULL,
 prior_dependencies_json TEXT NOT NULL CHECK(json_valid(prior_dependencies_json))
) STRICT;
CREATE TRIGGER team_assignment_root_immutable BEFORE UPDATE ON team_assignment_roots
BEGIN SELECT RAISE(ABORT,'Accepted Team execution roots are immutable'); END;
CREATE TRIGGER team_assignment_root_retained BEFORE DELETE ON team_assignment_roots
BEGIN SELECT RAISE(ABORT,'Accepted Team execution roots must be retained'); END;
CREATE TABLE team_execution_assignments (
 session_id TEXT NOT NULL REFERENCES team_sessions(id),
 plan_id TEXT NOT NULL REFERENCES team_plans(id),
 assignment_key TEXT NOT NULL,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id),
 member_id TEXT NOT NULL REFERENCES team_members(id),
 task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
 parent_task_id TEXT NOT NULL REFERENCES tasks(id),
 depends_on_json TEXT NOT NULL CHECK(json_valid(depends_on_json)),
 completion_sha256 TEXT NOT NULL,
 profile_id TEXT NOT NULL,
 profile_revision INTEGER NOT NULL,
 created_at INTEGER NOT NULL,
 PRIMARY KEY(session_id,assignment_key)
) STRICT;
CREATE INDEX idx_team_assignment_plan ON team_execution_assignments(plan_id);
CREATE TRIGGER team_assignment_immutable BEFORE UPDATE ON team_execution_assignments
BEGIN SELECT RAISE(ABORT,'Accepted Team assignments are immutable'); END;
CREATE TRIGGER team_assignment_retained BEFORE DELETE ON team_execution_assignments
BEGIN SELECT RAISE(ABORT,'Accepted Team assignment history must be retained'); END;
CREATE TABLE team_assignment_notices (
 task_id TEXT PRIMARY KEY REFERENCES tasks(id),
 checkpoint_id TEXT NOT NULL REFERENCES checkpoints(id),
 message_id TEXT NOT NULL UNIQUE REFERENCES team_messages(id)
) STRICT;
