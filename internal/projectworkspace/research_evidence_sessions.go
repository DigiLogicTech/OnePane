package projectworkspace

import (
 "context"
)

// ResearchEvidenceEligibleSession is an eligible pre-first-round Research
// Council session associated with an exact active Project Workspace.
// The service requires active human tenant membership. The API also checks\n// the existing Project and Team read capabilities.
type ResearchEvidenceEligibleSession struct {
 ID string `json:"id"`
 TaskID string `json:"task_id"`
 TeamID string `json:"team_id"`
 TeamName string `json:"team_name"`
 TaskObjective string `json:"task_objective"`
 CreatedAt int64 `json:"created_at"`
}
func(s *Service) EligibleResearchEvidenceSessions(
 ctx context.Context,projectID,workspaceID,actor string,
)([]ResearchEvidenceEligibleSession,error){
 if s==nil||s.db==nil||projectID==""||workspaceID==""||actor==""{
  return nil,ErrInvalidCommand
 }
 rows,err:=s.db.QueryContext(ctx,`SELECT ss.id,t.id,tm.id,tm.name,t.objective,ss.created_at
 FROM team_sessions ss
 JOIN team_session_manifests m ON m.session_id=ss.id
  AND m.research_mode=1 AND m.execution_mode='council'
 JOIN teams tm ON tm.id=ss.team_id AND tm.workspace_id=ss.workspace_id AND tm.status='active'
 JOIN tasks t ON t.id=ss.task_id AND t.workspace_id=ss.workspace_id
  AND t.project_id=? AND t.project_workspace_id=?
 JOIN projects p ON p.id=t.project_id AND p.workspace_id=t.workspace_id AND p.status='active'
 JOIN project_workspaces w ON w.id=t.project_workspace_id
  AND w.project_id=p.id AND w.status='active'
 JOIN workspaces tenant ON tenant.id=ss.workspace_id AND tenant.status='active'
 JOIN principals principal ON principal.id=? AND principal.status='active'
  AND principal.principal_type='human'
 JOIN workspace_memberships membership ON membership.workspace_id=ss.workspace_id
  AND membership.principal_id=principal.id AND membership.status='active'
 WHERE ss.status='deliberating' AND ss.round_number=0
  AND NOT EXISTS(SELECT 1 FROM team_turn_requests tr WHERE tr.session_id=ss.id)
  AND NOT EXISTS(SELECT 1 FROM research_session_evidence_metadata frozen
   WHERE frozen.session_id=ss.id)
 ORDER BY ss.created_at DESC,ss.id LIMIT 40`,projectID,workspaceID,actor)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]ResearchEvidenceEligibleSession{}
 for rows.Next(){
  var v ResearchEvidenceEligibleSession
  if err=rows.Scan(&v.ID,&v.TaskID,&v.TeamID,&v.TeamName,
   &v.TaskObjective,&v.CreatedAt);err!=nil{return nil,err}
  out=append(out,v)
 }
 return out,rows.Err()
}
