CREATE TABLE research_session_evidence_metadata (
 session_id TEXT PRIMARY KEY REFERENCES team_sessions(id) ON DELETE RESTRICT,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id),
 project_id TEXT NOT NULL REFERENCES projects(id),
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id),
 research_manifest_sha256 TEXT NOT NULL,
 evidence_manifest_sha256 TEXT NOT NULL,
 entries_json TEXT NOT NULL CHECK(json_valid(entries_json)),
 selection_json TEXT NOT NULL CHECK(json_valid(selection_json)),
 approved_by TEXT NOT NULL REFERENCES principals(id),
 approved_at INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_research_evidence_project ON research_session_evidence_metadata(project_id,project_workspace_id,approved_at);
