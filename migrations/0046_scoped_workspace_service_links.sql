-- RC11: service connectivity admission records only. No network interface, bind
-- mount, proxy port or forwarding permission is created by this schema.
-- Every grant pins a *verified* loopback route's identity and expires.
CREATE TABLE project_workspace_service_links (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 source_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 target_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 endpoint_id TEXT NOT NULL REFERENCES project_runtime_endpoints(id) ON DELETE RESTRICT,
 application_id TEXT NOT NULL REFERENCES project_applications(id) ON DELETE RESTRICT,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 approved_path TEXT NOT NULL CHECK(length(approved_path) BETWEEN 1 AND 128),
 application_revision INTEGER NOT NULL CHECK(application_revision>=1),
 endpoint_revision INTEGER NOT NULL CHECK(endpoint_revision>=1),
 container_spec_hash TEXT NOT NULL,
 verification_id TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms>0),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>=1),
 created_by TEXT NOT NULL REFERENCES principals(id),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 CHECK(source_workspace_id<>target_workspace_id),
 UNIQUE(project_id,source_workspace_id,target_workspace_id,endpoint_id,approved_path)
) STRICT;
CREATE INDEX idx_project_workspace_service_links_target
 ON project_workspace_service_links(project_id,target_workspace_id,enabled,expires_at_ms);
