package agentruntime

import (
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/policy"
)

func TestOriginRuntimeRequiresVerifiableLoopbackBeforeTransport(t *testing.T){
 node:="origin"
 data:=json.RawMessage(`{"max_confidentiality":"internal","allowed_residency":["origin_node"],"destination_kind":"origin_node"}`)
 service:=&Service{localNodeID:"origin",transports:NewRuntimeTransportRegistry()}
 if err:=service.transports.Register("builtin.agent_protocol_http","1",AgentProtocolHTTPTransport{});err!=nil{t.Fatal(err)}
 label:=policy.DataLabel{WorkspaceID:"ws",Confidentiality:policy.ConfidentialityPublic,Residency:policy.ResidencyOriginNode,Trust:policy.TrustUserInstruction,OriginNodeID:"origin"}
 for _,tc:=range []struct{name,base,adapter string;node *string;want bool}{
  {"literal ipv4 loopback","http://127.0.0.1:9000","builtin.agent_protocol_http",&node,true},
  {"literal ipv6 loopback","https://[::1]:9000","builtin.agent_protocol_http",&node,true},
  {"dns localhost","http://localhost:9000","builtin.agent_protocol_http",&node,false},
  {"remote internet","https://example.com","builtin.agent_protocol_http",&node,false},
  {"remote rfc1918","http://192.168.1.4","builtin.agent_protocol_http",&node,false},
  {"credential redirect style","http://127.0.0.1@evil.example:9000","builtin.agent_protocol_http",&node,false},
  {"malicious adapter","http://127.0.0.1:9000","custom.http.runtime",&node,false},
  {"missing canonical node","http://127.0.0.1:9000","builtin.agent_protocol_http",nil,false},
 }{
  t.Run(tc.name,func(t *testing.T){
   raw,_:=json.Marshal(map[string]string{"base_url":tc.base})
   c:=Connection{WorkspaceID:ptrRuntimeString("ws"),NodeID:tc.node,
    RuntimeKind:"hermes",Status:StatusConnected,TrustState:TrustUserTrusted,
    OperatingMode:ProposalOnly,AdapterName:tc.adapter,AdapterVersion:"1",
    EndpointJSON:raw,DataPolicyJSON:data}
   _,transport,err:=service.authorizeInvocation(c,label)
   if tc.want{
    if err!=nil||transport==nil{t.Fatalf("safe origin HTTP bridge denied: %v",err)}
   }else if !errors.Is(err,ErrRuntimeInformationFlow)||transport!=nil{
    t.Fatalf("unauthorised runtime destination accepted: err=%v,transport=%v",err,transport)
   }
  })
 }
}
func ptrRuntimeString(v string)*string{return &v}
