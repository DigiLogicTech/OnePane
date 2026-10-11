package agentworker

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/storage"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// At most six ancestor -> child delegation edges are allowed. This is a
// resource/safety cap, not a model preference or mutable completion setting.
const maxAutonomousDelegationDepth = 6

var (
 ErrDelegationDepthLimit = errors.New("autonomous delegation depth limit reached")
 ErrDelegationAncestry = errors.New("delegation ancestry or Workspace ownership invalid")
)

func matchesTaskScope(current sql.NullString,expected *string) bool {
 if expected==nil{return !current.Valid}
 return current.Valid && current.String==*expected
}

// verifyDelegationAncestry runs inside the SAME transaction which inserts the
// new child. No model-supplied parent IDs, Workspace hints, or completion JSON
// can bypass the bound or cross between Project Workspaces. Pre-existing
// inconsistent/cyclic genealogy fails closed rather than executing as legacy.
func verifyDelegationAncestry(ctx context.Context,tx storage.Tx,parent task.Task) error {
 if tx==nil||strings.TrimSpace(parent.ID)==""||strings.TrimSpace(parent.WorkspaceID)==""{
  return fmt.Errorf("%w: missing parent or transaction",ErrDelegationAncestry)
 }
 seen:=make(map[string]bool,maxAutonomousDelegationDepth)
 id:=parent.ID
 for depth:=0;id!="";depth++{
  if seen[id]{return fmt.Errorf("%w: cycle at %s",ErrDelegationAncestry,id)}
  if depth>=maxAutonomousDelegationDepth {
   return fmt.Errorf("%w: maximum %d generations",ErrDelegationDepthLimit,maxAutonomousDelegationDepth)
  }
  seen[id]=true
  var workspace string
  var project,projectWorkspace,ancestor sql.NullString
  if err:=tx.QueryRowContext(ctx,`SELECT workspace_id,project_id,project_workspace_id,parent_task_id
    FROM tasks WHERE id=?`,id).Scan(&workspace,&project,&projectWorkspace,&ancestor);err!=nil{
   return fmt.Errorf("%w: ancestor %s: %v",ErrDelegationAncestry,id,err)
  }
  if workspace!=parent.WorkspaceID||!matchesTaskScope(project,parent.ProjectID)||
    !matchesTaskScope(projectWorkspace,parent.ProjectWorkspaceID){
   return fmt.Errorf("%w: ancestor %s left parent Project/Workspace",ErrDelegationAncestry,id)
  }
  if depth==0&&!matchesTaskScope(ancestor,parent.ParentTaskID){
   return fmt.Errorf("%w: parent ancestry changed during delegation",ErrDelegationAncestry)
  }
  if ancestor.Valid{id=ancestor.String}else{id=""}
 }
 return nil
}
