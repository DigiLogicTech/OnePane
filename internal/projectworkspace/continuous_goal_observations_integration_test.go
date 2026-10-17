//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestLevel5GoalObservationAttestationIsScopedAndDoesNotStartMonitoring(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/goal-observations.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('foreign','Foreign','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Worker','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('outsider','human','Outsider','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('foreign','outsider','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,stmt);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Protect devices",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 workspace,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Monitoring",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 other,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Other",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:workspace.ID,ActorPrincipalID:"owner",
  Objective:"Monitor approved devices",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["uptime"]}`),
 })
 if err!=nil{t.Fatal(err)}
 obsSvc:=observation.NewService(db.SQL(),db,clock.Real{})
 record:=func(tenant,actor,probe string,observedAt int64)observation.Observation{
  t.Helper()
  o,err:=obsSvc.Record(ctx,observation.RecordCommand{
   WorkspaceID:tenant,SubjectRef:"device://private",ObservationType:"health.sample",
   ProbeToolID:probe,ProbeToolVersion:"v1",SourcePrincipalID:&actor,ActorPrincipalID:&actor,
   ObservedAt:observedAt,Value:json.RawMessage(`{"private_canary":"NEVER_COPY_SENSOR_VALUE","status":"ok"}`),
   Label:policy.DataLabel{WorkspaceID:tenant,Confidentiality:policy.ConfidentialityInternal,
    Residency:policy.ResidencyAny,Trust:policy.TrustAuthoritativeData},
  })
  if err!=nil{t.Fatal(err)}
  return o
 }
 fresh:=record("tenant","owner","uptime",0)
 unrelated:=record("foreign","outsider","uptime",0)
 unknownSource:=record("tenant","owner","unapproved",0)
 stale:=record("tenant","owner","uptime",time.Now().Add(-48*time.Hour).UnixMilli())
 attach:=func(id,projectID,actor string,revision int64)(GoalObservationReceipt,error){
  return svc.AttachGoalObservation(ctx,AttachGoalObservationCommand{
   ProjectID:projectID,GoalID:goal.ID,ObservationID:id,
   ExpectedRevision:revision,ActorPrincipalID:actor,
  })
 }
 // Defend the final attachment boundary against a corrupted legacy
 // source allowlist. A second JSON field must not silently replace the
 // operator-approved source ID, even when the attached observation is valid.
 corrupt:=`{"version":1,"source_ids":["uptime"],"source_ids":["unapproved"]}`
 if _,err:=db.SQL().ExecContext(ctx,
   "UPDATE project_continuous_goals SET monitoring_plan_json=? WHERE id=?",corrupt,goal.ID);err!=nil{t.Fatal(err)}
 if _,err:=attach(unknownSource.ID,project.ID,"owner",goal.Revision);err==nil{
  t.Fatal("tampered stored source list granted an unapproved observation")
 }
 if _,err:=svc.GoalEvidenceCoverage(ctx,project.ID,goal.ID);err==nil{
  t.Fatal("tampered monitoring plan was presented as valid coverage")
 }
 if _,err:=db.SQL().ExecContext(ctx,
   "UPDATE project_continuous_goals SET monitoring_plan_json=? WHERE id=?",
   string(goal.MonitoringPlanJSON),goal.ID);err!=nil{t.Fatal(err)}
 // The scope preflight runs before fetching or integrity-checking
 // foreign observations. Denials are indistinguishable from an unknown
 // observation ID and cannot reveal a private observation value.
 for _,candidate:=range []struct{
  label,id,actor,reason string
  want error
 }{
  {"foreign observation",unrelated.ID,"owner","scope unavailable",ErrInvalidCommand},
  {"unprivileged agent",fresh.ID,"robot","human attestation required",ErrPrincipalIneligible},
  {"unknown observation","absent-observation","owner","scope unavailable",ErrInvalidCommand},
 }{
  _,err:=attach(candidate.id,project.ID,candidate.actor,goal.Revision)
  if !errors.Is(err,candidate.want)||!strings.Contains(err.Error(),candidate.reason)||
   strings.Contains(err.Error(),"NEVER_COPY_SENSOR_VALUE")||
   strings.Contains(err.Error(),"device://private"){
   t.Fatalf("%s escaped the early scope gate: %v",candidate.label,err)
  }
 }
 for _,tc:=range []struct{name,id,projectID,actor string;revision int64}{
  {"foreign observation",unrelated.ID,project.ID,"owner",1},
  {"unapproved source",unknownSource.ID,project.ID,"owner",1},
  {"stale",stale.ID,project.ID,"owner",1},
  {"wrong Project",fresh.ID,other.ID,"owner",1},
  {"robot attestation",fresh.ID,project.ID,"robot",1},
  {"unknown observation","missing",project.ID,"owner",1},
  {"stale revision",fresh.ID,project.ID,"owner",2},
 }{
  t.Run(tc.name,func(t *testing.T){
   if _,err:=attach(tc.id,tc.projectID,tc.actor,tc.revision);err==nil{
    t.Fatal("unapproved observation attached")
   }
  })
 }
 var count int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM project_goal_observation_receipts").Scan(&count);err!=nil||count!=0{
  t.Fatalf("denied evidence mutated receipts: %d %v",count,err)
 }
 receipt,err:=attach(fresh.ID,project.ID,"owner",goal.Revision)
 if err!=nil{t.Fatal(err)}
 if receipt.ObservationID!=fresh.ID||receipt.SourceID!="uptime"||
  receipt.IntegrityHash!=fresh.IntegrityHash{t.Fatalf("incorrect receipt %+v",receipt)}
 raw,_:=json.Marshal(receipt)
 if strings.Contains(string(raw),"NEVER_COPY_SENSOR_VALUE")||strings.Contains(string(raw),"device://private"){
  t.Fatal("receipt exposed underlying observation data")
 }
 updated,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if updated.Revision!=goal.Revision+1||updated.Status!="configured"||
  updated.Health!="unobserved"||updated.NextDueAt!=0||updated.LastCheckedAt!=0||
  updated.LastVerifiedAt!=0{t.Fatalf("receipt falsely activated L5 monitoring: %+v",updated)}
 if _,err=attach(fresh.ID,project.ID,"owner",updated.Revision);!errors.Is(err,ErrInvalidCommand){
  t.Fatalf("duplicate evidence admitted: %v",err)
 }
 receipts,err:=svc.GoalObservations(ctx,project.ID,goal.ID)
 if err!=nil||len(receipts)!=1||receipts[0].ID!=receipt.ID{t.Fatalf("receipt lost: %+v %v",receipts,err)}
 if _,err=svc.GoalObservations(ctx,other.ID,goal.ID);err==nil{t.Fatal("cross-Project evidence disclosed")}
 if _,err=db.SQL().ExecContext(ctx,"UPDATE project_goal_observation_receipts SET source_id='different' WHERE id=?",receipt.ID);err==nil{
  t.Fatal("evidence receipt was mutable")
 }
 if _,err=db.SQL().ExecContext(ctx,"DELETE FROM project_goal_observation_receipts WHERE id=?",receipt.ID);err==nil{
  t.Fatal("evidence receipt could be deleted")
 }
 if err:=db.SQL().QueryRowContext(ctx,
   "SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='project.goal_observation_attached'",goal.ID,
 ).Scan(&count);err!=nil||count!=1{t.Fatalf("missing evidence event: %d %v",count,err)}
 var eventPayload string
 if err:=db.SQL().QueryRowContext(ctx,
   "SELECT payload_json FROM events WHERE aggregate_id=? AND event_type='project.goal_observation_attached'",goal.ID,
 ).Scan(&eventPayload);err!=nil{t.Fatal(err)}
 if strings.Contains(eventPayload,"NEVER_COPY_SENSOR_VALUE")||strings.Contains(eventPayload,"device://private"){
  t.Fatal("audit event leaked private observation")
 }
}
