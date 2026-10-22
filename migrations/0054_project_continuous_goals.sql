-- RC11: durable Level 5 goal registry. Additive migration only.
-- NO historical Project is converted or activated by this migration.
-- A configured objective is not equivalent to an observed healthy state.
CREATE TABLE project_continuous_goals (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 project_workspace_id TEXT REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 objective TEXT NOT NULL CHECK(length(trim(objective)) BETWEEN 1 AND 8192),
 monitoring_plan_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(monitoring_plan_json)),
 status TEXT NOT NULL DEFAULT 'configured'
  CHECK(status IN ('configured','active','paused','retired')),
 health TEXT NOT NULL DEFAULT 'unobserved'
  CHECK(health IN ('unobserved','healthy','degraded','attention','unknown')),
 cadence_minutes INTEGER NOT NULL DEFAULT 60
  CHECK(cadence_minutes BETWEEN 5 AND 10080),
 last_checked_at INTEGER NOT NULL DEFAULT 0 CHECK(last_checked_at>=0),
 last_verified_at INTEGER NOT NULL DEFAULT 0 CHECK(last_verified_at>=0),
 next_due_at INTEGER NOT NULL DEFAULT 0 CHECK(next_due_at>=0),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>=1),
 created_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 created_at INTEGER NOT NULL CHECK(created_at>0),
 updated_at INTEGER NOT NULL CHECK(updated_at>=created_at),
 CHECK(status!='retired' OR next_due_at=0),
 CHECK(health!='healthy' OR last_verified_at>0)
) STRICT;
CREATE INDEX idx_continuous_goals_due ON project_continuous_goals(status,next_due_at);
CREATE INDEX idx_continuous_goals_project ON project_continuous_goals(project_id,created_at);
