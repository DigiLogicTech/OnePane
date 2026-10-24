//go:build integration

package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestInferencePostDispatchBudgetCommitFailureRetainsUnknownQuota(t *testing.T) {
	for _, outcome := range []string{"success", "rate_limit", "invalid_response"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := context.Background()
			db, err := sqliteStore.Open(t.TempDir() + "/budget.db")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{
				`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('ws','Test','active',1,1)`,
				`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('agent','agent','Agent','active',1,1)`,
				`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',1,1)`,
				`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,last_seen_at,created_at,updated_at) VALUES('node','Local',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`,
				`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,created_at,updated_at) VALUES('task','ws','test','running','normal_task',0,'{}',1,1)`,
				`INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model','test/model','["text"]','{}','user_trusted',1,1)`,
				`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,discovered_at,updated_at) VALUES('deployment','model','node','local-openai','{}','ready','fp',1,1)`,
			} {
				if _, err = db.SQL().ExecContext(ctx, q); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch outcome {
				case "rate_limit":
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"error":"limited"}`))
				case "invalid_response":
					_, _ = w.Write([]byte(`invalid`))
				default:
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}],"usage":{"total_tokens":1}}`))
				}
			}))
			defer server.Close()
			registry := NewTransportRegistry()
			if err = registry.Register("local-openai", LocalOpenAITransport{Resolver: endpointResolverFunc(func(context.Context, string) (string, error) { return server.URL, nil })}); err != nil {
				t.Fatal(err)
			}
			store, err := artifact.NewLocalStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			svc := NewService(db.SQL(), db, clock.Real{})
			if err = svc.ConfigureExecution(artifact.NewService(db.SQL(), db, store, clock.Real{}), registry, nil, "node"); err != nil {
				t.Fatal(err)
			}
			quotas := budget.NewService(db.SQL(), db, clock.Real{})
			if err = svc.ConfigureBudget(quotas); err != nil {
				t.Fatal(err)
			}
			ws, taskID := "ws", "task"
			account, err := quotas.CreateAccount(ctx, budget.CreateAccountCommand{WorkspaceID: &ws, Name: "Quota", Unit: "requests", LimitAmount: 5})
			if err != nil {
				t.Fatal(err)
			}
			reservation, err := quotas.Reserve(ctx, budget.ReserveCommand{WorkspaceID: ws, AccountID: account.ID, TaskID: &taskID, Amount: 1, TTLMillis: 60000})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.SQL().ExecContext(ctx, `CREATE TRIGGER reject_budget_commit BEFORE INSERT ON events WHEN NEW.event_type='budget.committed' BEGIN SELECT RAISE(ABORT,'budget commit unavailable'); END`); err != nil {
				t.Fatal(err)
			}
			result, err := svc.Execute(ctx, ExecuteCommand{WorkspaceID: ws, TaskID: &taskID, PrincipalID: "agent", DeploymentID: "deployment", InputLabel: policy.DataLabel{WorkspaceID: ws, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUserInstruction, OriginNodeID: "node"}, RequestJSON: json.RawMessage(`{"messages":[{"role":"user","content":"test"}]}`), BudgetReservationID: &reservation.ID})
			if err == nil || !strings.Contains(err.Error(), "budget commit unavailable") || result.Status != RequestUnknown || result.ErrorCode == nil || *result.ErrorCode != "budget_commit_failed" || result.ResponseArtifactID != nil {
				t.Fatalf("spent quota was classified safe or response escaped: result=%+v error=%v", result, err)
			}
			stored, getErr := svc.Request(ctx, result.ID)
			if getErr != nil || stored.Status != RequestUnknown {
				t.Fatalf("durable outcome=%+v error=%v", stored, getErr)
			}
			held, getErr := quotas.Reservation(ctx, reservation.ID)
			if getErr != nil || held.Status != budget.ReservationReserved || held.InferenceRequestID == nil || *held.InferenceRequestID != result.ID {
				t.Fatalf("spent reservation lost: %+v %v", held, getErr)
			}
			balance, getErr := quotas.Account(ctx, account.ID)
			if getErr != nil || balance.ReservedAmount != 1 || balance.CommittedAmount != 0 {
				t.Fatalf("quota changed: %+v %v", balance, getErr)
			}
			// Reusing the same held reservation cannot replay the transport.
			_, _ = svc.Execute(ctx, ExecuteCommand{WorkspaceID: ws, TaskID: &taskID, PrincipalID: "agent", DeploymentID: "deployment", InputLabel: policy.DataLabel{WorkspaceID: ws, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyOriginNode, Trust: policy.TrustUserInstruction, OriginNodeID: "node"}, BudgetReservationID: &reservation.ID})
			var refunds, artifacts int
			if err = db.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM events WHERE event_type='budget.released'),(SELECT COUNT(*) FROM artifacts)`).Scan(&refunds, &artifacts); err != nil || refunds != 0 || artifacts != 0 || calls.Load() != 1 {
				t.Fatalf("replay/refund/output: calls=%d refunds=%d artifacts=%d error=%v", calls.Load(), refunds, artifacts, err)
			}
		})
	}
}
