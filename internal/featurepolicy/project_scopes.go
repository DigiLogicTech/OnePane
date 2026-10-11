package featurepolicy

import (
 "context"
 "database/sql"
 "fmt"
 "strings"
)

// ForProjectWorkspace resolves persisted feature policy for taskless Project
// reasoning without inventing a Task or reading a sibling Workspace policy.
// It is a restriction only: authorisation to dispatch inference and use tools
// remains independently enforced by their respective runtime gateways.
func(s StoredScopes)ForProjectWorkspace(ctx context.Context,tenantID,projectID,workspaceID string)(ScopedDecision,error){
 if s.DB==nil{return ScopedDecision{},fmt.Errorf("scope resolver unavailable")}
 return forProjectWorkspaceWithReader(ctx,s.DB,tenantID,projectID,workspaceID)
}

func forProjectWorkspaceWithReader(ctx context.Context,reader scopedRowReader,tenantID,projectID,workspaceID string)(ScopedDecision,error){
 tenantID,projectID,workspaceID=strings.TrimSpace(tenantID),strings.TrimSpace(projectID),strings.TrimSpace(workspaceID)
 if reader==nil||tenantID==""||projectID==""{return ScopedDecision{},fmt.Errorf("canonical Project scope required")}
 var tenantStatus string
 if err:=reader.QueryRowContext(ctx,"SELECT status FROM workspaces WHERE id=?",tenantID).Scan(&tenantStatus);err!=nil||tenantStatus!="active"{
  return ScopedDecision{},fmt.Errorf("execution tenant unavailable or inactive")
 }
 var policyJSON,projectStatus string
 if err:=reader.QueryRowContext(ctx,`SELECT project_policy_json,status FROM projects
   WHERE id=? AND workspace_id=?`,projectID,tenantID).Scan(&policyJSON,&projectStatus);err!=nil||projectStatus!="active"{
  return ScopedDecision{},fmt.Errorf("canonical Project unavailable or inactive")
 }
 project,configured,err:=parseFeatureDocument([]byte(policyJSON),AssistedDefaults())
 if err!=nil{return ScopedDecision{},err}
 child:=Policy{}
 childConfigured:=false
 if workspaceID!=""{
  var settings,status string
  err=reader.QueryRowContext(ctx,`SELECT ai_settings_json,status FROM project_workspaces
    WHERE id=? AND project_id=?`,workspaceID,projectID).Scan(&settings,&status)
  if err!=nil||status!="active"{
   return ScopedDecision{},fmt.Errorf("canonical Project Workspace unavailable or inactive")
  }
  child,childConfigured,err=parseFeatureDocument([]byte(settings),Policy{})
  if err!=nil{return ScopedDecision{},err}
 }
 return ScopedDecision{Effective:ResolvePolicyChain(project,child),
  Configured:configured||childConfigured},nil
}

// ForProjectWorkspaceTx can be used by future atomic dispatch admission when
// Task/Agent policy is carried into the same SQLite transaction.
func(s StoredScopes)ForProjectWorkspaceTx(ctx context.Context,tx scopedRowReader,tenantID,projectID,workspaceID string)(ScopedDecision,error){
 if tx==nil{return ScopedDecision{},fmt.Errorf("transactional Project scope resolver unavailable")}
 return forProjectWorkspaceWithReader(ctx,tx,tenantID,projectID,workspaceID)
}

// RequireProjectReasoning gates taskless Project inference on explicit
// configured feature policy without treating the policy as a tool grant.
func(dec ScopedDecision)RequireProjectReasoning()error{
 if !dec.Configured{return nil}
 if dec.Effective.Assistant!=Enabled||dec.Effective.ProjectAI!=Enabled||
   dec.Effective.ModelRouting!=Enabled||dec.Effective.CapabilityRouting!=Enabled{
  return fmt.Errorf("Project reasoning denied by effective feature policy")
 }
 return nil
}

// Keep the SQL-reader contract explicit for DB and transactions.
var _ scopedRowReader = (*sql.DB)(nil)
