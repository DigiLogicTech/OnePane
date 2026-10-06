#!/usr/bin/env python3
"""Dependency-free checks for Project Workspace / Project Runtime foundations."""
from pathlib import Path
import sqlite3

ROOT = Path(__file__).resolve().parents[1]
db = sqlite3.connect(":memory:")
db.execute("PRAGMA foreign_keys=ON")
for migration in sorted((ROOT / "migrations").glob("*.sql")):
    db.executescript(migration.read_text())

now = 1_700_000_000_000
stmts = [
    ("INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','W','active',1,?,?)", (now,now)),
    ("INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,?,?)", (now,now)),
    ("INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','admin','active',?,?)", (now,now)),
    ("INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES('node','local',1,'fp','local','{}','{}',?,1,?,?)", (now,now,now)),
    ("INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,revision,created_by,created_at,updated_at) VALUES('p','ws','Project','active','{}','{}',1,'admin',?,?)", (now,now)),
    ("INSERT INTO routines(id,workspace_id,name,definition_version,status,trigger_json,policy_json,timezone,created_by,created_at,updated_at) VALUES('r','ws','Routine',1,'active','{}','{}','Australia/Brisbane','admin',?,?)", (now,now)),
]
for q,args in stmts: db.execute(q,args)

db.execute("""INSERT INTO project_runtimes(id,project_id,node_id,isolation_mode,backend,desired_state,status,runtime_spec_json,resource_limits_json,network_policy_json,filesystem_policy_json,environment_bindings_json,revision,created_by,created_at,updated_at)
VALUES('pr','p','node','sandboxed_container','sandbox_runner','running','defined','{}','{}','{}','{}','{}',1,'admin',?,?)""",(now,now))
db.execute("""INSERT INTO project_applications(id,project_runtime_id,name,source_kind,source_ref,install_spec_json,runtime_spec_json,environment_bindings_json,desired_state,status,trust,revision,created_by,created_at,updated_at)
VALUES('app','pr','web','oci_image','example@sha256:abc','{}','{}','{}','running','declared','untrusted_content',1,'admin',?,?)""",(now,now))
db.execute("""INSERT INTO project_runtime_endpoints(id,project_runtime_id,application_id,name,protocol,internal_port,exposure,desired_state,status,revision,created_by,created_at,updated_at)
VALUES('ep','pr','app','preview','http',3000,'preview','enabled','declared',1,'admin',?,?)""",(now,now))
db.execute("""INSERT INTO project_routine_bindings(id,project_id,project_runtime_id,application_id,routine_id,action_kind,action_ref,action_spec_json,status,revision,created_by,created_at,updated_at)
VALUES('b','p','pr','app','r','app_command','refresh','{}','active',1,'admin',?,?)""",(now,now))

assert db.execute("SELECT desired_state,status FROM project_runtimes WHERE id='pr'").fetchone()==('running','defined')
assert db.execute("SELECT trust FROM project_applications WHERE id='app'").fetchone()[0]=='untrusted_content'
assert db.execute("SELECT exposure,status FROM project_runtime_endpoints WHERE id='ep'").fetchone()==('preview','declared')

# SQLite must reject unsupported execution/isolation states.
try:
    db.execute("UPDATE project_runtimes SET isolation_mode='host_process' WHERE id='pr'")
    raise AssertionError("unsafe isolation mode accepted")
except sqlite3.IntegrityError:
    pass
try:
    db.execute("UPDATE project_applications SET trust='trusted_control' WHERE id='app'")
    raise AssertionError("application promoted to trusted_control")
except sqlite3.IntegrityError:
    pass

print("M13 project workspace/runtime schema invariants: PASS")
