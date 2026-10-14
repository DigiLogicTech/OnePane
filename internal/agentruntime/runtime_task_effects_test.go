package agentruntime

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 _ "modernc.org/sqlite"
)

func TestAgentRuntimeTaskEgressEnforcesStoredWorkspaceNetworkAndSecrets(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"runtime-features.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE workspaces(id TEXT PRIMARY KEY,status TEXT NOT NULL)`,
  `CREATE TABLE projects(id TEXT PRIMARY KEY,workspace_id TEXT,status TEXT,project_policy_json TEXT)`,
  `CREATE TABLE project_workspaces(id TEXT PRIMARY KEY,project_id TEXT,status TEXT,ai_settings_json TEXT)`,
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT,completion_json TEXT)`,
  `INSERT INTO workspaces VALUES('ws','active')`,
  `INSERT INTO projects VALUES('game','ws','active','{"feature_policy":{"NetworkAccess":"enabled","SecretsAccess":"enabled"}}')`,
  `INSERT INTO project_workspaces VALUES('world','game','active','{"feature_policy":{"NetworkAccess":"disabled","SecretsAccess":"disabled"}}')`,
  `INSERT INTO tasks VALUES('task','ws','game','world','{}')`,
  `INSERT INTO tasks VALUES('legacy','ws',NULL,NULL,'{}')`,
 }{
  if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatalf("fixture failed %q: %v",q,err)}
 }
 svc:=NewService(db,nil,nil,clock.Real{})
 task:="task"
 cmd:=InvokeCommand{WorkspaceID:"ws",TaskID:&task}
 local:=Connection{AuthType:"none"}
 if err:=svc.requireTaskRuntimeEffects(ctx,cmd,local);
  !errors.Is(err,ErrRuntimeInformationFlow){
  t.Fatalf("network-denied Workspace admitted external runtime: %v",err)
 }
 _,err=db.ExecContext(ctx,`UPDATE project_workspaces SET ai_settings_json=
  '{"feature_policy":{"NetworkAccess":"enabled","SecretsAccess":"disabled"}}'
  WHERE id='world'`)
 if err!=nil{t.Fatal(err)}
 if err:=svc.requireTaskRuntimeEffects(ctx,cmd,local);err!=nil{
  t.Fatalf("explicitly approved network-only runtime denied: %v",err)
 }
 bearer:=Connection{AuthType:"bearer"}
 if err:=svc.requireTaskRuntimeEffects(ctx,cmd,bearer);
  !errors.Is(err,ErrRuntimeInformationFlow){
  t.Fatalf("secrets-disabled Workspace dispatched bearer credentials: %v",err)
 }
 _,err=db.ExecContext(ctx,`UPDATE project_workspaces SET ai_settings_json=
  '{"feature_policy":{"NetworkAccess":"enabled","SecretsAccess":"enabled"}}'
  WHERE id='world'`)
 if err!=nil{t.Fatal(err)}
 if err:=svc.requireTaskRuntimeEffects(ctx,cmd,bearer);err!=nil{
  t.Fatalf("two independently authorised effects denied: %v",err)
 }
 _,err=db.ExecContext(ctx,`UPDATE project_workspaces SET ai_settings_json=
  '{"feature_policy":{"NetworkAccess":"disabled","SecretsAccess":"enabled"}}'
  WHERE id='world'`)
 if err!=nil{t.Fatal(err)}
 if err:=svc.requireTaskRuntimeEffects(ctx,cmd,bearer);
  !errors.Is(err,ErrRuntimeInformationFlow){
  t.Fatalf("network revocation failed to block live runtime: %v",err)
 }
 cmd.WorkspaceID="other"
 if err:=svc.requireTaskRuntimeEffects(ctx,cmd,bearer);
  !errors.Is(err,ErrRuntimeInformationFlow){
  t.Fatalf("foreign Task owner borrowed another tenant's rights: %v",err)
 }
 cmd.WorkspaceID="ws"
 legacy:="legacy";cmd.TaskID=&legacy
 if err:=svc.requireTaskRuntimeEffects(ctx,cmd,local);err!=nil{
  t.Fatalf("legacy Task with unconfigured feature policy must retain existing independent authority: %v",err)
 }
 nonexistent:="missing";cmd.TaskID=&nonexistent
 if err:=svc.requireTaskRuntimeEffects(ctx,cmd,local);
  !errors.Is(err,ErrRuntimeInformationFlow){
  t.Fatalf("missing Task bypassed canonical Workspace policy: %v",err)
 }
}

func TestAgentRuntimeTaskEffectStoreMissingFailsClosed(t *testing.T){
 svc:=&Service{}
 task:="task"
 cmd:=InvokeCommand{WorkspaceID:"ws",TaskID:&task}
 if err:=svc.requireTaskRuntimeEffects(context.Background(),cmd,
  Connection{AuthType:"none"});
  !errors.Is(err,ErrRuntimeInformationFlow){
  t.Fatalf("missing policy DB admitted external runtime: %v",err)
 }
}

func TestTasklessAgentRuntimeOnlyPermitsUncredentialedLocalProposalBridge(t *testing.T){
 node:="node"
 service:=&Service{localNodeID:node}
 c:=Connection{
  NodeID:&node,AdapterName:"builtin.agent_protocol_http",AdapterVersion:"1",
  OperatingMode:ProposalOnly,AuthType:"none",
  EndpointJSON:json.RawMessage(`{"base_url":"http://127.0.0.1:9000"}`),
  DataPolicyJSON:json.RawMessage(`{"destination_kind":"origin_node"}`),
 }
 cmd:=InvokeCommand{WorkspaceID:"ws"}
 if err:=service.requireTaskRuntimeEffects(context.Background(),cmd,c);err!=nil{
  t.Fatalf("explicit local proposal without network secrets rejected: %v",err)
 }
 negatives:=[]struct{name string;alter func(*Connection)}{
  {"offhost",func(x *Connection){x.EndpointJSON=json.RawMessage(`{"base_url":"https://example.com"}`)}},
  {"ambiguous localhost dns",func(x *Connection){x.EndpointJSON=json.RawMessage(`{"base_url":"http://localhost:9000"}`)}},
  {"private nonloopback",func(x *Connection){x.EndpointJSON=json.RawMessage(`{"base_url":"http://192.168.1.9"}`)}},
  {"bearer credential",func(x *Connection){x.AuthType="bearer"}},
  {"gateway callback",func(x *Connection){x.OperatingMode=GatewayMediated}},
  {"unmanaged",func(x *Connection){x.OperatingMode=Unmanaged}},
  {"cloud classification",func(x *Connection){x.DataPolicyJSON=json.RawMessage(`{"destination_kind":"cloud"}`)}},
  {"untrusted classification",func(x *Connection){x.DataPolicyJSON=json.RawMessage(`{"destination_kind":"untrusted"}`)}},
  {"missing policy",func(x *Connection){x.DataPolicyJSON=json.RawMessage(`{}`)}},
  {"forged Node",func(x *Connection){n:="foreign-node";x.NodeID=&n}},
  {"unknown adapter",func(x *Connection){x.AdapterName="custom.external"}},
 }
 for _,tc:=range negatives{
  t.Run(tc.name,func(t *testing.T){
   clone:=c;tc.alter(&clone)
   if err:=service.requireTaskRuntimeEffects(context.Background(),cmd,clone);
    !errors.Is(err,ErrRuntimeInformationFlow){
    t.Fatalf("unscoped Agent Runtime bypassed Task boundary: %v",err)
   }
  })
 }
}
