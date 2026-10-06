-- Alpha 3.2 Research Council automatic multi-round orchestration.

ALTER TABLE team_turn_requests
ADD COLUMN round_number INTEGER NOT NULL DEFAULT 0 CHECK (round_number >= 0);

ALTER TABLE team_turn_requests
ADD COLUMN research_phase TEXT NOT NULL DEFAULT '' CHECK (
    research_phase IN ('','independent','critique','synthesis')
);

ALTER TABLE team_turn_requests
ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0);

ALTER TABLE team_turn_requests
ADD COLUMN retry_after INTEGER;

CREATE INDEX idx_team_turn_requests_round
ON team_turn_requests(session_id,round_number,status,member_id);

CREATE INDEX idx_team_turn_requests_retry
ON team_turn_requests(status,retry_after,created_at,id);
