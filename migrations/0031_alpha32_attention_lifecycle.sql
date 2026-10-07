-- User-specific attention disposition; source Event Ledger entries remain immutable.
CREATE TABLE IF NOT EXISTS ui_attention_dispositions (
    workspace_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    alert_id TEXT NOT NULL,
    disposition TEXT NOT NULL DEFAULT 'active' CHECK(disposition IN ('active','acknowledged','archived')),
    updated_at INTEGER NOT NULL,
    PRIMARY KEY(workspace_id,principal_id,alert_id)
);
CREATE INDEX IF NOT EXISTS idx_attention_dispositions_workspace ON ui_attention_dispositions(workspace_id,principal_id,disposition,updated_at DESC);
CREATE TABLE IF NOT EXISTS ui_attention_disposition_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    alert_id TEXT NOT NULL,
    disposition TEXT NOT NULL CHECK(disposition IN ('active','acknowledged','archived')),
    changed_at INTEGER NOT NULL
);
