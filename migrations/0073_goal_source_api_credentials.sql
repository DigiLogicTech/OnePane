-- RC11: a machine bearer credential is usable for Level 5 Goal evidence
-- ONLY when a human has bound its specific ID to one exact signer and Goal.
-- The bearer token itself is never stored here or anywhere in cleartext.
CREATE TABLE project_goal_source_credential_grants(
 credential_id TEXT PRIMARY KEY REFERENCES api_credentials(id) ON DELETE RESTRICT,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
 signer_id TEXT NOT NULL REFERENCES project_goal_observation_signers(id) ON DELETE RESTRICT,
 source_principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>approved_at),
 revoked_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 revoked_at INTEGER NOT NULL DEFAULT 0 CHECK(revoked_at>=0),
 CHECK ((revoked_by IS NULL AND revoked_at=0) OR
        (revoked_by IS NOT NULL AND revoked_at>=approved_at))
) STRICT;
CREATE INDEX idx_goal_source_credentials_signer
 ON project_goal_source_credential_grants(signer_id,expires_at DESC);
CREATE TRIGGER goal_source_credential_no_delete
 BEFORE DELETE ON project_goal_source_credential_grants
 BEGIN SELECT RAISE(ABORT,'source credential grant history is immutable'); END;
CREATE TRIGGER goal_source_credential_immutable
 BEFORE UPDATE ON project_goal_source_credential_grants
 WHEN NEW.credential_id!=OLD.credential_id OR
  NEW.project_id!=OLD.project_id OR NEW.goal_id!=OLD.goal_id OR
  NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.workspace_id!=OLD.workspace_id OR
  NEW.signer_id!=OLD.signer_id OR
  NEW.source_principal_id!=OLD.source_principal_id OR
  NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at OR
  NEW.expires_at!=OLD.expires_at OR OLD.revoked_by IS NOT NULL OR
  (NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at)
 BEGIN SELECT RAISE(ABORT,'source credential grant scope cannot change'); END;
