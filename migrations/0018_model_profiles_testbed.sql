-- M35: backend-neutral model placement, spec sheets, manual testbed and production admission.
-- Automated qualification proves technical operability; manual admission governs normal scheduling.

CREATE TABLE model_spec_sheets (
    deployment_id           TEXT PRIMARY KEY REFERENCES model_deployments(id),
    model_id                TEXT NOT NULL REFERENCES models(id),
    hardware_profile_id     TEXT NOT NULL REFERENCES local_hardware_profiles(id),
    placement_json          TEXT NOT NULL CHECK (json_valid(placement_json)),
    catalog_claims_json     TEXT NOT NULL CHECK (json_valid(catalog_claims_json)),
    llmfit_json             TEXT NOT NULL CHECK (json_valid(llmfit_json)),
    qualification_json      TEXT NOT NULL CHECK (json_valid(qualification_json)),
    restrictions_json       TEXT NOT NULL CHECK (json_valid(restrictions_json)),
    admission_status        TEXT NOT NULL CHECK (admission_status IN ('pending','accepted','restricted','rejected')),
    admitted_by             TEXT REFERENCES principals(id),
    admission_notes         TEXT,
    admitted_at             INTEGER,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_model_spec_admission ON model_spec_sheets(admission_status, updated_at DESC);

-- Existing RC7 managed deployments are intentionally moved behind the new
-- manual-admission gate, but must remain inspectable/Testbed-able after upgrade.
-- Backfill a pending spec sheet from the durable install plan so operators do
-- not have to re-download or re-qualify a model merely to review it.
INSERT INTO model_spec_sheets(
    deployment_id,model_id,hardware_profile_id,placement_json,
    catalog_claims_json,llmfit_json,qualification_json,restrictions_json,
    admission_status,revision,created_at,updated_at
)
SELECT
    mm.deployment_id,
    d.model_id,
    p.hardware_profile_id,
    COALESCE(json_extract(p.plan_json,'$.placement'),'{}'),
    CASE WHEN json_valid(m.static_metadata_json) THEN m.static_metadata_json ELSE '{}' END,
    COALESCE(json_extract(p.plan_json,'$.llmfit'),'{}'),
    COALESCE((
        SELECT json_object(
            'status', q.status,
            'verified_context', q.verified_context,
            'protocol_level', q.protocol_level,
            'tokens_per_second', q.tokens_per_second,
            'ttft_ms', q.ttft_ms,
            'metrics', json(q.metrics_json),
            'evidence', json(q.evidence_json)
        )
        FROM local_model_qualification_runs q
        WHERE q.deployment_id=mm.deployment_id
          AND q.status IN ('succeeded','limited')
        ORDER BY COALESCE(q.completed_at,q.updated_at) DESC
        LIMIT 1
    ), '{}'),
    '{}',
    'pending',
    1,
    CAST(strftime('%s','now') AS INTEGER)*1000,
    CAST(strftime('%s','now') AS INTEGER)*1000
FROM managed_local_models mm
JOIN model_deployments d ON d.id=mm.deployment_id
JOIN models m ON m.id=d.model_id
JOIN local_model_install_plans p ON p.id=mm.plan_id
WHERE NOT EXISTS (SELECT 1 FROM model_spec_sheets ms WHERE ms.deployment_id=mm.deployment_id);

CREATE TABLE model_testbed_sessions (
    id                      TEXT PRIMARY KEY,
    deployment_id           TEXT NOT NULL REFERENCES model_deployments(id),
    hardware_profile_id     TEXT NOT NULL REFERENCES local_hardware_profiles(id),
    placement_json          TEXT NOT NULL CHECK (json_valid(placement_json)),
    status                  TEXT NOT NULL CHECK (status IN ('active','completed','cancelled')),
    created_by              TEXT REFERENCES principals(id),
    notes                   TEXT,
    revision                INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    started_at              INTEGER NOT NULL,
    completed_at            INTEGER,
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
) STRICT;
CREATE INDEX idx_model_testbed_deployment ON model_testbed_sessions(deployment_id, created_at DESC);

CREATE TABLE model_testbed_turns (
    id                      TEXT PRIMARY KEY,
    session_id              TEXT NOT NULL REFERENCES model_testbed_sessions(id),
    sequence_no             INTEGER NOT NULL CHECK (sequence_no >= 1),
    request_json            TEXT NOT NULL CHECK (json_valid(request_json)),
    response_json           TEXT NOT NULL CHECK (json_valid(response_json)),
    usage_json              TEXT NOT NULL CHECK (json_valid(usage_json)),
    metrics_json            TEXT NOT NULL CHECK (json_valid(metrics_json)),
    synthetic_tool_probe    INTEGER NOT NULL DEFAULT 0 CHECK (synthetic_tool_probe IN (0,1)),
    created_at              INTEGER NOT NULL,
    UNIQUE (session_id, sequence_no)
) STRICT;
