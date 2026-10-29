//go:build integration

package teamworker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type reasoningEndpoint string

func (e reasoningEndpoint) ResolveLocalEndpoint(context.Context, string) (string, error) {
	return string(e), nil
}

func TestReasoningTurnUnknownAccountingCannotRefundOrReplayAfterReconstruction(t *testing.T) {
	for _, producer := range []string{"model", "runtime"} {
		for _, research := range []bool{false, true} {
			for _, rejectAudit := range []bool{false, true} {
				name := producer + "/team"
				if research {
					name = producer + "/research"
				}
				if rejectAudit {
					name += "/audit_failure"
				}
				t.Run(name, func(t *testing.T) {
					ctx := context.Background()
					db := migratedWorkerDatabase(t)
					exec := func(q string, args ...any) {
						t.Helper()
						if _, err := db.SQL().ExecContext(ctx, q, args...); err != nil {
							t.Fatal(err)
						}
					}
					for _, q := range []string{
						`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('tenant','Tenant','active',1,1)`,
						`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('human','human','Human','active',1,1)`,
						`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
						`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,created_at,updated_at) VALUES('node','Local',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1)`,
					} {
						exec(q)
					}
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						var req agentprotocol.Request
						if producer == "runtime" {
							if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
								t.Error(err)
								return
							}
							_ = json.NewEncoder(w).Encode(agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalWait, Proposal: json.RawMessage(`{}`)})
						} else {
							var outer struct {
								Messages []struct {
									Content string `json:"content"`
								} `json:"messages"`
							}
							if err := json.NewDecoder(r.Body).Decode(&outer); err != nil || len(outer.Messages) != 2 {
								t.Errorf("invalid wire request: %v", err)
								return
							}
							if err := json.Unmarshal([]byte(outer.Messages[1].Content), &req); err != nil {
								t.Error(err)
								return
							}
							response, _ := json.Marshal(agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalWait, Proposal: json.RawMessage(`{}`)})
							_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(response)}}}, "usage": map[string]int{"total_tokens": 1}})
						}
					}))
					defer server.Close()
					store, err := artifact.NewLocalStore(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					artifacts := artifact.NewService(db.SQL(), db, store, clock.Real{})
					quotas := budget.NewService(db.SQL(), db, clock.Real{})
					ws, node := "tenant", "node"
					account, err := quotas.CreateAccount(ctx, budget.CreateAccountCommand{WorkspaceID: &ws, Name: "Seats", Unit: "requests", LimitAmount: 4})
					if err != nil {
						t.Fatal(err)
					}
					infer := inference.NewService(db.SQL(), db, clock.Real{})
					transports := inference.NewTransportRegistry()
					if err = transports.Register("local-openai", inference.LocalOpenAITransport{Resolver: reasoningEndpoint(server.URL)}); err != nil {
						t.Fatal(err)
					}
					if err = infer.ConfigureExecution(artifacts, transports, nil, node); err != nil {
						t.Fatal(err)
					}
					if err = infer.ConfigureBudget(quotas); err != nil {
						t.Fatal(err)
					}
					registry := agentruntime.NewRegistry()
					if err = agentruntime.RegisterBuiltinAdapters(registry); err != nil {
						t.Fatal(err)
					}
					runtimes := agentruntime.NewService(db.SQL(), db, registry, clock.Real{})
					runtimeTransports := agentruntime.NewRuntimeTransportRegistry()
					if err = runtimeTransports.Register("builtin.agent_protocol_http", "1", agentruntime.AgentProtocolHTTPTransport{}); err != nil {
						t.Fatal(err)
					}
					if err = runtimes.ConfigureExecution(artifacts, runtimeTransports, nil, node); err != nil {
						t.Fatal(err)
					}
					if err = runtimes.ConfigureBudget(quotas); err != nil {
						t.Fatal(err)
					}
					if producer == "runtime" {
						conn, e := runtimes.Register(ctx, agentruntime.RegisterCommand{WorkspaceID: &ws, NodeID: &node, RuntimeKind: "hermes", DisplayName: "Loopback seat", AdapterName: "builtin.agent_protocol_http", AdapterVersion: "1", EndpointJSON: json.RawMessage(`{"base_url":"` + server.URL + `"}`), AuthType: "none", TrustState: agentruntime.TrustUserTrusted, OperatingMode: agentruntime.ProposalOnly, CapabilitiesJSON: json.RawMessage(`{"roles":["Reviewer"],"capabilities":["inference.general"],"protocol_level":"L2","context_max":64000,"scheduling":{"cost_class":"paid"}}`), DataPolicyJSON: json.RawMessage(`{"max_confidentiality":"secret","allowed_residency":["any"],"destination_kind":"origin_node","allow_raw_secrets":false}`)})
						if e != nil {
							t.Fatal(e)
						}
						if _, e = runtimes.SetStatus(ctx, agentruntime.SetStatusCommand{ConnectionID: conn.ID, ExpectedRevision: conn.Revision, Status: agentruntime.StatusConnected}); e != nil {
							t.Fatal(e)
						}
					} else {
						for _, q := range []string{
							`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,created_at,updated_at) VALUES('provider','tenant','synthetic','Synthetic','none','connected','{"data_policy":{"max_confidentiality":"internal","allowed_residency":["any"]},"scheduling":{"cost_class":"paid"}}',1,1)`,
							`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model','synthetic','["text"]','{}','user_trusted',1,1)`,
							`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,discovered_at,updated_at) VALUES('deployment','model','provider','local-openai','{}','ready','fp',1,1)`,
							`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,created_at,updated_at) VALUES('compatibility','deployment','inference.general','L2',0,64000,'low','supported','{}','{}','cp',1,1)`,
						} {
							exec(q)
						}
					}
					tasks := task.NewService(db.SQL(), db, clock.Real{})
					job, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: ws, Objective: "Reason conservatively"})
					if err != nil {
						t.Fatal(err)
					}
					teams := team.NewService(db.SQL(), db, clock.Real{}, tasks)
					groupConfig := json.RawMessage(`{}`)
					if research {
						groupConfig = json.RawMessage(`{"research_mode":true}`)
					}
					group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: ws, Name: "Reasoning", CreatedBy: "human"})
					if err != nil {
						t.Fatal(err)
					}
					if _, err = teams.UpdateConfiguration(ctx, team.UpdateConfigurationCommand{TeamID: group.ID, ActorPrincipalID: "human", ExpectedRevision: group.Revision, Configuration: groupConfig}); err != nil {
						t.Fatal(err)
					}
					route, _ := json.Marshal(map[string]any{"allow_potential_monetary_spend": true, "budget_account_id": account.ID, "budget_reserve_amount": 1})
					if _, err = teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Reviewer", RoleName: "Reviewer", CreatedBy: "human", ProtocolLevel: "L2", RoutePolicy: route}); err != nil {
						t.Fatal(err)
					}
					mode := "team"
					cfg := json.RawMessage(`{}`)
					if research {
						mode = "council"
						cfg = json.RawMessage(`{"research_mode":true}`)
					}
					session, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: job.ID, TeamID: group.ID, CreatedBy: "human", ExecutionMode: mode, Config: cfg})
					if err != nil {
						t.Fatal(err)
					}
					manifest, err := teams.SessionManifest(ctx, session.ID)
					if err != nil || manifest.ResearchMode != research {
						t.Fatalf("incorrect research fixture: %+v %v", manifest, err)
					}
					turns, err := teams.RequestRound(ctx, team.RequestRoundCommand{SessionID: session.ID, RequestedByPrincipalID: "human"})
					if err != nil || len(turns) != 1 {
						t.Fatalf("turns=%+v error=%v", turns, err)
					}
					exec(`CREATE TRIGGER reject_seat_commit BEFORE INSERT ON events WHEN NEW.event_type='budget.committed' BEGIN SELECT RAISE(ABORT,'seat accounting unavailable'); END`)
					if rejectAudit {
						exec(`CREATE TRIGGER reject_seat_reconciliation BEFORE INSERT ON events WHEN NEW.event_type='team.reasoning_reconciliation_required' BEGIN SELECT RAISE(ABORT,'seat recovery audit unavailable'); END`)
					}
					worker := func() *Service {
						return New(db.SQL(), db, clock.Real{}, teams, scheduler.NewService(db.SQL(), db, clock.Real{}), infer, runtimes, artifacts, quotas)
					}
					results, err := worker().Tick(ctx, 1)
					if err != nil || len(results) != 1 || results[0].Status != "unknown" || !strings.Contains(results[0].Error, "seat accounting unavailable") {
						t.Fatalf("uncertain quota became ordinary failure: results=%+v error=%v", results, err)
					}
					if rejectAudit && !strings.Contains(results[0].Error, "seat recovery audit unavailable") {
						t.Fatal("reconciliation write failure concealed")
					}
					var turnStatus string
					var messages, refunds, audits, attempts int
					if err = db.SQL().QueryRowContext(ctx, `SELECT status FROM team_turn_requests WHERE id=?`, turns[0].ID).Scan(&turnStatus); err != nil || turnStatus != "running" {
						t.Fatalf("lost durable nonretryable claim: %s %v", turnStatus, err)
					}
					var reservationID string
					if err = db.SQL().QueryRowContext(ctx, `SELECT budget_reservation_id FROM team_turn_requests WHERE id=?`, turns[0].ID).Scan(&reservationID); err != nil {
						t.Fatal(err)
					}
					held, err := quotas.Reservation(ctx, reservationID)
					if err != nil || held.Status != budget.ReservationReserved {
						t.Fatalf("potentially spent quota refunded: %+v %v", held, err)
					}
					for i := 0; i < 2; i++ {
						if more, e := worker().Tick(ctx, 1); e != nil || len(more) != 0 {
							t.Fatalf("reconstructed worker retried: %+v %v", more, e)
						}
					}
					if _, err = teams.RequestRound(ctx, team.RequestRoundCommand{SessionID: session.ID, RequestedByPrincipalID: "human"}); !errors.Is(err, team.ErrSessionState) {
						t.Fatalf("new round hid unresolved seat: %v", err)
					}
					if research {
						if _, err = teams.RetryResearchRound(ctx, team.RetryResearchRoundCommand{SessionID: session.ID, RequestedByPrincipalID: WorkerPrincipal, Automatic: true}); !errors.Is(err, team.ErrSessionState) {
							t.Fatalf("unknown automatically resumed: %v", err)
						}
					}
					if err = db.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM team_messages WHERE message_kind='agent'),(SELECT COUNT(*) FROM events WHERE event_type='budget.released'),(SELECT COUNT(*) FROM events WHERE event_type='team.reasoning_reconciliation_required'),(SELECT COUNT(*) FROM task_attempts)`).Scan(&messages, &refunds, &audits, &attempts); err != nil {
						t.Fatal(err)
					}
					wantAudits := 1
					if rejectAudit {
						wantAudits = 0
					}
					if calls.Load() != 1 || messages != 0 || refunds != 0 || audits != wantAudits || attempts != 0 {
						t.Fatalf("false output/replay/Attempt: calls=%d messages=%d refunds=%d audits=%d attempts=%d", calls.Load(), messages, refunds, audits, attempts)
					}
					balance, err := quotas.Account(ctx, account.ID)
					if err != nil || balance.ReservedAmount != 1 || balance.CommittedAmount != 0 {
						t.Fatalf("accounting changed: %+v %v", balance, err)
					}
				})
			}
		}
	}
}

func TestReasoningTurnProvenanceFailurePreventsAnyDispatch(t *testing.T) {
	for _, boundary := range []string{"candidate", "reservation", "reservation_release", "lost_owner"} {
		t.Run(boundary, func(t *testing.T) {
			ctx := context.Background()
			f := newFairnessFixture(t)
			ws := "healthy"
			quotas := budget.NewService(f.db.SQL(), f.db, clock.Real{})
			account, err := quotas.CreateAccount(ctx, budget.CreateAccountCommand{WorkspaceID: &ws, Name: "Provenance", Unit: "requests", LimitAmount: 4})
			if err != nil {
				t.Fatal(err)
			}
			route, _ := json.Marshal(map[string]any{"allow_potential_monetary_spend": true, "budget_account_id": account.ID, "budget_reserve_amount": 1})
			f.exec(t, `UPDATE team_members SET route_policy_json=? WHERE team_id=?`, string(route), f.groups[ws])
			for _, q := range []string{
				`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,created_at,updated_at) VALUES('provider','healthy','synthetic','Synthetic','none','connected','{"data_policy":{"max_confidentiality":"internal","allowed_residency":["any"]},"scheduling":{"cost_class":"paid"}}',1,1)`,
				`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model','synthetic','["text"]','{}','user_trusted',1,1)`,
				`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,discovered_at,updated_at) VALUES('deployment','model','provider','no-transport','{}','ready','fp',1,1)`,
				`INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,created_at,updated_at) VALUES('compatibility','deployment','inference.general','L2',0,64000,'low','supported','{}','{}','cp',1,1)`,
			} {
				f.exec(t, q)
			}
			ss, turnID := f.session(t, ws, 1, false)
			if boundary == "candidate" {
				f.exec(t, `CREATE TRIGGER reject_candidate_binding BEFORE UPDATE OF selected_candidate_id ON team_turn_requests BEGIN SELECT RAISE(ABORT,'candidate binding unavailable'); END`)
			} else if boundary == "lost_owner" {
				f.exec(t, `CREATE TRIGGER lose_turn_owner AFTER INSERT ON events WHEN NEW.event_type='budget.reserved' BEGIN UPDATE team_turn_requests SET status='cancelled' WHERE status='running'; END`)
			} else {
				f.exec(t, `CREATE TRIGGER reject_reservation_binding BEFORE UPDATE OF budget_reservation_id ON team_turn_requests BEGIN SELECT RAISE(ABORT,'reservation binding unavailable'); END`)
			}
			if boundary == "reservation_release" {
				f.exec(t, `CREATE TRIGGER reject_provenance_cleanup BEFORE INSERT ON events WHEN NEW.event_type='budget.released' BEGIN SELECT RAISE(ABORT,'provenance cleanup unavailable'); END`)
			}
			worker := f.worker()
			worker.budgets = quotas
			// No inference service is configured: a rejected provenance write
			// must return before even entering the service dispatch boundary.
			results, err := worker.Tick(ctx, 1)
			if err != nil || len(results) != 1 || results[0].Status != "unknown" {
				t.Fatalf("uncommitted provenance reached dispatch: %+v %v", results, err)
			}
			if boundary == "reservation_release" && !strings.Contains(results[0].Error, "provenance cleanup unavailable") {
				t.Fatal("reservation cleanup failure concealed")
			}
			var requests, reservations, audits int
			if err = f.db.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM inference_requests)+(SELECT COUNT(*) FROM agent_runtime_invocations),(SELECT COUNT(*) FROM budget_reservations),(SELECT COUNT(*) FROM events WHERE event_type='team.reasoning_reconciliation_required')`).Scan(&requests, &reservations, &audits); err != nil {
				t.Fatal(err)
			}
			wantReservations := 1
			if boundary == "candidate" {
				wantReservations = 0
			}
			wantAudits := 1
			wantStatus := "running"
			if boundary == "lost_owner" {
				wantAudits = 0
				wantStatus = "cancelled"
			}
			if requests != 0 || reservations != wantReservations || audits != wantAudits {
				t.Fatalf("wrong persisted boundary: requests=%d reservations=%d audits=%d", requests, reservations, audits)
			}
			balance, err := quotas.Account(ctx, account.ID)
			wantHeld := int64(0)
			if boundary == "reservation_release" {
				wantHeld = 1
			}
			if err != nil || balance.ReservedAmount != wantHeld || balance.CommittedAmount != 0 {
				t.Fatalf("wrong cleanup accounting: %+v %v", balance, err)
			}
			var status string
			if err = f.db.SQL().QueryRowContext(ctx, `SELECT status FROM team_turn_requests WHERE id=?`, turnID).Scan(&status); err != nil || status != wantStatus {
				t.Fatalf("lost exact turn: %s %v", status, err)
			}
			if _, err = f.teams.RequestRound(ctx, team.RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: "human"}); boundary != "lost_owner" && !errors.Is(err, team.ErrSessionState) {
				t.Fatalf("failed provenance silently retried: %v", err)
			}
		})
	}
}
