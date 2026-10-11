//go:build integration

package agentworker

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// Suspend just before the admission transaction, after the initial Workspace
// lookup and system membership checks have succeeded.
type suspendBeforeAdmissionTx struct {
	storage.Transactor
	db *sql.DB
	once sync.Once
}

func (x *suspendBeforeAdmissionTx) Within(ctx context.Context, fn func(context.Context,storage.Tx)error)error{
	var suspendErr error
	x.once.Do(func(){
		_,suspendErr=x.db.ExecContext(ctx,"UPDATE workspaces SET status='suspended' WHERE id='ws'")
	})
	if suspendErr!=nil{return suspendErr}
	return x.Transactor.Within(ctx,fn)
}

func setupAdmissionFixture(t *testing.T)(context.Context,*sqlite.DB,*task.Service,*Service,task.Task){
	t.Helper()
	ctx:=context.Background()
	db,err:=sqlite.Open(filepath.Join(t.TempDir(),"worker-admission.db"))
	if err!=nil{t.Fatal(err)}
	t.Cleanup(func(){db.Close()})
	if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
	now:=clock.Real{}.UnixMilli()
	if _,err:=db.SQL().ExecContext(ctx,
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
	tasks:=task.NewService(db.SQL(),db,clock.Real{})
	svc:=New(db.SQL(),db,clock.Real{},"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
	if err:=svc.ensureWorkspaceAccess(ctx,"ws");err!=nil{t.Fatal(err)}
	created,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"ws",Objective:"guarded Worker admission"})
	if err!=nil{t.Fatal(err)}
	ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:created.ID,ExpectedRevision:created.Revision})
	if err!=nil{t.Fatal(err)}
	return ctx,db,tasks,svc,ready
}

func assertAdmissionUnchanged(t *testing.T,ctx context.Context,db *sqlite.DB,tasks *task.Service,ready task.Task){
	t.Helper()
	row,err:=tasks.Get(ctx,ready.ID)
	if err!=nil{t.Fatal(err)}
	if row.State!=task.StateReady || row.Revision!=ready.Revision{
		t.Fatalf("failed admission mutated Task: before=%+v after=%+v",ready,row)
	}
	for _,q:=range []string{
		"SELECT COUNT(*) FROM agent_worker_runs",
		"SELECT COUNT(*) FROM task_attempts WHERE task_id='"+ready.ID+"'",
		"SELECT COUNT(*) FROM events WHERE event_type='agent_worker.started'",
	}{
		var n int
		if err:=db.SQL().QueryRowContext(ctx,q).Scan(&n);err!=nil{t.Fatal(err)}
		if n!=0{t.Fatalf("rejected Worker admission persisted rows (%s): %d",q,n)}
	}
}

func TestInactiveWorkspaceWithExistingMembershipNeverAdmitsWorker(t *testing.T){
	ctx,db,tasks,svc,ready:=setupAdmissionFixture(t)
	var membersBefore int
	if err:=db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id='ws'
		  AND principal_id IN (?,?,?) AND status='active'`,
		AuthorityPrincipal,WorkerPrincipal,VerifierPrincipal).Scan(&membersBefore);err!=nil{t.Fatal(err)}
	if membersBefore!=3{t.Fatalf("expected seeded system memberships, got %d",membersBefore)}
	if _,err:=db.SQL().ExecContext(ctx,"UPDATE workspaces SET status='suspended' WHERE id='ws'");err!=nil{t.Fatal(err)}
	if err:=svc.ensureWorkspaceAccess(ctx,"ws");err==nil||!strings.Contains(err.Error(),"state=suspended"){
		t.Fatalf("inactive Workspace access check failed open or lost status: %v",err)
	}
	run,result:=svc.startRun(ctx,ready)
	if result.Error==""||run.ID!=""{t.Fatalf("inactive Workspace admitted Worker: %+v %+v",run,result)}
	assertAdmissionUnchanged(t,ctx,db,tasks,ready)
	var membersAfter int
	if err:=db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id='ws'
		  AND principal_id IN (?,?,?) AND status='active'`,
		AuthorityPrincipal,WorkerPrincipal,VerifierPrincipal).Scan(&membersAfter);err!=nil{t.Fatal(err)}
	if membersAfter!=membersBefore{t.Fatalf("membership drift after rejected admission: before %d after %d",membersBefore,membersAfter)}
}

func TestWorkspaceSuspendedBetweenPreflightAndTransactionFailsClosed(t *testing.T){
	ctx,db,tasks,svc,ready:=setupAdmissionFixture(t)
	svc.tx=&suspendBeforeAdmissionTx{Transactor:db,db:db.SQL()}
	run,result:=svc.startRun(ctx,ready)
	if run.ID!=""||result.Error==""||!strings.Contains(result.Error,"state=suspended"){
		t.Fatalf("transaction status recheck failed open: run=%+v result=%+v",run,result)
	}
	assertAdmissionUnchanged(t,ctx,db,tasks,ready)
}

func TestWorkerStartRollsBackTaskAndAttemptWhenJournalWriteFails(t *testing.T){
	ctx,db,tasks,svc,ready:=setupAdmissionFixture(t)
	_,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER reject_worker_record BEFORE INSERT ON agent_worker_runs
	BEGIN SELECT RAISE(ABORT,'injected worker record failure'); END`)
	if err!=nil{t.Fatal(err)}
	run,result:=svc.startRun(ctx,ready)
	if run.ID!=""||result.Error==""{t.Fatalf("injected Worker write unexpectedly admitted: %+v %+v",run,result)}
	if errors.Is(err,sql.ErrNoRows){t.Fatal("unexpected fixture state")}
	assertAdmissionUnchanged(t,ctx,db,tasks,ready)
}
