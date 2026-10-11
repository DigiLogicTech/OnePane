-- RC11 bounded Project Orchestrator Task DAGs.
-- One human-authorized idempotent graph creates all Tasks/edges atomically.
CREATE TABLE project_orchestrator_task_graphs (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 idempotency_key TEXT NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 120),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=71),
 node_count INTEGER NOT NULL CHECK(node_count BETWEEN 1 AND 32),
 created_by TEXT NOT NULL REFERENCES principals(id),
 created_at INTEGER NOT NULL,
 UNIQUE(project_id,idempotency_key)
) STRICT;
CREATE TABLE project_orchestrator_task_graph_nodes (
 graph_id TEXT NOT NULL REFERENCES project_orchestrator_task_graphs(id) ON DELETE RESTRICT,
 node_key TEXT NOT NULL CHECK(length(node_key) BETWEEN 1 AND 40),
 task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE RESTRICT,
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 position INTEGER NOT NULL CHECK(position BETWEEN 0 AND 31),
 PRIMARY KEY(graph_id,node_key),
 UNIQUE(graph_id,position)
) STRICT;
CREATE INDEX idx_orchestrator_task_graphs_project ON project_orchestrator_task_graphs(project_id,created_at);
