-- RC11 P0: a separately operator-approved mTLS Node telemetry lane.
-- A successfully pinned peer TLS connection authenticates the transmitting
-- Node *connection*, not its physical device, sensor truth or Goal health.
-- Neither this grant nor a receipt is a source-signed Observation proof.
CREATE TABLE project_goal_node_measurement_grants (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK(length(source_id) BETWEEN 1 AND 128),
 peer_node_id TEXT NOT NULL REFERENCES harness_nodes(id) ON DELETE RESTRICT,
 peer_tls_fingerprint TEXT NOT NULL CHECK(substr(peer_tls_fingerprint,1,7)='sha256:'),
 status TEXT NOT NULL CHECK(status IN ('approved','revoked')),
 revision INTEGER NOT NULL CHECK(revision>=1),
 last_sequence INTEGER NOT NULL DEFAULT 0 CHECK(last_sequence>=0),
 approved_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 approved_at INTEGER NOT NULL CHECK(approved_at>0),
 expires_at INTEGER NOT NULL CHECK(expires_at>approved_at),
 revoked_by TEXT REFERENCES principals(id) ON DELETE RESTRICT,
 revoked_at INTEGER NOT NULL DEFAULT 0 CHECK(revoked_at>=0),
 CHECK((status='approved' AND revoked_by IS NULL AND revoked_at=0)
  OR(status='revoked' AND revoked_by IS NOT NULL AND revoked_at>=approved_at))
) STRICT;
CREATE UNIQUE INDEX idx_goal_node_measurement_active
 ON project_goal_node_measurement_grants(goal_id,source_id,peer_node_id)
 WHERE status='approved';
CREATE INDEX idx_goal_node_measurement_grants_goal
 ON project_goal_node_measurement_grants(project_id,goal_id,approved_at DESC);
CREATE TRIGGER goal_node_measurement_grant_no_delete
 BEFORE DELETE ON project_goal_node_measurement_grants
 BEGIN SELECT RAISE(ABORT,'Node transport grant history is immutable'); END;
CREATE TRIGGER goal_node_measurement_grant_immutable
 BEFORE UPDATE ON project_goal_node_measurement_grants
 WHEN NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id OR
  NEW.goal_id!=OLD.goal_id OR NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.source_id!=OLD.source_id OR NEW.peer_node_id!=OLD.peer_node_id OR
  NEW.peer_tls_fingerprint!=OLD.peer_tls_fingerprint OR
  NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at OR
  NEW.expires_at!=OLD.expires_at OR NEW.revision!=OLD.revision+1 OR
  NEW.last_sequence<OLD.last_sequence OR OLD.status='revoked' OR
  (NEW.status='revoked' AND (NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at))
 BEGIN SELECT RAISE(ABORT,'Node transport grant scope immutable'); END;

-- Only bounded self-reported connectivity outcomes are admitted here.
-- No host name, target address, raw sensor JSON, prompt, arbitrary device
-- identifiers, or network scan output is stored or echoed.
CREATE TABLE project_goal_node_measurement_receipts (
 id TEXT PRIMARY KEY,
 grant_id TEXT NOT NULL REFERENCES project_goal_node_measurement_grants(id) ON DELETE RESTRICT,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 peer_node_id TEXT NOT NULL REFERENCES harness_nodes(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL,
 sequence INTEGER NOT NULL CHECK(sequence>0),
 signal_type TEXT NOT NULL CHECK(signal_type='peer_connectivity'),
 outcome TEXT NOT NULL CHECK(outcome IN ('reachable','unreachable','unknown')),
 observed_at INTEGER NOT NULL CHECK(observed_at>0),
 received_at INTEGER NOT NULL CHECK(received_at>=observed_at),
 transport_kind TEXT NOT NULL DEFAULT 'pinned_peer_mtls'
  CHECK(transport_kind='pinned_peer_mtls'),
 UNIQUE(grant_id,sequence)
) STRICT;
CREATE INDEX idx_goal_node_measurement_recent
 ON project_goal_node_measurement_receipts(grant_id,received_at DESC);
CREATE INDEX idx_goal_node_measurement_by_goal
 ON project_goal_node_measurement_receipts(project_id,goal_id,received_at DESC);
CREATE TRIGGER goal_node_measurement_receipt_no_update
 BEFORE UPDATE ON project_goal_node_measurement_receipts
 BEGIN SELECT RAISE(ABORT,'Node transport receipt is immutable'); END;
CREATE TRIGGER goal_node_measurement_receipt_no_delete
 BEFORE DELETE ON project_goal_node_measurement_receipts
 BEGIN SELECT RAISE(ABORT,'Node transport receipt is immutable'); END;
