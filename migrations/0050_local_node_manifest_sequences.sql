-- Durable, origin-local federation manifest sequencing. A manifest is
-- allocated an increasing sequence before it is built/sent so two different
-- manifests created within the same millisecond cannot equivocate and an
-- OS clock rollback cannot leave an otherwise trusted Node offline.
-- One row per registered local Node; no remote Node may allocate from it.
CREATE TABLE local_node_manifest_sequences (
 node_id TEXT PRIMARY KEY REFERENCES harness_nodes(id) ON DELETE RESTRICT,
 sequence INTEGER NOT NULL CHECK(sequence>0),
 updated_at INTEGER NOT NULL
) STRICT;
