-- RC11 Level 5: immutable, operator-defined expectations for one approved
-- TCP handshake source. This is an evidence comparison, NOT verification of
-- device health, service correctness, TLS trust, or incident remediation.
CREATE TABLE project_goal_tcp_baseline_revisions (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 permit_id TEXT NOT NULL REFERENCES project_goal_tcp_probe_permits(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 plan_hash TEXT NOT NULL CHECK(length(plan_hash)=71 AND substr(plan_hash,1,7)='sha256:'),
 expected_tcp TEXT NOT NULL CHECK(expected_tcp IN ('connected','not_connected')),
 revision INTEGER NOT NULL CHECK(revision>0),
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 UNIQUE(goal_id,source_id,revision),
 UNIQUE(goal_id,source_id,id,revision)
) STRICT;
CREATE TABLE project_goal_tcp_baseline_heads (
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 revision INTEGER NOT NULL CHECK(revision>0),
 baseline_id TEXT NOT NULL REFERENCES project_goal_tcp_baseline_revisions(id) ON DELETE RESTRICT,
 PRIMARY KEY(goal_id,source_id),
 FOREIGN KEY(goal_id,source_id,baseline_id,revision)
   REFERENCES project_goal_tcp_baseline_revisions(goal_id,source_id,id,revision)
) STRICT;
CREATE TRIGGER goal_tcp_baselines_immutable_update
 BEFORE UPDATE ON project_goal_tcp_baseline_revisions
 BEGIN SELECT RAISE(ABORT,'TCP baseline revisions are immutable'); END;
CREATE TRIGGER goal_tcp_baselines_immutable_delete
 BEFORE DELETE ON project_goal_tcp_baseline_revisions
 BEGIN SELECT RAISE(ABORT,'TCP baseline revisions cannot be deleted'); END;
CREATE INDEX idx_goal_tcp_baseline_history
 ON project_goal_tcp_baseline_revisions(project_id,goal_id,source_id,revision DESC);
