-- A scheduling hold never rewrites the human-accepted assignment graph.
CREATE TABLE team_execution_holds (
 id TEXT PRIMARY KEY,
 session_id TEXT NOT NULL REFERENCES team_sessions(id),
 plan_id TEXT NOT NULL REFERENCES team_plans(id),
 root_task_id TEXT NOT NULL REFERENCES tasks(id),
 created_by TEXT NOT NULL REFERENCES principals(id),
 created_at INTEGER NOT NULL,
 released_by TEXT REFERENCES principals(id),
 released_at INTEGER
) STRICT;
CREATE UNIQUE INDEX idx_team_active_hold ON team_execution_holds(session_id) WHERE released_at IS NULL;
CREATE TABLE team_execution_held_tasks (
 hold_id TEXT NOT NULL REFERENCES team_execution_holds(id),
 task_id TEXT NOT NULL REFERENCES tasks(id),
 previous_state TEXT NOT NULL CHECK(previous_state IN ('ready','running')),
 PRIMARY KEY(hold_id,task_id)
) STRICT;
