package localai

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Actual migrated SQLite only. Every Request action is rejected before its
// goroutine boundary; progress/failure/in-use calls never execute a component.
// No package/model/process/download/user-data path is touched. Removal tests
// exercise only disposable fixture files beneath the test's private temp root.
func TestManagedComponentActionRejectedStateLeavesNoQueuedJob(t *testing.T) {
	for _, rejected := range []string{"job_insert", "state_update"} {
		t.Run(rejected, func(t *testing.T) {
			s, ctx := componentTestService(t)
			before, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			query := `CREATE TRIGGER reject_component_action BEFORE INSERT ON managed_component_jobs BEGIN SELECT RAISE(ABORT,'action rejected'); END`
			if rejected == "state_update" {
				query = `CREATE TRIGGER reject_component_action BEFORE UPDATE ON managed_component_states BEGIN SELECT RAISE(ABORT,'action rejected'); END`
			}
			if _, err = s.db.ExecContext(ctx, query); err != nil {
				t.Fatal(err)
			}
			// The retained implementation returns at the injected write failure before
			// go runComponentJob. The fixture never admits a successful action request.
			_, err = s.RequestComponentAction(ctx, "colibri", "install", nil)
			if err == nil || !strings.Contains(err.Error(), "action rejected") {
				t.Fatalf("injected nondispatch denial not propagated: %v", err)
			}
			var jobs int
			if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_component_jobs`).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			after, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if jobs != 0 || !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected request persisted a ghost lifecycle job: jobs=%d state_changed=%t", jobs, !reflect.DeepEqual(before, after))
			}
		})
	}
}

func seedComponentProgress(t *testing.T) (*Service, context.Context) {
	t.Helper()
	s, ctx := componentTestService(t)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO managed_component_jobs(id,component_id,action,status,stage,attempt_count,revision,created_at,updated_at) VALUES('atomic-job','colibri','install','queued','queued',0,1,1,1); UPDATE managed_component_states SET active_job_id='atomic-job',observed_state='queued' WHERE component_id='colibri'`); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}

func TestManagedComponentProgressRejectedStateRollsBackJob(t *testing.T) {
	for _, rejected := range []string{"jobs", "states", "none"} {
		t.Run(rejected, func(t *testing.T) {
			s, ctx := seedComponentProgress(t)
			beforeJob, err := s.ComponentJob(ctx, "atomic-job")
			if err != nil {
				t.Fatal(err)
			}
			beforeState, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if rejected != "none" {
				if _, err = s.db.ExecContext(ctx, `CREATE TRIGGER reject_component_progress BEFORE UPDATE ON managed_component_`+rejected+` BEGIN SELECT RAISE(ABORT,'progress rejected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err = s.updateComponentProgress(ctx, "atomic-job", "running", "downloading", "downloading", nil, false)
			afterJob, readErr := s.ComponentJob(ctx, "atomic-job")
			if readErr != nil {
				t.Fatal(readErr)
			}
			afterState, readErr := s.ManagedComponents(ctx)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if rejected != "none" {
				if err == nil || !strings.Contains(err.Error(), "progress rejected") || !reflect.DeepEqual(beforeJob, afterJob) || !reflect.DeepEqual(beforeState, afterState) {
					t.Fatalf("rejected progress was not atomic: err=%v job_changed=%t state_changed=%t", err, !reflect.DeepEqual(beforeJob, afterJob), !reflect.DeepEqual(beforeState, afterState))
				}
				return
			}
			if err != nil || afterJob.Status != "running" || afterJob.AttemptCount != 1 || afterState["colibri"].State != "downloading" {
				t.Fatalf("healthy progress failed: %v job=%+v state=%+v", err, afterJob, afterState["colibri"])
			}
		})
	}
}

func TestManagedComponentFailureRejectedStateRollsBackJob(t *testing.T) {
	s, ctx := seedComponentProgress(t)
	beforeJob, err := s.ComponentJob(ctx, "atomic-job")
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := s.ManagedComponents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `CREATE TRIGGER reject_component_failure BEFORE UPDATE ON managed_component_states BEGIN SELECT RAISE(ABORT,'failure state rejected'); END`); err != nil {
		t.Fatal(err)
	}
	err = s.failComponentJob(ctx, "atomic-job", errors.New("fixture lifecycle failure"))
	if err == nil || !strings.Contains(err.Error(), "failure state rejected") {
		t.Fatalf("failure persistence error was hidden: %v", err)
	}
	afterJob, err := s.ComponentJob(ctx, "atomic-job")
	if err != nil {
		t.Fatal(err)
	}
	afterState, err := s.ManagedComponents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeJob, afterJob) || !reflect.DeepEqual(beforeState, afterState) {
		t.Fatalf("rejected failure silently persisted mismatched terminal job: job=%+v state=%+v", afterJob, afterState["colibri"])
	}
}

func TestManagedComponentColibriRealSchemaUsageStatusesAndPID(t *testing.T) {
	s, ctx := componentTestService(t)
	for _, query := range []string{
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node','Fixture',1,'fp','local','{}','{}',1,1,1)`,
		`INSERT INTO models(id,provider_name,model_ref,architecture,quantization,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model','local','fixture/model','fixture','Q4_K_M','["text"]','{}','user_trusted',1,1)`,
		`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_version,runtime_config_json,status,residency_state,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('deployment','model','node','colibri','1.12.1','{}','ready','stopped','dfp',1,1,1)`,
		`INSERT INTO local_hardware_profiles(id,node_id,fingerprint,os_name,architecture,cpu_json,memory_json,accelerators_json,runtimes_json,storage_json,detected_at) VALUES('hardware','node','hfp','linux','amd64','{}','{}','[]','[]','{}',1)`,
		`INSERT INTO local_model_install_plans(id,node_id,hardware_profile_id,role_name,use_case,model_ref,source_ref,runtime_name,quantization,context_tokens,fit_level,run_mode,memory_required_bytes,disk_required_bytes,download_scratch_bytes,plan_json,status,revision,created_at,updated_at) VALUES('plan','node','hardware','worker','coding','fixture/model','fixture://model','colibri','Q4_K_M',1024,'good','cpu',1,1,0,'{}','ready',1,1,1)`,
		`INSERT INTO managed_local_runtimes(id,node_id,runtime_name,runtime_version,install_root,executable_path,source_url,source_sha256,status,managed_by_harness,revision,updated_at) VALUES('runtime','node','colibri','1.12.1','fixture','fixture','fixture://runtime','fixture','ready',1,1,1)`,
		`INSERT INTO managed_local_models(id,node_id,plan_id,model_id,deployment_id,runtime_id,model_ref,source_ref,local_path,status,revision,updated_at) VALUES('managed-model','node','plan','model','deployment','runtime','fixture/model','fixture://model','fixture','ready',1,1)`,
		`INSERT INTO local_runtime_instances(id,node_id,deployment_id,runtime_id,managed_model_id,bind_address,port,status,process_fingerprint,launch_json,health_json,revision,created_at,updated_at) VALUES('instance','node','deployment','runtime','managed-model','127.0.0.1',18080,'stopped','fixture','{}','{}',1,1,1)`,
	} {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"starting", "healthy", "busy", "draining", "orphaned", "failed", "stopped"} {
		for _, hasPID := range []bool{false, true} {
			t.Run(status+map[bool]string{false: "_without_pid", true: "_with_recorded_pid"}[hasPID], func(t *testing.T) {
				var pid any
				if hasPID {
					pid = 424242
				} // Recorded observation only; never signalled or inspected.
				if _, err := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status=?,pid=? WHERE id='instance'`, status, pid); err != nil {
					t.Fatal(err)
				}
				expected := hasPID || status != "stopped"
				if got := s.colibriInUse(ctx); got != expected {
					t.Fatalf("unsafe usage classification for schema-real status=%s pid=%v: got=%t want=%t", status, pid, got, expected)
				}
			})
		}
	}
	var failures int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&failures); err != nil || failures != 0 {
		t.Fatalf("invalid actual-schema fixture: %v count=%d", err, failures)
	}
}

func TestManagedComponentRemoveJournalFailurePreventsFilesystemEffect(t *testing.T) {
	s, ctx := seedComponentProgress(t)
	if _, err := s.db.ExecContext(ctx, `UPDATE managed_component_jobs SET action='remove' WHERE id='atomic-job'; CREATE TRIGGER reject_remove_progress BEFORE UPDATE ON managed_component_states BEGIN SELECT RAISE(ABORT,'remove progress rejected'); END`); err != nil {
		t.Fatal(err)
	}
	root := s.componentRuntimeRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "preserve-fixture.txt")
	if err := os.WriteFile(marker, []byte("preserve fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	err := s.runComponentJob(ctx, "atomic-job")
	if err == nil || !strings.Contains(err.Error(), "remove progress rejected") {
		t.Fatalf("removal journal failure hidden: %v", err)
	}
	contents, readErr := os.ReadFile(marker)
	if readErr != nil || string(contents) != "preserve fixture" {
		t.Fatalf("filesystem effect preceded recorded removal: %v", readErr)
	}
	job, err := s.ComponentJob(ctx, "atomic-job")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "running" || job.Stage != "queued" {
		t.Fatalf("rejected progress/failure produced false terminal state: %+v", job)
	}
}

func TestManagedComponentAlreadyRunningClaimRefusesDispatch(t *testing.T) {
	s, ctx := seedComponentProgress(t)
	if _, err := s.db.ExecContext(ctx, `UPDATE managed_component_jobs SET status='running' WHERE id='atomic-job'; CREATE TRIGGER reject_claim_effect BEFORE UPDATE ON managed_component_states BEGIN SELECT RAISE(ABORT,'unexpected post-claim effect'); END`); err != nil {
		t.Fatal(err)
	}
	before, err := s.ComponentJob(ctx, "atomic-job")
	if err != nil {
		t.Fatal(err)
	}
	err = s.runComponentJob(ctx, "atomic-job")
	if err == nil || !strings.Contains(err.Error(), "not queued under its current owner") {
		t.Fatalf("duplicate claim admitted: %v", err)
	}
	after, readErr := s.ComponentJob(ctx, "atomic-job")
	if readErr != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("duplicate claim mutated journal: %v %+v", readErr, after)
	}
}

func TestManagedComponentQueuedOmniRemovePreservesDataAndCompletesOwner(t *testing.T) {
	for _, unknownOwner := range []bool{false, true} {
		t.Run(map[bool]string{false: "known_disabled", true: "recorded_unknown_pid"}[unknownOwner], func(t *testing.T) {
			s, ctx := componentTestService(t)
			if _, err := s.ManagedComponents(ctx); err != nil {
				t.Fatal(err)
			}
			metadata := "{}"
			desired := "disabled"
			if unknownOwner {
				metadata = `{"pid":424242,"private_marker":"keep"}`
				desired = "enabled"
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO managed_component_jobs(id,component_id,action,status,stage,revision,created_at,updated_at) VALUES('omni-remove','omniroute','remove','queued','queued',1,1,1); UPDATE managed_component_states SET installed_version='3.8.51',desired_state=?,observed_state='queued',active_job_id='omni-remove',metadata_json=? WHERE component_id='omniroute'`, desired, metadata); err != nil {
				t.Fatal(err)
			}
			runtimeRoot, dataRoot := s.omniRouteRuntimeRoot(), s.omniRouteDataRoot()
			for _, root := range []string{runtimeRoot, dataRoot} {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte("private fixture data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			runErr := s.runComponentJob(ctx, "omni-remove")
			job, err := s.ComponentJob(ctx, "omni-remove")
			if err != nil {
				t.Fatal(err)
			}
			all, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			state := all["omniroute"]
			if unknownOwner {
				if !errors.Is(runErr, errOmniRouteOwnershipDeferred) || job.Status != "failed" || state.State != "degraded" || state.DesiredState != "enabled" || state.ActiveJobID != nil || string(state.Metadata) != metadata {
					t.Fatalf("unknown ownership hidden/rewritten: err=%v job=%+v state=%+v", runErr, job, state)
				}
				if bytes, err := os.ReadFile(filepath.Join(runtimeRoot, "fixture.txt")); err != nil || string(bytes) != "private fixture data" {
					t.Fatalf("unknown-owner runtime changed: %v", err)
				}
			} else {
				if runErr != nil || job.Status != "succeeded" || job.AttemptCount != 1 || job.CompletedAt == nil || state.State != "removed" || state.Installed || state.ActiveJobID != nil {
					t.Fatalf("known-disabled removal lost completion ownership: err=%v job=%+v state=%+v", runErr, job, state)
				}
				if _, err := os.Stat(runtimeRoot); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("fixture runtime not removed: %v", err)
				}
				var metadata map[string]any
				if err := json.Unmarshal(state.Metadata, &metadata); err != nil || metadata["preserved_data_root"] != dataRoot {
					t.Fatalf("preserved root provenance missing: %v %s", err, state.Metadata)
				}
			}
			if bytes, err := os.ReadFile(filepath.Join(dataRoot, "fixture.txt")); err != nil || string(bytes) != "private fixture data" {
				t.Fatalf("private fixture data changed: %v", err)
			}
		})
	}
}

func TestManagedComponentColibriUnreadableUsageFailsClosed(t *testing.T) {
	for _, condition := range []string{"cancelled", "closed", "idle"} {
		t.Run(condition, func(t *testing.T) {
			s, ctx := componentTestService(t)
			switch condition {
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "closed":
				if err := s.db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			blocked := s.colibriInUse(ctx)
			if condition == "idle" {
				if blocked {
					t.Fatal("readable idle inventory refused")
				}
				return
			}
			if !blocked {
				t.Fatal("unreadable runtime inventory reported safe to replace/remove Colibri")
			}
		})
	}
}

func TestManagedComponentProgressCannotClearAnotherActiveOwner(t *testing.T) {
	s, ctx := seedComponentProgress(t)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO managed_component_jobs(id,component_id,action,status,stage,revision,created_at,updated_at) VALUES('new-owner','colibri','install','queued','queued',1,2,2); UPDATE managed_component_states SET active_job_id='new-owner' WHERE component_id='colibri'`); err != nil {
		t.Fatal(err)
	}
	beforeJob, err := s.ComponentJob(ctx, "atomic-job")
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := s.ManagedComponents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = s.updateComponentProgress(ctx, "atomic-job", "succeeded", "complete", "installed_disabled", nil, true)
	afterJob, readErr := s.ComponentJob(ctx, "atomic-job")
	if readErr != nil {
		t.Fatal(readErr)
	}
	afterState, readErr := s.ManagedComponents(ctx)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err == nil || !reflect.DeepEqual(beforeJob, afterJob) || !reflect.DeepEqual(beforeState, afterState) {
		t.Fatalf("stale terminal progress replaced a newer active owner: err=%v job_changed=%t state=%+v", err, !reflect.DeepEqual(beforeJob, afterJob), afterState["colibri"])
	}
}
