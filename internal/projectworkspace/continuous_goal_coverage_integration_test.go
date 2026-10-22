//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "testing"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)
func TestGoalEvidenceCoverageReportsMissingAndRecentReceiptsWithoutClaimingHealth(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/goal-coverage.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Coverage",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Sensors",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
  Objective:"Review attached uptime and DNS evidence",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["uptime","dns"]}`),
 })
 if err!=nil{t.Fatal(err)}
 before,err:=svc.GoalEvidenceCoverage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if before.Assessment!="incomplete_evidence"||len(before.Sources)!=2||
   before.Sources[0].EvidenceState!="missing"||before.Sources[1].EvidenceState!="missing"||
   before.GoalHealth!="unobserved"||before.MonitoringActive{
   t.Fatalf("missing evidence was represented as healthy: %+v",before)
 }
 actor:="human"
 obs,err:=observation.NewService(db.SQL(),db,clock.Real{}).Record(ctx,observation.RecordCommand{
  WorkspaceID:"tenant",SubjectRef:"device://secret-host",
  ObservationType:"host.uptime",ProbeToolID:"uptime",ProbeToolVersion:"v1",
  SourcePrincipalID:&actor,ActorPrincipalID:&actor,Value:json.RawMessage(`{"secret":"CANARY_DO_NOT_EXPOSE"}`),
  Label:policy.DataLabel{WorkspaceID:"tenant",Confidentiality:policy.ConfidentialityInternal,
   Residency:policy.ResidencyAny,Trust:policy.TrustAuthoritativeData},
 })
 if err!=nil{t.Fatal(err)}
 if _,err=svc.AttachGoalObservation(ctx,AttachGoalObservationCommand{
  ProjectID:project.ID,GoalID:goal.ID,ObservationID:obs.ID,
  ExpectedRevision:goal.Revision,ActorPrincipalID:"human",
 });err!=nil{t.Fatal(err)}
 after,err:=svc.GoalEvidenceCoverage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if after.Assessment!="incomplete_evidence"||
   after.Sources[0].EvidenceState!="recent_attached"||
   after.Sources[1].EvidenceState!="missing"||
   after.GoalHealth!="unobserved"||after.MonitoringActive{
  t.Fatalf("coverage incorrectly treated receipts as health: %+v",after)
 }
 raw,_:=json.Marshal(after)
 if string(raw)==""||containsSensitiveCoverage(string(raw)){t.Fatalf("coverage response disclosed private observation: %s",raw)}
 future:=NewService(db.SQL(),db,coverageTime{ms:clock.Real{}.UnixMilli()+61*60*1000})
 stale,err:=future.GoalEvidenceCoverage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if stale.Sources[0].EvidenceState!="stale_attached"||
   stale.Sources[1].EvidenceState!="missing"||
   stale.GoalHealth!="unobserved"||stale.MonitoringActive{
  t.Fatalf("old attached observation was treated as recent or healthy: %+v",stale)
 }
 if _,err=db.SQL().ExecContext(ctx,"UPDATE project_workspaces SET status='archived' WHERE id=?",ws.ID);err!=nil{t.Fatal(err)}
 if _,err=svc.GoalEvidenceCoverage(ctx,project.ID,goal.ID);err==nil{
  t.Fatal("archived Workspace yielded current evidence coverage")
 }
}
func containsSensitiveCoverage(v string)bool{
 return strings.Contains(v,"secret-host")||strings.Contains(v,"CANARY_DO_NOT_EXPOSE")
}

type coverageTime struct{ms int64}
func(c coverageTime)Now()time.Time{return time.UnixMilli(c.ms)}
func(c coverageTime)UnixMilli()int64{return c.ms}
