-- RC11 P0-AM: a restored identical monitoring-plan hash cannot revive
-- a historical human TCP4 grant and its old completed source reports.
-- Existing permits are intentionally invalidated on upgrade until a
-- human creates a NEW scope under the CURRENT Goal generation.
ALTER TABLE project_goal_tcp_probe_permits
 ADD COLUMN plan_generation INTEGER NOT NULL DEFAULT 0 CHECK(plan_generation>=0);

-- This generation is part of the original human approval, not a display
-- field. Even a successful plan rollback may not rewrite old consent.
CREATE TRIGGER goal_tcp_probe_generation_immutable
 BEFORE UPDATE OF plan_generation ON project_goal_tcp_probe_permits
 BEGIN SELECT RAISE(ABORT,'TCP approved plan generation immutable'); END;
