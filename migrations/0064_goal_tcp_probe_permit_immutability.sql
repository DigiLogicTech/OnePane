-- RC11: after human approval, a network grant's source, exact target,
-- scope and expiry cannot be broadened or silently edited in place. The
-- ONLY permitted update is an audited transition to revoked with revision
-- increment. Any different target requires a fresh permit and human grant.
CREATE TRIGGER goal_tcp_probe_permits_scope_immutable
 BEFORE UPDATE ON project_goal_tcp_probe_permits
 WHEN NEW.id!=OLD.id OR NEW.project_id!=OLD.project_id
  OR NEW.goal_id!=OLD.goal_id
  OR NEW.project_workspace_id!=OLD.project_workspace_id
  OR NEW.source_id!=OLD.source_id OR NEW.plan_hash!=OLD.plan_hash
  OR NEW.probe_kind!=OLD.probe_kind OR NEW.target_ipv4!=OLD.target_ipv4
  OR NEW.target_port!=OLD.target_port OR NEW.timeout_ms!=OLD.timeout_ms
  OR NEW.effect_class!=OLD.effect_class
  OR NEW.approved_by!=OLD.approved_by OR NEW.approved_at!=OLD.approved_at
  OR NEW.expires_at!=OLD.expires_at
  OR OLD.status!='approved' OR NEW.status!='revoked'
  OR NEW.revision!=OLD.revision+1
  OR NEW.revoked_by IS NULL OR NEW.revoked_at<OLD.approved_at
 BEGIN SELECT RAISE(ABORT,'approved network probe scope is immutable'); END;
