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

func TestHumanOnlyNodeMeasurementGrantsDoNotActivateMonitoring(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/goal-mtls-operator-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Human','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('robot','agent','Robot','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','robot','active',1,1)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Narrow Node read-only connectivity",
  CreatedBy:"operator",ProjectPolicyJSON:json.RawMessage(
   `{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 workspace,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Node",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:workspace.ID,
  Objective:"Observe paired Node transport only",
  ActorPrincipalID:"operator",MonitoringPlanJSON:json.RawMessage(
   `{"version":1,"source_ids":["peer-link"]}`),
 })
 if err!=nil{t.Fatal(err)}
 now:=time.Now().UnixMilli()
 fp:="sha256:"+strings.Repeat("a",64)
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,
  protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
  VALUES('peer','Peer',0,'peer-id','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,
  now,now,now)
 if err!=nil{t.Fatal(err)}
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO node_pairings(
  id,peer_node_id,direction,status,pairing_token,pairing_token_hash,
  pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,
  peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,
  revision,created_at,updated_at) VALUES('peer-pair','peer','outbound',
  'paired','token','hashed','000000','hashed',1,1,?,'',?,?,1,?,?)`,
  fp,now+3600000,now,now,now)
 if err!=nil{t.Fatal(err)}
 handler:=NewServer(svc,nil,goalAPIAuth{}).Handler()
 base:="/v1/projects/"+project.ID+"/continuous-goals/"+goal.ID
 grants:=base+"/node-measurement-grants"
 call:=func(method,who,path,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,strings.NewReader(body))
  if who!=""{req.Header.Set("Authorization","Bearer "+who)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder();handler.ServeHTTP(w,req)
  return w
 }
 input:=fmt.Sprintf(`{"peer_node_id":"peer","source_id":"peer-link",
  "expected_goal_revision":%d,"expires_at":%d}`,
  goal.Revision,now+30*60000)
 for _,tc:=range []struct{who string;expect int}{
  {"",401},{"outsider",403},{"robot",403},
 }{
  got:=call("POST",tc.who,grants,input)
  if got.Code!=tc.expect{t.Fatalf("nonhuman %s granted Node telemetry %d %s",
   tc.who,got.Code,got.Body)}
 }
 if got:=call("POST","operator",grants,
  strings.Replace(input,"peer-link","not-allowed",1));got.Code<400{
  t.Fatalf("nonallowlisted Node source approved: %d",got.Code)
 }
 if got:=call("POST","operator",grants,
  strings.TrimSuffix(input,"}")+`,"peer_node_id":"other"}`);got.Code!=400{
  t.Fatalf("ambiguous peer ID accepted: %d",got.Code)
 }
 approved:=call("POST","operator",grants,input)
 if approved.Code!=201{t.Fatalf("operator mTLS grant rejected: %d %s",
  approved.Code,approved.Body)}
 var grant projectworkspace.GoalNodeMeasurementGrant
 if err=json.Unmarshal(approved.Body.Bytes(),&grant);err!=nil{t.Fatal(err)}
 if grant.PeerNodeID!="peer"||grant.PeerTLSFingerprint!=fp||
  grant.Status!="approved"||grant.LastSequence!=0{
  t.Fatalf("signed Node grant wrongly scoped: %+v",grant)
 }
 if got:=call("POST","operator",grants,input);got.Code<400{
  t.Fatalf("stale Goal revision permitted grant duplicate: %d",got.Code)
 }
 metadata:=call("GET","operator",grants,"")
 if metadata.Code!=200||!strings.Contains(metadata.Body.String(),grant.ID){
  t.Fatalf("operator cannot audit approved grant: %d %s",metadata.Code,metadata.Body)
 }
 if got:=call("GET","outsider",grants,"");got.Code!=403{
  t.Fatalf("cross-tenant grant list permitted: %d",got.Code)
 }
 if got:=call("GET","robot",grants,"");got.Code!=403{
  t.Fatalf("agent may enumerate Node grant metadata: %d",got.Code)
 }
 receipts:=call("GET","operator",base+"/node-measurements","")
 if receipts.Code!=200||!strings.Contains(receipts.Body.String(),"[]"){
  t.Fatalf("unactivated Node telemetry should have no receipts: %d %s",
   receipts.Code,receipts.Body)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 revokedBody:=fmt.Sprintf(`{"expected_goal_revision":%d,"expected_grant_revision":1}`,
  goal.Revision)
 revokeURL:=grants+"/"+grant.ID+"/revoke"
 if got:=call("POST","robot",revokeURL,revokedBody);got.Code!=403{
  t.Fatalf("agent revoked Node transport: %d",got.Code)
 }
 revoked:=call("POST","operator",revokeURL,revokedBody)
 if revoked.Code!=200||!strings.Contains(revoked.Body.String(),"\"revoked\""){
  t.Fatalf("human revocation failed: %d %s",revoked.Code,revoked.Body)
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if goal.Health!="unobserved"||goal.LastVerifiedAt!=0||
  goal.Status!="configured"{
  t.Fatalf("telemetry authority falsely activated protective monitoring: %+v",goal)
 }
}
