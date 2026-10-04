#!/usr/bin/env python3
"""Dependency-free M9 schema/constraint validation."""
from pathlib import Path
import sqlite3

ROOT = Path(__file__).resolve().parents[1]
con = sqlite3.connect(":memory:")
con.execute("PRAGMA foreign_keys=ON")
for migration in sorted((ROOT / "migrations").glob("*.sql")):
    con.executescript(migration.read_text(encoding="utf-8"))

now = 1_800_000_000_000
con.execute("INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)", (now, now))
con.execute("""INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
VALUES('node','local',1,'fp','local','LOCAL_TRUSTED','{}','{}',?,1,?,?)""", (now, now, now))
con.execute("""INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,revision,created_at,updated_at)
VALUES('provider','ws','openai_compatible','Provider','secret_ref','connected','{}',1,?,?)""", (now, now))
con.execute("""INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at)
VALUES('model','example/model','[\"text\"]','{}','user_trusted',?,?)""", (now, now))
con.execute("""INSERT INTO model_deployments(id,model_id,node_id,provider_connection_id,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at)
VALUES('deployment','model','node','provider','{}','discovered','fingerprint',1,?,?)""", (now, now))
con.execute("""INSERT INTO agent_runtime_connections(id,workspace_id,runtime_kind,display_name,adapter_name,adapter_version,endpoint_json,auth_type,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json,revision,created_at,updated_at)
VALUES('hermes','ws','hermes','Hermes','builtin.agent_protocol_http','1','{\"base_url\":\"http://127.0.0.1:9000\"}','none','registered','user_trusted','proposal_only','{\"agent_protocol\":\"v1\"}','{}','{\"max_confidentiality\":\"public\",\"allowed_residency\":[\"any\"],\"allow_raw_secrets\":false}',1,?,?)""", (now, now))

assert con.execute("SELECT COUNT(*) FROM agent_runtime_connections WHERE runtime_kind='hermes'").fetchone()[0] == 1
assert con.execute("SELECT context_max_verified FROM model_deployments WHERE id='deployment'").fetchone()[0] is None

for column, bad in [
    ("status", "ready"),
    ("trust_state", "trusted_model"),
    ("operating_mode", "direct_root"),
]:
    try:
        con.execute(f"UPDATE agent_runtime_connections SET {column}=? WHERE id='hermes'", (bad,))
        raise AssertionError(f"invalid agent_runtime_connections.{column} accepted")
    except sqlite3.IntegrityError:
        pass

try:
    con.execute("UPDATE model_deployments SET status='verified' WHERE id='deployment'")
    raise AssertionError("invalid model deployment status accepted")
except sqlite3.IntegrityError:
    pass

print("M9 inference catalog + external agent runtime schema: PASS")
