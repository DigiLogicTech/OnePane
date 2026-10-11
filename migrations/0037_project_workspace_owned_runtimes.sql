-- Non-destructive canonical Workspace runtime migration.
-- Existing Project runtimes remain legacy shared Project environments.
-- New runtime rows own exactly one Project Workspace, with independent
-- sandbox-runner runtime identity. All child applications, endpoints, routines,
-- observations and change proposals continue referencing the same runtime IDs.
CREATE TABLE project_runtimes_rebuilt (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id),
 project_workspace_id TEXT REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 node_id TEXT REFERENCES harness_nodes(id),
 isolation_mode TEXT NOT NULL CHECK(isolation_mode IN ('sandboxed_container','microvm')),
 backend TEXT NOT NULL CHECK(backend IN ('sandbox_runner')),
 desired_state TEXT NOT NULL CHECK(desired_state IN ('stopped','running','suspended')),
 status TEXT NOT NULL CHECK(status IN ('defined','provisioning','stopped','starting','running','degraded','failed','suspended','deleting')),
 runtime_spec_json TEXT NOT NULL CHECK(json_valid(runtime_spec_json)),
 resource_limits_json TEXT NOT NULL CHECK(json_valid(resource_limits_json)),
 network_policy_json TEXT NOT NULL CHECK(json_valid(network_policy_json)),
 filesystem_policy_json TEXT NOT NULL CHECK(json_valid(filesystem_policy_json)),
 environment_bindings_json TEXT NOT NULL CHECK(json_valid(environment_bindings_json)),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>=1),
 created_by TEXT NOT NULL REFERENCES principals(id),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 CHECK(project_workspace_id IS NULL OR project_workspace_id <> '')
) STRICT;

INSERT INTO project_runtimes_rebuilt(
 id,project_id,project_workspace_id,node_id,isolation_mode,backend,
 desired_state,status,runtime_spec_json,resource_limits_json,
 network_policy_json,filesystem_policy_json,environment_bindings_json,
 revision,created_by,created_at,updated_at)
 SELECT id,project_id,NULL,node_id,isolation_mode,backend,
 desired_state,status,runtime_spec_json,resource_limits_json,
 network_policy_json,filesystem_policy_json,environment_bindings_json,
 revision,created_by,created_at,updated_at
 FROM project_runtimes;

DROP TABLE project_runtimes;
ALTER TABLE project_runtimes_rebuilt RENAME TO project_runtimes;
CREATE INDEX idx_project_runtimes_status ON project_runtimes(status,desired_state,updated_at);
-- One legacy shared runtime per Project, never implicitly adopted by a Workspace.
CREATE UNIQUE INDEX idx_project_runtime_legacy_project
 ON project_runtimes(project_id) WHERE project_workspace_id IS NULL;
-- One independent runtime per Workspace at this stage. Additional build/test
-- environments will use explicit child instances in a subsequent migration.
CREATE UNIQUE INDEX idx_project_runtime_workspace
 ON project_runtimes(project_workspace_id) WHERE project_workspace_id IS NOT NULL;
CREATE INDEX idx_project_runtimes_project_workspace
 ON project_runtimes(project_id,project_workspace_id);
