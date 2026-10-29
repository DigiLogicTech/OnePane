package scheduler

import (
	"context"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestCatalogResidentFollowsLiveRuntimeNotStaticFlag(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestore.Open(t.TempDir() + "/sched.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node','Local',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`,
		`INSERT INTO models(id,provider_name,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('m','local','demo','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('cold','m','node','llama','{"compute":{"mode":"cpu","resident":true}}','ready','fp-cold',1,1,1)`,
		`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('hot','m','node','llama','{"compute":{"mode":"cpu","resident":false}}','ready','fp-hot',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('cp-cold','cold','agent.reason','L2',0,64000,'low','supported','{}','{}','cpf-cold',1,1,1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('cp-hot','hot','agent.reason','L2',0,64000,'low','supported','{}','{}','cpf-hot',1,1,1)`,
		`INSERT INTO local_hardware_profiles(id,node_id,fingerprint,os_name,architecture,cpu_json,memory_json,accelerators_json,runtimes_json,storage_json,detected_at) VALUES('hw','node','hwfp','linux','amd64','{}','{}','[]','[]','{}',1)`,
		`INSERT INTO local_model_install_plans(id,node_id,hardware_profile_id,role_name,use_case,model_ref,source_ref,runtime_name,quantization,context_tokens,fit_level,run_mode,memory_required_bytes,disk_required_bytes,download_scratch_bytes,plan_json,status,revision,created_at,updated_at) VALUES('plan','node','hw','assistant','chat','demo','src','llama','Q4',1024,'good','cpu',1024,1024,0,'{}','ready',1,1,1)`,
		`INSERT INTO managed_local_runtimes(id,node_id,runtime_name,runtime_version,install_root,executable_path,source_url,source_sha256,status,managed_by_harness,revision,updated_at) VALUES('rt','node','llama','1','/r','/r/llama','http://example.invalid/rt','aa','ready',1,1,1)`,
		`INSERT INTO managed_local_models(id,node_id,plan_id,deployment_id,runtime_id,model_ref,source_ref,local_path,status,revision,updated_at) VALUES('mm','node','plan','hot','rt','demo','src','/m','ready',1,1)`,
		`INSERT INTO local_runtime_instances(id,node_id,deployment_id,runtime_id,managed_model_id,bind_address,port,pid,status,process_fingerprint,launch_json,health_json,revision,created_at,updated_at) VALUES('lri','node','hot','rt','mm','127.0.0.1',8088,42,'healthy','fp','{}','{}',1,1,1)`,
		`INSERT INTO model_spec_sheets(deployment_id,model_id,hardware_profile_id,placement_json,catalog_claims_json,llmfit_json,qualification_json,restrictions_json,admission_status,revision,created_at,updated_at) VALUES('hot','m','hw','{}','{}','{}','{}','{}','accepted',1,1,1)`,
	}
	for i, q := range stmts {
		if _, err = db.SQL().Exec(q); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	cat := NewCatalog(db.SQL())
	cands, err := cat.Candidates(ctx, "ws", "agent.reason", "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Candidate{}
	for _, c := range cands {
		byID[c.ID] = c
	}
	if byID["cold"].Resident {
		t.Fatal("static compute.resident marked an unloaded model loaded")
	}
	if !byID["hot"].Resident {
		t.Fatal("live healthy runtime was not treated as loaded")
	}
	label := policy.DataLabel{WorkspaceID: "ws", Confidentiality: policy.ConfidentialityPublic, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}
	routed, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label}, []Candidate{byID["cold"], byID["hot"]})
	if err != nil || routed.Selected == nil || routed.Selected.Candidate.ID != "hot" {
		t.Fatalf("routing did not prefer the loaded model: %+v %v", routed.Selected, err)
	}
}
