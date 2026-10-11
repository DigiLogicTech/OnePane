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
 // Unlike editable instructions, the Agent capability policy has an
 // independent revision head. Both must stay stable across queued inference.
 PolicyRevision int64
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
 var promptRevision,policyRevision int64
 var missingPolicyRevision int
 // Resolve active Agent ownership, prompt revision and policy-head
 // provenance in one SQLite statement. A dangling Agent policy head is
 // never treated as a legacy inherited-permissions profile.
 err=s.scopes.DB.QueryRowContext(ctx,`SELECT a.revision,COALESCE(h.revision,0),
  CASE WHEN h.revision IS NOT NULL AND r.revision IS NULL THEN 1 ELSE 0 END
  FROM agent_profiles a
  LEFT JOIN agent_policy_heads h ON h.profile_id=a.id
  LEFT JOIN agent_policy_revisions r
    ON r.profile_id=h.profile_id AND r.revision=h.revision AND r.workspace_id=a.workspace_id
  WHERE a.id=? AND a.workspace_id=? AND a.status='active'`,id,cmd.WorkspaceID).
  Scan(&promptRevision,&policyRevision,&missingPolicyRevision)
 if err!=nil||promptRevision<1||missingPolicyRevision!=0 {
  return taskAgentProfileRevision{},fmt.Errorf("%w: Task Agent prompt or policy missing, revoked or foreign",ErrInformationFlow)
 }
 return taskAgentProfileRevision{ProfileID:id,Revision:promptRevision,PolicyRevision:policyRevision},nil
}

// A custom Agent or its independent deny-only capability policy may change
// while the inference runner is queued. Even if a feature check happens to
// allow the current action, the queued request cannot mix Agent revisions.
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
