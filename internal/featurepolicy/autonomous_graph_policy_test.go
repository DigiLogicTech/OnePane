package featurepolicy

import (
 "context"
 "testing"
)

func TestAutonomousGraphTaskRequiresCurrentStandingProjectGrant(t *testing.T){
 ctx:=context.Background()
 db:=setupStoredPolicyDB(t)
 if _,err:=db.ExecContext(ctx,`INSERT INTO tasks VALUES(
  'auto-task','tenant','project','story',
  '{"source":"project-orchestrator-autonomous-graph",
    "onepane_routing":{"compute_preference":"auto"}}')`);err!=nil{t.Fatal(err)}
 scopes:=StoredScopes{DB:db}
 if ok,err:=scopes.AuthorizeTask(ctx,"tenant","auto-task","inference.local");err!=nil||ok{
  t.Fatalf("autonomous Task ran without standing consent: allowed=%v err=%v",ok,err)
 }
 if _,err:=db.ExecContext(ctx,`UPDATE projects SET project_policy_json=
  '{"feature_policy":{"AutonomousRoutines":"enabled"}}' WHERE id='project'`);err!=nil{t.Fatal(err)}
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
