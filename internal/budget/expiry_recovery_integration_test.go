//go:build integration

package budget

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type expiryClock struct{ ms int64 }

func (c *expiryClock) Now() time.Time   { return time.UnixMilli(c.ms) }
func (c *expiryClock) UnixMilli() int64 { return c.ms }

type countingExpiryTransactions struct {
	storage.Transactor
	calls int
}

func (c *countingExpiryTransactions) Within(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	c.calls++
	return c.Transactor.Within(ctx, fn)
}

func TestBudgetExpiryRetainsBoundOrInterruptedRecoveryQuota(t *testing.T) {
	for _, scenario := range []string{"inference_unknown", "inference_succeeded", "inference_failed", "inference_rate_limited", "runtime_unknown", "runtime_succeeded", "runtime_failed", "attempt_only", "ledger_only", "ancestor_attempt", "ancestor_ledger", "foreign_ancestry", "cyclic_ancestry", "ancestry_257", "unused_ancestry_256", "unused"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			db, err := sqlite.Open(t.TempDir() + "/expiry.db")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{
				`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('ws','Workspace','active',1,1)`,
				`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('agent','agent','Agent','active',1,1)`,
				`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,created_at,updated_at) VALUES('parent','ws','parent','blocked','normal_task',0,'{}',1,1),('task','ws','test','blocked','normal_task',0,'{}',1,1)`,
			} {
				if _, err = db.SQL().ExecContext(ctx, q); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "cyclic_ancestry" || scenario == "foreign_ancestry" {
				if _, err = db.SQL().ExecContext(ctx, `UPDATE tasks SET parent_task_id='parent' WHERE id='task'`); err != nil {
					t.Fatal(err)
				}
				if scenario == "cyclic_ancestry" {
					_, err = db.SQL().ExecContext(ctx, `UPDATE tasks SET parent_task_id='task' WHERE id='parent'`)
				} else {
					if _, err = db.SQL().ExecContext(ctx, `INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('foreign','Foreign','active',1,1)`); err != nil {
						t.Fatal(err)
					}
					_, err = db.SQL().ExecContext(ctx, `UPDATE tasks SET workspace_id='foreign' WHERE id='parent'`)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "ancestry_257" || scenario == "unused_ancestry_256" {
				parents := 256
				if scenario == "unused_ancestry_256" {
					parents = 255
				}
				var previous any
				for i := 0; i < parents; i++ {
					id := fmt.Sprintf("ancestor-%03d", i)
					if _, err = db.SQL().ExecContext(ctx, `INSERT INTO tasks(id,workspace_id,parent_task_id,objective,state,scheduling_class,priority,completion_json,created_at,updated_at) VALUES(?,'ws',?,'ancestor','blocked','normal_task',0,'{}',1,1)`, id, previous); err != nil {
						t.Fatal(err)
					}
					previous = id
				}
				if _, err = db.SQL().ExecContext(ctx, `UPDATE tasks SET parent_task_id=? WHERE id='task'`, previous); err != nil {
					t.Fatal(err)
				}
			}
			clk := &expiryClock{ms: 1000}
			tracked := &countingExpiryTransactions{Transactor: db}
			svc := NewService(db.SQL(), tracked, clk)
			ws, taskID := "ws", "task"
			account, err := svc.CreateAccount(ctx, CreateAccountCommand{WorkspaceID: &ws, Name: "Quota", Unit: "requests", LimitAmount: 5})
			if err != nil {
				t.Fatal(err)
			}
			reservation, err := svc.Reserve(ctx, ReserveCommand{WorkspaceID: ws, AccountID: account.ID, TaskID: &taskID, Amount: 1, TTLMillis: 10})
			if err != nil {
				t.Fatal(err)
			}
			owner := "task"
			if scenario == "ancestor_attempt" || scenario == "ancestor_ledger" {
				owner = "parent"
				if _, err = db.SQL().ExecContext(ctx, `UPDATE tasks SET parent_task_id='parent' WHERE id='task'`); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "attempt_only", "ancestor_attempt":
				_, err = db.SQL().ExecContext(ctx, `INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) VALUES('original',?,1,'interrupted','{}')`, owner)
			case "ledger_only", "ancestor_ledger":
				// Historical corruption can leave ledger-only provenance. Even malformed
				// payload/foreign scope cannot clear the existing admission fence.
				_, err = db.SQL().ExecContext(ctx, `INSERT INTO events(id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) VALUES('interruption','task.attempt_interrupted','task',?,'{}',1)`, owner)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(scenario) > 10 && scenario[:10] == "inference_" {
				if _, err = db.SQL().ExecContext(ctx, `INSERT INTO inference_requests(id,workspace_id,task_id,principal_id,status,capability_json,context_manifest_json,classification_json,request_json,created_at,updated_at) VALUES('request','ws','task','agent',?,'{}','{}','{}','{}',1,1)`, scenario[10:]); err != nil {
					t.Fatal(err)
				}
				if _, err = db.SQL().ExecContext(ctx, `UPDATE budget_reservations SET inference_request_id='request' WHERE id=?`, reservation.ID); err != nil {
					t.Fatal(err)
				}
			}
			if len(scenario) > 8 && scenario[:8] == "runtime_" {
				if _, err = db.SQL().ExecContext(ctx, `INSERT INTO agent_runtime_connections(id,runtime_kind,display_name,adapter_name,adapter_version,endpoint_json,auth_type,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json,created_at,updated_at) VALUES('connection','hermes','Test','builtin.agent_protocol_http','1','{}','none','connected','user_trusted','proposal_only','{}','{}','{}',1,1)`); err != nil {
					t.Fatal(err)
				}
				if _, err = db.SQL().ExecContext(ctx, `INSERT INTO agent_runtime_invocations(id,workspace_id,task_id,principal_id,connection_id,status,request_json,created_at,updated_at) VALUES('invocation','ws','task','agent','connection',?,'{}',1,1)`, scenario[8:]); err != nil {
					t.Fatal(err)
				}
				if _, err = db.SQL().ExecContext(ctx, `UPDATE budget_reservations SET agent_runtime_invocation_id='invocation' WHERE id=?`, reservation.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, err := svc.Reservation(ctx, reservation.ID)
			if err != nil {
				t.Fatal(err)
			}
			priorAccount, err := svc.Account(ctx, account.ID)
			if err != nil {
				t.Fatal(err)
			}
			var priorEvents int
			if err = db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&priorEvents); err != nil {
				t.Fatal(err)
			}
			clk.ms = 2000
			tracked.calls = 0
			expired, err := svc.ExpireDue(ctx, 10)
			if strings.HasPrefix(scenario, "unused") {
				if err != nil || expired != 1 || tracked.calls != 1 {
					t.Fatalf("unused expiry failed: count=%d calls=%d err=%v", expired, tracked.calls, err)
				}
				after, e := svc.Reservation(ctx, reservation.ID)
				if e != nil || after.Status != ReservationExpired {
					t.Fatalf("unused reservation=%+v err=%v", after, e)
				}
				return
			}
			if err != nil || expired != 0 || tracked.calls != 0 {
				t.Fatalf("candidate query admitted recovery quota: expired=%d transactions=%d err=%v", expired, tracked.calls, err)
			}
			// Directly exercise the transactional fence as though the binding/history
			// changed after candidate selection. Neither guard can rely on the other.
			if err = svc.expire(ctx, reservation.ID); !errors.Is(err, ErrReservationActive) {
				t.Errorf("transaction refunded recovery quota: %v", err)
			}
			after, err := svc.Reservation(ctx, reservation.ID)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("reservation changed: before=%+v after=%+v err=%v", before, after, err)
			}
			balance, err := svc.Account(ctx, account.ID)
			if err != nil || !reflect.DeepEqual(balance, priorAccount) || balance.ReservedAmount != 1 || balance.CommittedAmount != 0 {
				t.Fatalf("account changed: before=%+v after=%+v err=%v", priorAccount, balance, err)
			}
			var events int
			if err = db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&events); err != nil || events != priorEvents {
				t.Fatalf("expiry published audit: before=%d after=%d err=%v", priorEvents, events, err)
			}
		})
	}
}

func TestBudgetExpiryRecoveryProbesUseOwnershipIndexes(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(t.TempDir() + "/plans.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('ws','Workspace','active',1,1)`,
		`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<512) INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,created_at,updated_at) SELECT 'unrelated-'||i,'ws','unrelated','blocked','normal_task',0,'{}',1,1 FROM n`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) SELECT 'attempt-'||id,id,1,'interrupted','{}' FROM tasks`,
		`INSERT INTO events(id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at) SELECT 'event-'||id,'task.attempt_interrupted','task',id,'{}',1 FROM tasks`,
	} {
		if _, err = db.SQL().ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.SQL().QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT `+reservationTaskRecoveryFenceSQL+` FROM (SELECT ? AS task_id) br`, "absent-task")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plans, "\n")
	for _, probe := range []string{"SEARCH tasks USING INDEX", "SEARCH p USING INDEX", "SEARCH attempt USING", "SEARCH e USING"} {
		if !strings.Contains(joined, probe) {
			t.Errorf("ownership probe lacks indexed search %s:\n%s", probe, joined)
		}
	}
	for _, scan := range []string{"SCAN tasks", "SCAN p ", "SCAN attempt", "SCAN e "} {
		if strings.Contains(joined, scan) {
			t.Errorf("unrelated-history scan %s:\n%s", scan, joined)
		}
	}
}
