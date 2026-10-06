#!/usr/bin/env python3
import sqlite3
from pathlib import Path

root = Path(__file__).resolve().parents[1]
con = sqlite3.connect(":memory:")
con.execute("PRAGMA foreign_keys=ON")
con.executescript((root / "migrations" / "0001_initial.sql").read_text())
now = 1_800_000_000_000

con.execute("INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)", (now,now))
con.execute("INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('verifier','system','Verifier','active',1,?,?)", (now,now))
con.execute("INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,?,?)", (now,now))
con.execute("INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',?,?)", (now,now))
con.execute("INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','verify','verifying','normal_task',0,'{}',7,?,?)", (now,now))
con.execute("INSERT INTO verifications(id,workspace_id,task_id,subject_ref,required_level,achieved_level,status,spec_json,result_json,verified_by,started_at,completed_at,revision) VALUES('v','ws','task','task:task','V2','V2','pass','{}','{}','verifier',?,?,2)", (now,now+1))
con.execute("INSERT INTO checkpoints(id,workspace_id,task_id,verification_id,state_json,status,created_at) VALUES('cp','ws','task','v','{}','valid',?)", (now+2,))

valid = con.execute("""
SELECT COUNT(*)
FROM checkpoints c
JOIN verifications v ON v.id=c.verification_id
JOIN tasks t ON t.id=c.task_id
WHERE c.id='cp' AND c.task_id='task' AND c.status='valid'
  AND c.workspace_id=t.workspace_id AND v.workspace_id=t.workspace_id
  AND v.task_id=t.id AND v.status='pass' AND v.achieved_level IS NOT NULL
  AND v.completed_at IS NOT NULL AND c.created_at>=v.completed_at
  AND NOT EXISTS (
    SELECT 1 FROM operations o WHERE o.task_id=t.id
      AND o.state IN ('unknown_outcome','blocked_unknown_outcome')
  )
""").fetchone()[0]
assert valid == 1

con.execute("""INSERT INTO operations(
 id,workspace_id,task_id,principal_id,idempotency_key,state,capability_id,resource_ref,
 tool_id,tool_version,adapter_id,adapter_version,desired_state_json,precondition_json,
 reconciliation_json,policy_revision,input_hash,revision,created_at,updated_at
) VALUES('op','ws','task','agent','k','unknown_outcome','c','r','t','1','a','1','{}','{}','{}',1,'h',1,?,?)""", (now,now))
blocked = con.execute("""
SELECT COUNT(*) FROM operations
WHERE task_id='task' AND state IN ('unknown_outcome','blocked_unknown_outcome')
""").fetchone()[0]
assert blocked == 1

for table, column, bad in [
    ("verifications", "required_level", "V6"),
    ("verifications", "status", "approved"),
    ("checkpoints", "status", "fresh"),
]:
    try:
        con.execute(f"UPDATE {table} SET {column}=?", (bad,))
        raise AssertionError(f"invalid {table}.{column} accepted")
    except sqlite3.IntegrityError:
        pass

print("M8 verification/checkpoint completion guard: PASS")
