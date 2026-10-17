-- RC11: persistence-only rotation cursor for passive receipt review.
-- Not an authority record. It stores no observation payload or goal permit.
-- A cursor advance after a denied permit prevents invalid sources from
-- permanently occupying the bounded worker prefix on constrained hardware.
CREATE TABLE goal_review_worker_cursors (
 worker_id TEXT PRIMARY KEY CHECK(worker_id='receipt_metadata'),
 after_permit_id TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_at INTEGER NOT NULL DEFAULT 1 CHECK(updated_at>0)
) STRICT;
INSERT INTO goal_review_worker_cursors(worker_id,after_permit_id,revision,updated_at)
 VALUES('receipt_metadata','',1,1);
