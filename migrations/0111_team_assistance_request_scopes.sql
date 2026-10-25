-- Capture scope at the committed human wait. Historical requests without a
-- scope record remain unavailable; current Task scope is not past evidence.
CREATE TABLE team_assistance_request_scopes (
 request_id TEXT PRIMARY KEY REFERENCES team_assistance_requests(id),
 project_id TEXT REFERENCES projects(id),
 project_workspace_id TEXT REFERENCES project_workspaces(id),
 artifact_session_id TEXT REFERENCES artifact_sessions(id)
) STRICT;
CREATE TRIGGER team_assistance_request_scope_immutable BEFORE UPDATE ON team_assistance_request_scopes
BEGIN SELECT RAISE(ABORT,'Team assistance request scopes are immutable'); END;
CREATE TRIGGER team_assistance_request_scope_retained BEFORE DELETE ON team_assistance_request_scopes
BEGIN SELECT RAISE(ABORT,'Team assistance request scopes must be retained'); END;
