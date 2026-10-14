//go:build integration

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestSQLiteSchedulerProtectsSubscriptionAndPrefersStrictFree(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/harness.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	seed := []string{
		`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,revision,created_at,updated_at) VALUES('p-free','ws','omniroute','OmniRoute','none','connected','{"data_policy":{"max_confidentiality":"public","allowed_residency":["any"],"destination_kind":"cloud"},"scheduling":{"cost_class":"free","hard_zero_incremental_cost":true}}',1,1,1)`,
		`INSERT INTO models(id,provider_name,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('m-free','omniroute','auto/coding','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('d-free','m-free','p-free','openai-compatible','{}','ready','fp-free',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('cp-free','d-free','agent.reason','L2',0,64000,'low','supported','{}','{}','cpf-free',1,1,1)`,
		`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,revision,created_at,updated_at) VALUES('p-sub','openai','openai_chatgpt_plan','ChatGPT Plan','oauth2-pkce','connected','{"data_policy":{"max_confidentiality":"public","allowed_residency":["any"],"destination_kind":"cloud"},"scheduling":{"cost_class":"included_subscription","hard_zero_incremental_cost":false}}',1,1,1)`,
	}
	// Correct workspace typo in the final statement separately so the fixture is
	// explicit and foreign-key checked.
	for i, q := range seed[:4] {
		if _, err := db.SQL().ExecContext(ctx, q); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,revision,created_at,updated_at) VALUES('p-sub','ws','openai_chatgpt_plan','ChatGPT Plan','oauth2-pkce','connected','{"data_policy":{"max_confidentiality":"public","allowed_residency":["any"],"destination_kind":"cloud"},"scheduling":{"cost_class":"included_subscription","hard_zero_incremental_cost":false}}',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	for i, q := range []string{
		`INSERT INTO models(id,provider_name,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('m-sub','openai','gpt-sub','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('d-sub','m-sub','p-sub','openai-chatgpt-plan','{}','ready','fp-sub',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('cp-sub','d-sub','agent.reason','L2',0,64000,'low','verified','{}','{}','cpf-sub',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('cp-sub-only','d-sub','agent.subscription-only','L2',0,64000,'low','verified','{}','{}','cpf-sub-only',1,1,1)`,
	} {
		if _, err := db.SQL().ExecContext(ctx, q); err != nil {
			t.Fatalf("subscription seed %d: %v", i, err)
		}
	}

	svc := NewService(db.SQL(), db, clock.Real{})
	label := policy.DataLabel{WorkspaceID: "ws", Confidentiality: policy.ConfidentialityPublic, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}
	d, err := svc.Route(ctx, RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label, AllowSubscriptionUsage: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.Selected == nil || d.Selected.Candidate.ID != "d-free" {
		t.Fatalf("strict free route should beat opted-in subscription: %#v", d.Selected)
	}

	if _, err := svc.Route(ctx, RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.subscription-only", ProtocolLevel: "L1", DataLabel: label}); err != ErrNoEligibleCandidate {
		t.Fatalf("subscription-only route should be protected by default, got %v", err)
	}
	d, err = svc.Route(ctx, RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.subscription-only", ProtocolLevel: "L1", DataLabel: label, AllowSubscriptionUsage: true})
	if err != nil || d.Selected == nil || d.Selected.Candidate.ID != "d-sub" {
		t.Fatalf("explicit subscription opt-in failed: %v %#v", err, d.Selected)
	}
}


func TestSQLiteProviderBackedOriginNodeCannotMasqueradeAsFreeLocal(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/provider-origin.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for i,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('ws','Test','active',1,1,1)`,
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,
   protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
   VALUES('origin','Origin',1,'origin-fp','local','LOCAL_TRUSTED','{}','{}',1,1,1,1)`,
  `INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,
   connection_json,revision,created_at,updated_at) VALUES(
   'provider','ws','openai-compatible','Provider','none','connected',
   '{"data_policy":{"max_confidentiality":"public","allowed_residency":["any"],
   "destination_kind":"cloud"},"scheduling":{"cost_class":"paid",
   "hard_zero_incremental_cost":false}}',1,1,1)`,
  `INSERT INTO models(id,provider_name,model_ref,modalities_json,static_metadata_json,
   trust_state,created_at,updated_at)
   VALUES('model','openai-compatible','some/model','["text"]','{}','user_trusted',1,1)`,
  `INSERT INTO model_deployments(id,model_id,node_id,provider_connection_id,runtime_name,
   runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at)
   VALUES('deployment','model','origin','provider','openai-compatible',
   '{"scheduling":{"cost_class":"local","hard_zero_incremental_cost":true},
   "compute":{"mode":"cpu"}}','ready','fp',1,1,1)`,
  `INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,
   context_min,context_max_supported,risk_class,qualification,mediation_json,
   evidence_json,profile_fingerprint,revision,created_at,updated_at)
   VALUES('compat','deployment','agent.reason','L1',0,4000,'low','verified',
   '{}','{}','cpf',1,1,1)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatalf("seed %d: %v",i,err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 label:=policy.DataLabel{WorkspaceID:"ws",Confidentiality:policy.ConfidentialityPublic,
  Residency:policy.ResidencyAny,Trust:policy.TrustUserInstruction}
 base:=RouteRequest{WorkspaceID:"ws",CapabilityID:"agent.reason",ProtocolLevel:"L1",DataLabel:label}
 checkDenied:=func(name string,q RouteRequest){
  t.Helper()
  dec,err:=svc.Route(ctx,q)
  if err!=ErrNoEligibleCandidate||dec.Selected!=nil{
   t.Fatalf("%s bypassed provider classification: selected=%+v err=%v",name,dec.Selected,err)
  }
 }
 localOnly:=base;localOnly.LocalOnly=true;localOnly.AllowPotentialMonetarySpend=true
 checkDenied("local-only",localOnly)
 checkDenied("no-spend default",base)
 noCost:=base;noCost.AllowPotentialMonetarySpend=true;noCost.RequireZeroIncrementalCost=true
 checkDenied("zero-cost",noCost)
 cpuOnly:=base;cpuOnly.AllowPotentialMonetarySpend=true;cpuOnly.ComputePreference="cpu_only"
 checkDenied("CPU-only mode cannot be spoofed",cpuOnly)
 paid:=base;paid.AllowPotentialMonetarySpend=true
 dec,err:=svc.Route(ctx,paid)
 if err!=nil||dec.Selected==nil{t.Fatalf("explicit paid provider route unavailable: %v",err)}
 c:=dec.Selected.Candidate
 if c.ID!="deployment"||c.Local||c.CostClass!=CostPaid||c.HardZeroIncrementalCost||c.ComputeMode!="cloud"{
  t.Fatalf("unverified provider presented as local or free: %+v",c)
 }
 // User/provider JSON must not impersonate an independently verified
 // host-local or paired-Node destination and bypass information flow.
 for _,tc:=range []struct{label string;want policy.DestinationKind}{
  {"origin_node",policy.DestinationCloud},
  {"trusted_node",policy.DestinationCloud},
  {"untrusted",policy.DestinationUntrusted},
 }{
  if _,err:=db.SQL().ExecContext(ctx,`UPDATE provider_connections SET
   connection_json=json_set(connection_json,'$.data_policy.destination_kind',?)
   WHERE id='provider'`,tc.label);err!=nil{t.Fatal(err)}
  candidates,e:=NewCatalog(db.SQL()).Candidates(ctx,"ws","agent.reason","")
  if e!=nil{t.Fatal(e)}
  found:=false
  for _,item:=range candidates{
   if item.ID!="deployment"{continue}
   found=true
   if item.Destination.Kind!=tc.want||item.Destination.NodeID!=""||
    item.Local||item.CostClass!=CostPaid||
    item.HardZeroIncrementalCost||item.ComputeMode!="cloud"{
    t.Fatalf("provider JSON %q spoofed a verified local destination: %+v",tc.label,item)
   }
  }
  if !found{t.Fatalf("provider candidate %q missing",tc.label)}
 }
 // Simulate a damaged legacy import with a dangling provider association.
 // SQLite normally enforces the FK. The scheduler must still deny rather
 // than silently reclassifying it as a free trusted paired Node.
 conn,err:=db.SQL().Conn(ctx)
 if err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,"PRAGMA foreign_keys=OFF");err!=nil{
  conn.Close();t.Fatal(err)
 }
 if _,err=conn.ExecContext(ctx,`UPDATE model_deployments
  SET provider_connection_id='missing-provider' WHERE id='deployment'`);err!=nil{
  conn.Close();t.Fatal(err)
 }
 if _,err=conn.ExecContext(ctx,"PRAGMA foreign_keys=ON");err!=nil{
  conn.Close();t.Fatal(err)
 }
 if err=conn.Close();err!=nil{t.Fatal(err)}
 cands,err:=NewCatalog(db.SQL()).Candidates(ctx,"ws","agent.reason","")
 if err!=nil{t.Fatal(err)}
 found:=false
 for _,item:=range cands{
  if item.ID!="deployment"{continue}
  found=true
  if item.Schedulable||item.Local||item.HardZeroIncrementalCost||
   item.CostClass!=CostUnknown||item.ComputeMode!="cloud"||
   item.TrustedNodeVerified{
   t.Fatalf("dangling provider association borrowed local trusted compute: %+v",item)
  }
 }
 if !found{t.Fatal("dangling provider model was hidden instead of explicitly denied")}
 if dec,err:=svc.Route(ctx,paid);err!=ErrNoEligibleCandidate||dec.Selected!=nil{
  t.Fatalf("damaged provider association unexpectedly routed: %+v %v",dec.Selected,err)
 }
}


func TestRevokedHostNodeNeverRoutesAsTrustedFreeLocalModel(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/revoked-local-node.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 seeds:=[]string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,1,1)`,
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
  VALUES('origin','Origin',1,'origin-key','local','LOCAL_TRUSTED','{}','{}',1,1,1,1)`,
  `INSERT INTO models(id,provider_name,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at)
  VALUES('host-model','local','llama.cpp','["text"]','{}','user_trusted',1,1)`,
  `INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at)
  VALUES('local-model','host-model','origin','llama.cpp','{"scheduling":{"cost_class":"local","hard_zero_incremental_cost":true},"compute":{"mode":"gpu"}}','ready','fp',1,1,1)`,
  `INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at)
  VALUES('cap','local-model','agent.reason','L1',0,8192,'low','verified','{}','{}','cap-fp',1,1,1)`,
 }
 for _,q:=range seeds{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 cat:=NewCatalog(db.SQL())
 candidates,err:=cat.Candidates(ctx,"ws","agent.reason","")
 if err!=nil{t.Fatal(err)}
 if len(candidates)!=1||!candidates[0].Local||!candidates[0].Schedulable||
  candidates[0].CostClass!=CostLocal||!candidates[0].HardZeroIncrementalCost{
  t.Fatalf("genuinely trusted local model was rejected: %+v",candidates)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE harness_nodes SET trust_state='revoked',revision=revision+1 WHERE id='origin'`);err!=nil{t.Fatal(err)}
 candidates,err=cat.Candidates(ctx,"ws","agent.reason","")
 if err!=nil{t.Fatal(err)}
 if len(candidates)!=1{t.Fatalf("missing revoked candidate: %+v",candidates)}
 denied:=candidates[0]
 if denied.Local||denied.Schedulable||denied.CostClass!=CostUnknown||
  denied.HardZeroIncrementalCost||denied.TrustedNodeVerified||
  denied.Destination.Kind!=policy.DestinationUntrusted||denied.ComputeMode!="unavailable"{
  t.Fatalf("revoked host borrowed local compute rights from READY/model metadata: %+v",denied)
 }
 req:=RouteRequest{WorkspaceID:"ws",CapabilityID:"agent.reason",ProtocolLevel:"L1",
  DataLabel:policy.DataLabel{WorkspaceID:"ws",Confidentiality:policy.ConfidentialityPublic,
   Residency:policy.ResidencyAny,Trust:policy.TrustUserInstruction},
  LocalOnly:true,RequireZeroIncrementalCost:true}
 if decision,err:=Route(req,candidates);err!=ErrNoEligibleCandidate||decision.Selected!=nil{
  t.Fatalf("revoked origin Node still schedulable: %+v %v",decision.Selected,err)
 }
}

func TestUnpairedOrDisabledRemoteNodeCannotAdvertiseVerifiedZeroCost(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/unpaired-node-cost.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,1,1)`,
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
   VALUES('remote','Unpaired host',0,'remote-key','paired','LOCAL_TRUSTED','{}','{}',1,1,1,1)`,
  `INSERT INTO models(id,provider_name,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at)
   VALUES('remote-model','local','model','["text"]','{}','trusted',1,1)`,
  `INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at)
   VALUES('off-host','remote-model','remote','llama.cpp','{"scheduling":{"cost_class":"local","hard_zero_incremental_cost":true}}','ready','fp',1,1,1)`,
  `INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at)
   VALUES('cap','off-host','agent.reason','L1',0,2048,'low','verified','{}','{}','cap',1,1,1)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 cat:=NewCatalog(db.SQL())
 candidates,err:=cat.Candidates(ctx,"ws","agent.reason","")
 if err!=nil||len(candidates)!=1{t.Fatalf("catalogue: %+v %v",candidates,err)}
 c:=candidates[0]
 if c.Schedulable||c.TrustedNodeVerified||c.Local||
  c.CostClass!=CostUnknown||c.HardZeroIncrementalCost||
  c.Destination.Kind!=policy.DestinationUntrusted{
  t.Fatalf("unpaired Node presented as trusted zero-cost: %+v",c)
 }
 // A stale deployment declaring local/$0 cannot override the disabled
 // remote compute policy or an absent signed pairing/manifest.
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO remote_node_compute_policies(
  node_id,enabled,idle_only,created_at,updated_at)
  VALUES('remote',0,0,1,1)`);err!=nil{t.Fatal(err)}
 candidates,err=cat.Candidates(ctx,"ws","agent.reason","")
 if err!=nil||len(candidates)!=1{t.Fatalf("disabled catalogue: %+v %v",candidates,err)}
 c=candidates[0]
 if c.Schedulable||c.TrustedNodeVerified||c.CostClass!=CostUnknown||
  c.HardZeroIncrementalCost||c.Destination.Kind!=policy.DestinationUntrusted{
  t.Fatalf("disabled remote Node still claimed free trusted compute: %+v",c)
 }
 req:=RouteRequest{WorkspaceID:"ws",CapabilityID:"agent.reason",
  ProtocolLevel:"L1",DataLabel:policy.DataLabel{WorkspaceID:"ws",
   Confidentiality:policy.ConfidentialityPublic,Residency:policy.ResidencyAny,
   Trust:policy.TrustUserInstruction},LocalOnly:true,AllowTrustedNodes:true,
  AllowedTrustedNodeIDs:[]string{"remote"},AllowPotentialMonetarySpend:true}
 if dec,e:=Route(req,candidates);e!=ErrNoEligibleCandidate||dec.Selected!=nil{
  t.Fatalf("unpaired remote Node bypassed allowlist: %+v %v",dec.Selected,e)
 }
}
