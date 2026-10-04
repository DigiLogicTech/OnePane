#!/usr/bin/env python3
"""Dependency-free SQLite validation for the M4 Task/TaskAttempt invariants."""
from pathlib import Path
import sqlite3

ROOT = Path(__file__).resolve().parents[1]
SCHEMA = ROOT / "migrations" / "0001_initial.sql"


def main() -> None:
    db = sqlite3.connect(":memory:")
    db.execute("PRAGMA foreign_keys=ON")
    db.executescript(SCHEMA.read_text())

    now = 1_700_000_000_000
    db.execute(
        "INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?)",
        ("ws_test", "Test", "active", 1, now, now),
    )
    db.execute(
        """
        INSERT INTO tasks(
            id,workspace_id,objective,state,scheduling_class,priority,
            completion_json,revision,created_at,updated_at
        ) VALUES(?,?,?,?,?,?,?,?,?,?)
        """,
        ("task_1", "ws_test", "Inspect state", "created", "normal_task", 0, "{}", 1, now, now),
    )

    cur = db.execute(
        """
        UPDATE tasks SET state='ready', revision=revision+1, ready_at=?, updated_at=?
        WHERE id='task_1' AND revision=1 AND state='created'
        """,
        (now + 1, now + 1),
    )
    assert cur.rowcount == 1

    db.execute(
        """
        INSERT INTO task_attempts(
            id,task_id,attempt_number,status,started_at,metadata_json
        ) VALUES('attempt_1','task_1',1,'running',?, '{}')
        """,
        (now + 2,),
    )
    db.execute(
        "UPDATE tasks SET state='running', revision=revision+1, updated_at=? WHERE id='task_1' AND revision=2",
        (now + 2,),
    )

    duplicate_blocked = False
    try:
        db.execute(
            """
            INSERT INTO task_attempts(
                id,task_id,attempt_number,status,started_at,metadata_json
            ) VALUES('attempt_2','task_1',2,'queued',?, '{}')
            """,
            (now + 3,),
        )
    except sqlite3.IntegrityError:
        duplicate_blocked = True
    assert duplicate_blocked, "partial unique index did not reject a second active attempt"

    db.execute(
        "UPDATE task_attempts SET status='interrupted', ended_at=? WHERE id='attempt_1' AND status='running'",
        (now + 4,),
    )
    db.execute(
        "UPDATE tasks SET state='blocked', revision=revision+1, updated_at=? WHERE id='task_1' AND revision=3",
        (now + 4,),
    )

    state, revision = db.execute("SELECT state,revision FROM tasks WHERE id='task_1'").fetchone()
    attempt_state = db.execute("SELECT status FROM task_attempts WHERE id='attempt_1'").fetchone()[0]
    assert (state, revision, attempt_state) == ("blocked", 4, "interrupted")

    # Once the prior attempt is terminal, a future recovery path can admit a new one.
    db.execute(
        """
        INSERT INTO task_attempts(
            id,task_id,attempt_number,status,started_at,metadata_json
        ) VALUES('attempt_2','task_1',2,'running',?, '{}')
        """,
        (now + 5,),
    )

    # Optimistic revision write with stale revision must affect zero rows.
    stale = db.execute(
        "UPDATE tasks SET state='ready', revision=revision+1 WHERE id='task_1' AND revision=2"
    )
    assert stale.rowcount == 0

    print("task lifecycle valid: optimistic_revision=PASS one_active_attempt=PASS interruption=PASS")


if __name__ == "__main__":
    main()
