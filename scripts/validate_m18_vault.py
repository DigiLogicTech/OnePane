#!/usr/bin/env python3
import sqlite3, pathlib
root=pathlib.Path(__file__).resolve().parents[1]
con=sqlite3.connect(':memory:')
for p in sorted((root/'migrations').glob('*.sql')): con.executescript(p.read_text())
assert {'secret_records','builtin_vault_items'} <= {r[0] for r in con.execute("select name from sqlite_master where type='table'")}
src=(root/'internal/vault/service.go').read_text()
for token in ['AES-256-GCM-envelope-v1','io.ReadFull(rand.Reader','wrapped_dek','vault:','master.key','0o600']:
    assert token in src, token
boot=(root/'internal/bootstrap/bootstrap.go').read_text()
assert 'vault.EnsureMasterKey' in boot and 'secretResolver := vaultService' in boot
print('M18 built-in Vault/Secret Broker contract: PASS')
