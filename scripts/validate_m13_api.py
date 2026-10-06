#!/usr/bin/env python3
"""Dependency-free M13 REST/SSE control-boundary checks."""
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
auth=(ROOT/'internal/api/auth.go').read_text()
server=(ROOT/'internal/api/server.go').read_text()
sse=(ROOT/'internal/api/sse.go').read_text()
main=(ROOT/'cmd/harnessd/main.go').read_text()
config=(ROOT/'internal/config/config.go').read_text()
required=[
    ('auth',auth,'credential_hash'),('auth',auth,'workspace_scope_json'),('auth',auth,'capability_scope_json'),
    ('server',server,'CreatedBy: i.PrincipalID'),('server',server,'http.MaxBytesReader'),('server',server,'DisallowUnknownFields'),
    ('sse',sse,'workspace_id'),('sse',sse,'events.read'),('sse',sse,'X-Accel-Buffering'),
    ('main',main,'ListenAndServe'),('config',config,'non-loopback server.listen requires https server.public_origin'),
]
for name,text,token in required:
    assert token in text, f'{name}: missing {token}'
assert 'Access-Control-Allow-Origin' not in server+sse, 'CORS must stay disabled by default'
print('M13 REST/SSE authorization and transport invariants: PASS')
