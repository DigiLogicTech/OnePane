-- RC11 Level 5 network-observation preparation.
-- Exact TCP connect target grants are human-owned, source-bound, short-lived.
-- This migration does NOT enable a probe scheduler or network connection.
-- A source name in monitoring_plan_json is NOT network authority.
CREATE TABLE project_goal_tcp_probe_permits (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 plan_hash TEXT NOT NULL CHECK(length(plan_hash)=71 AND substr(plan_hash,1,7)='sha256:'),
 probe_kind TEXT NOT NULL DEFAULT 'tcp_connect' CHECK(probe_kind='tcp_connect'),
 target_ipv4 TEXT NOT NULL CHECK(length(target_ipv4) BETWEEN 7 AND 15),
 target_port INTEGER NOT NULL CHECK(target_port BETWEEN 1 AND 65535),
 timeout_ms INTEGER NOT NULL CHECK(timeout_ms BETWEEN 100 AND 1500),
 effect_class TEXT NOT NULL DEFAULT 'network_tcp_connect'
   CHECK(effect_class='network_tcp_connect'),
 status TEXT NOT NULL DEFAULT 'approved' CHECK(status IN ('approved','revoked')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 revoked_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>approved_at),
 revoked_at INTEGER NOT NULL DEFAULT 0 CHECK(revoked_at>=0),
 CHECK((status='approved' AND revoked_by IS NULL AND revoked_at=0)
  OR (status='revoked' AND revoked_by IS NOT NULL AND revoked_at>=approved_at))
) STRICT;
-- A source cannot silently point to two network targets. Repointing a
-- source requires explicit revoke + new human approval.
CREATE UNIQUE INDEX idx_goal_tcp_probe_active_source
 ON project_goal_tcp_probe_permits(goal_id,source_id) WHERE status='approved';
CREATE INDEX idx_goal_tcp_probe_scope
 ON project_goal_tcp_probe_permits(project_id,goal_id,approved_at DESC);
CREATE TRIGGER goal_tcp_probe_permits_no_delete
 BEFORE DELETE ON project_goal_tcp_probe_permits
 BEGIN SELECT RAISE(ABORT,'network probe permits are retained for audit'); END;
