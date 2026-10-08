-- OnePane Assistant model pin. An absent row means automatic scheduler routing.
-- Preferences are thread-scoped and cannot affect Project Orchestrator routing.
CREATE TABLE IF NOT EXISTS assistant_model_preferences (
 thread_id TEXT PRIMARY KEY REFERENCES assistant_threads(id) ON DELETE CASCADE,
 deployment_id TEXT NOT NULL REFERENCES model_deployments(id) ON DELETE CASCADE,
 updated_at INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS idx_assistant_model_preferences_deployment ON assistant_model_preferences(deployment_id);
