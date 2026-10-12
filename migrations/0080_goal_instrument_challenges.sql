-- Out-of-band human-enrolled instrument public key: possession of an
-- independent Ed25519 key scoped to one human-approved TLS endpoint.
-- This does NOT attest a physical device or assert truthful measurements.
CREATE TABLE project_goal_instrument_keys (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 declared_target_id TEXT NOT NULL REFERENCES project_goal_tcp_declared_target_bindings(id) ON DELETE RESTRICT,
 tls_pin_id TEXT NOT NULL REFERENCES project_goal_tls_endpoint_pins(id) ON DELETE RESTRICT,
 plan_hash TEXT NOT NULL,
 public_key BLOB NOT NULL CHECK(length(public_key)=32),
 status TEXT NOT NULL CHECK(status IN ('approved','revoked')),
 revision INTEGER NOT NULL CHECK(revision>=1),
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>approved_at),
 revoked_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 revoked_at INTEGER NOT NULL DEFAULT 0 CHECK(revoked_at>=0),
 CHECK((status='approved' AND revoked_by IS NULL AND revoked_at=0)
  OR (status='revoked' AND revoked_by IS NOT NULL AND revoked_at>=approved_at))
) STRICT;
CREATE UNIQUE INDEX idx_instrument_one_live_key_per_tls_pin
 ON project_goal_instrument_keys(tls_pin_id) WHERE status='approved';
CREATE INDEX idx_instrument_keys_goal_history ON project_goal_instrument_keys
 (project_id,goal_id,approved_at DESC);
CREATE TRIGGER instrument_key_no_delete BEFORE DELETE ON project_goal_instrument_keys
 BEGIN SELECT RAISE(ABORT,'instrument identity audit is immutable'); END;
CREATE TRIGGER instrument_key_fixed_scope BEFORE UPDATE ON project_goal_instrument_keys
 WHEN NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id OR
  NEW.goal_id!=OLD.goal_id OR NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.declared_target_id!=OLD.declared_target_id OR NEW.tls_pin_id!=OLD.tls_pin_id OR
  NEW.plan_hash!=OLD.plan_hash OR NEW.public_key!=OLD.public_key OR
  NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at OR
  NEW.expires_at!=OLD.expires_at OR OLD.status='revoked' OR
  NEW.status!='revoked' OR NEW.revision!=OLD.revision+1 OR
  NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at
 BEGIN SELECT RAISE(ABORT,'instrument key scope cannot change'); END;

-- Challenges reserve a cryptographic nonce before any external response.
-- Their status can change exactly once. Expired challenges are retained
-- for replay exclusion; operators can issue at most 4/5min/key.
CREATE TABLE project_goal_instrument_challenges (
 id TEXT PRIMARY KEY,
 instrument_key_id TEXT NOT NULL REFERENCES project_goal_instrument_keys(id) ON DELETE RESTRICT,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 nonce BLOB NOT NULL UNIQUE CHECK(length(nonce)=32),
 issued_at INTEGER NOT NULL CHECK(issued_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>issued_at),
 status TEXT NOT NULL CHECK(status IN ('issued','accepted')),
 accepted_at INTEGER NOT NULL DEFAULT 0 CHECK(accepted_at>=0),
 CHECK((status='issued' AND accepted_at=0) OR
  (status='accepted' AND accepted_at>=issued_at))
) STRICT;
CREATE INDEX idx_instrument_challenges_budget ON project_goal_instrument_challenges
 (instrument_key_id,issued_at DESC);
CREATE TRIGGER instrument_challenge_no_delete BEFORE DELETE ON project_goal_instrument_challenges
 BEGIN SELECT RAISE(ABORT,'instrument challenge replay ledger is immutable'); END;
CREATE TRIGGER instrument_challenge_once BEFORE UPDATE ON project_goal_instrument_challenges
 WHEN NEW.id!=OLD.id OR NEW.instrument_key_id!=OLD.instrument_key_id OR
  NEW.project_id!=OLD.project_id OR NEW.goal_id!=OLD.goal_id OR
  NEW.nonce!=OLD.nonce OR NEW.issued_at!=OLD.issued_at OR
  NEW.expires_at!=OLD.expires_at OR OLD.status!='issued' OR NEW.status!='accepted'
 BEGIN SELECT RAISE(ABORT,'instrument challenge is single-use'); END;

CREATE TABLE project_goal_instrument_proofs (
 id TEXT PRIMARY KEY,
 instrument_key_id TEXT NOT NULL REFERENCES project_goal_instrument_keys(id) ON DELETE RESTRICT,
 challenge_id TEXT NOT NULL UNIQUE REFERENCES project_goal_instrument_challenges(id) ON DELETE RESTRICT,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 declared_target_id TEXT NOT NULL REFERENCES project_goal_tcp_declared_target_bindings(id) ON DELETE RESTRICT,
 tls_pin_id TEXT NOT NULL REFERENCES project_goal_tls_endpoint_pins(id) ON DELETE RESTRICT,
 signature_sha256 TEXT NOT NULL CHECK(length(signature_sha256)=71),
 signature BLOB NOT NULL CHECK(length(signature)=64),
 accepted_at INTEGER NOT NULL CHECK(accepted_at>0),
 proof_class TEXT NOT NULL DEFAULT 'human_bound_instrument_key_possession'
  CHECK(proof_class='human_bound_instrument_key_possession')
) STRICT;
CREATE INDEX idx_instrument_proofs_goal ON project_goal_instrument_proofs
 (project_id,goal_id,accepted_at DESC);
CREATE TRIGGER instrument_proof_no_delete BEFORE DELETE ON project_goal_instrument_proofs
 BEGIN SELECT RAISE(ABORT,'instrument proof ledger is immutable'); END;
CREATE TRIGGER instrument_proof_no_update BEFORE UPDATE ON project_goal_instrument_proofs
 BEGIN SELECT RAISE(ABORT,'instrument proof cannot be rewritten'); END;
