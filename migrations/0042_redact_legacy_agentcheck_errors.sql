-- RC11 privacy correction: historical manual Agent Check qualification evidence
-- from the v2 formatter can contain raw runtime/probe exception text.
-- Replace only known free-text evidence.errors arrays, retaining every other
-- qualification field, deployment, admission, identity and testbed history.
-- Additive migration; no filesystem paths, models or Workspace data touched.
--
-- This repairs live SQLite logical rows; it does not erase previously exported
-- copies, database backups, or residual WAL/free-page data.
UPDATE model_spec_sheets
SET qualification_json=json_set(
 json_remove(qualification_json,'$.evidence.errors'),
 '$.evidence.legacy_error_details','redacted_on_upgrade'),
 revision=revision+1,
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE json_extract(qualification_json,'$.profile_version')='onepane.manual-agent-check/v2'
  AND json_type(qualification_json,'$.evidence.errors') IS NOT NULL;

-- Model identity admissions may snapshot the entire manual qualification as
-- $.qualification; clean that nested copy independently without destroying
-- the immutable model identity or admission decision.
UPDATE model_identity_specs
SET qualification_json=json_set(
 json_remove(qualification_json,'$.qualification.evidence.errors'),
 '$.qualification.evidence.legacy_error_details','redacted_on_upgrade'),
 updated_at=CAST(strftime('%s','now') AS INTEGER)*1000
WHERE json_extract(qualification_json,'$.qualification.profile_version')='onepane.manual-agent-check/v2'
  AND json_type(qualification_json,'$.qualification.evidence.errors') IS NOT NULL;
