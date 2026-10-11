//go:build integration

package assurance

import (
 "context"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
 "github.com/DigiLogicTech/OnePane/internal/verification"
)

// One corrupt or unsupported independent verification must not prevent a
// different Task's previously resolved verification from finalizing after
// restart. Return BOTH completed results and the actionable per-ID error.
func TestAssuranceTickDoesNotStarveIndependentResolvedVerification(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"independent-assurance.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO principals(
  id,principal_type,display_name,status,revision,created_at,updated_at)
  VALUES(?,'system','Assurance Verifier','active',1,?,?)`,
  VerifierPrincipal,now,now);err!=nil{t.Fatal(err)}
 spec:=`{"assurance_version":1,"source":"integration-test","proposed_result":{"ok":true},"criteria":{"result_checks":[{"pointer":"/ok","operator":"equals","value":true}]},"evidence":{}}`
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO verifications(
  id,workspace_id,subject_ref,required_level,status,spec_json,started_at,revision)
  VALUES('bad','tenant','bad-evidence','V0','pending',?,?,1)`,spec,now-2);err!=nil{t.Fatal(err)}
 // More than one bad sibling must preserve both actionable IDs.
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO verifications(
  id,workspace_id,subject_ref,required_level,status,spec_json,started_at,revision)
  VALUES('bad-2','tenant','other-bad-evidence','V0','pending',?,?,1)`,spec,now-2);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO verifications(
  id,workspace_id,subject_ref,required_level,achieved_level,status,spec_json,
  result_json,verified_by,started_at,completed_at,revision)
  VALUES('good','tenant','good-evidence','V1','V1','pass',?,?,?,?,?,2)`,
  spec,`{"checks":[{"kind":"result_json","status":"pass"}]}`,
  VerifierPrincipal,now-1,now);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO assurance_runs(
  id,verification_id,workspace_id,status,required_level,achieved_level,
  result_json,revision,started_at,updated_at)
  VALUES('good-run','good','tenant','queued','V1','V1','{}',1,?,?)`,
  now,now);err!=nil{t.Fatal(err)}
 clk:=clock.Real{}
 svc:=New(db.SQL(),db,clk,
  verification.NewService(db.SQL(),db,clk),
  task.NewService(db.SQL(),db,clk),
  artifact.NewService(db.SQL(),db,nil,clk),
  observation.NewService(db.SQL(),db,clk),nil)
 completed,problem:=svc.Tick(ctx,10)
 if problem==nil||(!strings.Contains(problem.Error(),"bad")||!strings.Contains(problem.Error(),"bad-2"))||
  len(completed)!=1||completed[0].VerificationID!="good"||
  completed[0].Status!=RunPassed{
  t.Fatalf("corrupt earlier verification starved valid later verification: completed=%+v error=%v",completed,problem)
 }
 var goodStatus,badStatus string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM assurance_runs
  WHERE verification_id='good'`).Scan(&goodStatus);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM verifications
  WHERE id='bad'`).Scan(&badStatus);err!=nil{t.Fatal(err)}
 if goodStatus!=string(RunPassed)||badStatus!=string(verification.StatusPending){
  t.Fatalf("independent evaluation changed wrong status good=%s bad=%s",goodStatus,badStatus)
 }
 completed,problem=svc.Tick(ctx,10)
 if problem==nil||len(completed)!=0||!strings.Contains(problem.Error(),"bad"){
  t.Fatalf("repeat Tick silently accepted invalid verification or duplicated good: %+v %v",completed,problem)
 }
}
