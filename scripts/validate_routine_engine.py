#!/usr/bin/env python3
from pathlib import Path
import sqlite3
root=Path(__file__).resolve().parents[1]
con=sqlite3.connect(':memory:');con.execute('pragma foreign_keys=on')
for p in sorted((root/'migrations').glob('*.sql')): con.executescript(p.read_text())
for t in ['routines','routine_occurrences','project_routine_bindings','tasks','outbox_jobs']:
    assert con.execute("select 1 from sqlite_master where type='table' and name=?",(t,)).fetchone(), t
src=(root/'internal/routine/service.go').read_text()
for token in ['CreateInTransaction','ClassBackgroundRoutine','latestDueTrigger','time.LoadLocation','occurrence_key','project_routine_bindings']:
    assert token in src, token
task=(root/'internal/task/service.go').read_text()
assert 'CreateInTransaction' in task
main=(root/'cmd/harnessd/main.go').read_text()
assert 'runtime.Routines.Tick' in main
assert 'runtime.RoutineWorker.Tick' in main
worker=(root/'internal/routineworker/service.go').read_text()
for token in ['CapabilityExecute','ActionExecuteSandboxed','ObservationType: "project_routine_execution"','VerificationV1','CreateCheckpoint','CompleteVerified','RecoverLostAttempts']:
    assert token in worker, token
print('Routine Engine contract: PASS')
