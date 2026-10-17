-- Efficient true rolling five-minute TLS handshake cadence per approved
-- leaf-certificate pin. The earlier fixed 5-minute bucket alone could
-- permit two socket handshakes across a wall-clock bucket boundary.
CREATE INDEX IF NOT EXISTS idx_goal_tls_check_pin_claimed_at
 ON project_goal_tls_endpoint_checks(pin_id,claimed_at DESC);
