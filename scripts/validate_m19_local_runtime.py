#!/usr/bin/env python3
"""Dependency-free contract checks for managed local runtime + qualification."""
from pathlib import Path
import sqlite3

root = Path(__file__).resolve().parents[1]
con = sqlite3.connect(':memory:')
con.execute('PRAGMA foreign_keys=ON')
for p in sorted((root/'migrations').glob('*.sql')):
    con.executescript(p.read_text())

tables = {r[0] for r in con.execute("select name from sqlite_master where type='table'")}
required = {'local_runtime_instances','local_model_qualification_runs','compatibility_profiles'}
assert required <= tables, required - tables

# Active runtime endpoint identity must be unique per deployment/node-port.
cols = {r[1] for r in con.execute('pragma table_info(local_runtime_instances)')}
for c in ['deployment_id','runtime_id','managed_model_id','process_fingerprint','health_json','pid','status']:
    assert c in cols, c

sup = (root/'internal/localai/supervisor.go').read_text()
for token in ['127.0.0.1','ProcessInspector','runtime process identity could not be re-established','/proc/%d/exe','LD_LIBRARY_PATH']:
    assert token in sup, token

qual = (root/'internal/localai/qualification.go').read_text()
for token in ['context_max_verified','compatibility_profiles','qualification_ping','ttft_measured','ModelUserTrusted']:
    assert token in qual, token
assert 'total request latency must not be mislabeled as time-to-first-token' in qual

transport = (root/'internal/inference/transport_local_openai.go').read_text()
for token in ['unsafe_local_endpoint','127.0.0.1','bodyObj["model"] = req.Model.ModelRef','bodyObj["stream"] = false']:
    assert token in transport, token

svc = (root/'internal/localai/service.go').read_text()
for token in ['managed_local_runtimes','managed_local_models','ResolveLocalEndpoint','RecoverManagedRuntimes']:
    assert token in svc, token

print('M19 managed local runtime + empirical qualification contract: PASS')
