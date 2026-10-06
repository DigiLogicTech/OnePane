//go:build integration

package budget

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestExpiredReservationAttachedToActiveTaskIsNotReclaimed(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().UnixMilli()
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	clk := clock.Real{}
	tasks := task.NewService(db.SQL(), db, clk)
	tr, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "active budget work", SchedulingClass: task.ClassUserInteractive})
	if err != nil {
		t.Fatal(err)
	}
	tr, err = tasks.MarkReady(ctx, task.TransitionCommand{TaskID: tr.ID, ExpectedRevision: tr.Revision})
	if err != nil {
		t.Fatal(err)
	}

	budgets := NewService(db.SQL(), db, clk)
	ws := "ws"
	account, err := budgets.CreateAccount(ctx, CreateAccountCommand{WorkspaceID: &ws, Name: "requests", Unit: "requests", LimitAmount: 10})
	if err != nil {
		t.Fatal(err)
	}
	ttl := int64(1)
	reservation, err := budgets.Reserve(ctx, ReserveCommand{WorkspaceID: ws, AccountID: account.ID, TaskID: &tr.ID, Amount: 1, TTLMillis: ttl})
	if err != nil {
		t.Fatal(err)
	}
	tr, _, err = tasks.Start(ctx, task.StartCommand{TaskID: tr.ID, ExpectedRevision: tr.Revision})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)

	expired, err := budgets.ExpireDue(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if expired != 0 {
		t.Fatalf("expired=%d", expired)
	}
	stored, err := budgets.Reservation(ctx, reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ReservationReserved {
		t.Fatalf("reservation status=%s", stored.Status)
	}
}
