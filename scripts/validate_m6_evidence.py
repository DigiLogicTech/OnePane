#!/usr/bin/env python3
"""Dependency-free checks for M6 evidence persistence invariants."""

import sqlite3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
conn = sqlite3.connect(":memory:")
conn.execute("PRAGMA foreign_keys=ON")
conn.executescript((ROOT / "migrations" / "0001_initial.sql").read_text())
now = 1_700_000_000_000
conn.execute("INSERT INTO workspaces VALUES('ws','Test','active',1,?,?)", (now, now))
conn.execute("INSERT INTO principals VALUES('agent','agent','Agent','active',1,?,?)", (now, now))
conn.execute("INSERT INTO workspace_memberships VALUES('ws','agent','active',?,?)", (now, now))

conn.execute(
    """INSERT INTO observations(
       id,workspace_id,subject_ref,observation_type,probe_tool_id,probe_tool_version,
       source_principal_id,value_json,confidentiality,residency,trust,integrity_hash,
       observed_at,created_at)
       VALUES('obs','ws','subject','probe','tool','1','agent','{}','internal','any',
       'authoritative_data','sha256:test',?,?)""",
    (now, now),
)
for sql in (
    "UPDATE observations SET value_json='[]' WHERE id='obs'",
    "DELETE FROM observations WHERE id='obs'",
):
    try:
        conn.execute(sql)
        raise AssertionError("observation immutability trigger did not fire")
    except sqlite3.IntegrityError:
        pass

# Frozen label constraints reject values outside the declared dimensions.
try:
    conn.execute(
        """INSERT INTO artifacts(id,workspace_id,content_hash,media_type,size_bytes,storage_ref,
           confidentiality,residency,trust,status,metadata_json,created_at)
           VALUES('bad','ws','sha256:x','text/plain',0,'x','INTERNAL','any',
           'user_instruction','active','{}',?)""",
        (now,),
    )
    raise AssertionError("uppercase schema label unexpectedly accepted")
except sqlite3.IntegrityError:
    pass

print("m6 evidence valid: observation_immutability=PASS label_constraints=PASS")
