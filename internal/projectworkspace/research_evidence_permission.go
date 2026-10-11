package projectworkspace

import (
 "context"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// validateResearchEvidenceTx requires a live human operator, frozen Research
// mode session with no initiated rounds, and exact current version grants.
// Called during the same SQLite write transaction as the frozen record.
func(s *Service) validateResearchEvidenceTx(
 ctx context.Context,tx storage.Tx,
 sessionID,actor,projectID,workspaceID,manifestSHA string,
 entries []WorkspaceEvidenceEntry,
)(string,error){
 var tenant,liveSHA,actorType string
 err:=tx.QueryRowContext(ctx,`SELECT ss.workspace_id,m.snapshot_sha256,actor.principal_type
 FROM team_sessions ss
 JOIN team_session_manifests m ON m.session_id=ss.id
  AND m.research_mode=1 AND m.execution_mode='council'
 JOIN tasks t ON t.id=ss.task_id AND t.workspace_id=ss.workspace_id
  AND t.project_id=? AND t.project_workspace_id=?
 JOIN projects p ON p.id=t.project_id AND p.workspace_id=ss.workspace_id
  AND p.status='active'
 JOIN project_workspaces w ON w.id=t.project_workspace_id
  AND w.project_id=p.id AND w.status='active'
 JOIN workspaces tenant ON tenant.id=ss.workspace_id AND tenant.status='active'
 JOIN principals actor ON actor.id=? AND actor.status='active'
 JOIN workspace_memberships member ON member.workspace_id=ss.workspace_id
  AND member.principal_id=actor.id AND member.status='active'
 WHERE ss.id=? AND ss.status='deliberating' AND ss.round_number=0
 AND NOT EXISTS(SELECT 1 FROM team_turn_requests turn WHERE turn.session_id=ss.id)`,
 projectID,workspaceID,actor,sessionID).Scan(&tenant,&liveSHA,&actorType)
 if err!=nil||actorType!="human"||liveSHA!=manifestSHA{
  return "",ErrCrossWorkspace
 }
 now:=s.clock.UnixMilli()
 for _,entry:=range entries{
  var count int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*)
 FROM project_library_assets a
 JOIN project_library_asset_versions v ON v.asset_id=a.id
 JOIN projects p ON p.id=a.project_id AND p.status='active'
 JOIN project_workspaces w ON w.id=? AND w.project_id=p.id AND w.status='active'
 WHERE p.id=? AND a.id=? AND a.archived=0 AND v.version=?
  AND v.content_hash=? AND v.size_bytes=? AND v.mime_type=?
  AND (
   EXISTS(SELECT 1 FROM workspace_library_grants g
    WHERE g.asset_id=a.id AND g.project_workspace_id=w.id AND g.enabled=1
     AND json_extract(g.permissions_json,'$.read')=1
     AND ((g.version_policy='latest' AND v.version=a.current_version)
       OR (g.version_policy='pinned' AND g.pinned_version=v.version)))
   OR EXISTS(SELECT 1 FROM project_workspace_publications pub
    JOIN project_workspace_links link ON link.id=pub.link_id
    WHERE pub.asset_id=a.id AND pub.asset_version=v.version
     AND pub.content_hash=v.content_hash AND link.project_id=p.id
     AND link.target_workspace_id=w.id AND link.enabled=1
     AND (link.expires_at_ms IS NULL OR link.expires_at_ms>?)
     AND EXISTS(SELECT 1 FROM project_workspaces source
      WHERE source.id=link.source_workspace_id AND source.project_id=p.id
       AND source.status='active'))
  )`,workspaceID,projectID,entry.AssetID,entry.Version,entry.ContentHash,
  entry.SizeBytes,entry.MIMEType,now).Scan(&count)
  if err!=nil||count!=1{return "",ErrCrossWorkspace}
 }
 return tenant,nil
}
