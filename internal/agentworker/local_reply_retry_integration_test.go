//go:build integration

package agentworker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

func TestMalformedLocalReplyRetriesWithErrorAndCloudReconciles(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		w, ctx, run := prepareReplyRoute(t, true)
		var calls atomic.Int64
		var sawError atomic.Bool
		server := newReplyServer(t, &calls, &sawError, func(n int64, req agentprotocol.Request) agentprotocol.Response {
			if n == 1 {
				return agentprotocol.Response{}
			}
			return agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalWait, Message: "corrected", Proposal: json.RawMessage(`{"reason":"need a moment","duration_ms":1000}`)}
		}, true)
		defer server.Close()
		wireReplyTransport(t, w, server)
		first := dispatchPrepared(t, w, ctx, run)
		if first.Status != "retry_reasoning" || w.dispatchReconciliationFailed {
			t.Fatalf("malformed local reply went to reconciliation: %+v halted=%v", first, w.dispatchReconciliationFailed)
		}
		reloaded, err := w.getRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		second := dispatchPrepared(t, w, ctx, reloaded)
		if !sawError.Load() || calls.Load() != 2 {
			t.Fatalf("retry did not feed the error back: calls=%d saw=%v result=%+v", calls.Load(), sawError.Load(), second)
		}
		if second.Status == "blocked" && strings.Contains(second.Error, "operator reconciliation") {
			t.Fatalf("corrected local reply was reconciled: %+v", second)
		}
	})

	t.Run("exhausted", func(t *testing.T) {
		w, ctx, run := prepareReplyRoute(t, true)
		var calls atomic.Int64
		server := newReplyServer(t, &calls, nil, func(int64, agentprotocol.Request) agentprotocol.Response {
			return agentprotocol.Response{}
		}, true)
		defer server.Close()
		wireReplyTransport(t, w, server)
		current := run
		var last TickResult
		for i := 0; i < maxLocalMalformedReplyRetries+1; i++ {
			last = dispatchPrepared(t, w, ctx, current)
			if w.dispatchReconciliationFailed {
				t.Fatalf("known malformed local reply halted scheduling: %+v", last)
			}
			var err error
			current, err = w.getRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
		if calls.Load() != int64(maxLocalMalformedReplyRetries+1) || last.Status != "blocked" || !strings.Contains(last.Error, "bounded retries") {
			t.Fatalf("retries were not bounded: calls=%d result=%+v", calls.Load(), last)
		}
		var unknown int
		if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND status='unknown'`, run.ID).Scan(&unknown); err != nil || unknown != 0 {
			t.Fatalf("known local failure was journaled as unknown: %d %v", unknown, err)
		}
	})

	t.Run("cloud", func(t *testing.T) {
		w, ctx, run := prepareReplyRoute(t, false)
		var calls atomic.Int64
		server := newReplyServer(t, &calls, nil, func(int64, agentprotocol.Request) agentprotocol.Response {
			return agentprotocol.Response{}
		}, false)
		defer server.Close()
		wireReplyTransport(t, w, server)
		result := dispatchPrepared(t, w, ctx, run)
		if result.Status == "retry_reasoning" || calls.Load() != 1 {
			t.Fatalf("cloud malformed reply was retried: calls=%d %+v", calls.Load(), result)
		}
		if result.Status != "blocked" && !w.dispatchReconciliationFailed {
			t.Fatalf("uncertain cloud reply was not held for reconciliation: %+v halted=%v", result, w.dispatchReconciliationFailed)
		}
		reloaded, err := w.getRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.Status == RunRunning && !w.dispatchReconciliationFailed {
			again := dispatchPrepared(t, w, ctx, reloaded)
			if calls.Load() != 1 {
				t.Fatalf("cloud reply was replayed: calls=%d %+v", calls.Load(), again)
			}
		}
	})
}

func prepareReplyRoute(t *testing.T, local bool) (*Service, context.Context, Run) {
	t.Helper()
	w, ctx, run, cleanup := prepareUnknownDispatch(t)
	t.Cleanup(cleanup)
	statements := []string{
		`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node-local','Local',1,'reply-fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`,
		`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('reply-model','reply-model','["text"]','{}','user_trusted',1,1)`,
	}
	if local {
		statements = append(statements,
			`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('reply-deployment','reply-model','node-local','no-transport','{}','ready','reply-fp',1,1,1)`,
		)
	} else {
		statements = append(statements,
			`INSERT INTO provider_connections(id,workspace_id,provider,display_name,auth_type,status,connection_json,created_at,updated_at) VALUES('reply-provider','tenant','synthetic','Synthetic','none','connected','{"data_policy":{"max_confidentiality":"internal","allowed_residency":["any"]},"scheduling":{"cost_class":"paid"}}',1,1)`,
			`INSERT INTO model_deployments(id,model_id,provider_connection_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES('reply-deployment','reply-model','reply-provider','no-transport','{}','ready','reply-fp',1,1,1)`,
		)
	}
	statements = append(statements, `INSERT INTO compatibility_profiles(id,deployment_id,capability_id,protocol_level,context_min,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,revision,created_at,updated_at) VALUES('reply-compatibility','reply-deployment','inference.general','L1',0,64000,'low','supported','{}','{}','reply-cp',1,1,1)`)
	for _, statement := range statements {
		if _, err := w.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	w.scheduler = scheduler.NewService(w.db, w.tx, clock.Real{})
	w.verification = verification.NewService(w.db, w.tx, clock.Real{})
	if !local {
		w.budgets = budget.NewService(w.db, w.tx, clock.Real{})
		workspace := run.WorkspaceID
		account, err := w.budgets.CreateAccount(ctx, budget.CreateAccountCommand{WorkspaceID: &workspace, Name: "Cloud", Unit: "requests", LimitAmount: 5})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(routePolicy{RoutingEnabled: true, AllowRemote: true, AllowPotentialMonetarySpend: true, AllowLimited: true, AllowMediated: true, AllowDegraded: true, ComputePreference: "auto", BudgetAccountID: account.ID, BudgetReserveAmount: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.db.ExecContext(ctx, `UPDATE agent_worker_runs SET route_policy_json=? WHERE id=?`, string(raw), run.ID); err != nil {
			t.Fatal(err)
		}
		run, err = w.getRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	return w, ctx, run
}

func wireReplyTransport(t *testing.T, w *Service, server *httptest.Server) {
	t.Helper()
	store, err := artifact.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w.artifacts = artifact.NewService(w.db, w.tx, store, clock.Real{})
	w.inference = inference.NewService(w.db, w.tx, clock.Real{})
	transports := inference.NewTransportRegistry()
	if err := transports.Register("no-transport", inference.LocalOpenAITransport{Resolver: responseJournalEndpoint(server.URL), Client: server.Client()}); err != nil {
		t.Fatal(err)
	}
	if err := w.inference.ConfigureExecution(w.artifacts, transports, nil, "node-local"); err != nil {
		t.Fatal(err)
	}
	if err := w.inference.ConfigureRuntimeCoordinator(runtimecoord.NewService(w.db, clock.Real{})); err != nil {
		t.Fatal(err)
	}
	if w.budgets != nil {
		if err := w.inference.ConfigureBudget(w.budgets); err != nil {
			t.Fatal(err)
		}
	}
}

func newReplyServer(t *testing.T, calls *atomic.Int64, sawError *atomic.Bool, respond func(int64, agentprotocol.Request) agentprotocol.Response, local bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		n := calls.Add(1)
		body, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
		if err != nil {
			http.Error(rw, "bad request", http.StatusBadRequest)
			return
		}
		if sawError != nil && strings.Contains(string(body), "malformed or truncated") {
			sawError.Store(true)
		}
		var envelope struct {
			Messages []struct {
				Role, Content string
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			http.Error(rw, "bad envelope", http.StatusBadRequest)
			return
		}
		var req agentprotocol.Request
		for _, message := range envelope.Messages {
			if message.Role == "user" {
				_ = json.Unmarshal([]byte(message.Content), &req)
			}
		}
		response := respond(n, req)
		if response.ProtocolVersion == "" {
			_ = json.NewEncoder(rw).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"protocol_version":`}}}, "usage": map[string]any{"completion_tokens": 1}})
			return
		}
		raw, _ := json.Marshal(response)
		_ = json.NewEncoder(rw).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(raw)}}}, "usage": map[string]any{"completion_tokens": 1}})
		_ = local
	}))
}

func dispatchPrepared(t *testing.T, w *Service, ctx context.Context, run Run) TickResult {
	t.Helper()
	prepared := w.prepareStep(ctx, run)
	if prepared.dispatch == nil {
		t.Fatalf("dispatch not prepared: %+v", prepared.result)
	}
	return prepared.dispatch()()
}
