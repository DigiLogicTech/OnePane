package featurepolicy

import (
 "context"
 "testing"
)

func TestTasklessProjectWorkspacePolicyFollowsCanonicalOwnership(t *testing.T){
 ctx:=context.Background()
 db:=setupStoredPolicyDB(t)
 s:=StoredScopes{DB:db}
 world,err:=s.ForProjectWorkspace(ctx,"tenant","project","world")
 if err!=nil{t.Fatal(err)}
 if !world.Configured||world.Effective.CloudRouting!=Disabled||
  world.Effective.Files!=Disabled{
  t.Fatalf("Workspace escaped restrictive Project features: %+v",world)
 }
 story,err:=s.ForProjectWorkspace(ctx,"tenant","project","story")
 if err!=nil{t.Fatal(err)}
 if story.Effective.CloudRouting!=Enabled||story.Effective.Files!=Enabled{
  t.Fatalf("sibling Workspace incorrectly inherited world policy: %+v",story)
 }
 top,err:=s.ForProjectWorkspace(ctx,"tenant","project","")
 if err!=nil{t.Fatal(err)}
 if top.Effective.Files!=Enabled||top.Effective.CloudRouting!=Enabled{
  t.Fatalf("Project-only reasoning scope invalid: %+v",top)
 }
 for _,bad:=range []struct{tenant,project,workspace string}{
  {"other","project","world"},{"tenant","missing","world"},
  {"tenant","project","other"},{"tenant","project","unknown"},
 }{
  if _,err:=s.ForProjectWorkspace(ctx,bad.tenant,bad.project,bad.workspace);err==nil{
   t.Fatalf("unauthorised Project reasoning scope resolved: %+v",bad)
  }
 }
 if _,err:=db.Exec("UPDATE project_workspaces SET status='archived' WHERE id='world'");err!=nil{t.Fatal(err)}
 if _,err:=s.ForProjectWorkspace(ctx,"tenant","project","world");err==nil{
  t.Fatal("archived Workspace admitted to model context")
 }
 if _,err:=db.Exec("UPDATE projects SET status='archived' WHERE id='project'");err!=nil{t.Fatal(err)}
 if _,err:=s.ForProjectWorkspace(ctx,"tenant","project","story");err==nil{
  t.Fatal("archived Project admitted to reasoning")
 }
}
func TestTasklessProjectReasoningRespectsParentAndWorkspaceDenials(t *testing.T){
 ctx:=context.Background()
 db:=setupStoredPolicyDB(t)
 s:=StoredScopes{DB:db}
 for _,tc:=range []struct{policy,workspace string}{
  {`{"feature_policy":{"Assistant":"disabled"}}`,"story"},
  {`{"feature_policy":{"ProjectAI":"disabled"}}`,"story"},
  {`{"feature_policy":{"ModelRouting":"disabled"}}`,"story"},
  {`{"feature_policy":{"CapabilityRouting":"disabled"}}`,"story"},
 }{
  if _,err:=db.Exec("UPDATE projects SET project_policy_json=? WHERE id='project'",tc.policy);err!=nil{t.Fatal(err)}
  dec,err:=s.ForProjectWorkspace(ctx,"tenant","project",tc.workspace)
  if err!=nil{t.Fatal(err)}
  if err:=dec.RequireProjectReasoning();err==nil{
   t.Fatalf("denied Project feature bypassed: %s",tc.policy)
  }
 }
 if _,err:=db.Exec(`UPDATE projects SET project_policy_json='{}' WHERE id='project'`);err!=nil{t.Fatal(err)}
 if _,err:=db.Exec(`UPDATE project_workspaces SET ai_settings_json='{"feature_policy":{"ProjectAI":"disabled"}}' WHERE id='world'`);err!=nil{t.Fatal(err)}
 world,err:=s.ForProjectWorkspace(ctx,"tenant","project","world")
 if err!=nil||world.RequireProjectReasoning()==nil{t.Fatalf("Workspace reasoning denial failed: %+v %v",world,err)}
 top,err:=s.ForProjectWorkspace(ctx,"tenant","project","story")
 if err!=nil||top.RequireProjectReasoning()!=nil{t.Fatalf("unconfigured sibling policy unexpectedly denied: %+v %v",top,err)}
 if _,err:=db.Exec(`UPDATE project_workspaces SET ai_settings_json='{"feature_policy":null}' WHERE id='world'`);err!=nil{t.Fatal(err)}
 if _,err:=s.ForProjectWorkspace(ctx,"tenant","project","world");err==nil{
  t.Fatal("malformed Workspace policy admitted")
 }
}
