-- RC11 P0 Agent policy v1: immutable revisions, CAS-controlled head.
-- A policy may narrow the Project/Workspace grants, never replace them.
CREATE TABLE agent_policy_revisions (
  profile_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision>0),
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
  policy_json TEXT NOT NULL CHECK(json_valid(policy_json) AND json_type(policy_json)='object'),
  issued_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
  issued_at INTEGER NOT NULL CHECK (issued_at>0),
  PRIMARY KEY(profile_id,revision)
) STRICT;
CREATE TABLE agent_policy_heads (
  profile_id TEXT PRIMARY KEY REFERENCES agent_profiles(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision>0),
  FOREIGN KEY(profile_id,revision) REFERENCES agent_policy_revisions(profile_id,revision)
) STRICT;
CREATE TRIGGER agent_policy_revisions_no_update BEFORE UPDATE ON agent_policy_revisions
 BEGIN SELECT RAISE(ABORT,'Agent policy revisions are immutable'); END;
CREATE TRIGGER agent_policy_revisions_no_delete BEFORE DELETE ON agent_policy_revisions
 BEGIN SELECT RAISE(ABORT,'Agent policy revisions are immutable'); END;
CREATE INDEX idx_agent_policy_rev_workspace ON agent_policy_revisions(workspace_id,profile_id,revision);
