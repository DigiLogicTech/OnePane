#!/usr/bin/env python3
from pathlib import Path
import sqlite3
root=Path(__file__).resolve().parents[1]
con=sqlite3.connect(':memory:')
for p in sorted((root/'migrations').glob('*.sql')): con.executescript(p.read_text())
assert con.execute("select 1 from sqlite_master where type='table' and name='watchdog_states'").fetchone()
policy=(root/'internal/policy/engine.go').read_text()
types=(root/'internal/policy/types.go').read_text()
wd=(root/'internal/watchdog/service.go').read_text()
boot=(root/'internal/bootstrap/bootstrap.go').read_text()
main=(root/'cmd/harnessd/main.go').read_text()
for token in ['ReasonWatchdogUnavailable','e.watchdog == nil','ActionExecuteSandboxed']:
    assert token in policy+types, token
for token in ['heartbeat_at','Healthy(ctx context.Context)','15*time.Second']:
    assert token.replace(' ','') in wd.replace(' ',''), token
assert 'SetWatchdog(watchdogService)' in boot
assert 'runtime.Watchdog.Run' in main
print('Deterministic watchdog contract: PASS')
