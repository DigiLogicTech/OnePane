-- Preserve content and identities; record durable insertion order independently
-- of wall-clock adjustments. Backfill only additive sequence metadata.
ALTER TABLE team_messages ADD COLUMN message_sequence INTEGER CHECK(message_sequence IS NULL OR message_sequence > 0);
UPDATE team_messages SET message_sequence=rowid;
CREATE UNIQUE INDEX idx_team_messages_live_cursor ON team_messages(session_id,message_sequence);
CREATE TRIGGER team_message_sequence_generated BEFORE INSERT ON team_messages
WHEN NEW.message_sequence IS NOT NULL
BEGIN SELECT RAISE(ABORT,'Team message sequence is assigned by storage'); END;
CREATE TRIGGER team_message_sequence_assign AFTER INSERT ON team_messages
WHEN NEW.message_sequence IS NULL
BEGIN
  UPDATE team_messages SET message_sequence=(SELECT COALESCE(MAX(message_sequence),0)+1 FROM team_messages WHERE session_id=NEW.session_id) WHERE rowid=NEW.rowid;
END;
CREATE TRIGGER team_message_sequence_immutable BEFORE UPDATE OF message_sequence ON team_messages
WHEN OLD.message_sequence IS NOT NULL
BEGIN SELECT RAISE(ABORT,'Team message sequence is immutable'); END;
CREATE INDEX IF NOT EXISTS idx_team_turns_live_member ON team_turn_requests(session_id,workspace_id,member_id,round_number DESC,created_at DESC,id DESC);
