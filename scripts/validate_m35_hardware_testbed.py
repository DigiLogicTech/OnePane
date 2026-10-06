from pathlib import Path

root = Path(__file__).resolve().parents[1]

def need(rel, *tokens):
    p = root / rel
    if not p.exists():
        raise SystemExit(f'M35 hardware/Testbed validation: FAIL missing {rel}')
    s = p.read_text(errors='ignore')
    miss = [t for t in tokens if t not in s]
    if miss:
        raise SystemExit(f'M35 hardware/Testbed validation: FAIL {rel} missing {miss}')

need('migrations/0018_model_profiles_testbed.sql',
     'model_spec_sheets', 'model_testbed_sessions', 'model_testbed_turns',
     "'pending','accepted','restricted','rejected'",
     'Existing RC7 managed deployments', 'INSERT INTO model_spec_sheets')
need('internal/localai/types.go',
     'PlacementSingleDevice', 'PlacementLayerSharded', 'PlacementRowSharded',
     'PlacementTensorSharded', 'PlacementCPUOffload', 'PlacementCPUOnly',
     'RuntimeDevice', 'Backends')
need('internal/localai/recommend.go',
     'bestPlacement', 'groupByBackend', 'PlacementTensorSharded',
     'runtimeDeviceName')
need('internal/localai/supervisor.go',
     'llamaPlacementArgs', '--device', '--split-mode', '--tensor-split',
     'Acquire', 'ensureCapacity')
need('internal/localai/catalog_signed.go',
     'wantedBackend', '"cuda"', '"rocm"', '"vulkan"', '"sycl"', '"metal"', '"opencl"', '"cann"')
need('internal/localai/llmfit.go',
     '/api/v1/models', 'EstimateConfidence', 'UsableContext', 'DiskSizeGB',
     'SupportsTP', 'MeasuredTPS', 'VerifyCommand')
need('internal/localai/testbed.go',
     'ModelSpecSheet', 'StartTestbed', 'RunTestbedTurn', 'SyntheticToolProbe',
     'production admission requires a completed manual testbed session',
     'FederatedInferenceAllowed')
need('internal/scheduler/catalog.go',
     'applyManagedAdmission', 'DenyCapabilities', 'DenyRoles', 'MaxContextTokens', 'AllowToolUse')
need('internal/nodefederation/transport.go',
     '/federation/v1/models/spec-sheet', '/federation/v1/models/testbed/start',
     '/federation/v1/models/testbed/turn', '/federation/v1/models/testbed/complete',
     '/federation/v1/models/admit', 'RemoteModelSpecSheet', 'RemoteStartModelTestbed')
need('internal/api/server.go',
     '/v1/model-deployments/{deploymentID}/spec-sheet',
     '/v1/model-deployments/{deploymentID}/testbed/sessions',
     '/v1/model-deployments/{deploymentID}/admission',
     '/v1/nodes/{nodeID}/models/{deploymentID}/spec-sheet',
     '/v1/nodes/{nodeID}/models/{deploymentID}/testbed/sessions')
need('docs/model-placement-testbed.md',
     'llmfit advisory input', 'synthetic tool probe', 'Remote-node operation')

sched = (root/'internal/scheduler/catalog.go').read_text()
segment = sched.split('func (c *Catalog) modelCandidates',1)[1].split('func (c *Catalog) bestProfile',1)[0]
if segment.count('out = append(out, cand)') != 1:
    raise SystemExit('M35 hardware/Testbed validation: FAIL model scheduler candidate append count is not exactly one')

print('M35 Hardware-portable placement + Model Testbed validation: PASS')

need('internal/localai/testbed.go', "hardware_profile_id=? AND placement_json=? AND status='completed'", 'ListTestbedTurns')

# Upgrade safety: an RC7 managed deployment must be backfilled to a pending
# spec sheet so it can be manually reviewed without re-downloading/requalifying.
import sqlite3, json
con = sqlite3.connect(':memory:')
con.execute('PRAGMA foreign_keys=ON')
for m in sorted((root/'migrations').glob('0*.sql')):
    if m.name.startswith('0018'):
        break
    con.executescript(m.read_text())
con.execute("INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('n','node',1,'fp','local','{}','{}',1,1,1)")
con.execute("INSERT INTO local_hardware_profiles(id,node_id,fingerprint,os_name,architecture,cpu_json,memory_json,accelerators_json,runtimes_json,storage_json,detected_at) VALUES('hw','n','hfp','linux','amd64','{\"name\":\"cpu\",\"logical_cores\":8,\"architecture\":\"amd64\"}','{\"total_bytes\":100,\"available_bytes\":90}','[]','[]','{\"path\":\"/models\",\"capacity_bytes\":1000,\"available_bytes\":900}',1)")
con.execute("INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('m','example/model','[\"text\"]','{\"claim\":true}','user_trusted',1,1)")
con.execute("INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_version,runtime_config_json,status,residency_state,context_max_reported,context_max_verified,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('d','m','n','llama.cpp','1','{}','ready','stopped',8192,8192,'dfp',1,1,1)")
plan = json.dumps({'placement': {'mode':'cpu_only','backend':'cpu','devices':[{'kind':'cpu','name':'cpu','backend':'cpu','capacity_bytes':90,'allocated_bytes':50}]}, 'llmfit': {'source':'llmfit'}})
con.execute("INSERT INTO local_model_install_plans(id,node_id,hardware_profile_id,role_name,use_case,model_ref,source_ref,runtime_name,quantization,context_tokens,fit_level,run_mode,memory_required_bytes,disk_required_bytes,download_scratch_bytes,plan_json,status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", ('p','n','hw','general','general','example/model','src','llama.cpp','Q4_K_M',8192,'good','cpu',50,40,40,plan,'ready',1,1,1))
con.execute("INSERT INTO managed_local_models(id,node_id,plan_id,model_id,deployment_id,model_ref,source_ref,local_path,status,revision,updated_at) VALUES('mm','n','p','m','d','example/model','src','/models/m.gguf','ready',1,1)")
con.executescript((root/'migrations/0018_model_profiles_testbed.sql').read_text())
row = con.execute("SELECT admission_status,placement_json,llmfit_json,catalog_claims_json FROM model_spec_sheets WHERE deployment_id='d'").fetchone()
if row is None or row[0] != 'pending' or json.loads(row[1]).get('mode') != 'cpu_only' or json.loads(row[2]).get('source') != 'llmfit' or json.loads(row[3]).get('claim') is not True:
    raise SystemExit(f'M35 hardware/Testbed validation: FAIL RC7 upgrade backfill {row!r}')
print('M35 RC7 upgrade backfill: PASS')
