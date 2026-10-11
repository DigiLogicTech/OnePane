package projectorchestrator

import (
 "context"
 "database/sql"
 "strings"
)

// selectModelProposedWorkspace allows the Project Orchestrator to suggest an
// active sibling Workspace only when the operator did not explicitly select
// one. Ownership is checked in SQLite rather than trusting a model response.
func selectModelProposedWorkspace(ctx context.Context, db *sql.DB,
 projectID, operatorSelected, current string, proposed any) string {
 if strings.TrimSpace(operatorSelected)!="" || db==nil {return current}
 candidate,ok:=proposed.(string)
 if !ok{return current}
 candidate=strings.TrimSpace(candidate)
 if candidate==""{return current}
 var count int
 if err:=db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces
  WHERE id=? AND project_id=? AND status='active'`,candidate,projectID).Scan(&count);err!=nil||count!=1 {
  return current
 }
 return candidate
}
