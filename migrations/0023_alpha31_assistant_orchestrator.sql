-- Alpha 3.1: global Assistant and one logical Project Orchestrator per Project.

CREATE TABLE assistant_threads (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title             TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    active_project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    created_by        TEXT NOT NULL REFERENCES principals(id),
    revision          INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_assistant_threads_workspace ON assistant_threads(workspace_id,status,updated_at DESC);

CREATE TABLE assistant_turns (
    id               TEXT PRIMARY KEY,
    thread_id        TEXT NOT NULL REFERENCES assistant_threads(id) ON DELETE CASCADE,
    role             TEXT NOT NULL CHECK (role IN ('user','assistant','system')),
    content          TEXT NOT NULL,
    project_id       TEXT REFERENCES projects(id) ON DELETE SET NULL,
    task_id          TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    provenance_json  TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(provenance_json)),
    created_at       INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_assistant_turns_thread ON assistant_turns(thread_id,created_at,id);

CREATE TABLE project_orchestrators (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready','working','waiting','blocked','degraded')),
    configuration_json  TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(configuration_json)),
    last_event_sequence INTEGER NOT NULL DEFAULT 0 CHECK (last_event_sequence >= 0),
    revision            INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE project_orchestrator_turns (
    id                  TEXT PRIMARY KEY,
    orchestrator_id     TEXT NOT NULL REFERENCES project_orchestrators(id) ON DELETE CASCADE,
    assistant_thread_id TEXT REFERENCES assistant_threads(id) ON DELETE SET NULL,
    task_id             TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    role                TEXT NOT NULL CHECK (role IN ('user','orchestrator','system')),
    content             TEXT NOT NULL,
    context_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(context_json)),
    provenance_json     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(provenance_json)),
    created_at          INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_project_orchestrator_turns ON project_orchestrator_turns(orchestrator_id,created_at,id);

CREATE TABLE assistant_project_handoffs (
    id                   TEXT PRIMARY KEY,
    assistant_thread_id  TEXT NOT NULL REFERENCES assistant_threads(id) ON DELETE CASCADE,
    assistant_turn_id    TEXT REFERENCES assistant_turns(id) ON DELETE SET NULL,
    project_id           TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    orchestrator_id      TEXT NOT NULL REFERENCES project_orchestrators(id) ON DELETE CASCADE,
    orchestrator_turn_id TEXT REFERENCES project_orchestrator_turns(id) ON DELETE SET NULL,
    task_id              TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    status               TEXT NOT NULL CHECK (status IN ('delegated','working','waiting','completed','failed','cancelled')),
    objective            TEXT NOT NULL,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_assistant_handoffs_project ON assistant_project_handoffs(project_id,status,updated_at DESC);
