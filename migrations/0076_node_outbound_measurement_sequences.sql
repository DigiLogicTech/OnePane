-- Local-only durable monotonic counters for explicitly invoked Node
-- telemetry uploads. Sequence reservations intentionally survive failed
-- requests and restarts: no replay after a network timeout.
CREATE TABLE node_outbound_measurement_sequences (
 local_node_id TEXT NOT NULL REFERENCES harness_nodes(id) ON DELETE RESTRICT,
 peer_node_id TEXT NOT NULL REFERENCES harness_nodes(id) ON DELETE RESTRICT,
 grant_id TEXT NOT NULL CHECK(length(grant_id) BETWEEN 1 AND 128),
 sequence INTEGER NOT NULL CHECK(sequence>0),
 updated_at INTEGER NOT NULL CHECK(updated_at>0),
 PRIMARY KEY(local_node_id,peer_node_id,grant_id)
) STRICT;
CREATE TRIGGER node_outbound_measurement_sequence_no_delete
 BEFORE DELETE ON node_outbound_measurement_sequences
 BEGIN SELECT RAISE(ABORT,'outbound transport sequences must survive restarts'); END;
CREATE TRIGGER node_outbound_measurement_sequence_monotonic
 BEFORE UPDATE ON node_outbound_measurement_sequences
 WHEN NEW.local_node_id!=OLD.local_node_id OR
  NEW.peer_node_id!=OLD.peer_node_id OR NEW.grant_id!=OLD.grant_id OR
  NEW.sequence<=OLD.sequence
 BEGIN SELECT RAISE(ABORT,'outbound measurement sequence must increase'); END;
