#!/usr/bin/env python3
"""Dependency-free M10 InferenceRequest schema lifecycle checks."""
from pathlib import Path
import sqlite3

ROOT=Path(__file__).resolve().parents[1]
db=sqlite3.connect(":memory:");db.execute("PRAGMA foreign_keys=ON")
for migration in sorted((ROOT/"migrations").glob("*.sql")): db.executescript(migration.read_text())
now=1_800_000_000_000
db.execute("INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)",(now,now))
db.execute("INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,?,?)",(now,now))
db.execute("INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',?,?)",(now,now))
db.execute("INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model','fake','[]','{}','user_trusted',?,?)",(now,now))
db.execute("INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node','local',1,'fp','local','{}','{}',1,?,?)",(now,now))
db.execute("INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('dep','model','node','fake','{}','ready','fp',1,?,?)",(now,now))
db.execute("""INSERT INTO inference_requests(id,workspace_id,principal_id,deployment_id,origin_node_id,execution_node_id,status,capability_json,context_manifest_json,classification_json,request_json,created_at,updated_at)
VALUES('req','ws','agent','dep','node','node','created','{}','{}','{}','{}',?,?)""",(now,now))
for state in ('routed','dispatched','executing','succeeded'):
    db.execute("UPDATE inference_requests SET status=?,updated_at=? WHERE id='req'",(state,now+1))
assert db.execute("SELECT status FROM inference_requests WHERE id='req'").fetchone()[0]=='succeeded'
try:
    db.execute("UPDATE inference_requests SET status='complete' WHERE id='req'")
    raise AssertionError('invalid inference status accepted')
except sqlite3.IntegrityError: pass
print('M10 inference request schema lifecycle: PASS')
