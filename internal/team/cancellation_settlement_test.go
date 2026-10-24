package team

import (
	"context"
	"encoding/json"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type cancellationLoopback string

func (r cancellationLoopback) ResolveLocalEndpoint(context.Context, string) (string, error) {
	return string(r), nil
}

// Real SQLite + HTTP transport/artifact/coordinator evidence. The loopback
// server is a deterministic protocol fixture, never physical model acceptance.
func cancellationFixture(t *testing.T, beforeAccept ...func(*Service, *sqlitestore.DB, Session)) (*Service, *sqlitestore.DB, Session, string, string) {
	return cancellationFixtureWithBeforeCancel(t, nil, beforeAccept...)
}

func cancellationFixtureWithBeforeCancel(t *testing.T, beforeCancel func(*Service, *sqlitestore.DB, Session), beforeAccept ...func(*Service, *sqlitestore.DB, Session)) (*Service, *sqlitestore.DB, Session, string, string) {
	t.Helper()
	ctx := context.Background()
	s, db, ss, specs := assignmentFixture(t)
	for _, q := range []string{
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('system:team-worker','system','Worker','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','system:team-worker','active',1,1)`,
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('cancel-node','Local',1,'cancel-fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`,
	} {
		if _, err := db.SQL().Exec(q); err != nil {
			t.Fatalf("fixture step 35: %v", err)
		}
	}
	turns, err := s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", MemberIDs: []string{specs[0].MemberID}})
	if err != nil {
		t.Fatalf("fixture step 40: %v", err)
	}
	turn := turns[0]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"reasoning response"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	t.Cleanup(server.Close)
	store, err := artifact.NewLocalStore(t.TempDir() + "/artifacts")
	if err != nil {
		t.Fatalf("fixture step 50: %v", err)
	}
	artifacts := artifact.NewService(db.SQL(), db, store, clock.Real{})
	infer := inference.NewService(db.SQL(), db, clock.Real{})
	transports := inference.NewTransportRegistry()
	if err = transports.Register("llamacpp", inference.LocalOpenAITransport{Resolver: cancellationLoopback(server.URL)}); err != nil {
		t.Fatalf("fixture step 56: %v", err)
	}
	if err = infer.ConfigureExecution(artifacts, transports, nil, "cancel-node"); err != nil {
		t.Fatalf("fixture step 59: %v", err)
	}
	if err = infer.ConfigureRuntimeCoordinator(runtimecoord.NewService(db.SQL(), clock.Real{})); err != nil {
		t.Fatalf("fixture step 62: %v", err)
	}
	model, err := infer.RegisterModel(ctx, inference.RegisterModelCommand{ModelRef: "test/loopback", TrustState: inference.ModelUserTrusted})
	if err != nil {
		t.Fatalf("fixture step 66: %v", err)
	}
	node, runtime := "cancel-node", "llamacpp"
	dep, err := infer.RegisterDeployment(ctx, inference.RegisterDeploymentCommand{ModelID: model.ID, NodeID: &node, RuntimeName: &runtime, RuntimeConfigJSON: json.RawMessage(`{"runtime_backend":"llamacpp","placement":{"mode":"cpu"}}`)})
	if err != nil {
		t.Fatalf("fixture step 71: %v", err)
	}
	dep, err = infer.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: inference.DeploymentQualifying})
	if err != nil {
		t.Fatal(err)
	}
	dep, err = infer.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: inference.DeploymentReady})
	if err != nil {
		t.Fatalf("fixture step 75: %v", err)
	}
	frozen, err := s.SessionManifest(ctx, ss.ID)
	if err != nil {
		t.Fatalf("fixture step 79: %v", err)
	}
	manifest, _ := json.Marshal(map[string]any{"session_id": ss.ID, "turn_id": turn.ID, "member_id": turn.MemberID, "round": turn.RoundNumber, "candidate_id": dep.ID, "candidate_kind": "model_deployment", "session_manifest_sha256": frozen.SnapshotSHA256})
	if _, err = db.SQL().Exec(`UPDATE team_turn_requests SET status='running',selected_candidate_kind='model_deployment',selected_candidate_id=? WHERE id=?`, dep.ID, turn.ID); err != nil {
		t.Fatalf("fixture step 83: %v", err)
	}
	request, err := infer.Execute(ctx, inference.ExecuteCommand{WorkspaceID: ss.WorkspaceID, TaskID: &ss.TaskID, PrincipalID: "system:team-worker", DeploymentID: dep.ID, CapabilityJSON: json.RawMessage(`{"capability_id":"team.deliberate"}`), ContextManifestJSON: manifest, ClassificationMetadata: json.RawMessage(`{"team_mode":true,"deliberation_only":true}`), InputLabel: policy.DataLabel{WorkspaceID: ss.WorkspaceID, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}, RequestJSON: json.RawMessage(`{"messages":[{"role":"user","content":"reason"}]}`)})
	if err != nil {
		t.Fatalf("fixture step 87: %v", err)
	}
	if request.Status != inference.RequestSucceeded {
		t.Fatalf("loopback inference did not succeed: %+v", request)
	}
	content, _ := json.Marshal(map[string]any{"text": "reasoning response", "candidate_id": dep.ID, "candidate_kind": "model_deployment"})
	if _, err = s.CompleteReasoningTurn(ctx, ss.ID, turn.ID, turn.MemberID, "system:team-worker", turn.RoundNumber, content); err != nil {
		t.Fatalf("fixture step 94: %v", err)
	}
	// Retain a genuinely unclaimed pending turn across accepted-plan handoff.
	if _, err = s.RequestRound(ctx, RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human", MemberIDs: []string{specs[1].MemberID}}); err != nil {
		t.Fatalf("fixture step 98: %v", err)
	}
	for _, setup := range beforeAccept {
		setup(s, db, ss)
	}
	acceptAssignments(t, s, ss, specs)
	current, err := s.Session(ctx, ss.ID)
	if err != nil {
		t.Fatalf("fixture step 103: %v", err)
	}
	if beforeCancel != nil {
		beforeCancel(s, db, current)
	}
	root, err := s.tasks.Get(ctx, ss.TaskID)
	if err != nil {
		t.Fatalf("fixture step 107: %v", err)
	}
	if _, err = s.RequestExecutionCancellation(ctx, CancelExecutionCommand{SessionID: ss.ID, PlanID: *current.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}); err != nil {
		t.Fatalf("fixture step 110: %v", err)
	}
	if _, err = s.tasks.SettleUnstartedCancellations(ctx, 16); err != nil {
		t.Fatalf("fixture step 113: %v", err)
	}
	return s, db, current, request.ID, turn.ID
}

func TestTeamCancellationDefersUnprovenEvidence(t *testing.T) {
	cases := []struct{ name, query string }{
		{"root_only_intent", `UPDATE events SET payload_json=json_set(payload_json,'$.scope','task_only') WHERE event_type='team.execution_cancellation_requested'`},
		{"stale_intent", `UPDATE events SET payload_json=json_set(payload_json,'$.task_revision',0) WHERE event_type='team.execution_cancellation_requested'`},
		{"unknown_inference", `UPDATE inference_requests SET status='unknown' WHERE id=?`},
		{"live_inference", `UPDATE inference_requests SET status='executing',completed_at=NULL WHERE id=?`},
		{"unattributed_inference", `UPDATE inference_requests SET context_manifest_json=json_remove(context_manifest_json,'$.turn_id') WHERE id=?`},
		{"foreign_worker", `UPDATE inference_requests SET principal_id='human' WHERE id=?`},
		{"wrong_capability", `UPDATE inference_requests SET capability_json='{}' WHERE id=?`},
		{"wrong_manifest", `UPDATE inference_requests SET context_manifest_json=json_set(context_manifest_json,'$.session_manifest_sha256','forged') WHERE id=?`},
		{"foreign_message", `UPDATE team_messages SET author_principal_id='human' WHERE id=(SELECT response_message_id FROM team_turn_requests WHERE id=?)`},
		{"missing_route_evidence", `DELETE FROM events WHERE event_type='inference_request.routed'`},
		{"missing_completion_evidence", `DELETE FROM events WHERE event_type='inference_request.succeeded'`},
		{"forged_artifact_hash", `UPDATE artifacts SET content_hash='sha256:forged' WHERE id=(SELECT response_artifact_id FROM inference_requests WHERE id=?)`},
		{"external_transport", `UPDATE artifacts SET metadata_json=json_set(metadata_json,'$.transport','agent_runtime') WHERE id=(SELECT response_artifact_id FROM inference_requests WHERE id=?)`},
		{"claimed_pending_turn", `UPDATE team_turn_requests SET attempt_count=1 WHERE status='pending'`},
		{"live_turn", `UPDATE team_turn_requests SET status='running' WHERE status='pending'`},
		{"foreign_scope", `UPDATE tasks SET workspace_id='other-tenant' WHERE parent_task_id=?`},
		{"changed_child_objective", `UPDATE tasks SET objective='changed' WHERE parent_task_id=?`},
		{"changed_dependencies", `DELETE FROM task_dependencies WHERE task_id=?`},
		{"changed_root_criteria", `UPDATE tasks SET completion_json='{}' WHERE id=?`},
		{"missing_child_audit", `DELETE FROM events WHERE event_type='task.cancelled'`},
		{"resource_queue_live", `INSERT INTO runtime_execution_queue(id,workspace_id,task_id,inference_request_id,deployment_id,node_id,state,queued_at) SELECT 'live-queue',workspace_id,task_id,id,deployment_id,execution_node_id,'running',1 FROM inference_requests WHERE id=?`},
		{"resource_queue_unmatched", `INSERT INTO runtime_execution_queue(id,workspace_id,task_id,deployment_id,state,queued_at) SELECT 'unmatched-queue',workspace_id,task_id,deployment_id,'completed',1 FROM inference_requests WHERE id=?`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, db, ss, requestID, turnID := cancellationFixture(t)
			args := []any{}
			if strings.Contains(tc.query, "?") {
				arg := requestID
				if tc.name == "foreign_message" {
					arg = turnID
				}
				if tc.name == "foreign_scope" || tc.name == "changed_child_objective" || tc.name == "changed_dependencies" || tc.name == "changed_root_criteria" {
					arg = ss.TaskID
				}
				args = append(args, arg)
			}
			if strings.Contains(tc.query, "UPDATE events") || strings.Contains(tc.query, "DELETE FROM events") {
				// Deliberately simulate incomplete/corrupt ledger evidence only in
				// this disposable database; production remains append-only.
				for _, query := range []string{`DROP TRIGGER events_no_update`, `DROP TRIGGER events_no_delete`} {
					if _, err := db.SQL().Exec(query); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.name == "foreign_scope" {
				if _, err := db.SQL().Exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('other-tenant','Other','active',1,1,1)`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.SQL().Exec(tc.query, args...); err != nil {
				t.Fatal(err)
			}
			if n, err := s.SettleUnstartedCancellations(context.Background(), 16); err != nil || n != 0 {
				t.Fatalf("unsafe settlement: %d %v", n, err)
			}
			root, err := s.tasks.Get(context.Background(), ss.TaskID)
			if err != nil || root.State != task.StateCancelRequested {
				t.Fatalf("root changed: %+v %v", root, err)
			}
			var events int
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE event_type IN ('team.execution_cancelled','team.pending_turns_cancelled')`).Scan(&events); err != nil || events != 0 {
				t.Fatalf("unexpected audit %d %v", events, err)
			}
		})
	}
}

func TestTeamCancellationAuditRollbackAndIndependentRetry(t *testing.T) {
	s, db, ss, _, _ := cancellationFixture(t)
	if _, err := db.SQL().Exec(`CREATE TRIGGER reject_team_cancel BEFORE INSERT ON events WHEN NEW.event_type='team.execution_cancelled' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.SettleUnstartedCancellations(context.Background(), 16); err == nil || n != 0 {
		t.Fatalf("expected rollback %d %v", n, err)
	}
	root, err := s.tasks.Get(context.Background(), ss.TaskID)
	if err != nil || root.State != task.StateCancelRequested {
		t.Fatalf("partial root commit %+v %v", root, err)
	}
	var pending int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_turn_requests WHERE session_id=? AND status='pending'`, ss.ID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("partial turn commit %d %v", pending, err)
	}
	if _, err = db.SQL().Exec(`DROP TRIGGER reject_team_cancel`); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(db.SQL(), db, clock.Real{}, s.tasks)
	if n, err := restarted.SettleUnstartedCancellations(context.Background(), 16); err != nil || n != 1 {
		t.Fatalf("retry %d %v", n, err)
	}
}

func TestTeamCancellationDefersEveryExternalRuntimeOutcome(t *testing.T) {
	for _, outcome := range []string{"succeeded", "unknown", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			s, db, ss, _, turnID := cancellationFixture(t)
			if _, err := db.SQL().Exec(`INSERT INTO agent_runtime_connections(id,workspace_id,runtime_kind,display_name,adapter_name,adapter_version,endpoint_json,auth_type,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json,created_at,updated_at) VALUES('external','tenant','test','External','test','1','{}','none','connected','user_trusted','proposal_only','{}','{}','{}',1,1)`); err != nil {
				t.Fatal(err)
			}
			// Even a wrongly attributed Task must not hide the durable turn link.
			body, _ := json.Marshal(map[string]any{"context_manifest": map[string]any{"turn_id": turnID}})
			if _, err := db.SQL().Exec(`INSERT INTO agent_runtime_invocations(id,workspace_id,principal_id,connection_id,status,request_json,created_at,updated_at,completed_at) VALUES('external-invocation','tenant','system:team-worker','external',?,?,1,1,1)`, outcome, string(body)); err != nil {
				t.Fatal(err)
			}
			if n, err := s.SettleUnstartedCancellations(context.Background(), 16); err != nil || n != 0 {
				t.Fatalf("external effects guessed safe: %d %v", n, err)
			}
			root, err := s.tasks.Get(context.Background(), ss.TaskID)
			if err != nil || root.State != task.StateCancelRequested {
				t.Fatalf("root changed %+v %v", root, err)
			}
		})
	}
}

func TestTeamCancellationSettlesPausedUnstartedGraph(t *testing.T) {
	ctx := context.Background()
	s, db, ss, _, _ := cancellationFixtureWithBeforeCancel(t, func(s *Service, db *sqlitestore.DB, ss Session) {
		root, err := s.tasks.Get(ctx, ss.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		var child string
		if err = db.SQL().QueryRow(`SELECT task_id FROM team_execution_assignments WHERE session_id=? AND assignment_key='build'`, ss.ID).Scan(&child); err != nil {
			t.Fatal(err)
		}
		row, err := s.tasks.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: child, ExpectedRevision: row.Revision}); err != nil {
			t.Fatal(err)
		}
		// Acceptance leaves this fixture root READY without an Attempt.
		if _, err = s.PauseExecution(ctx, ExecutionControlCommand{SessionID: ss.ID, PlanID: *ss.AcceptedPlanID, ExpectedTaskRevision: root.Revision, ActorPrincipalID: "human"}); err != nil {
			t.Fatal(err)
		}
	})
	if n, err := s.SettleUnstartedCancellations(ctx, 16); err != nil || n != 1 {
		t.Fatalf("paused settlement %d %v", n, err)
	}
	var active int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM team_execution_holds WHERE session_id=? AND released_at IS NULL`, ss.ID).Scan(&active); err != nil || active != 0 {
		t.Fatalf("stranded hold %d %v", active, err)
	}
	root, err := s.tasks.Get(ctx, ss.TaskID)
	if err != nil || root.State != task.StateCancelled {
		t.Fatalf("root %+v %v", root, err)
	}
}

func TestTeamCancellationSettlesAttributedLocalReasoning(t *testing.T) {
	ctx := context.Background()
	s, db, ss, requestID, _ := cancellationFixture(t)
	n, err := s.SettleUnstartedCancellations(ctx, 16)
	if err != nil || n != 1 {
		t.Fatalf("settle: %d %v", n, err)
	}
	root, err := s.tasks.Get(ctx, ss.TaskID)
	if err != nil || root.State != "cancelled" {
		t.Fatalf("root: %+v %v", root, err)
	}
	room, err := s.Session(ctx, ss.ID)
	if err != nil || room.Status != "cancelled" {
		t.Fatalf("session: %+v %v", room, err)
	}
	var status string
	if err = db.SQL().QueryRow(`SELECT status FROM inference_requests WHERE id=?`, requestID).Scan(&status); err != nil || status != "succeeded" {
		t.Fatalf("reasoning evidence changed: %s %v", status, err)
	}
	var pending, cancelled int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_turn_requests WHERE session_id=? AND status='pending'`, ss.ID).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("pending turn not cancelled", err)
	}
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_turn_requests WHERE session_id=? AND status='cancelled'`, ss.ID).Scan(&cancelled); err != nil || cancelled != 1 {
		t.Fatalf("cancelled turns %d %v", cancelled, err)
	}
	restarted := NewService(db.SQL(), db, clock.Real{}, s.tasks)
	if n, err = restarted.SettleUnstartedCancellations(ctx, 16); err != nil || n != 0 {
		t.Fatalf("restart replay: %d %v", n, err)
	}
}
