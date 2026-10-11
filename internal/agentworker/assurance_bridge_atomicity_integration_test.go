//go:build integration

package agentworker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

func TestWorkerAssuranceSuccessRequiresExactDurableTaskVerificationAndAudit(t *testing.T){
	ctx:=context.Background()
	db,err:=sqlite.Open(filepath.Join(t.TempDir(),"assurance-bridge.db"))
	if err!=nil{t.Fatal(err)}
	defer db.Close()
	if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
	now:=clock.Real{}.UnixMilli()
	if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
	 VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
	tasks:=task.NewService(db.SQL(),db,clock.Real{})
	worker:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
	if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
	created,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",Objective:"Independent verified completion"})
	if err!=nil{t.Fatal(err)}
	ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
		TaskID:created.ID,ExpectedRevision:created.Revision,
	})
	if err!=nil{t.Fatal(err)}
	run,res:=worker.startRun(ctx,ready)
	if res.Error!=""{t.Fatal(res.Error)}
	// An arbitrary blocked Worker cannot be declared successful.
	if err:=worker.finishRun(ctx,run.ID,RunBlocked,"pending external assurance");err!=nil{t.Fatal(err)}
	if err:=worker.AssurancePassed(ctx,run.ID,"fabricated","fabricated");!errors.Is(err,ErrInvalidWorkerState){
		t.Fatalf("blocked Worker without Task completion claimed success: %v",err)
	}
	requested,err:=tasks.RequestCompletion(ctx,task.TransitionCommand{
		TaskID:created.ID,ExpectedRevision:ready.Revision+1,
	})
	if err!=nil{t.Fatal(err)}
	verifying,err:=tasks.BeginVerification(ctx,task.TransitionCommand{
		TaskID:created.ID,ExpectedRevision:requested.Revision,
	})
	if err!=nil{t.Fatal(err)}
	ver:=verification.NewService(db.SQL(),db,clock.Real{})
	v,err:=ver.Create(ctx,verification.CreateCommand{
		WorkspaceID:"tenant",TaskID:&created.ID,SubjectRef:"agent_worker_run:"+run.ID,
		RequiredLevel:policy.VerificationV1,
		Spec:[]byte(`{"source":"agent_completion_proposal","worker_run_id":"`+run.ID+`"}`),
	})
	if err!=nil{t.Fatal(err)}
	lvl:=policy.VerificationV1
	v,err=ver.Resolve(ctx,verification.ResolveCommand{
		VerificationID:v.ID,ExpectedRevision:v.Revision,
		Status:verification.StatusPass,AchievedLevel:&lvl,
		VerifiedBy:VerifierPrincipal,
	})
	if err!=nil{t.Fatal(err)}
	cp,err:=ver.CreateCheckpoint(ctx,verification.CheckpointCommand{
		WorkspaceID:"tenant",TaskID:created.ID,VerificationID:v.ID,
		State:[]byte(`{"verified":true}`),
	})
	if err!=nil{t.Fatal(err)}
	if err:=worker.AssurancePassed(ctx,run.ID,v.ID,cp.ID);!errors.Is(err,ErrInvalidWorkerState){
		t.Fatalf("passed evidence before actual Task completion claimed Worker success: %v",err)
	}
	complete,err:=tasks.CompleteVerified(ctx,task.CompleteCommand{
		TaskID:created.ID,ExpectedRevision:verifying.Revision,
		CheckpointID:cp.ID,Result:[]byte(`{"result":"done"}`),
	})
	if err!=nil{t.Fatal(err)}
	if complete.State!=task.StateComplete{t.Fatalf("not complete: %+v",complete)}
	for _,bad:=range []struct{name,verificationID,checkpointID string}{
		{"wrong_verification","other",cp.ID},
		{"wrong_checkpoint",v.ID,"other"},
	}{
		if err:=worker.AssurancePassed(ctx,run.ID,bad.verificationID,bad.checkpointID);!errors.Is(err,ErrInvalidWorkerState){
			t.Fatalf("%s published a false assurance receipt: %v",bad.name,err)
		}
	}
	// Force journal failure AFTER the Worker state change. Both must roll
	// back atomically, including any success event/outbox.
	if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER reject_assurance_journal
	 BEFORE INSERT ON agent_worker_steps
	 WHEN NEW.step_kind='verification' AND NEW.status='succeeded'
	 BEGIN SELECT RAISE(ABORT,'injected immutable journal failure'); END`);err!=nil{t.Fatal(err)}
	if err:=worker.AssurancePassed(ctx,run.ID,v.ID,cp.ID);err==nil{
		t.Fatal("injected journal failure committed Worker success")
	}
	cur,err:=worker.getRun(ctx,run.ID)
	if err!=nil{t.Fatal(err)}
	if cur.Status!=RunBlocked{t.Fatalf("failed audit committed Worker=%s",cur.Status)}
	var n int
	if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
	 WHERE run_id=? AND step_kind='verification' AND status='succeeded'`,run.ID).Scan(&n);err!=nil{t.Fatal(err)}
	if n!=0{t.Fatalf("uncommitted success journal rows=%d",n)}
	if _,err:=db.SQL().ExecContext(ctx,`DROP TRIGGER reject_assurance_journal`);err!=nil{t.Fatal(err)}
	if err:=worker.AssurancePassed(ctx,run.ID,v.ID,cp.ID);err!=nil{
		t.Fatalf("valid atomic assurance completion failed: %v",err)
	}
	cur,err=worker.getRun(ctx,run.ID)
	if err!=nil||cur.Status!=RunSucceeded{t.Fatalf("valid assurance did not close Worker: %+v %v",cur,err)}
	if err:=worker.AssurancePassed(ctx,run.ID,v.ID,cp.ID);err!=nil{t.Fatalf("idempotent assurance rejected: %v",err)}
	if err:=worker.AssurancePassed(ctx,run.ID,"different-verification",cp.ID);!errors.Is(err,ErrInvalidWorkerState){
		t.Fatalf("completed Worker accepted unrelated verification: %v",err)
	}
	if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_steps
	 WHERE run_id=? AND step_kind='verification' AND status='succeeded'`,run.ID).Scan(&n);err!=nil{t.Fatal(err)}
	if n!=1{t.Fatalf("success audit not exactly once: %d",n)}
}
