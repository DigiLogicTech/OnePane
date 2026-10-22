//go:build integration

package agentworker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// An invalid CREATED Task transition must not stall all later Tasks on a
// local CPU-only deployment. Both the successes and failed IDs are durable.
func TestIndependentCreatedTaskAdmissionSurvivesCorruptSibling(t *testing.T){
	ctx:=context.Background()
	db,err:=sqlite.Open(filepath.Join(t.TempDir(),"independent-created.db"))
	if err!=nil{t.Fatal(err)}
	defer db.Close()
	if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
	now:=clock.Real{}.UnixMilli()
	if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
		id,name,status,revision,created_at,updated_at)
		VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
	tasks:=task.NewService(db.SQL(),db,clock.Real{})
	worker:=New(db.SQL(),db,clock.Real{},"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
	if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
	create:=func(objective string,priority int)task.Task{
		t.Helper()
		row,err:=tasks.Create(ctx,task.CreateCommand{
			WorkspaceID:"tenant",Objective:objective,Priority:priority,
		})
		if err!=nil{t.Fatal(err)}
		return row
	}
	broken:=create("Corrupt high-priority Task",100)
	healthy:=create("Independent valid Task",90)
	otherBroken:=create("Other corrupt Task",80)
	for _,row:=range []task.Task{broken,otherBroken}{
		_,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER fail_task_ready_`+
			strings.ReplaceAll(row.ID,"-","_")+
			` BEFORE UPDATE OF state ON tasks
			WHEN NEW.id='`+row.ID+`' AND NEW.state='ready'
			BEGIN SELECT RAISE(ABORT,'synthetic per-Task ready failure'); END`)
		if err!=nil{t.Fatal(err)}
	}
	count,err:=worker.admitCreated(ctx,10)
	if count!=1||err==nil||!strings.Contains(err.Error(),broken.ID)||
		!strings.Contains(err.Error(),otherBroken.ID){
		t.Fatalf("per-Task failure starved sibling or dropped diagnostics: count=%d err=%v",count,err)
	}
	var ready,created int
	for _,tc:=range []struct{
		id string
		want task.State
	}{{broken.ID,task.StateCreated},{healthy.ID,task.StateReady},{otherBroken.ID,task.StateCreated}}{
		got,getErr:=tasks.Get(ctx,tc.id)
		if getErr!=nil{t.Fatal(getErr)}
		if got.State!=tc.want{t.Fatalf("Task %s state %s want %s",tc.id,got.State,tc.want)}
		if got.State==task.StateReady{ready++}else{created++}
	}
	if ready!=1||created!=2{t.Fatalf("admission state mismatch: %d ready %d created",ready,created)}
	var readyEvents int
	if err:=db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.ready'`,healthy.ID).
		Scan(&readyEvents);err!=nil{t.Fatal(err)}
	if readyEvents!=1{t.Fatalf("healthy Task ready events=%d",readyEvents)}
	count,err=worker.admitCreated(ctx,10)
	if count!=0||err==nil{
		t.Fatalf("repeat admission silently dropped rejected Tasks: %d %v",count,err)
	}
	if err:=db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='task.ready'`,healthy.ID).
		Scan(&readyEvents);err!=nil{t.Fatal(err)}
	if readyEvents!=1{t.Fatalf("repeat admission duplicated a healthy Task: %d",readyEvents)}
}
