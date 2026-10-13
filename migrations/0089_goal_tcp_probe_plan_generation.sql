-- RC11 P0-AM: a restored identical monitoring-plan hash cannot revive
-- a historical human TCP4 grant and its old completed source reports.
-- Existing permits are intentionally invalidated on upgrade until a
-- human creates a NEW scope under the CURRENT Goal generation.
ALTER TABLE project_goal_tcp_probe_permits
 ADD COLUMN plan_generation INTEGER NOT NULL DEFAULT 0 CHECK(plan_generation>=0);
