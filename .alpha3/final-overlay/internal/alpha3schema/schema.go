package alpha3schema

import (
	"context"
	"database/sql"
	"fmt"
)

func Ensure(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("alpha3 schema: database unavailable")
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS project_workspaces (
id TEXT PRIMARY KEY,
project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
name TEXT NOT NULL,
description TEXT,
status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
layout_json TEXT NOT NULL DEFAULT '{"schema_version":1,"components":[]}' CHECK (json_valid(layout_json)),
ai_settings_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(ai_settings_json)),
resource_scope_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(resource_scope_json)),
state_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(state_json)),
storage_root TEXT,
revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL
) STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_project_workspaces_project ON project_workspaces(project_id,status,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS workspace_app_instances (
id TEXT PRIMARY KEY,
project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE CASCADE,
app_id TEXT NOT NULL,
app_version TEXT,
enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
configuration_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(configuration_json)),
state_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(state_json)),
permissions_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(permissions_json)),
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL,
UNIQUE(project_workspace_id,app_id,id)
) STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_workspace_apps_workspace ON workspace_app_instances(project_workspace_id,enabled)`,
		`CREATE TABLE IF NOT EXISTS workspace_integration_grants (
id TEXT PRIMARY KEY,
project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE CASCADE,
connection_id TEXT NOT NULL,
enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
permissions_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(permissions_json)),
agent_overrides_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(agent_overrides_json)),
revision INTEGER NOT NULL DEFAULT 1,
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL,
UNIQUE(project_workspace_id,connection_id)
) STRICT`,
		`CREATE TABLE IF NOT EXISTS project_library_assets (
id TEXT PRIMARY KEY,
project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
name TEXT NOT NULL,
asset_type TEXT NOT NULL,
current_version INTEGER NOT NULL DEFAULT 1 CHECK (current_version >= 1),
archived INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0,1)),
security_json TEXT NOT NULL DEFAULT '{"executable":false,"trusted":false}' CHECK (json_valid(security_json)),
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL
) STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_project_library_assets_project ON project_library_assets(project_id,archived,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS project_library_asset_versions (
asset_id TEXT NOT NULL REFERENCES project_library_assets(id) ON DELETE CASCADE,
version INTEGER NOT NULL CHECK (version >= 1),
content_hash TEXT NOT NULL,
size_bytes INTEGER,
mime_type TEXT,
storage_uri TEXT NOT NULL,
provenance_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(provenance_json)),
created_at INTEGER NOT NULL,
PRIMARY KEY(asset_id,version)
) STRICT`,
		`CREATE TABLE IF NOT EXISTS workspace_library_grants (
id TEXT PRIMARY KEY,
project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE CASCADE,
asset_id TEXT NOT NULL REFERENCES project_library_assets(id) ON DELETE CASCADE,
enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
version_policy TEXT NOT NULL DEFAULT 'latest' CHECK (version_policy IN ('latest','pinned')),
pinned_version INTEGER,
permissions_json TEXT NOT NULL DEFAULT '{"read":true,"modify":false,"create_derivative":true,"execute":false,"delete":false}' CHECK (json_valid(permissions_json)),
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL,
UNIQUE(project_workspace_id,asset_id)
) STRICT`,
		`CREATE TABLE IF NOT EXISTS model_identity_specs (
model_uid TEXT PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
schema_version TEXT NOT NULL DEFAULT 'onepane.model-spec/v1',
investigation_state TEXT NOT NULL DEFAULT 'uninvestigated' CHECK (investigation_state IN ('uninvestigated','specified','benchmarked','agent-tested','qualified')),
spec_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(spec_json)),
qualification_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(qualification_json)),
archived INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0,1)),
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL
) STRICT`,
		`INSERT OR IGNORE INTO model_identity_specs(model_uid,investigation_state,spec_json,qualification_json,created_at,updated_at)
SELECT m.id,
CASE WHEN EXISTS (SELECT 1 FROM model_spec_sheets ms JOIN model_deployments d ON d.id=ms.deployment_id WHERE d.model_id=m.id AND ms.admission_status IN ('accepted','restricted')) THEN 'qualified' ELSE 'uninvestigated' END,
'{}','{}',m.created_at,m.updated_at FROM models m`,
		`CREATE TABLE IF NOT EXISTS runtime_compute_profiles (
deployment_id TEXT PRIMARY KEY REFERENCES model_deployments(id) ON DELETE CASCADE,
compute_mode TEXT NOT NULL DEFAULT 'auto' CHECK (compute_mode IN ('auto','cpu','gpu','hybrid','cloud','remote')),
backend TEXT,
cpu_threads INTEGER,
ram_bytes INTEGER,
gpu_device_ids_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(gpu_device_ids_json)),
vram_bytes INTEGER,
gpu_layers INTEGER,
total_layers INTEGER,
gpu_layer_ratio REAL,
cached INTEGER NOT NULL DEFAULT 0 CHECK (cached IN (0,1)),
resident INTEGER NOT NULL DEFAULT 0 CHECK (resident IN (0,1)),
metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
updated_at INTEGER NOT NULL
) STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_runtime_compute_mode ON runtime_compute_profiles(compute_mode,backend,resident,cached)`,
		`CREATE TABLE IF NOT EXISTS runtime_execution_queue (
id TEXT PRIMARY KEY,
workspace_id TEXT,
project_id TEXT,
project_workspace_id TEXT,
task_id TEXT,
inference_request_id TEXT,
deployment_id TEXT NOT NULL REFERENCES model_deployments(id),
node_id TEXT,
priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('background','normal','high','interactive')),
state TEXT NOT NULL CHECK (state IN ('queued','waiting_runtime','waiting_swap','loading','ready','running','draining','completed','failed','cancelled')),
safe_boundary TEXT NOT NULL DEFAULT 'safe_pause' CHECK (safe_boundary IN ('unsafe','safe_turn','safe_pause','complete')),
resource_claim_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(resource_claim_json)),
queued_at INTEGER NOT NULL,
started_at INTEGER,
completed_at INTEGER,
failure_reason TEXT,
revision INTEGER NOT NULL DEFAULT 1
) STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_runtime_queue_state ON runtime_execution_queue(state,priority,queued_at)`,
		`CREATE INDEX IF NOT EXISTS idx_runtime_queue_deployment ON runtime_execution_queue(deployment_id,state,queued_at)`,
		`CREATE TABLE IF NOT EXISTS remote_node_compute_policies (
node_id TEXT PRIMARY KEY REFERENCES harness_nodes(id) ON DELETE CASCADE,
enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
idle_only INTEGER NOT NULL DEFAULT 0 CHECK (idle_only IN (0,1)),
availability_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(availability_json)),
limits_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(limits_json)),
allow_model_downloads INTEGER NOT NULL DEFAULT 1 CHECK (allow_model_downloads IN (0,1)),
runtime_installation TEXT NOT NULL DEFAULT 'confirm' CHECK (runtime_installation IN ('allow','confirm','deny')),
project_scope TEXT NOT NULL DEFAULT 'all' CHECK (project_scope IN ('all','selected')),
allowed_projects_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(allowed_projects_json)),
updated_by TEXT,
revision INTEGER NOT NULL DEFAULT 1,
created_at INTEGER NOT NULL,
updated_at INTEGER NOT NULL
) STRICT`,
		`CREATE TABLE IF NOT EXISTS project_storage_locations (
project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
storage_root TEXT NOT NULL,
previous_root TEXT,
move_state TEXT NOT NULL DEFAULT 'ready' CHECK (move_state IN ('ready','preparing','copying','verifying','switching','rollback','failed')),
move_detail_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(move_detail_json)),
revision INTEGER NOT NULL DEFAULT 1,
updated_at INTEGER NOT NULL
) STRICT`,
		`CREATE TABLE IF NOT EXISTS global_resource_identities (
resource_uid TEXT PRIMARY KEY,
kind TEXT NOT NULL,
canonical_identity TEXT NOT NULL UNIQUE,
location_json TEXT NOT NULL CHECK (json_valid(location_json)),
version TEXT,
content_hash TEXT,
updated_at INTEGER NOT NULL
) STRICT`,
		`CREATE TABLE IF NOT EXISTS global_resource_leases (
id TEXT PRIMARY KEY,
resource_uid TEXT NOT NULL REFERENCES global_resource_identities(resource_uid) ON DELETE CASCADE,
lease_mode TEXT NOT NULL CHECK (lease_mode IN ('read','write')),
status TEXT NOT NULL CHECK (status IN ('queued','active','released','expired','cancelled')),
owner_json TEXT NOT NULL CHECK (json_valid(owner_json)),
expected_version TEXT,
expected_hash TEXT,
requested_at INTEGER NOT NULL,
acquired_at INTEGER,
heartbeat_at INTEGER,
expires_at INTEGER,
released_at INTEGER,
revision INTEGER NOT NULL DEFAULT 1
) STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_global_resource_lease_queue ON global_resource_leases(resource_uid,status,requested_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_global_resource_one_active_writer ON global_resource_leases(resource_uid) WHERE status='active' AND lease_mode='write'`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("alpha3 schema: %w", err)
		}
	}
	return nil
}
