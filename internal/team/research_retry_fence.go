package team

import (
 "context"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// Automatic Research retries are intentionally bounded. After two provider-
// timer retries the Council remains paused for explicit operator intervention.
const maxAutomaticResearchRetryAttempts = 2

var errResearchConsentNoLongerValid=errors.New("Research evidence consent or Project scope changed; create a new protected Council")

// No retry may convert a previously evidence-protected Council into an
// ordinary unprotected session, including after grant withdrawal, Team
// archiving, human membership revocation or a pinned manifest change.
// Called INSIDE the same write transaction as the pending turn transition.
func (s *Service) researchRetryConsentTx(ctx context.Context,tx storage.Tx,sessionID,tenant string)error{
 var existing int
 if err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM research_evidence_seat_approvals
  WHERE session_id=?`,sessionID).Scan(&existing);err!=nil{return err}
 if existing==0{return nil}
 var eligible int
 err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM research_evidence_seat_approvals approval
 JOIN research_session_evidence_metadata frozen ON frozen.session_id=approval.session_id
  AND frozen.workspace_id=approval.workspace_id
  AND frozen.project_id=approval.project_id
  AND frozen.project_workspace_id=approval.project_workspace_id
  AND frozen.research_manifest_sha256=approval.research_manifest_sha256
  AND frozen.evidence_manifest_sha256=approval.evidence_manifest_sha256
 JOIN team_sessions session ON session.id=approval.session_id
  AND session.workspace_id=approval.workspace_id
 JOIN team_session_manifests manifest ON manifest.session_id=session.id
  AND manifest.snapshot_sha256=approval.research_manifest_sha256
  AND manifest.research_mode=1 AND manifest.execution_mode='council'
 JOIN teams team ON team.id=session.team_id AND team.workspace_id=session.workspace_id
  AND team.status='active'
 JOIN tasks task ON task.id=session.task_id AND task.workspace_id=session.workspace_id
  AND task.project_id=approval.project_id
  AND task.project_workspace_id=approval.project_workspace_id
 JOIN projects project ON project.id=task.project_id
  AND project.workspace_id=session.workspace_id AND project.status='active'
 JOIN project_workspaces workspace ON workspace.id=task.project_workspace_id
  AND workspace.project_id=project.id AND workspace.status='active'
 JOIN workspaces tenant ON tenant.id=session.workspace_id AND tenant.status='active'
 JOIN principals human ON human.id=approval.approved_by
  AND human.principal_type='human' AND human.status='active'
 JOIN workspace_memberships membership ON membership.principal_id=human.id
  AND membership.workspace_id=session.workspace_id AND membership.status='active'
 WHERE approval.session_id=? AND approval.workspace_id=?
  AND approval.policy='local_only' AND approval.state='approved'`,
 sessionID,tenant).Scan(&eligible)
 if err!=nil{return err}
 if eligible!=1{return errResearchConsentNoLongerValid}
 // A human consent record is not a permanent Library lease. Before resuming
 // retries, every exact frozen version must still be readable by this PWS.
 var count,denied int
 err=tx.QueryRowContext(ctx,`SELECT COUNT(*),
  COALESCE(SUM(CASE WHEN EXISTS(
   SELECT 1 FROM project_library_assets a
   JOIN project_library_asset_versions v ON v.asset_id=a.id
   JOIN research_session_evidence_metadata frozen
    ON frozen.session_id=? AND frozen.project_id=a.project_id
   WHERE a.id=json_extract(item.value,'$.asset_id')
    AND a.archived=0 AND v.version=json_extract(item.value,'$.version')
    AND v.content_hash=json_extract(item.value,'$.content_hash')
    AND v.size_bytes=json_extract(item.value,'$.size_bytes')
    AND v.mime_type=json_extract(item.value,'$.mime_type')
    AND (
     EXISTS(SELECT 1 FROM workspace_library_grants grant
      WHERE grant.asset_id=a.id
       AND grant.project_workspace_id=frozen.project_workspace_id
       AND grant.enabled=1
       AND json_extract(grant.permissions_json,'$.read')=1
       AND ((grant.version_policy='latest' AND v.version=a.current_version)
        OR (grant.version_policy='pinned' AND grant.pinned_version=v.version)))
     OR EXISTS(SELECT 1 FROM project_workspace_publications pub
      JOIN project_workspace_links link ON link.id=pub.link_id
      WHERE pub.asset_id=a.id AND pub.asset_version=v.version
       AND pub.content_hash=v.content_hash
       AND link.project_id=a.project_id
       AND link.target_workspace_id=frozen.project_workspace_id
       AND link.enabled=1
       AND (link.expires_at_ms IS NULL OR link.expires_at_ms>?)
       AND EXISTS(SELECT 1 FROM project_workspaces source
        WHERE source.id=link.source_workspace_id
         AND source.project_id=a.project_id AND source.status='active'))
    )
  ) THEN 0 ELSE 1 END),0)
  FROM json_each((SELECT entries_json
   FROM research_session_evidence_metadata WHERE session_id=?)) item`,
 sessionID,s.clock.UnixMilli(),sessionID).Scan(&count,&denied)
 if err!=nil{return err}
 if count<1||count>8||denied!=0{return errResearchConsentNoLongerValid}
 return nil
}

func researchAutomaticRetryAllowed(total,eligible int)bool{
 // Every blocked/failed unresolved seat MUST have an explicit, elapsed
 // provider timer and be within the bounded automatic attempt budget.
 return total>0&&total==eligible
}
