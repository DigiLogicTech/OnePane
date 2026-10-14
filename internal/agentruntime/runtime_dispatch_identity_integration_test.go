//go:build integration

package agentruntime

import(
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestQueuedAgentRuntimeIdentityAndTrustRevocationFailBeforeTransport(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/runtime-identity.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,sql:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('ws','Test','active',1,1,1)`,
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,
    trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
    VALUES('node','Node',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1,1,1)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,sql);err!=nil{t.Fatal(err)}
 }
 registry:=NewRegistry()
 if err:=RegisterBuiltinAdapters(registry);err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,registry,clock.Real{})
 transport:=NewRuntimeTransportRegistry()
 if err:=transport.Register("builtin.agent_protocol_http","1",AgentProtocolHTTPTransport{});err!=nil{t.Fatal(err)}
 svc.transports=transport
 svc.localNodeID="node"
 ws,node:="ws","node"
 original,err:=svc.Register(ctx,RegisterCommand{
  WorkspaceID:&ws,NodeID:&node,RuntimeKind:"hermes",
  DisplayName:"Node-local bridge",AdapterName:"builtin.agent_protocol_http",
  AdapterVersion:"1",EndpointJSON:json.RawMessage(`{"base_url":"http://127.0.0.1:9000"}`),
  AuthType:"none",TrustState:TrustUserTrusted,OperatingMode:ProposalOnly,
  DataPolicyJSON:json.RawMessage(`{"max_confidentiality":"public","allowed_residency":["origin_node"],"destination_kind":"origin_node"}`),
 })
 if err!=nil{t.Fatal(err)}
 original,err=svc.SetStatus(ctx,SetStatusCommand{
  ConnectionID:original.ID,ExpectedRevision:original.Revision,Status:StatusConnected})
 if err!=nil{t.Fatal(err)}
 label:=policy.DataLabel{WorkspaceID:"ws",Confidentiality:policy.ConfidentialityPublic,
  Residency:policy.ResidencyOriginNode,Trust:policy.TrustUserInstruction,OriginNodeID:"node"}
 if err:=svc.requireCurrentRuntimeDispatch(ctx,original,label);err!=nil{
  t.Fatalf("stable canonical local bridge denied: %v",err)
 }
 cases:=[]struct{name,sql string}{
  {"connection revoked",`UPDATE agent_runtime_connections SET status='revoked',revision=revision+1 WHERE id=?`},
  {"adapter replaced",`UPDATE agent_runtime_connections SET adapter_name='malicious.adapter',revision=revision+1 WHERE id=?`},
  {"endpoint rerouted external",`UPDATE agent_runtime_connections SET endpoint_json='{"base_url":"https://example.com"}',revision=revision+1 WHERE id=?`},
  {"endpoint switched to hostname",`UPDATE agent_runtime_connections SET endpoint_json='{"base_url":"http://localhost:9000"}',revision=revision+1 WHERE id=?`},
  {"policy revoked",`UPDATE agent_runtime_connections SET data_policy_json='{"max_confidentiality":"public","allowed_residency":["any"],"destination_kind":"cloud"}',revision=revision+1 WHERE id=?`},
  {"secret swapped",`UPDATE agent_runtime_connections SET secret_ref='vault:different',revision=revision+1 WHERE id=?`},
  {"operating mode changed",`UPDATE agent_runtime_connections SET operating_mode='unmanaged',revision=revision+1 WHERE id=?`},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   if _,err:=db.SQL().ExecContext(ctx,tc.sql,original.ID);err!=nil{t.Fatal(err)}
   if err:=svc.requireCurrentRuntimeDispatch(ctx,original,label);
    !errors.Is(err,ErrRuntimeInformationFlow){
    t.Fatalf("mutated runtime dispatch unexpectedly admitted: %v",err)
   }
   // Restore exactly the original, canonical database row between cases.
   if _,err:=db.SQL().ExecContext(ctx,`UPDATE agent_runtime_connections
    SET status=?,trust_state=?,adapter_name=?,adapter_version=?,
     endpoint_json=?,data_policy_json=?,secret_ref=?,
     operating_mode=?,revision=?,updated_at=?
    WHERE id=?`,original.Status,original.TrustState,
    original.AdapterName,original.AdapterVersion,string(original.EndpointJSON),
    string(original.DataPolicyJSON),original.SecretRef,
    original.OperatingMode,original.Revision,original.UpdatedAt,original.ID);
    err!=nil{t.Fatal(err)}
   if err:=svc.requireCurrentRuntimeDispatch(ctx,original,label);err!=nil{
    t.Fatalf("stable identity rejected after fixture restore: %v",err)
   }
  })
 }
}
