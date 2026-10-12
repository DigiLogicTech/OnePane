package api

import (
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestGoalSignedSourceMachineCredentialAndHumanKeyAPIBoundaries(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/signed-source-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,sqlText:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('sensor','service','Local sensor','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('wrong-sensor','service','Impostor','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','sensor','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','wrong-sensor','active',1,1)`,
 }{if _,err=db.SQL().ExecContext(ctx,sqlText);err!=nil{t.Fatal(err)}}
 // REAL bearer authorisation and scoped API credentials, not a test fake
 // that automatically grants all capabilities.
 issueKey:=func(id,principal,workspace,caps string){
  t.Helper()
  digest:=sha256.Sum256([]byte(id))
  _,err:=db.SQL().ExecContext(ctx,`INSERT INTO api_credentials(
   id,principal_id,name,credential_hash,workspace_scope_json,
   capability_scope_json,created_at) VALUES(?,?,?,?,?,?,?)`,
   "cred-"+id,principal,id,hex.EncodeToString(digest[:]),
   workspace,caps,time.Now().UnixMilli())
  if err!=nil{t.Fatal(err)}
 }
 issueKey("operator-key","operator",`["tenant"]`,`["project.read","project.write"]`)
 issueKey("source-key","sensor",`["tenant"]`,`["goal.source.submit"]`)
 issueKey("write-only-key","sensor",`["tenant"]`,`["project.write"]`)
 issueKey("read-only-key","sensor",`["tenant"]`,`["project.read"]`)
 issueKey("foreign-workspace-key","sensor",`["foreign"]`,`["goal.source.submit"]`)
 issueKey("impostor-key","wrong-sensor",`["tenant"]`,`["goal.source.submit"]`)
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Local signed sensors",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Read-only evidence",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 g,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"operator",
  Objective:"Authenticate exact local source",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["gateway"]}`),
 })
 if err!=nil{t.Fatal(err)}
 h:=NewServer(svc,nil,NewBearerAuthorizer(db.SQL(),clock.Real{})).Handler()
 route:="/v1/projects/"+p.ID+"/continuous-goals/"+g.ID
 request:=func(method,path,token,body string)*httptest.ResponseRecorder{
  t.Helper()
  req:=httptest.NewRequest(method,path,strings.NewReader(body))
  if token!=""{req.Header.Set("Authorization","Bearer "+token)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder();h.ServeHTTP(w,req);return w
 }
 pub,priv,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
 input,err:=json.Marshal(map[string]any{
  "source_id":"gateway","source_principal_id":"sensor",
  "public_key":pub,"expected_goal_revision":g.Revision,
  "expires_at":time.Now().Add(time.Hour).UnixMilli(),
 })
 if err!=nil{t.Fatal(err)}
 signers:=route+"/source-signers"
 if got:=request("POST",signers,"source-key",string(input));got.Code!=403{
  t.Fatalf("service granted itself signer identity: %d %s",got.Code,got.Body.String())
 }
 if got:=request("POST",signers,"","{}");got.Code!=401{
  t.Fatalf("unauthenticated signup: %d",got.Code)
 }
 if got:=request("POST",signers,"operator-key",`{"source_id":"gateway","source_id":"evil"}`);got.Code!=400{
  t.Fatalf("duplicate source field accepted: %d",got.Code)
 }
 if got:=request("POST",signers,"operator-key",strings.Repeat("x",8193));got.Code!=400{
  t.Fatalf("oversize signup accepted: %d",got.Code)
 }
 approved:=request("POST",signers,"operator-key",string(input))
 if approved.Code!=201{t.Fatalf("human signer admission: %d %s",approved.Code,approved.Body.String())}
 var key projectworkspace.GoalObservationSigner
 if err=json.Unmarshal(approved.Body.Bytes(),&key);err!=nil{t.Fatal(err)}
 if key.SourceID!="gateway"||key.SourcePrincipalID!="sensor"||key.Status!="approved"{
  t.Fatalf("unscoped key %+v",key)
 }
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 source:="sensor"
 obsSvc:=observation.NewService(db.SQL(),db,clock.Real{})
 makeObservation:=func()observation.Observation{
  t.Helper()
  o,err:=obsSvc.Record(ctx,observation.RecordCommand{
   WorkspaceID:"tenant",SubjectRef:"sensor://CONFIDENTIAL_HOST",
   ObservationType:"gateway.sample",ProbeToolID:"gateway",
   ProbeToolVersion:"1",SourcePrincipalID:&source,ActorPrincipalID:&source,
   Value:json.RawMessage(`{"credential":"SENSITIVE_SENSOR_PAYLOAD","reachable":true}`),
   Label:policy.DataLabel{WorkspaceID:"tenant",
    Confidentiality:policy.ConfidentialityInternal,Residency:policy.ResidencyAny,
    Trust:policy.TrustUnverifiedDerived},
  })
  if err!=nil{t.Fatal(err)}
  return o
 }
 first:=makeObservation()
 submitBody:=func(o observation.Observation,seq int64,private ed25519.PrivateKey)string{
  t.Helper()
  signed,err:=projectworkspace.SignedGoalObservationMessage(p.ID,g.ID,key.ID,
   o.ID,o.IntegrityHash,seq)
  if err!=nil{t.Fatal(err)}
  body,err:=json.Marshal(map[string]any{
   "signer_id":key.ID,"observation_id":o.ID,
   "sequence":seq,"expected_goal_revision":g.Revision,
   "signature":ed25519.Sign(private,signed),
  })
  if err!=nil{t.Fatal(err)}
  return string(body)
 }
 submit:=route+"/signed-observations/submit"
 valid:=submitBody(first,1,priv)
 for _,tc:=range []struct{token,body string;want int}{
  {"",valid,401},
  {"operator-key",valid,403},
  {"write-only-key",valid,403},
  {"read-only-key",valid,403},
  {"foreign-workspace-key",valid,403},
 }{
  got:=request("POST",submit,tc.token,tc.body)
  if got.Code!=tc.want{t.Fatalf("unauthorised proof submit token=%s: %d %s",tc.token,got.Code,got.Body.String())}
 }
 if got:=request("POST",submit,"impostor-key",valid);got.Code<400{
  t.Fatalf("different service accepted another source key: %d",got.Code)
 }
 spoof:=strings.TrimSuffix(valid,"}")+`,"actor_principal_id":"operator"}`
 if got:=request("POST",submit,"source-key",spoof);got.Code!=400{
  t.Fatalf("spoofed actor field accepted: %d",got.Code)
 }
 dupe:=strings.TrimSuffix(valid,"}")+`,"sequence":999}`
 if got:=request("POST",submit,"source-key",dupe);got.Code!=400{
  t.Fatalf("duplicate anti-replay sequence accepted: %d",got.Code)
 }
 if got:=request("POST",submit,"source-key",strings.Replace(valid,"\"sequence\":1","\"sequence\":999",1));got.Code<400{
  t.Fatalf("wrong sequence was accepted with mismatched signature: %d",got.Code)
 }
 successful:=request("POST",submit,"source-key",valid)
 if successful.Code!=201{t.Fatalf("authorised signed evidence rejected: %d %s",successful.Code,successful.Body.String())}
 var receipt projectworkspace.SignedGoalObservationProof
 if err=json.Unmarshal(successful.Body.Bytes(),&receipt);err!=nil{t.Fatal(err)}
 if receipt.SourceID!="gateway"||receipt.ObservationID!=first.ID{
  t.Fatalf("signed receipt misbound %+v",receipt)
 }
 for _,s:=range []string{successful.Body.String(),approved.Body.String()}{
  if bytes.Contains([]byte(s),[]byte("SENSITIVE_SENSOR_PAYLOAD"))||
   bytes.Contains([]byte(s),[]byte("CONFIDENTIAL_HOST")){
   t.Fatalf("sensor payload leaked by HTTP: %s",s)
  }
 }
 if got:=request("POST",submit,"source-key",valid);got.Code<400{
  t.Fatalf("replayed signed evidence was accepted: %d",got.Code)
 }
 if got:=request("POST",route+"/signed-observations","source-key",valid);got.Code!=405{
  t.Fatalf("read-only metadata GET became writable: %d",got.Code)
 }
 list:=request("GET",route+"/signed-observations","operator-key","")
 if list.Code!=200||!strings.Contains(list.Body.String(),receipt.ID){
  t.Fatalf("signed proof history absent: %d %s",list.Code,list.Body.String())
 }
 if strings.Contains(list.Body.String(),"SENSITIVE_SENSOR_PAYLOAD"){
  t.Fatal("raw Observation leaked via history")
 }
 revoke:=fmt.Sprintf(`{"expected_goal_revision":%d,"expected_signer_revision":2}`,g.Revision)
 revokeRoute:=route+"/source-signers/"+key.ID+"/revoke"
 if got:=request("POST",revokeRoute,"source-key",revoke);got.Code!=403{
  t.Fatalf("sensor revoked own source identity: %d",got.Code)
 }
 revoked:=request("POST",revokeRoute,"operator-key",revoke)
 if revoked.Code!=200{t.Fatalf("human key revocation: %d %s",revoked.Code,revoked.Body.String())}
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 second:=makeObservation()
 if got:=request("POST",submit,"source-key",submitBody(second,2,priv));got.Code<400{
  t.Fatalf("revoked signer admitted new proof: %d",got.Code)
 }
 var count int
 if err=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM project_goal_signed_observation_proofs").Scan(&count);err!=nil||count!=1{
  t.Fatalf("invalid attempts persisted signed proofs: %d %v",count,err)
 }
 state,err:=svc.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 if state.Status!="configured"||state.Health!="unobserved"||state.LastVerifiedAt!=0{
  t.Fatalf("signed API activated goal monitoring: %+v",state)
 }
}
