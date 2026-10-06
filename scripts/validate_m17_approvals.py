#!/usr/bin/env python3
import sqlite3, pathlib
root=pathlib.Path(__file__).resolve().parents[1]
con=sqlite3.connect(':memory:')
for p in sorted((root/'migrations').glob('*.sql')): con.executescript(p.read_text())
cols={r[1] for r in con.execute('pragma table_info(approvals)')}
for c in ['operation_hash','policy_revision','status','expires_at','revision']: assert c in cols,c
src=(root/'internal/approval/service.go').read_text()
for token in ['ErrSelfApproval','ConsumeTx','operationHash','r.role_class=\'human\'','ApprovalAdmin']:
    assert token in src, token
op=(root/'internal/operation/coordinator.go').read_text()
for token in ['ContinueApproved','c.approvals.ConsumeTx','Compensate(','CompensatesOperationID']:
    assert token in op, token
print('M17 approvals/compensation contract: PASS')
