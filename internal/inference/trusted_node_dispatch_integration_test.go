//go:build integration

package inference

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

const approvedPeerJSON = `{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["peer"]}}}`

func TestTrustedNodeGatewayEnforcesPersistedGrantsAndFreshPairing(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/peer.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=time.Now().UTC().UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES('agent','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('tenant','agent','active',1,1)`,
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Managed",CreatedBy:"agent",
  ProjectPolicyJSON:json.RawMessage(approvedPeerJSON),
 });if err!=nil{t.Fatal(err)}
 ws,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Monitor",ActorPrincipalID:"agent",
  AISettingsJSON:json.RawMessage(approvedPeerJSON),
 });if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 trow,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&ws.ID,
  Objective:"authorised task",
 });if err!=nil{t.Fatal(err)}
 cmd:=ExecuteCommand{WorkspaceID:"tenant",TaskID:&trow.ID}
 peer:="peer"
 dep:=ModelDeployment{NodeID:&peer}
 infer:=NewService(db.SQL(),db,clock.Real{})
 infer.localNodeID="origin"
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);err==nil{
  t.Fatal("missing pairing/manifest authorised remote transfer")
 }
 if _,err=db.SQL().ExecContext(ctx,
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,
   protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
   VALUES('peer','Peer',0,'peer-fingerprint','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,
   now,now,now);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  `INSERT INTO node_pairings(id,peer_node_id,direction,status,pairing_token,pairing_token_hash,
   pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,peer_tls_fingerprint,
   peer_certificate_pem,expires_at,paired_at,revision,created_at,updated_at)
   VALUES('pair-peer','peer','outbound','paired','token','hashed','000000','hashed',1,1,
   'sha256:dead','',?,?,1,?,?)`,now+60000,now,now,now);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  `INSERT INTO node_capability_manifests(peer_node_id,sequence,manifest_json,received_at,
   expires_at,revision,updated_at) VALUES('peer',1,'{"node_id":"peer","protocol":"onepane-federation-v1","models":[]}',?,?,1,?)`,
   now,now+3600000,now);err!=nil{t.Fatal(err)}
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);err!=nil{
  t.Fatalf("explicitly allowed paired Node denied: %v",err)
 }
 // Selected Project compute policy is additive to canonical Task consent.
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO remote_node_compute_policies(
  node_id,enabled,project_scope,allowed_projects_json,created_at,updated_at)
  VALUES('peer',1,'selected','["another-project"]',?,?)`,now,now);err!=nil{
  t.Fatal(err)
 }
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);!errors.Is(err,ErrRemoteNodeUnsupported){
  t.Fatalf("selected Project outside allowlist escaped Node policy: %v",err)
 }
 allowedProjects,_:=json.Marshal([]string{p.ID})
 if _,err=db.SQL().ExecContext(ctx,`UPDATE remote_node_compute_policies
  SET allowed_projects_json=? WHERE node_id='peer'`,string(allowedProjects));err!=nil{t.Fatal(err)}
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);err!=nil{
  t.Fatalf("explicitly selected Task Project was denied: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE remote_node_compute_policies SET enabled=0 WHERE node_id='peer'`);err!=nil{t.Fatal(err)}
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);!errors.Is(err,ErrRemoteNodeUnsupported){
  t.Fatalf("disabled Node still exported Task data: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE remote_node_compute_policies SET enabled=1,project_scope='all' WHERE node_id='peer'`);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE node_pairings SET peer_confirmed=0 WHERE peer_node_id='peer'`);err!=nil{t.Fatal(err)}
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);!errors.Is(err,ErrRemoteNodeUnsupported){
  t.Fatalf("one-sided pairing admitted off-host Task: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE node_pairings SET peer_confirmed=1 WHERE peer_node_id='peer'`);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE node_capability_manifests
  SET manifest_json='{"node_id":"different","protocol":"onepane-federation-v1","models":[]}'
  WHERE peer_node_id='peer'`);err!=nil{t.Fatal(err)}
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);!errors.Is(err,ErrRemoteNodeUnsupported){
  t.Fatalf("wrong origin manifest admitted Task transfer: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE node_capability_manifests
  SET manifest_json='{"node_id":"peer","protocol":"onepane-federation-v1","models":[]}'
  WHERE peer_node_id='peer'`);err!=nil{t.Fatal(err)}
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);err!=nil{
  t.Fatalf("restored trusted Node admission rejected: %v",err)
 }
 cloudProvider:="provider"
 bad:=dep
 bad.ProviderConnectionID=&cloudProvider
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,bad);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("provider-backed remote should need separate cloud policy: %v",err)
 }
 noTask:=cmd;noTask.TaskID=nil
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,noTask,dep);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("taskless remote call must not gain implicit transfer scope: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_workspaces SET ai_settings_json='{}' WHERE id=?",ws.ID);err!=nil{t.Fatal(err)}
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("revoked Workspace approval did not stop transfer: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE project_workspaces SET ai_settings_json=? WHERE id=?",approvedPeerJSON,ws.ID);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE node_pairings SET status='revoked' WHERE peer_node_id='peer'");err!=nil{t.Fatal(err)}
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);!errors.Is(err,ErrRemoteNodeUnsupported){
  t.Fatalf("revoked pairing did not stop transfer: %v",err)
 }
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE node_pairings SET status='paired' WHERE peer_node_id='peer'");err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  "UPDATE node_capability_manifests SET expires_at=? WHERE peer_node_id='peer'",now-1000);err!=nil{t.Fatal(err)}
 if err=infer.authorizeTrustedNodeTaskDispatch(ctx,cmd,dep);!errors.Is(err,ErrRemoteNodeUnsupported){
  t.Fatalf("stale manifest did not stop transfer: %v",err)
 }
}
