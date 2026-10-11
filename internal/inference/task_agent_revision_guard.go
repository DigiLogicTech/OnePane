package inference

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "strings"
)

type taskAgentProfileRevision struct{
 ProfileID string
 Revision int64
}

// pinnedAgentProfileAtInference re-reads the Task's canonical completion
// record, never a provider or model-supplied profile claim. A custom profile
// must remain active in the Task's exact tenant Workspace.
func(s *Service)pinnedAgentProfileAtInference(ctx context.Context,cmd ExecuteCommand)(taskAgentProfileRevision,error){
 if cmd.TaskID==nil{return taskAgentProfileRevision{},nil}
 if s==nil||s.scopes.DB==nil{return taskAgentProfileRevision{},ErrInformationFlow}
 var completion string
 err:=s.scopes.DB.QueryRowContext(ctx,`SELECT COALESCE(completion_json,'{}') FROM tasks
  WHERE id=? AND workspace_id=?`,*cmd.TaskID,cmd.WorkspaceID).Scan(&completion)
 if err!=nil{return taskAgentProfileRevision{},fmt.Errorf("%w: canonical Task inference scope unavailable",ErrInformationFlow)}
 var doc struct{
  Routing struct{
   AgentProfile string `json:"agent_profile"`
  } `json:"onepane_routing"`
 }
 if err:=json.Unmarshal([]byte(completion),&doc);err!=nil{
  return taskAgentProfileRevision{},fmt.Errorf("%w: invalid Task Agent binding",ErrInformationFlow)
 }
 id:=strings.TrimSpace(doc.Routing.AgentProfile)
 if id==""||id=="agent.md"||strings.EqualFold(id,"onepane-default"){
  return taskAgentProfileRevision{},nil
 }
 var rev int64
 err=s.scopes.DB.QueryRowContext(ctx,`SELECT revision FROM agent_profiles
  WHERE id=? AND workspace_id=? AND status='active'`,id,cmd.WorkspaceID).Scan(&rev)
 if err!=nil||rev<1{
  return taskAgentProfileRevision{},fmt.Errorf("%w: Task Agent profile missing, archived or foreign",ErrInformationFlow)
 }
 return taskAgentProfileRevision{ProfileID:id,Revision:rev},nil
}

// A custom Agent may change while the inference runner is queued. Even if
// the model routing choice and Task feature grant remain valid, a prompt
// compiled under a retired Agent revision must not cross a transport.
func(s *Service)revalidateTaskAgentBeforeTransport(ctx context.Context,cmd ExecuteCommand,
 expected taskAgentProfileRevision)error{
 current,err:=s.pinnedAgentProfileAtInference(ctx,cmd)
 if err!=nil{return err}
 if current!=expected{
  return fmt.Errorf("%w: Task Agent policy or prompt revision changed before inference dispatch",ErrInformationFlow)
 }
 return nil
}

var _ = sql.ErrNoRows
