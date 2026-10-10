package projectworkspace

import "context"

// ResearchFrozenSession identifies a previously approved Research Council
// session in the exact Project Workspace. It carries no Library asset
// identifiers, hashes, plaintext, access grants or transferable lease.
type ResearchFrozenSession struct {
 ID string `json:"id"`
 TaskID string `json:"task_id"`
 TeamID string `json:"team_id"`
 TeamName string `json:"team_name"`
 TaskObjective string `json:"task_objective"`
 Status string `json:"status"`
 RoundNumber int64 `json:"round_number"`
 ApprovedAt int64 `json:"approved_at"`
}

// FrozenResearchEvidenceSessions is metadata discovery for active human
// tenant members only. It deliberately does not re-grant access to the frozen
// Library versions: a separate authenticated read/preflight must independently
// revalidate current read grants, content integrity and the frozen manifest.
func(s *Service) FrozenResearchEvidenceSessions(
 ctx context.Context,projectID,workspaceID,actor string,
)([]ResearchFrozenSession,error){
 if s==nil||s.db==nil||projectID==""||workspaceID==""||actor==""{
  return nil,ErrInvalidCommand
 }
 rows,err:=s.db.QueryContext(ctx,`SELECT ss.id,t.id,tm.id,tm.name,
  t.objective,ss.status,ss.round_number,e.approved_at
 FROM research_session_evidence_metadata e
 JOIN team_sessions ss ON ss.id=e.session_id AND e.workspace_id=ss.workspace_id
 JOIN team_session_manifests m ON m.session_id=ss.id
  AND m.research_mode=1 AND m.execution_mode='council'
  AND m.snapshot_sha256=e.research_manifest_sha256
 JOIN teams tm ON tm.id=ss.team_id AND tm.workspace_id=ss.workspace_id AND tm.status='active'
 JOIN tasks t ON t.id=ss.task_id AND t.workspace_id=ss.workspace_id
  AND t.project_id=? AND t.project_workspace_id=?
  AND e.project_id=t.project_id AND e.project_workspace_id=t.project_workspace_id
 JOIN projects p ON p.id=t.project_id AND p.workspace_id=ss.workspace_id
  AND p.status='active'
 JOIN project_workspaces w ON w.id=t.project_workspace_id
  AND w.project_id=p.id AND w.status='active'
 JOIN workspaces tenant ON tenant.id=ss.workspace_id AND tenant.status='active'
 JOIN principals principal ON principal.id=? AND principal.status='active'
  AND principal.principal_type='human'
 JOIN workspace_memberships member ON member.workspace_id=ss.workspace_id
  AND member.principal_id=principal.id AND member.status='active'
 ORDER BY e.approved_at DESC,ss.id LIMIT 40`,projectID,workspaceID,actor)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]ResearchFrozenSession{}
 for rows.Next(){
  var item ResearchFrozenSession
  if err=rows.Scan(&item.ID,&item.TaskID,&item.TeamID,&item.TeamName,
   &item.TaskObjective,&item.Status,&item.RoundNumber,&item.ApprovedAt);err!=nil{
   return nil,err
  }
  out=append(out,item)
 }
 return out,rows.Err()
}
