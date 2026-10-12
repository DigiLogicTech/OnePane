-- Durably rotate the small, human-opt-in TCP observation worker through
-- exact-target grants. A cursor is NOT network permission, and booting the
-- application does not create a recurring grant for any existing source.
CREATE TABLE goal_tcp_recurring_worker_cursors (
 worker_id TEXT PRIMARY KEY CHECK(worker_id='exact_tcp_connect'),
 after_grant_id TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_at INTEGER NOT NULL DEFAULT 1 CHECK(updated_at>0)
) STRICT;
INSERT INTO goal_tcp_recurring_worker_cursors
 (worker_id,after_grant_id,revision,updated_at)
 VALUES('exact_tcp_connect','',1,1);
