-- Node telemetry grants must bind original Goal plan and generation.
ALTER TABLE project_continuous_goals
 ADD COLUMN plan_generation INTEGER NOT NULL DEFAULT 1 CHECK(plan_generation>=1);

-- Plan edits advance generation, including edits later reversed to old JSON.
CREATE TRIGGER goal_monitoring_plan_generation_on_edit
 AFTER UPDATE OF monitoring_plan_json ON project_continuous_goals
 WHEN NEW.monitoring_plan_json!=OLD.monitoring_plan_json
 BEGIN
  UPDATE project_continuous_goals SET plan_generation=OLD.plan_generation+1
   WHERE id=NEW.id;
 END;

ALTER TABLE project_goal_node_measurement_grants
 ADD COLUMN plan_sha256 TEXT NOT NULL DEFAULT ''
 CHECK(plan_sha256='' OR
  (length(plan_sha256)=71 AND substr(plan_sha256,1,7)='sha256:'));

-- Historical grants get 0 and empty hash; never grandfather their authority.
ALTER TABLE project_goal_node_measurement_grants
 ADD COLUMN plan_generation INTEGER NOT NULL DEFAULT 0 CHECK(plan_generation>=0);

DROP TRIGGER goal_node_measurement_grant_immutable;
CREATE TRIGGER goal_node_measurement_grant_immutable
 BEFORE UPDATE ON project_goal_node_measurement_grants
 WHEN NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id OR
  NEW.goal_id!=OLD.goal_id OR NEW.project_workspace_id!=OLD.project_workspace_id OR
  NEW.source_id!=OLD.source_id OR NEW.peer_node_id!=OLD.peer_node_id OR
  NEW.peer_tls_fingerprint!=OLD.peer_tls_fingerprint OR
  NEW.plan_sha256!=OLD.plan_sha256 OR
  NEW.plan_generation!=OLD.plan_generation OR
  NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at OR
  NEW.expires_at!=OLD.expires_at OR NEW.revision!=OLD.revision+1 OR
  NEW.last_sequence<OLD.last_sequence OR OLD.status='revoked' OR
  (NEW.status='revoked' AND
    (NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at))
 BEGIN SELECT RAISE(ABORT,'Node transport grant scope immutable'); END;
