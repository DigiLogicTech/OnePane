-- An unresolved turn can belong to any historical round. Seek by both fields
-- without scanning all completed rounds or all tenants' running turns.
CREATE INDEX idx_team_turn_requests_session_status
ON team_turn_requests(session_id,status);
