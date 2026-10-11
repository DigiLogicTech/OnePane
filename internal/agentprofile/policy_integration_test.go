//go:build integration

package agentprofile

import (
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/featurepolicy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestVersionedAgentRestrictionsAreLiveRevocableAndDoNotExpandProjectGrants(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/agent-policy-integration.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('other','Other','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES('human','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES('robot','agent','Worker','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('tenant','human','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Scoped AI project",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"feature_policy":{"FileWrite":"enabled"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Work",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 agent:=NewService(db.SQL(),db,clock.Real{})
 p,err:=agent.Create(ctx,CreateCommand{
  WorkspaceID:"tenant",Name:"Local Builder",InstructionsMD:"Build with approved tools.",
  CreatedBy:"human",
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 completion,_:=json.Marshal(map[string]any{"onepane_routing":map[string]any{
  "agent_profile":p.ID,
 }})
 row,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&project.ID,ProjectWorkspaceID:&ws.ID,
  Objective:"Build local artifact",Completion:completion,
 })
 if err!=nil{t.Fatal(err)}
 scopes:=featurepolicy.StoredScopes{DB:db.SQL()}
 allow:=func()bool{
  t.Helper()
  yes,e:=scopes.AuthorizeTask(ctx,"tenant",row.ID,"files.write")
  if e!=nil{t.Fatal(e)}
  return yes
 }
 if !allow(){t.Fatal("legacy custom Agent without policy incorrectly denied Task")}
 put:=func(actor,policy string,expected int64)(PolicyRevision,error){
  return agent.UpdatePolicy(ctx,UpdateAgentPolicyCommand{
   ProfileID:p.ID,WorkspaceID:"tenant",ActorPrincipalID:actor,
   ExpectedRevision:expected,PolicyJSON:json.RawMessage(policy),
  })
 }
 restrictFile:=`{"feature_policy":{"FileWrite":"disabled"}}`
 first,err:=put("robot",restrictFile,0)
 if err!=nil{t.Fatal(err)}
 if first.Revision!=1||allow(){t.Fatalf("Agent restriction not enforced live: %+v",first)}
 restrictedMore:=`{"feature_policy":{"FileWrite":"disabled","NetworkAccess":"disabled"}}`
 second,err:=put("robot",restrictedMore,1)
 if err!=nil||second.Revision!=2{t.Fatalf("Agent failed to tighten its restrictions: %+v %v",second,err)}
 for _,tc:=range []struct{actor,policy string;revision int64}{
  {"robot",`{"feature_policy":{"FileWrite":"inherited","NetworkAccess":"disabled"}}`,2},
  {"human",`{"feature_policy":{"ProcessExecute":"enabled"}}`,2},
  {"human",`{"feature_policy":{"FileWrite":"enabled"}}`,2},
  {"human",`{"feature_policy":{"FileRead":"unknown"}}`,2},
 }{
  if _,err:=put(tc.actor,tc.policy,tc.revision);err==nil{
   t.Fatalf("unsafe Agent policy admitted: %s",tc.policy)
  }
 }
 if _,err:=put("human",restrictFile,1);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale Agent policy CAS admitted: %v",err)
 }
 newPolicy:=`{"feature_policy":{"FileWrite":"inherited","NetworkAccess":"disabled"}}`
 third,err:=put("human",newPolicy,2)
 if err!=nil||third.Revision!=3{t.Fatalf("human cannot review denial removal: %+v %v",third,err)}
 if !allow(){t.Fatal("human-reviewed FileWrite denial removal did not reenable Project permitted write")}
 if _,err:=agent.Policy(ctx,p.ID,"other");err==nil{t.Fatal("cross-tenant Agent policy read admitted")}
 if _,err:=agent.UpdatePolicy(ctx,UpdateAgentPolicyCommand{
  ProfileID:p.ID,WorkspaceID:"other",ExpectedRevision:3,
  ActorPrincipalID:"robot",PolicyJSON:json.RawMessage(restrictFile),
 });err==nil{t.Fatal("cross-tenant Agent policy mutation admitted")}
 if _,err:=db.SQL().ExecContext(ctx,"UPDATE agent_policy_revisions SET revision=999 WHERE profile_id=?",p.ID);err==nil{
  t.Fatal("historical Agent revision is mutable")
 }
 if _,err:=db.SQL().ExecContext(ctx,"DELETE FROM agent_policy_revisions WHERE profile_id=?",p.ID);err==nil{
  t.Fatal("historical Agent revisions can be deleted")
 }
 var n int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM agent_policy_revisions WHERE profile_id=?",p.ID).Scan(&n);err!=nil||n!=3{
  t.Fatalf("revision history not retained: %d %v",n,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,"UPDATE agent_profiles SET status='archived' WHERE id=?",p.ID);err!=nil{t.Fatal(err)}
 if _,err:=scopes.AuthorizeTask(ctx,"tenant",row.ID,"files.write");err==nil{
  t.Fatal("archived Agent continued to issue Task rights")
 }
}
