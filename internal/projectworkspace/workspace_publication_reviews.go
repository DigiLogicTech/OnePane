package projectworkspace

import "context"

// WorkspacePublicationReview is a stale, unresolved durable publication
// reservation. It reports only Task-local metadata, not raw bytes, stored
// blob IDs or a claimed outcome of an uncertain external side effect.
type WorkspacePublicationReview struct {
 TaskID string `json:"task_id"`
 RelativePath string `json:"relative_path"`
 Stage string `json:"stage"`
 LastUpdatedAt int64 `json:"last_updated_at"`
}

const publicationReviewAgeMS int64 = 5 * 60 * 1000

// WorkspacePublicationReviews exposes pending reservations only after they
// have remained unchanged for five minutes. "Stale" is NOT proof a process
// died: operators must inspect evidence rather than blindly retry a write.
// Never create content, change publication state or reconcile in a GET.
func (s *Service) WorkspacePublicationReviews(ctx context.Context,projectID,workspaceID string)([]WorkspacePublicationReview,error){
 if projectID==""||workspaceID==""{return nil,ErrInvalidCommand}
 var active int
 err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces w
 JOIN projects p ON p.id=w.project_id
 WHERE w.id=? AND w.project_id=? AND w.status='active' AND p.status='active'`,
 workspaceID,projectID).Scan(&active)
 if err!=nil{return nil,err}
 if active!=1{return nil,ErrCrossWorkspace}
 cutoff:=s.clock.UnixMilli()-publicationReviewAgeMS
 rows,err:=s.db.QueryContext(ctx,`SELECT fp.task_id,fp.relative_path,
  CASE WHEN fp.artifact_id IS NULL THEN 'reserved' ELSE 'artifact_recorded' END,
  fp.updated_at
 FROM workspace_file_publications fp
 JOIN tasks t ON t.id=fp.task_id AND t.project_id=fp.project_id
  AND t.project_workspace_id=fp.project_workspace_id
 WHERE fp.project_id=? AND fp.project_workspace_id=?
  AND fp.status='in_progress' AND fp.updated_at<=?
 ORDER BY fp.updated_at ASC,fp.task_id LIMIT 50`,
 projectID,workspaceID,cutoff)
 if err!=nil{return nil,err}
 defer rows.Close()
 pending:=[]WorkspacePublicationReview{}
 for rows.Next(){
  var item WorkspacePublicationReview
  if err=rows.Scan(&item.TaskID,&item.RelativePath,&item.Stage,&item.LastUpdatedAt);err!=nil{return nil,err}
  pending=append(pending,item)
 }
 return pending,rows.Err()
}
