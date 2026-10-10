-- Research Council explicit, seat-scoped, local-only human consent.
-- The local-origin Team Worker reads this metadata only after independently
-- checking live Council isolation, version grants and managed artifact bytes.
CREATE TABLE research_evidence_seat_approvals (
 session_id TEXT PRIMARY KEY REFERENCES team_sessions(id) ON DELETE RESTRICT,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id),
 project_id TEXT NOT NULL REFERENCES projects(id),
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id),
 research_manifest_sha256 TEXT NOT NULL,
 evidence_manifest_sha256 TEXT NOT NULL,
 seat_ids_json TEXT NOT NULL CHECK(json_valid(seat_ids_json)),
 policy TEXT NOT NULL CHECK(policy='local_only'),
 state TEXT NOT NULL CHECK(state IN ('approved','revoked')),
 approved_by TEXT NOT NULL REFERENCES principals(id),
 approved_at INTEGER NOT NULL,
 revoked_by TEXT REFERENCES principals(id),
 revoked_at INTEGER,
 CHECK((state='approved' AND revoked_by IS NULL AND revoked_at IS NULL)
    OR (state='revoked' AND revoked_by IS NOT NULL AND revoked_at IS NOT NULL))
) STRICT;
CREATE INDEX idx_research_seat_approval_workspace
 ON research_evidence_seat_approvals(project_id,project_workspace_id,state);
