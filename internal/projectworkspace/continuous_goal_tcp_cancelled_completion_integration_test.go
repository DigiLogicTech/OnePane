//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "net"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type cancelTCPOnDial struct {
 cancel context.CancelFunc
 calls int
}
func(d *cancelTCPOnDial)DialContext(ctx context.Context,network,address string)(net.Conn,error){
 d.calls++
 d.cancel()
 return nil,context.Canceled
}

// Regress the actual manual and recurrent Worker call paths: a user shutdown
// after the claimed request must not lose its durable terminal receipt.
// This creates no external socket and does not claim device health.
func TestCancelledTCPRequestPersistsTerminalEvidenceWithoutRetry(t *testing.T){
 background:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/cancelled-tcp-evidence.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(background);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','owner','active',1,1)`,
 }{
  if _,err=db.SQL().ExecContext(background,q);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 if err:=svc.EnsureTCPRecurringWorkerPrincipal(background);err!=nil{t.Fatal(err)}
 project,err:=svc.CreateProject(background,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Cancelled observer",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(background,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"LAN",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(background,ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"Only these two explicitly approved private targets",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["manual","recurring"]}`),
 })
 if err!=nil{t.Fatal(err)}
 expiry:=time.Now().Add(time.Hour).UnixMilli()
 manual,err:=svc.ApproveGoalTCPProbe(background,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"manual",
  TargetIPv4:"10.9.8.1",TargetPort:443,TimeoutMS:200,
  ExpiresAt:expiry,ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(background,goal.ID);if err!=nil{t.Fatal(err)}
 recurrent,err:=svc.ApproveGoalTCPProbe(background,ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"recurring",
  TargetIPv4:"10.9.8.2",TargetPort:443,TimeoutMS:200,
  ExpiresAt:expiry,ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(background,goal.ID);if err!=nil{t.Fatal(err)}
 grant,err:=svc.ApproveTCPRecurringGrant(background,ApproveTCPRecurringGrantCommand{
  ProjectID:project.ID,GoalID:goal.ID,ManualPermitID:recurrent.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedManualPermitRevision:recurrent.Revision,
  ExpiresAt:expiry,ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(background,goal.ID);if err!=nil{t.Fatal(err)}

 manualCtx,cancelManual:=context.WithCancel(background)
 manualDial:=&cancelTCPOnDial{cancel:cancelManual}
 m,err:=svc.runManualGoalTCPProbe(manualCtx,RunGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,PermitID:manual.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:manual.Revision,
  ActorPrincipalID:"owner",
 },manualDial)
 if err!=nil||m.Status!="cancelled"||m.CompletedAt==0||manualDial.calls!=1{
  t.Fatalf("cancelled manual handshake has no terminal receipt: %+v err=%v calls=%d",m,err,manualDial.calls)
 }

 recurringCtx,cancelRecurring:=context.WithCancel(background)
 recurringDial:=&cancelTCPOnDial{cancel:cancelRecurring}
 r,err:=svc.runRecurringGoalTCPProbe(recurringCtx,dueTCPRecurringGrant{
  projectID:project.ID,goalID:goal.ID,
  grantID:grant.ID,manualPermitID:recurrent.ID,
  goalRevision:goal.Revision,permitRevision:recurrent.Revision,
 },recurringDial)
 if err!=nil||r.Status!="cancelled"||r.CompletedAt==0||recurringDial.calls!=1{
  t.Fatalf("cancelled scheduled handshake has no terminal receipt: %+v err=%v calls=%d",r,err,recurringDial.calls)
 }
 for _,attempt:=range []GoalTCPProbeAttempt{m,r}{
  if _,err:=svc.completeTCPAttempt(background,attempt,"connected");!errors.Is(err,ErrInvalidTransition){
   t.Fatalf("claimed cadence was replayed after cancel: %v",err)
  }
  var completed,unfinished int
  err:=db.SQL().QueryRowContext(background,`SELECT
    COUNT(CASE WHEN event_type='project.goal_tcp_probe_finished' THEN 1 END),
    COUNT(CASE WHEN event_type='project.goal_tcp_probe_claimed' THEN 1 END)
    FROM events WHERE aggregate_type='project_goal_tcp_probe_attempt' AND aggregate_id=?`,
   attempt.ID).Scan(&completed,&unfinished)
  if err!=nil||completed!=1||unfinished!=1{
   t.Fatalf("failed to retain one immutable claim+finish event: got %d,%d err=%v",unfinished,completed,err)
  }
 }
 var claimed int
 if err:=db.SQL().QueryRowContext(background,`SELECT COUNT(*) FROM project_goal_tcp_probe_attempts
   WHERE goal_id=? AND status='claimed'`,goal.ID).Scan(&claimed);err!=nil||claimed!=0{
  t.Fatalf("permanently claimed attempts after cancellation: %d %v",claimed,err)
 }
 // Even a restarted worker cannot consume the same source/window twice.
 restarted:=NewService(db.SQL(),db,clock.Real{})
 probe:=&fakeGoalTCPDialer{}
 none,err:=restarted.tickDueTCPRecurringProbes(background,1,probe)
 if err!=nil||len(none)!=0||probe.calls!=0{
  t.Fatalf("cancelled cadence unexpectedly redialled: attempts=%d calls=%d err=%v",len(none),probe.calls,err)
 }
}
