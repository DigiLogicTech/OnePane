#!/usr/bin/env python3
from pathlib import Path
import sqlite3, sys

ROOT = Path(__file__).resolve().parents[1]

def fail(msg):
    print('M25 local AI install validation failed:', msg, file=sys.stderr)
    raise SystemExit(1)

def main():
    db=sqlite3.connect(':memory:')
    db.execute('PRAGMA foreign_keys=ON')
    for m in sorted((ROOT/'migrations').glob('*.sql')):
        db.executescript(m.read_text())
    cols={r[1] for r in db.execute('PRAGMA table_info(local_ai_catalogs)')}
    for c in ['catalog_version','key_id','payload_json','payload_sha256','signature_b64','expires_at','status']:
        if c not in cols: fail('missing local_ai_catalogs.'+c)
    cols={r[1] for r in db.execute('PRAGMA table_info(local_ai_install_jobs)')}
    for c in ['plan_id','catalog_id','deployment_id','qualification_run_id','status','attempt_count']:
        if c not in cols: fail('missing local_ai_install_jobs.'+c)
    idx={r[1] for r in db.execute("PRAGMA index_list('local_ai_install_jobs')")}
    if 'idx_local_ai_one_active_job_per_plan' not in idx: fail('missing active-job uniqueness index')
    src=(ROOT/'internal/localai/catalog_signed.go').read_text()
    for token in ['ed25519.Verify','catalog signature re-verification failed','ResolvePlanWithCatalog','ModelSpecifications']:
        if token not in src: fail('catalog contract missing '+token)
    jobs=(ROOT/'internal/localai/install_jobs.go').read_text()
    for token in ['RecoverInstallJobs','QueueOneClickInstall','RunInstallJob','InstallJobQualifying']:
        if token not in jobs: fail('install job contract missing '+token)
    svc=(ROOT/'internal/localai/service.go').read_text()
    for token in ['ReapIdleManagedRuntimes','ExpectedSHA256','managed runtime target already exists']:
        if token not in svc: fail('managed local AI lifecycle missing '+token)
    print('M25 local AI install pipeline: PASS')

if __name__=='__main__': main()
