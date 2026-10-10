-- RC11 immutable, human-approved general-purpose Workspace toolchain revisions.
-- Never inferred from model output, inventory scan or claimed successful builds.
-- Each revision is append-only and tenant-scoped by canonical Project/Workspace FKs.
CREATE TABLE project_workspace_toolchain_manifests (
 project_workspace_id TEXT NOT NULL REFERENCES project_workspaces(id) ON DELETE RESTRICT,
 revision INTEGER NOT NULL CHECK(revision>=1),
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
 application_id TEXT NOT NULL REFERENCES project_applications(id) ON DELETE RESTRICT,
 image_ref TEXT NOT NULL,
 application_revision INTEGER NOT NULL CHECK(application_revision>=1),
 requirements_json TEXT NOT NULL CHECK(json_valid(requirements_json) AND json_type(requirements_json)='array'),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
 approved_by TEXT NOT NULL REFERENCES principals(id),
 approved_at INTEGER NOT NULL,
 PRIMARY KEY(project_workspace_id, revision)
) STRICT;
CREATE INDEX idx_project_workspace_toolchain_manifests_project
 ON project_workspace_toolchain_manifests(project_id,project_workspace_id,revision);
CREATE TRIGGER project_workspace_toolchain_manifests_immutable_update
 BEFORE UPDATE ON project_workspace_toolchain_manifests
 BEGIN SELECT RAISE(ABORT,'approved Workspace toolchain revisions are immutable'); END;
CREATE TRIGGER project_workspace_toolchain_manifests_immutable_delete
 BEFORE DELETE ON project_workspace_toolchain_manifests
 BEGIN SELECT RAISE(ABORT,'approved Workspace toolchain revisions are immutable'); END;
