-- Alpha 3.2 Models follow-up: adopted external artifacts and durable discovery/install reconciliation.

CREATE TABLE IF NOT EXISTS local_ai_adopted_models (
    id TEXT PRIMARY KEY,
    model_ref TEXT NOT NULL,
    display_name TEXT NOT NULL,
    provider TEXT NOT NULL,
    quantization TEXT NOT NULL,
    runtime_name TEXT NOT NULL DEFAULT 'llamacpp',
    source_ref TEXT NOT NULL,
    source_url TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    filename TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    spec_json TEXT NOT NULL,
    verification_source TEXT NOT NULL,
    created_by TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(model_ref,quantization,runtime_name)
);

CREATE INDEX IF NOT EXISTS idx_local_ai_adopted_models_ref
ON local_ai_adopted_models(model_ref,quantization,runtime_name);
