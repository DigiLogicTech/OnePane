package api

import (
 "context"
 "encoding/json"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestSignalTriageAPIIsHumanReadOnlyAndTenantScoped(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/signal-triage-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('robot','agent','Agent','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','robot','active',1,1)`,
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Read-only evidence triage",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 view,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Evidence",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:view.ID,ActorPrincipalID:"operator",
  Objective:"Inspect already-admitted metadata only",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["peer"]}`),
 })
 if err!=nil{t.Fatal(err)}
 handler:=NewServer(svc,nil,goalAPIAuth{}).Handler()
 path:="/v1/projects/"+p.ID+"/continuous-goals/"+goal.ID+"/signal-triage"
 check:=func(method,who string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,strings.NewReader("{}"))
  if who!=""{req.Header.Set("Authorization","Bearer "+who)}
  w:=httptest.NewRecorder()
  handler.ServeHTTP(w,req)
  return w
 }
 for _,tc:=range []struct{principal string;want int}{
  {"",401},{"outsider",403},{"robot",403},{"operator",200},
 }{
  r:=check("GET",tc.principal)
  if r.Code!=tc.want{t.Fatalf("%s GET=%d wanted %d: %s",tc.principal,r.Code,tc.want,r.Body)}
  if tc.principal=="operator"{
   var triage projectworkspace.GoalSignalTriage
   if err=json.Unmarshal(r.Body.Bytes(),&triage);err!=nil{t.Fatal(err)}
   if triage.GoalID!=goal.ID||triage.State!="no_recent_measurements"||
    triage.MonitoringActive||triage.DeviceIdentityVerified||
    triage.RemediationAuthorized||len(triage.Sources)!=1{
    t.Fatalf("read-only triage falsely claimed active security: %+v",triage)
   }
  }
 }
 // Dedicated GET endpoint cannot be hijacked by the Goal lifecycle POST
 // catch-all route. It must never activate an unattended monitor.
 if got:=check("POST","operator");got.Code<400{
  t.Fatalf("POST created effects through read-only triage path: %d",got.Code)
 }
 current,err:=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Status!="configured"||current.Health!="unobserved"||
  current.LastVerifiedAt!=0{t.Fatalf("API mutated Goal health: %+v",current)}
}
