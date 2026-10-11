-- Human-only historical Project Task graph dependency reattestation.
-- No Task, predecessor, or execution status is rewritten by this audit.
CREATE TABLE project_orchestrator_graph_reattestations (
 graph_id TEXT PRIMARY KEY REFERENCES project_orchestrator_task_graphs(id) ON DELETE RESTRICT,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=71),
 reviewed_by TEXT NOT NULL REFERENCES principals(id) ON DELETE RESTRICT,
 reviewed_at INTEGER NOT NULL,
 review_evidence_sha256 TEXT NOT NULL CHECK(length(review_evidence_sha256)=64),
 CHECK(reviewed_at > 0)
) STRICT;
CREATE INDEX idx_graph_reattestations_project ON project_orchestrator_graph_reattestations(project_id, reviewed_at);
