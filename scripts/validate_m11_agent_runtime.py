#!/usr/bin/env python3
"""Dependency-free M11 external agent-runtime invocation checks."""
from pathlib import Path
import sqlite3
ROOT=Path(__file__).resolve().parents[1]
db=sqlite3.connect(":memory:");db.execute("PRAGMA foreign_keys=ON")
for migration in sorted((ROOT/"migrations").glob("*.sql")):db.executescript(migration.read_text())
now=1_800_000_000_000
db.execute("INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)",(now,now))
db.execute("INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,?,?)",(now,now))
db.execute("INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node','local',1,'fp','local','{}','{}',1,?,?)",(now,now))
db.execute("""INSERT INTO agent_runtime_connections(id,workspace_id,node_id,runtime_kind,display_name,adapter_name,adapter_version,endpoint_json,auth_type,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json,revision,created_at,updated_at)
VALUES('rt','ws','node','hermes','Hermes','builtin.agent_protocol_http','1','{}','none','connected','user_trusted','proposal_only','{}','{}','{\"max_confidentiality\":\"secret\",\"allowed_residency\":[\"origin_node\"],\"destination_kind\":\"origin_node\",\"allow_raw_secrets\":false}',1,?,?)""",(now,now))
db.execute("""INSERT INTO agent_runtime_invocations(id,workspace_id,principal_id,connection_id,status,request_json,revision,created_at,updated_at)
VALUES('inv','ws','agent','rt','created','{}',1,?,?)""",(now,now))
for state in ('authorized','dispatched','executing','succeeded'):
    db.execute("UPDATE agent_runtime_invocations SET status=?,revision=revision+1,updated_at=? WHERE id='inv'",(state,now+1))
assert db.execute("SELECT status FROM agent_runtime_invocations WHERE id='inv'").fetchone()[0]=='succeeded'
try:
    db.execute("UPDATE agent_runtime_invocations SET status='complete' WHERE id='inv'")
    raise AssertionError('invalid invocation status accepted')
except sqlite3.IntegrityError: pass
print('M11 agent runtime invocation schema lifecycle: PASS')
