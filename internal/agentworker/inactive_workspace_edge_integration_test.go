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

func TestWorkerRestartRejectsStaleSystemMembershipOfSuspendedWorkspace(t *testing.T) {
	ctx,db,tasks,_,ready:=setupAdmissionFixture(t)
	if _,err:=db.SQL().ExecContext(ctx,`UPDATE workspaces SET status='suspended' WHERE id='ws'`);err!=nil{t.Fatal(err)}
	// Recreated service models a backend restart; active membership rows persist.
	fresh:=New(db.SQL(),db,clock.Real{},"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
	run,res:=fresh.startRun(ctx,ready)
	if run.ID!=""||!strings.Contains(res.Error,"state=suspended"){
		t.Fatalf("restart reused stale active membership to admit Worker: %+v %+v",run,res)
	}
	assertAdmissionUnchanged(t,ctx,db,tasks,ready)
}

func TestMissingWorkspaceDoesNotCreateOrReuseWorkerMemberships(t *testing.T) {
	ctx:=context.Background()
	db,err:=sqlite.Open(filepath.Join(t.TempDir(),"absent-worker-workspace.db"))
	if err!=nil{t.Fatal(err)}
	defer db.Close()
	if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
	tasks:=task.NewService(db.SQL(),db,clock.Real{})
	svc:=New(db.SQL(),db,clock.Real{},"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
	if err:=svc.ensureWorkspaceAccess(ctx,"absent");err==nil{
		t.Fatal("missing Workspace granted system membership")
	}
	var memberships int
	if err:=db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id='absent'`).Scan(&memberships);err!=nil{t.Fatal(err)}
	if memberships!=0{t.Fatalf("created %d memberships for missing Workspace",memberships)}
}
