-- Single-use independently signed instrument CLAIM about one explicitly
-- human-declared endpoint. A key signature authenticates the enrolled key,
-- NOT actual observation, device identity, or measurement vantage.
CREATE TABLE project_goal_instrument_measurement_claims (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 goal_id TEXT NOT NULL REFERENCES project_continuous_goals(id) ON DELETE RESTRICT,
 instrument_key_id TEXT NOT NULL REFERENCES project_goal_instrument_keys(id) ON DELETE RESTRICT,
 challenge_id TEXT NOT NULL UNIQUE REFERENCES project_goal_instrument_challenges(id) ON DELETE RESTRICT,
 declared_target_id TEXT NOT NULL REFERENCES project_goal_tcp_declared_target_bindings(id) ON DELETE RESTRICT,
 tls_pin_id TEXT NOT NULL REFERENCES project_goal_tls_endpoint_pins(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL,
 measurement_type TEXT NOT NULL CHECK(measurement_type='endpoint_tcp_connect_claim'),
 outcome TEXT NOT NULL CHECK(outcome IN ('connected','not_connected','timed_out','unknown')),
 observed_at INTEGER NOT NULL CHECK(observed_at>0),
 accepted_at INTEGER NOT NULL CHECK(accepted_at>=observed_at),
 signed_payload BLOB NOT NULL CHECK(length(signed_payload) BETWEEN 256 AND 4096),
 signature BLOB NOT NULL CHECK(length(signature)=64),
 signature_sha256 TEXT NOT NULL CHECK(length(signature_sha256)=71 AND substr(signature_sha256,1,7)='sha256:'),
 proof_class TEXT NOT NULL DEFAULT 'instrument_self_report_signed_not_attested'
  CHECK(proof_class='instrument_self_report_signed_not_attested')
) STRICT;
CREATE INDEX idx_goal_instrument_measurements_bounded ON project_goal_instrument_measurement_claims
 (project_id,goal_id,accepted_at DESC);
CREATE TRIGGER goal_instrument_measurement_no_delete
 BEFORE DELETE ON project_goal_instrument_measurement_claims
 BEGIN SELECT RAISE(ABORT,'signed measurement history is immutable'); END;
CREATE TRIGGER goal_instrument_measurement_no_update
 BEFORE UPDATE ON project_goal_instrument_measurement_claims
 BEGIN SELECT RAISE(ABORT,'signed measurement cannot be rewritten'); END;
