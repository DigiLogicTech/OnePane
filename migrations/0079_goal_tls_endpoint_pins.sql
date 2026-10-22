-- Human-chosen pinned TLS *certificate* for ONE operator-declared TCP
-- endpoint. Certificate possession does not establish hardware identity,
-- network security or that the endpoint is an intended physical device.
CREATE TABLE project_goal_tls_endpoint_pins (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 declared_target_id TEXT NOT NULL REFERENCES project_goal_tcp_declared_target_bindings(id) ON DELETE RESTRICT,
 permit_id TEXT NOT NULL REFERENCES project_goal_tcp_probe_permits(id) ON DELETE RESTRICT,
 permit_revision INTEGER NOT NULL CHECK(permit_revision>=1),
 plan_hash TEXT NOT NULL,
 certificate_sha256 TEXT NOT NULL
  CHECK(length(certificate_sha256)=71 AND substr(certificate_sha256,1,7)='sha256:'),
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
CREATE UNIQUE INDEX idx_goal_one_live_tls_pin
 ON project_goal_tls_endpoint_pins(declared_target_id) WHERE status='approved';
CREATE INDEX idx_goal_tls_pin_history ON project_goal_tls_endpoint_pins
 (project_id,goal_id,approved_at DESC);
CREATE TRIGGER goal_tls_pin_no_delete BEFORE DELETE ON project_goal_tls_endpoint_pins
 BEGIN SELECT RAISE(ABORT,'TLS pin history is immutable'); END;
CREATE TRIGGER goal_tls_pin_immutable BEFORE UPDATE ON project_goal_tls_endpoint_pins
 WHEN NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id OR
  NEW.goal_id!=OLD.goal_id OR NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.declared_target_id!=OLD.declared_target_id OR NEW.permit_id!=OLD.permit_id OR
  NEW.permit_revision!=OLD.permit_revision OR NEW.plan_hash!=OLD.plan_hash OR
  NEW.certificate_sha256!=OLD.certificate_sha256 OR
  NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at OR
  NEW.expires_at!=OLD.expires_at OR NEW.revision!=OLD.revision+1 OR
  OLD.status='revoked' OR NEW.status!='revoked' OR
  NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at
 BEGIN SELECT RAISE(ABORT,'TLS pin scope cannot be rewritten'); END;

-- A claim is reserved BEFORE opening any socket, with a per-pin 5-minute
-- cadence limit even when TLS or network connections fail.
CREATE TABLE project_goal_tls_endpoint_checks(
 id TEXT PRIMARY KEY,
 pin_id TEXT NOT NULL REFERENCES project_goal_tls_endpoint_pins(id) ON DELETE RESTRICT,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 claimed_at INTEGER NOT NULL CHECK(claimed_at>0),
 cadence_window INTEGER NOT NULL CHECK(cadence_window>=0),
 status TEXT NOT NULL CHECK(status IN
  ('claimed','matched','mismatched','tls_failed','unavailable','cancelled')),
 completed_at INTEGER NOT NULL DEFAULT 0 CHECK(completed_at>=0),
 CHECK((status='claimed' AND completed_at=0) OR
  (status!='claimed' AND completed_at>=claimed_at)),
 UNIQUE(pin_id,cadence_window)
) STRICT;
CREATE INDEX idx_goal_tls_check_history ON project_goal_tls_endpoint_checks
 (project_id,goal_id,claimed_at DESC);
CREATE TRIGGER goal_tls_check_no_delete BEFORE DELETE ON project_goal_tls_endpoint_checks
 BEGIN SELECT RAISE(ABORT,'TLS check history is immutable'); END;
CREATE TRIGGER goal_tls_check_immutable BEFORE UPDATE ON project_goal_tls_endpoint_checks
 WHEN NEW.id!=OLD.id OR NEW.pin_id!=OLD.pin_id OR
  NEW.project_id!=OLD.project_id OR NEW.goal_id!=OLD.goal_id OR
  NEW.claimed_at!=OLD.claimed_at OR NEW.cadence_window!=OLD.cadence_window OR
  OLD.status!='claimed' OR NEW.status='claimed'
 BEGIN SELECT RAISE(ABORT,'TLS check cannot be rewritten'); END;
