-- RC11 L5: optional cryptographic origin proofs for already recorded
-- immutable Observations. A SHA-256 checksum alone is NOT source authentication.
-- No existing human-attested receipt is upgraded by this migration.
CREATE TABLE project_goal_observation_signers (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 source_principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 plan_hash TEXT NOT NULL CHECK(length(plan_hash)=71 AND substr(plan_hash,1,7)='sha256:'),
 algorithm TEXT NOT NULL DEFAULT 'ed25519' CHECK(algorithm='ed25519'),
 public_key BLOB NOT NULL CHECK(length(public_key)=32),
 status TEXT NOT NULL CHECK(status IN ('approved','revoked')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 last_sequence INTEGER NOT NULL DEFAULT 0 CHECK(last_sequence>=0),
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>approved_at),
 revoked_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 revoked_at INTEGER NOT NULL DEFAULT 0 CHECK(revoked_at>=0),
 UNIQUE(goal_id,source_id,public_key),
 CHECK((status='approved' AND revoked_by IS NULL AND revoked_at=0)
  OR(status='revoked' AND revoked_by IS NOT NULL AND revoked_at>=approved_at))
) STRICT;
CREATE INDEX idx_goal_observation_signer_lookup
 ON project_goal_observation_signers(project_id,goal_id,source_id,status);
CREATE TRIGGER goal_observation_signer_no_delete
 BEFORE DELETE ON project_goal_observation_signers
 BEGIN SELECT RAISE(ABORT,'signed source identity must be retained'); END;
CREATE TRIGGER goal_observation_signer_immutable_scope
 BEFORE UPDATE ON project_goal_observation_signers
 WHEN NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id OR
  NEW.goal_id!=OLD.goal_id OR NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.source_id!=OLD.source_id OR NEW.source_principal_id!=OLD.source_principal_id OR
  NEW.plan_hash!=OLD.plan_hash OR NEW.algorithm!=OLD.algorithm OR
  NEW.public_key!=OLD.public_key OR NEW.approved_by!=OLD.approved_by OR
  NEW.approved_at!=OLD.approved_at OR NEW.expires_at!=OLD.expires_at OR
  NEW.revision!=OLD.revision+1 OR NEW.last_sequence<OLD.last_sequence OR
  OLD.status='revoked' OR
  (NEW.status='approved' AND
   (NEW.revoked_by IS NOT NULL OR NEW.revoked_at!=0)) OR
  (NEW.status='revoked' AND
   (NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at))
 BEGIN SELECT RAISE(ABORT,'signed source identity/revocation is immutable'); END;

CREATE TABLE project_goal_signed_observation_proofs (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 signer_id TEXT NOT NULL REFERENCES project_goal_observation_signers(id) ON DELETE RESTRICT,
 observation_id TEXT NOT NULL REFERENCES observations(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL,
 integrity_hash TEXT NOT NULL CHECK(length(integrity_hash)=71 AND substr(integrity_hash,1,7)='sha256:'),
 sequence INTEGER NOT NULL CHECK(sequence>0),
 signature BLOB NOT NULL CHECK(length(signature)=64),
 observed_at INTEGER NOT NULL CHECK(observed_at>0),
 accepted_at INTEGER NOT NULL CHECK(accepted_at>=observed_at),
 UNIQUE(signer_id,sequence),
 UNIQUE(goal_id,observation_id)
) STRICT;
CREATE INDEX idx_goal_signed_observation_history
 ON project_goal_signed_observation_proofs(goal_id,source_id,accepted_at DESC);
CREATE TRIGGER goal_signed_observation_no_update
 BEFORE UPDATE ON project_goal_signed_observation_proofs
 BEGIN SELECT RAISE(ABORT,'signed observation proof is immutable'); END;
CREATE TRIGGER goal_signed_observation_no_delete
 BEFORE DELETE ON project_goal_signed_observation_proofs
 BEGIN SELECT RAISE(ABORT,'signed observation proof is immutable'); END;
