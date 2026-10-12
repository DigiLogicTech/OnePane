package api

import (
 "context"
 "encoding/json"
 "fmt"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestGoalEvidenceReviewPermitAPIRequiresHumanCASAndDoesNotActivateProbes(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/review-permit-http.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Agent','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Source review",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Review",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=projects.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,Objective:"Read existing evidence",
  ActorPrincipalID:"operator",MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns"]}`),
 })
 if err!=nil{t.Fatal(err)}
 h:=NewServer(projects,nil,goalAPIAuth{}).Handler()
 route:="/v1/projects/"+p.ID+"/continuous-goals/"+goal.ID+"/evidence-review-permits"
 call:=func(method,who,url,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,url,strings.NewReader(body))
  if who!=""{req.Header.Set("Authorization","Bearer "+who)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder();h.ServeHTTP(w,req)
  return w
 }
 input:=fmt.Sprintf(`{"source_id":"dns","expected_goal_revision":1,"expires_at":%d}`,
  time.Now().Add(1*time.Hour).UnixMilli())
 if w:=call("POST","",route,input);w.Code!=401{t.Fatalf("unauthenticated permit created: %d",w.Code)}
 if w:=call("POST","outsider",route,input);w.Code!=403{t.Fatalf("foreign grant: %d",w.Code)}
 if w:=call("POST","robot",route,input);w.Code!=403{t.Fatalf("agent self-grant: %d %s",w.Code,w.Body)}
 if w:=call("POST","operator",route,strings.Replace(input,"dns","router.admin",1));w.Code<400{
  t.Fatalf("unapproved probe name granted: %d",w.Code)
 }
 created:=call("POST","operator",route,input)
 if created.Code!=201{t.Fatalf("human review grant rejected: %d %s",created.Code,created.Body)}
 var permit projectworkspace.GoalEvidenceReviewPermit
 if err:=json.Unmarshal(created.Body.Bytes(),&permit);err!=nil{t.Fatal(err)}
 if permit.SourceID!="dns"||permit.ProjectWorkspaceID!=ws.ID||
   permit.EffectClass!="existing_receipt_metadata"||permit.Status!="approved"{
  t.Fatalf("unscoped review authority: %+v",permit)
 }
 if w:=call("POST","operator",route,input);w.Code!=409{
  t.Fatalf("stale CAS accepted: %d %s",w.Code,w.Body)
 }
 listing:=call("GET","operator",route,"")
 if listing.Code!=200||!strings.Contains(listing.Body.String(),permit.ID){
  t.Fatalf("permit metadata hidden from owner: %d %s",listing.Code,listing.Body)
 }
 if w:=call("GET","outsider",route,"");w.Code!=403{t.Fatalf("cross-tenant listing: %d",w.Code)}
 revokeRoute:=route+"/"+permit.ID+"/revoke"
 revoke:=`{"expected_goal_revision":2,"expected_permit_revision":1}`
 if w:=call("POST","robot",revokeRoute,revoke);w.Code!=403{t.Fatalf("Agent revoked permit: %d %s",w.Code,w.Body)}
 if w:=call("POST","operator",revokeRoute,revoke);w.Code!=200{t.Fatalf("revocation failed: %d %s",w.Code,w.Body)}
 if w:=call("POST","operator",revokeRoute,revoke);w.Code!=409{
  t.Fatalf("replayed permit revocation: %d %s",w.Code,w.Body)
 }
 current,err:=projects.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Status!="configured"||current.Health!="unobserved"||
  current.NextDueAt!=0||current.LastVerifiedAt!=0||current.LastCheckedAt!=0{
  t.Fatalf("permit API fabricated active monitoring: %+v",current)
 }
 var count int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&count);err!=nil||count!=0{
  t.Fatalf("permit API launched Tasks: %d %v",count,err)
 }
}
