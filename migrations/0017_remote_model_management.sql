-- M34: explicit per-peer authority for remote model recommendation/install management.
-- Pairing permits inference only; model management remains separately opt-in.

CREATE TABLE node_model_management_grants (
    peer_node_id        TEXT PRIMARY KEY REFERENCES harness_nodes(id),
    enabled             INTEGER NOT NULL CHECK (enabled IN (0,1)),
    allow_catalog_install INTEGER NOT NULL DEFAULT 1 CHECK (allow_catalog_install IN (0,1)),
    updated_by          TEXT REFERENCES principals(id),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE node_remote_model_jobs (
    peer_node_id        TEXT NOT NULL REFERENCES harness_nodes(id),
    job_id              TEXT NOT NULL REFERENCES local_ai_install_jobs(id),
    created_at          INTEGER NOT NULL,
    PRIMARY KEY (peer_node_id, job_id)
) STRICT;
