-- RC11 immutable operator-approved Task graph dependency snapshots.
-- The original 0047 schema recorded the graph hash but not the precise
-- expected predecessor list independently of mutable task_dependencies.
-- Never backfill a guessed "approved" list for historical graphs: the
-- original operator plan cannot be proven from a mutable edge inventory.
-- Legacy nodes remain review-required until separately re-authorized.
ALTER TABLE project_orchestrator_task_graph_nodes
 ADD COLUMN approved_dependencies_json TEXT NOT NULL DEFAULT '[]'
 CHECK(json_valid(approved_dependencies_json));
ALTER TABLE project_orchestrator_task_graph_nodes
 ADD COLUMN approved_dependencies_recorded INTEGER NOT NULL DEFAULT 0
 CHECK(approved_dependencies_recorded IN (0,1));
