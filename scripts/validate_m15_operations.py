#!/usr/bin/env python3
"""Dependency-free SQLite checks for M15 OperationCoordinator invariants."""
from pathlib import Path
import sqlite3

root = Path(__file__).resolve().parents[1]
db = sqlite3.connect(":memory:")
db.execute("PRAGMA foreign_keys=ON")
for m in sorted((root / "migrations").glob("*.sql")):
    db.executescript(m.read_text())
now=1_900_000_000_000

def q(sql,args=()): db.execute(sql,args)
q("INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','W','active',1,?,?)",(now,now))
q("INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,?,?)",(now,now))
q("INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,?,?)",(now,now))
q("INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','admin','active',?,?)",(now,now))
q("INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',?,?)",(now,now))
q("INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task','ws','mutate','running','normal_task',0,'{}',1,?,?)",(now,now))
q("INSERT INTO capability_leases(id,workspace_id,principal_id,task_id,capability_id,scope_json,status,issued_by,issued_at,expires_at,usage_count,revision) VALUES('cl','ws','agent','task','cap','{\"resource_refs\":[\"resource://x\"],\"actions\":[\"mutate\"]}','active','admin',?,?,0,1)",(now,now+60000))
q("INSERT INTO resource_leases(id,workspace_id,task_id,resource_ref,lease_mode,status,acquired_at,expires_at,revision) VALUES('rl','ws','task','resource://x','exclusive_mutation','active',?,?,1)",(now,now+60000))
q("""INSERT INTO operations(id,workspace_id,task_id,principal_id,idempotency_key,state,capability_id,resource_ref,tool_id,tool_version,adapter_id,adapter_version,desired_state_json,precondition_json,reconciliation_json,policy_revision,input_hash,revision,created_at,updated_at,capability_lease_id,capability_lease_revision,resource_lease_id,required_verification,required_approval)
VALUES('op','ws','task','agent','k','prepared','cap','resource://x','tool','1','adapter','1','{}','{}','{}',1,'sha256:x',3,?,?,'cl',1,'rl','V2','none')""",(now,now))
row=db.execute("SELECT state,capability_lease_id,resource_lease_id,required_verification,required_approval FROM operations WHERE id='op'").fetchone()
assert row==('prepared','cl','rl','V2','none')

# One active writer per resource is a durable database invariant.
try:
    q("INSERT INTO resource_leases(id,workspace_id,task_id,resource_ref,lease_mode,status,acquired_at,expires_at,revision) VALUES('rl2','ws','task','resource://x','exclusive_mutation','active',?,?,1)",(now,now+60000))
    raise AssertionError("second active writer accepted")
except sqlite3.IntegrityError:
    pass

# Policy snapshot levels remain schema constrained.
for col,bad in [('required_verification','V9'),('required_approval','owner')]:
    try:
        q(f"UPDATE operations SET {col}=? WHERE id='op'",(bad,))
        raise AssertionError(f"invalid {col} accepted")
    except sqlite3.IntegrityError:
        pass

# Unknown outcomes are represented explicitly and remain discoverable for retry guards.
q("UPDATE operations SET state='unknown_outcome' WHERE id='op'")
assert db.execute("SELECT count(*) FROM operations WHERE workspace_id='ws' AND resource_ref='resource://x' AND state IN ('unknown_outcome','blocked_unknown_outcome')").fetchone()[0]==1

# ExecutionPermit must never become durable schema state.
cols={r[1] for r in db.execute("PRAGMA table_info(operations)")}
assert 'execution_permit' not in cols and 'execution_permit_hash' not in cols
print("M15 OperationCoordinator/resource lease schema invariants: PASS")
