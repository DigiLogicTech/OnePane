-- M33: paired HarnessNode federation, LAN discovery, capability manifests and remote inference receipts.

CREATE TABLE node_pairings (
    id                      TEXT PRIMARY KEY,
    peer_node_id            TEXT NOT NULL UNIQUE REFERENCES harness_nodes(id),
    direction               TEXT NOT NULL CHECK (direction IN ('outbound','inbound')),
    status                  TEXT NOT NULL CHECK (status IN ('requested','pairing','paired','rejected','revoked','expired','failed')),
    pairing_token           TEXT NOT NULL,
    pairing_token_hash      TEXT NOT NULL,
    pairing_code            TEXT NOT NULL,
    pairing_code_hash       TEXT NOT NULL,
    local_confirmed         INTEGER NOT NULL DEFAULT 0 CHECK (local_confirmed IN (0,1)),
    peer_confirmed          INTEGER NOT NULL DEFAULT 0 CHECK (peer_confirmed IN (0,1)),
    peer_tls_fingerprint    TEXT NOT NULL,
    peer_certificate_pem    TEXT NOT NULL,
    expires_at              INTEGER NOT NULL,
    paired_at               INTEGER,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_node_pairings_status ON node_pairings(status, expires_at);

CREATE TABLE node_capability_manifests (
    peer_node_id            TEXT PRIMARY KEY REFERENCES harness_nodes(id),
    sequence                INTEGER NOT NULL CHECK (sequence >= 1),
    manifest_json           TEXT NOT NULL CHECK (json_valid(manifest_json)),
    received_at             INTEGER NOT NULL,
    expires_at              INTEGER NOT NULL,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    updated_at              INTEGER NOT NULL
) STRICT;

CREATE TABLE node_federated_inference_receipts (
    id                      TEXT PRIMARY KEY,
    peer_node_id            TEXT NOT NULL REFERENCES harness_nodes(id),
    remote_request_id       TEXT NOT NULL,
    deployment_id           TEXT NOT NULL REFERENCES model_deployments(id),
    status                  TEXT NOT NULL CHECK (status IN ('executing','succeeded','failed','unknown')),
    response_json           TEXT CHECK (response_json IS NULL OR json_valid(response_json)),
    usage_json              TEXT CHECK (usage_json IS NULL OR json_valid(usage_json)),
    error_code              TEXT,
    created_at              INTEGER NOT NULL,
    completed_at            INTEGER,
    UNIQUE (peer_node_id, remote_request_id)
) STRICT;
CREATE INDEX idx_node_federated_receipts_peer ON node_federated_inference_receipts(peer_node_id, created_at);
