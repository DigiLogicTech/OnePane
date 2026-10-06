#!/usr/bin/env python3
import sqlite3, pathlib
root=pathlib.Path(__file__).resolve().parents[1]
con=sqlite3.connect(':memory:')
for p in sorted((root/'migrations').glob('*.sql')): con.executescript(p.read_text())
tables={r[0] for r in con.execute("select name from sqlite_master where type='table'")}
required={'local_hardware_profiles','local_model_install_plans','managed_local_runtimes','managed_local_models'}
assert required <= tables, required-tables
src=(root/'internal/localai/recommend.go').read_text()
for token in ['FitPerfect','FitGood','FitMarginal','RunGPU','RunCPUGPU','StorageHeadroomPct']:
    assert token in src, token
prov=(root/'internal/localai/provision.go').read_text()
for token in ['https://','sha256 mismatch','safeArchivePath','symlinks are not allowed']:
    assert token in prov, token
svc=(root/'internal/localai/service.go').read_text()
assert 'DeploymentQualifying' in svc
assert 'ModelQuarantined' in svc
print('Local AI bootstrap contract: PASS')
