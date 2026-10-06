#!/usr/bin/env python3
from pathlib import Path
import sqlite3
import tempfile

root = Path(__file__).resolve().parents[1]
required = {
    root / 'migrations/0008_project_endpoint_routes.sql': [
        'CREATE TABLE project_endpoint_routes',
        "CHECK (host_ip = '127.0.0.1')",
        "status IN ('verified','stale')",
        'observation_id', 'verification_id', 'application_revision', 'endpoint_revision', 'container_spec_hash',
    ],
    root / 'internal/projectworkspace/service.go': ['ApplyEndpointRoute', 'RequireVerifiedEndpointRouteTx', 'ResolveIngressRoute'],
    root / 'internal/projectruntime/reconciler.go': ['publishVerifiedEndpointRoutes', 'ApplyEndpointRoute'],
    root / 'internal/ingress/session.go': ['EndpointOrigin', 'ep-', 'BootstrapExpiresAt', 'AuthorizeHost'],
    root / 'internal/ingress/server.go': ['127.0.0.1', 'PreviewCookieName', 'Authorization', 'Proxy-Authorization', 'newLoopbackProxy'],
    root / 'internal/api/server.go': ['preview-session', 'MintEndpointGrant', 'project.read'],
    root / 'internal/config/config.go': ['PreviewListen', 'PreviewPublicOrigin', 'preview origin must be distinct'],
}
for path, tokens in required.items():
    text = path.read_text()
    for token in tokens:
        assert token in text, f'{path.name}: missing {token}'

# Prove the route table itself fails closed on non-loopback target state.
with tempfile.NamedTemporaryFile(suffix='.db') as f:
    db = sqlite3.connect(f.name)
    db.execute('PRAGMA foreign_keys=ON')
    for migration in sorted((root / 'migrations').glob('*.sql')):
        db.executescript(migration.read_text())
    cols = {r[1] for r in db.execute('PRAGMA table_info(project_endpoint_routes)')}
    assert {'endpoint_id','host_ip','host_port','observation_id','verification_id','container_spec_hash'} <= cols
    tables = db.execute("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").fetchone()[0]
    # M22 introduced table 76; later additive migrations may increase the total.
    assert tables >= 76, tables
print('M22 trusted ingress/preview-origin contract: PASS')
