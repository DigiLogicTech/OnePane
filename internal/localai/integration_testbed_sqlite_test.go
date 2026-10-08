//go:build integration

package localai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestSQLiteManualTestbedRequiredForProductionAdmission(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/harness.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := int64(1_700_000_000_000)
	rec := Recommendation{Model: ModelSpec{ModelRef: "example/model", DisplayName: "Example", Provider: "test", ParamsB: 7, ContextLength: 32768, UseCases: []UseCase{UseCoding}, Runtime: "llamacpp", Quantizations: []string{"Q4_K_M"}}, Quantization: "Q4_K_M", ContextTokens: 16384, FitLevel: FitGood, RunMode: RunGPU, MemoryRequired: 8 << 30, MemoryAvailable: 16 << 30, DiskRequired: 5 << 30, Placement: PlacementPlan{Mode: PlacementSingleDevice, Backend: "cuda", Devices: []PlacementDevice{{Kind: "accelerator", Backend: "cuda", RuntimeDevice: "CUDA0", CapacityBytes: 16 << 30, AllocatedBytes: 8 << 30}}}}
	planJSON, _ := json.Marshal(rec)
	seed := []string{
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node','Node',1,'fp','local','{}','{}',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,1,1)`,
		`INSERT INTO models(id,provider_name,model_ref,architecture,quantization,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model','local','example/model','example','Q4_K_M','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_version,runtime_config_json,status,residency_state,context_max_reported,context_max_verified,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('dep','model','node','llamacpp','1','{"runtime_backend":"cuda"}','ready','stopped',32768,16384,'dfp',1,1,1)`,
		`INSERT INTO local_hardware_profiles(id,node_id,fingerprint,os_name,architecture,cpu_json,memory_json,accelerators_json,runtimes_json,storage_json,detected_at) VALUES('hw','node','hfp','linux','amd64','{"name":"cpu","logical_cores":8,"architecture":"amd64"}','{"total_bytes":68719476736,"available_bytes":51539607552,"unified":false}','[{"vendor":"nvidia","name":"gpu","vram_bytes":17179869184,"backend":"cuda","backends":["cuda"],"device_index":0}]','[]','{"path":"/models","capacity_bytes":1099511627776,"available_bytes":549755813888}',1)`,
		`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_verified,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,verified_at,revision,created_at,updated_at) VALUES('cp','dep','agent.reason','L2',0,16384,32768,'low','verified','{}','{}','cpf',1,1,1,1)`,
	}
	for _, q := range seed {
		if _, err := db.SQL().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO local_model_install_plans(id,node_id,hardware_profile_id,role_name,use_case,model_ref,source_ref,runtime_name,quantization,context_tokens,fit_level,run_mode,memory_required_bytes,disk_required_bytes,download_scratch_bytes,plan_json,status,approved_by,revision,created_at,updated_at) VALUES('plan','node','hw','worker','coding','example/model','hf://example/model','llamacpp','Q4_K_M',16384,'good','gpu',8589934592,5368709120,5368709120,?,'ready','admin',1,1,1)`, string(planJSON)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO managed_local_models(id,node_id,plan_id,model_id,deployment_id,model_ref,source_ref,local_path,status,revision,installed_at,updated_at) VALUES('mm','node','plan','model','dep','example/model','hf://example/model','/models/example.gguf','ready',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO model_spec_sheets(deployment_id,model_id,hardware_profile_id,placement_json,catalog_claims_json,llmfit_json,qualification_json,restrictions_json,admission_status,revision,created_at,updated_at) VALUES('dep','model','hw','{"mode":"single_device","backend":"cuda","devices":[]}','{}','{}','{}','{}','pending',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}

	inf := inference.NewService(db.SQL(), db, clock.Real{})
	svc := NewService(db.SQL(), db, clock.Real{}, inf, t.TempDir(), nil)
	// This integration fixture intentionally tests SQLite testbed lifecycle and
	// admission, not runtime binary availability. Runtime preflight is covered
	// separately by supervisor tests; do not launch a non-existent CUDA binary.
	svc.supervisor = nil
	if _, err := svc.AdmitModel(ctx, "dep", AdmissionCommand{Status: AdmissionAccepted, ActorPrincipalID: "admin"}); err == nil || !strings.Contains(err.Error(), "completed manual testbed") {
		t.Fatalf("expected manual-testbed gate, got %v", err)
	}
	session, err := svc.StartTestbed(ctx, "dep", strPtr("admin"), "manual smoke test")
	if err != nil {
		t.Fatal(err)
	}
	// Seed one recorded mock inference response into the SQLite fixture.
	// Production completion still rejects sessions with zero successful turns;
	// these tests verify database semantics without requiring a GPU runtime.
	if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO model_testbed_turns
	 (id,session_id,sequence_no,request_json,response_json,usage_json,metrics_json,synthetic_tool_probe,created_at)
	 VALUES('test-turn',?,1,'{"prompt":"Reply with exactly ONEPANE_OK"}',
	 '{"choices":[{"message":{"content":"ONEPANE_OK"}}]}','{}','{}',0,?)`,
	 session.ID,now);err!=nil{t.Fatal(err)}
	if err := svc.CompleteTestbed(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	// A failed runtime launch must be abortable without violating the DB's
	// active/completed/cancelled testbed lifecycle CHECK constraint.
	failed, err := svc.StartTestbed(ctx, "dep", strPtr("admin"), "failing CUDA launch")
	if err != nil { t.Fatal(err) }
	if err := svc.AbortTestbed(ctx, failed.ID, "CUDA executable was missing"); err != nil { t.Fatal(err) }
	aborted, err := svc.TestbedSession(ctx, failed.ID)
	if err != nil { t.Fatal(err) }
	if aborted.Status != "cancelled" { t.Fatalf("failed testbed should be cancelled, got %q",aborted.Status) }
	sheetAfterAbort,err:=svc.SpecSheet(ctx,"dep")
	if err!=nil {t.Fatal(err)}
	if !strings.Contains(string(sheetAfterAbort.Qualification),"CUDA executable was missing"){
	 t.Fatalf("missing failed Agent Check diagnostic evidence: %s",sheetAfterAbort.Qualification)
	}
	allowTools := false
	sheet, err := svc.AdmitModel(ctx, "dep", AdmissionCommand{Status: AdmissionRestricted, ActorPrincipalID: "admin", Restrictions: ModelRestrictions{MaxContextTokens: 8192, DenyCapabilities: []string{"agent.tool"}, AllowToolUse: &allowTools, Notes: []string{"manual trial found weak tool calling"}}})
	if err != nil {
		t.Fatal(err)
	}
	if sheet.AdmissionStatus != AdmissionRestricted || sheet.Model.ModelRef != "example/model" || sheet.Quantization != "Q4_K_M" || sheet.RuntimeBackend != "cuda" || sheet.VerifiedContext == nil || *sheet.VerifiedContext != 16384 {
		t.Fatalf("unexpected sheet: %+v", sheet)
	}

	// A completed trial is bound to the exact hardware/placement tuple. Simulate
	// a later qualification changing placement: the old trial must not admit it.
	newPlacement := `{"mode":"cpu_only","backend":"cpu","devices":[{"kind":"cpu","name":"cpu","backend":"cpu","capacity_bytes":68719476736,"allocated_bytes":8589934592}]}`
	if _, err := db.SQL().ExecContext(ctx, `UPDATE model_spec_sheets SET placement_json=?,admission_status='pending',restrictions_json='{}',admitted_by=NULL,admitted_at=NULL,revision=revision+1 WHERE deployment_id='dep'`, newPlacement); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdmitModel(ctx, "dep", AdmissionCommand{Status: AdmissionAccepted, ActorPrincipalID: "admin"}); err == nil || !strings.Contains(err.Error(), "current hardware profile and placement") {
		t.Fatalf("expected exact-tuple testbed gate, got %v", err)
	}
}

func strPtr(v string) *string { return &v }
