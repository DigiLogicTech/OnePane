package agentworker

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
 "strings"
)

// agentProfileBinding is an execution-time fence for a Task's explicitly
// pinned custom Agent profile. A profile's instructions never grant tools;
// the Worker must still independently admit every capability.
type agentProfileBinding struct {
 ID string
 Revision int64
}

// resolveTaskAgentProfileBinding deliberately preserves legacy default-agent
// behaviour, but an explicit custom pin must resolve to an active, canonical
// same-tenant profile. A missing, archived or cross-Workspace profile must
// not silently fall back to agent.md.
func resolveTaskAgentProfileBinding(ctx context.Context,db *sql.DB,workspaceID,profileID string)(agentProfileBinding,error){
 profileID=strings.TrimSpace(profileID)
 workspaceID=strings.TrimSpace(workspaceID)
 if profileID==""||strings.EqualFold(profileID,"onepane-default")||profileID=="agent.md"{
  return agentProfileBinding{},nil
 }
 if db==nil||workspaceID==""{return agentProfileBinding{},fmt.Errorf("Agent profile scope unavailable")}
 var id,status string
 var revision int64
 var owner sql.NullString
 err:=db.QueryRowContext(ctx,`SELECT id,workspace_id,status,revision FROM agent_profiles WHERE id=?`,profileID).
  Scan(&id,&owner,&status,&revision)
 if errors.Is(err,sql.ErrNoRows){return agentProfileBinding{},fmt.Errorf("pinned custom Agent profile is unavailable")}
 if err!=nil{return agentProfileBinding{},err}
 if status!="active"||!owner.Valid||owner.String!=workspaceID||revision<1{
  return agentProfileBinding{},fmt.Errorf("pinned Agent profile is inactive, revoked or outside Task workspace")
 }
 return agentProfileBinding{ID:id,Revision:revision},nil
}

// enforceTaskAgentProfileBinding fences a custom Agent after the potentially
// long and expensive scheduler route, before a Worker dispatch. Any update,
// archival, or change of profile ownership cancels this inference admission.
func enforceTaskAgentProfileBinding(ctx context.Context,db *sql.DB,workspaceID string,expected agentProfileBinding)error{
 if expected.ID==""{return nil}
 current,err:=resolveTaskAgentProfileBinding(ctx,db,workspaceID,expected.ID)
 if err!=nil{return err}
 if current.ID!=expected.ID||current.Revision!=expected.Revision{
  return fmt.Errorf("pinned Agent profile revision changed during Worker admission")
 }
 return nil
}
