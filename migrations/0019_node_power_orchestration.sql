-- OnePane node power orchestration. Additive only; 0001 remains frozen.
-- No FK names are assumed here so this migration remains compatible with the
-- existing HarnessNode schema while node_id values are validated by application code.

CREATE TABLE IF NOT EXISTS node_power_profiles (
    node_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    wake_method TEXT NOT NULL DEFAULT 'wol',
    wake_targets_json TEXT NOT NULL DEFAULT '[]',
    wake_timeout_ms INTEGER NOT NULL DEFAULT 90000 CHECK (wake_timeout_ms > 0),
    ready_stabilization_ms INTEGER NOT NULL DEFAULT 2000 CHECK (ready_stabilization_ms >= 0),
    allow_wake_on_battery INTEGER NOT NULL DEFAULT 1 CHECK (allow_wake_on_battery IN (0,1)),
    minimum_battery_percent INTEGER NOT NULL DEFAULT 0 CHECK (minimum_battery_percent BETWEEN 0 AND 100),
    default_power_hold_ttl_ms INTEGER NOT NULL DEFAULT 300000 CHECK (default_power_hold_ttl_ms > 0),
    max_power_hold_ttl_ms INTEGER NOT NULL DEFAULT 900000 CHECK (max_power_hold_ttl_ms >= default_power_hold_ttl_ms),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS node_wake_attempts (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    task_id TEXT,
    provider TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('requested','wake_sent','reconnecting','preparing','ready','failed','timed_out')),
    requested_at TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    ready_at TEXT,
    failure TEXT,
    idempotency_key TEXT,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_node_wake_attempts_node_requested ON node_wake_attempts(node_id, requested_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_node_wake_attempts_idempotency ON node_wake_attempts(node_id, idempotency_key) WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE TABLE IF NOT EXISTS node_power_holds (
    lease_id TEXT PRIMARY KEY,
    origin_node_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_node_power_holds_expiry ON node_power_holds(expires_at);
