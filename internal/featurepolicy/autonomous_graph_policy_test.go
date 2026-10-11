package featurepolicy

import (
 "context"
 "testing"
)

func TestAutonomousGraphTaskRequiresCurrentStandingProjectGrant(t *testing.T){
 ctx:=context.Background()
 db:=setupStoredPolicyDB(t)
 for _,q:=range []string{
  `CREATE TABLE project_orchestrator_task_graphs(id TEXT PRIMARY KEY,project_id TEXT,workspace_id TEXT)`,
  `CREATE TABLE project_orchestrator_task_graph_nodes(
   graph_id TEXT,node_key TEXT,task_id TEXT,project_workspace_id TEXT,approved_dependencies_recorded INTEGER)`,
  `INSERT INTO project_orchestrator_task_graphs VALUES('approved-graph','project','tenant')`,
  `INSERT INTO tasks VALUES('auto-task','tenant','project','story',
   '{"source":"project-orchestrator-autonomous-graph","graph_id":"approved-graph",
     "node_key":"build","standing_autonomy_policy":true,"operator_approved":false,
     "onepane_routing":{"compute_preference":"auto"}}')`,
  `INSERT INTO project_orchestrator_task_graph_nodes
    VALUES('approved-graph','build','auto-task','story',1)`,
 }{
  if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 scopes:=StoredScopes{DB:db}
 if ok,err:=scopes.AuthorizeTask(ctx,"tenant","auto-task","inference.local");err!=nil||ok{
  t.Fatalf("autonomous Task ran without standing consent: allowed=%v err=%v",ok,err)
 }
 // An attacker cannot elevate a Task to model-graph authority by
 // forging an origin label, even when autonomy is explicitly granted.
 if _,err:=db.ExecContext(ctx,`INSERT INTO tasks VALUES('forged-auto','tenant','project','story',
   '{"source":"project-orchestrator-autonomous-graph","graph_id":"approved-graph",
     "node_key":"build","standing_autonomy_policy":true,"operator_approved":false}')`);err!=nil{t.Fatal(err)}
 if _,err:=db.ExecContext(ctx,`UPDATE projects SET project_policy_json=
  '{"feature_policy":{"AutonomousRoutines":"enabled"}}' WHERE id='project'`);err!=nil{t.Fatal(err)}
 if ok,err:=scopes.AuthorizeTask(ctx,"tenant","forged-auto","inference.local");err==nil||ok{
  t.Fatalf("forged model graph origin admitted without SQLite DAG membership: %t %v",ok,err)
 }
 for _,action:=range []string{"inference.local","files.read"}{
  if ok,err:=scopes.AuthorizeTask(ctx,"tenant","auto-task",action);err!=nil||!ok{
   t.Fatalf("consented autonomous Task could not run safe scoped action: %s %t %v",action,ok,err)
  }
 }
 if _,err:=db.ExecContext(ctx,`UPDATE project_workspaces SET ai_settings_json=
  '{"feature_policy":{"AutonomousRoutines":"disabled"}}' WHERE id='story'`);err!=nil{t.Fatal(err)}
 if ok,err:=scopes.AuthorizeTask(ctx,"tenant","auto-task","inference.local");err!=nil||ok{
  t.Fatalf("live Workspace autonomy revocation ignored: %t %v",ok,err)
 }
 if _,err:=db.ExecContext(ctx,`UPDATE project_workspaces SET ai_settings_json='{}' WHERE id='story'`);err!=nil{t.Fatal(err)}
 if _,err:=db.ExecContext(ctx,`UPDATE projects SET project_policy_json=
  '{"feature_policy":{"AutonomousRoutines":"enabled","ProjectAI":"disabled"}}' WHERE id='project'`);err!=nil{t.Fatal(err)}
 if ok,err:=scopes.AuthorizeTask(ctx,"tenant","auto-task","inference.local");err!=nil||ok{
  t.Fatalf("Task bypassed Project AI denial through autonomy: %t %v",ok,err)
 }
 if _,err:=db.ExecContext(ctx,`UPDATE projects SET project_policy_json=
  '{"feature_policy":{"AutonomousRoutines":"enabled"}}' WHERE id='project'`);err!=nil{t.Fatal(err)}
 if _,err:=db.ExecContext(ctx,`UPDATE tasks SET project_id=NULL WHERE id='auto-task'`);err!=nil{t.Fatal(err)}
 if ok,err:=scopes.AuthorizeTask(ctx,"tenant","auto-task","inference.local");err==nil||ok{
  t.Fatalf("orphan autonomous Task became a legacy unrestricted Task: %t %v",ok,err)
 }
}
