#!/usr/bin/env python3
"""Dependency-free SQLite checks for the M5 durable lease predicates."""

import sqlite3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
schema = (ROOT / "migrations" / "0001_initial.sql").read_text()
conn = sqlite3.connect(":memory:")
conn.execute("PRAGMA foreign_keys=ON")
conn.executescript(schema)
now = 1_700_000_000_000

for sql in (
    "INSERT INTO workspaces VALUES('ws','Test','active',1,?,?)",
    "INSERT INTO principals VALUES('admin','human','Admin','active',1,?,?)",
    "INSERT INTO principals VALUES('agent','agent','Agent','active',1,?,?)",
):
    conn.execute(sql, (now, now))
conn.execute("INSERT INTO workspace_memberships VALUES('ws','admin','active',?,?)", (now, now))
conn.execute("INSERT INTO workspace_memberships VALUES('ws','agent','active',?,?)", (now, now))

scope = '{"resource_refs":["repo://alpha/README.md"],"actions":["mutate"]}'
conn.execute(
    """INSERT INTO capability_leases(
       id,workspace_id,principal_id,task_id,capability_id,scope_json,status,
       issued_by,issued_at,expires_at,usage_limit,usage_count,revision)
       VALUES('lease','ws','agent',NULL,'repo.file.write',?,'active','admin',?,?,1,0,1)""",
    (scope, now, now + 1000),
)

# First and only use must atomically exhaust the lease.
cur = conn.execute(
    """UPDATE capability_leases
       SET usage_count=usage_count+1,status='exhausted',revision=revision+1
       WHERE id='lease' AND revision=1 AND status='active' AND expires_at>?
       AND (usage_limit IS NULL OR usage_count+1<=usage_limit)""",
    (now,),
)
assert cur.rowcount == 1
status, usage_count, revision = conn.execute(
    "SELECT status,usage_count,revision FROM capability_leases WHERE id='lease'"
).fetchone()
assert (status, usage_count, revision) == ("exhausted", 1, 2)

# The durable predicate must reject a second use.
cur = conn.execute(
    """UPDATE capability_leases
       SET usage_count=usage_count+1,revision=revision+1
       WHERE id='lease' AND revision=2 AND status='active' AND expires_at>?
       AND (usage_limit IS NULL OR usage_count+1<=usage_limit)""",
    (now,),
)
assert cur.rowcount == 0

# Event failure must roll the surrounding transaction back.
conn.commit()
try:
    conn.execute("BEGIN")
    conn.execute(
        """INSERT INTO capability_leases(
           id,workspace_id,principal_id,task_id,capability_id,scope_json,status,
           issued_by,issued_at,expires_at,usage_limit,usage_count,revision)
           VALUES('rollback','ws','agent',NULL,'repo.file.read',?,'active','admin',?,?,NULL,0,1)""",
        (scope, now, now + 1000),
    )
    conn.execute(
        """INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,
           actor_principal_id,payload_json,occurred_at)
           VALUES('evt_bad','ws','capability_lease.issued','capability_lease','rollback',
           'missing','{}',?)""",
        (now,),
    )
    conn.commit()
    raise AssertionError("expected foreign-key failure")
except sqlite3.IntegrityError:
    conn.rollback()
assert conn.execute("SELECT COUNT(*) FROM capability_leases WHERE id='rollback'").fetchone()[0] == 0

print("m5 storage valid: exhaustion=PASS durable_predicate=PASS event_rollback=PASS")
