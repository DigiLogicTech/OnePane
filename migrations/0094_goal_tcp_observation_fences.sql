-- Extra operator stop fence for the existing, separately approved
-- unattended TCP observer. No old grant becomes network authority.
CREATE TABLE project_goal_tcp_observation_fences (
 goal_id TEXT PRIMARY KEY REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 status TEXT NOT NULL CHECK(status IN ('paused','running')),
 revision INTEGER NOT NULL CHECK(revision>=1),
 changed_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 changed_at INTEGER NOT NULL CHECK(changed_at>0)
) STRICT;
CREATE INDEX idx_goal_tcp_observer_fence_by_project
 ON project_goal_tcp_observation_fences(project_id,goal_id,status);
CREATE TRIGGER goal_tcp_observer_fence_no_delete
 BEFORE DELETE ON project_goal_tcp_observation_fences
 BEGIN SELECT RAISE(ABORT,'observer fence history is retained'); END;
CREATE TRIGGER goal_tcp_observer_fence_scope_no_rewrite
 BEFORE UPDATE ON project_goal_tcp_observation_fences
 WHEN NEW.goal_id!=OLD.goal_id OR NEW.project_id!=OLD.project_id
  OR NEW.project_workspace_id!=OLD.project_workspace_id
  OR NEW.revision!=OLD.revision+1 OR NEW.status=OLD.status
  OR NEW.changed_at<OLD.changed_at
 BEGIN SELECT RAISE(ABORT,'observer fence scope or transition invalid'); END;
