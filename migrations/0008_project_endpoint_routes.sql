-- M22 trusted Project ingress. Routes are derived only from V2+ verified
-- project-application observations; API proxying never trusts caller targets.
CREATE TABLE project_endpoint_routes (
    endpoint_id              TEXT PRIMARY KEY REFERENCES project_runtime_endpoints(id),
    project_runtime_id       TEXT NOT NULL REFERENCES project_runtimes(id),
    application_id           TEXT NOT NULL REFERENCES project_applications(id),
    host_ip                  TEXT NOT NULL CHECK (host_ip = '127.0.0.1'),
    host_port                INTEGER NOT NULL CHECK (host_port >= 1 AND host_port <= 65535),
    transport_protocol       TEXT NOT NULL CHECK (transport_protocol IN ('tcp')),
    observation_id           TEXT NOT NULL REFERENCES observations(id),
    verification_id          TEXT NOT NULL REFERENCES verifications(id),
    application_revision     INTEGER NOT NULL CHECK (application_revision >= 1),
    endpoint_revision        INTEGER NOT NULL CHECK (endpoint_revision >= 1),
    container_spec_hash      TEXT NOT NULL,
    status                   TEXT NOT NULL CHECK (status IN ('verified','stale')),
    updated_at               INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_project_endpoint_routes_runtime ON project_endpoint_routes(project_runtime_id,status,updated_at);
CREATE INDEX idx_project_endpoint_routes_app ON project_endpoint_routes(application_id,status,updated_at);
