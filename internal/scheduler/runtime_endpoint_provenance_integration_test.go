//go:build integration

package scheduler

import(
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/policy"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestRuntimeCatalogueCannotPromoteSelfDeclaredZeroCostOrOffHostOrigin(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/runtime-catalog.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,1,1)`,
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
    VALUES('origin','Host',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1,1,1)`,
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
    VALUES('remote','Remote',0,'fp2','paired','LOCAL_TRUSTED','{}','{}',1,1,1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 endpoint:=func(base string)string{
  raw,_:=json.Marshal(map[string]string{"base_url":base});return string(raw)
 }
 policyJSON:=func(kind string)string{
  raw,_:=json.Marshal(map[string]any{
   "max_confidentiality":"public","allowed_residency":[]string{"any"},
   "destination_kind":kind,
  });return string(raw)
 }
 caps:=`{"roles":[],"capabilities":["agent.reason"],"protocol_level":"L1","scheduling":{"cost_class":"local","hard_zero_incremental_cost":true}}`
 for _,tc:=range []struct{id,node,base,kind string}{
  {"host-loopback","origin","http://127.0.0.1:9000","origin_node"},
  {"dns-localhost","origin","http://localhost:9000","origin_node"},
  {"external-under-local-node","origin","https://example.com","origin_node"},
  {"rfc1918-under-local-node","origin","http://192.168.1.10:9000","origin_node"},
  {"remote-node-spoof","remote","http://127.0.0.1:9000","origin_node"},
  {"unsupported-paired-transfer","remote","https://example.com","trusted_node"},
  {"claimed-cloud-free","origin","https://example.com","cloud"},
 }{
  _,err:=db.SQL().ExecContext(ctx,`INSERT INTO agent_runtime_connections(
   id,workspace_id,node_id,runtime_kind,display_name,adapter_name,
   adapter_version,endpoint_json,auth_type,status,trust_state,operating_mode,
   protocol_json,capabilities_json,data_policy_json,revision,created_at,updated_at)
   VALUES(?,'ws',?,'hermes',?,'builtin.agent_protocol_http','1',?,'none',
   'connected','user_trusted','proposal_only','{}',?,?,1,1,1)`,
   tc.id,tc.node,tc.id,endpoint(tc.base),caps,policyJSON(tc.kind))
  if err!=nil{t.Fatal(err)}
 }
 candidates,err:=NewCatalog(db.SQL()).Candidates(ctx,"ws","agent.reason","")
 if err!=nil{t.Fatal(err)}
 seen:=map[string]bool{}
 for _,c:=range candidates{
  if c.Kind!=CandidateAgentRuntime{continue}
  seen[c.ID]=true
  if c.Local||c.TrustedNodeVerified||c.HardZeroIncrementalCost||
   c.CostClass!=CostUnknown||c.NodeID!=""{
   t.Errorf("runtime %s self-proclaimed compute trust or $0: %+v",c.ID,c)
  }
  switch c.ID{
  case "host-loopback":
   if !c.Schedulable||c.Destination.Kind!=policy.DestinationOriginNode||
    c.Destination.NodeID!="origin"{
    t.Errorf("genuine loopback bridge lost ordinary scheduled availability: %+v",c)
   }
  case "claimed-cloud-free":
   if !c.Schedulable||c.Destination.Kind!=policy.DestinationCloud{
    t.Errorf("remote runtime misclassified: %+v",c)
   }
  default:
   if c.Schedulable{t.Errorf("%s masqueraded as approved host/paired runtime: %+v",c.ID,c)}
  }
 }
 if len(seen)!=7{t.Fatalf("lost scheduler runtime candidates: %v",seen)}
 for _,restriction:=range []struct{name string;req RouteRequest}{
  {"strict-local",RouteRequest{LocalOnly:true,AllowPotentialMonetarySpend:true}},
  {"strict-free",RouteRequest{RequireZeroIncrementalCost:true,AllowPotentialMonetarySpend:true}},
  {"no-cloud-consent",RouteRequest{}},
 }{
  req:=restriction.req
  req.WorkspaceID="ws";req.CapabilityID="agent.reason";req.ProtocolLevel="L1"
  req.DataLabel=policy.DataLabel{WorkspaceID:"ws",Confidentiality:policy.ConfidentialityPublic,
   Residency:policy.ResidencyAny,Trust:policy.TrustUserInstruction}
  dec,e:=Route(req,candidates)
  if e!=ErrNoEligibleCandidate||dec.Selected!=nil{
   t.Fatalf("%s routed unverified runtime: %+v %v",restriction.name,dec.Selected,e)
  }
 }
}
