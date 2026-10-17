//go:build integration

package projectworkspace

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// Previously approved signers upgrade to generation zero, preserving history
// but never inheriting current authority. A human may revoke them and enroll
// a NEW key under the current Goal generation. There is no socket or Node.
func TestLegacySignedSourceMigratesFailClosedUntilFreshHumanEnrollment(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/signed-source-upgrade.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Human','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('source','service','Sensor','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','source','active',1,1)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,stmt);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Safe signed-source upgrade",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Evidence",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 raw:=json.RawMessage(`{"version":1,"source_ids":["legacy-sensor"]}`)
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"Migrated key is never new consent",MonitoringPlanJSON:raw,
 })
 if err!=nil{t.Fatal(err)}
 _,hash,err:=goalReviewPlan(string(raw))
 if err!=nil{t.Fatal(err)}
 oldKey,_,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 expiry:=now+60*60*1000
 // Simulate a pre-migration row by omitting the newly added plan_generation
 // column. SQLite's migration default of zero must deny current authority.
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_observation_signers
  (id,project_id,goal_id,project_workspace_id,source_id,source_principal_id,
   plan_hash,public_key,status,revision,last_sequence,approved_by,approved_at,expires_at)
  VALUES(?,?,?,?,?,?,?,?,'approved',1,0,?,?,?)`,
  "old-signer",project.ID,goal.ID,ws.ID,"legacy-sensor","source",
  hash,oldKey,"owner",now,expiry)
 if err!=nil{t.Fatal(err)}
 list,err:=svc.ListGoalObservationSigners(ctx,project.ID,goal.ID)
 if err!=nil||len(list)!=1||list[0].Status!="approved"||
  list[0].PlanGeneration!=0||list[0].EffectiveScopeState!="goal_scope_inactive"{
  t.Fatalf("upgraded historical key revived as live authority: %+v %v",list,err)
 }
 if _,err=svc.MintGoalSourceCredential(ctx,MintGoalSourceCredentialCommand{
  ProjectID:project.ID,GoalID:goal.ID,SignerID:"old-signer",
  ActorPrincipalID:"owner",ExpiresAt:expiry,
 });!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("historical source minted a new bearer token: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_goal_observation_signers
  SET plan_generation=1 WHERE id='old-signer'`);err==nil{
  t.Fatal("historic human source approval was rewritten to a new generation")
 }
 revoked,err:=svc.RevokeGoalObservationSigner(ctx,RevokeGoalObservationSignerCommand{
  ProjectID:project.ID,GoalID:goal.ID,SignerID:"old-signer",
  ActorPrincipalID:"owner",ExpectedGoalRevision:goal.Revision,
  ExpectedSignerRevision:1,
 })
 if err!=nil||revoked.Status!="revoked"{t.Fatalf("historical key revocation: %+v %v",revoked,err)}
 newKey,_,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 replacement,err:=svc.ApproveGoalObservationSigner(ctx,ApproveGoalObservationSignerCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"legacy-sensor",
  SourcePrincipalID:"source",ActorPrincipalID:"owner",PublicKey:newKey,
  ExpectedGoalRevision:goal.Revision,ExpiresAt:expiry,
 })
 if err!=nil{t.Fatal(err)}
 if replacement.PlanGeneration<1||replacement.PlanGeneration==revoked.PlanGeneration||
  replacement.ID==revoked.ID{
  t.Fatalf("fresh human approval did not create new authority: %+v",replacement)
 }
 list,err=svc.ListGoalObservationSigners(ctx,project.ID,goal.ID)
 if err!=nil||len(list)!=2{t.Fatalf("signer historical audit lost: %+v %v",list,err)}
 var archived,active int
 for _,signer:=range list{
  if signer.ID==revoked.ID&&signer.EffectiveScopeState=="revoked"{archived++}
  if signer.ID==replacement.ID&&signer.EffectiveScopeState=="active"{active++}
 }
 if archived!=1||active!=1{
  t.Fatalf("historical approval and new human authority were conflated: %+v",list)
 }
 minted,err:=svc.MintGoalSourceCredential(ctx,MintGoalSourceCredentialCommand{
  ProjectID:project.ID,GoalID:goal.ID,SignerID:replacement.ID,
  ActorPrincipalID:"owner",ExpiresAt:expiry,
 })
 if err!=nil||minted.Credential.Status!="active"||minted.BearerToken==""{
  t.Fatalf("freshly approved source could not mint scoped bearer: %+v %v",minted.Credential,err)
 }
}
