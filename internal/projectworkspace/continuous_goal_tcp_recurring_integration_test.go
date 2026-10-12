//go:build integration

package projectworkspace

import (
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestRecurringTCPConsentRequiresSeparateHumanGrantAndIsImmutable(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-recurring-consent.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Worker','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,stmt);err!=nil{t.Fatal(err)}}
 s:=NewService(db.SQL(),db,clock.Real{})
 p,err:=s.CreateProject(ctx,CreateProjectCommand{WorkspaceID:"tenant",
  Name:"Bounded recurrence",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=s.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Observation",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 goal,err:=s.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"operator",
  Objective:"Periodic read-only TCP handshake",
  CadenceMinutes:15,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 permit,err:=s.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.3.11",TargetPort:443,TimeoutMS:300,
  ExpiresAt:now+23*60*60*1000,
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=s.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 c:=ApproveTCPRecurringConsentCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedPermitRevision:permit.Revision,
  ExpiresAt:now+60*60*1000,ActorPrincipalID:"operator",
 }
 bad:=c;bad.ActorPrincipalID="robot"
 if _,err:=s.ApproveTCPRecurringConsent(ctx,bad);!errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent self-approved recurring network traffic: %v",err)
 }
 bad=c;bad.ExpiresAt=permit.ExpiresAt+1
 if _,err:=s.ApproveTCPRecurringConsent(ctx,bad);err==nil{
  t.Fatal("recurring TCP consent expanded beyond underlying exact target permit")
 }
 bad=c;bad.ExpectedGoalRevision++
 if _,err:=s.ApproveTCPRecurringConsent(ctx,bad);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale Goal revision admitted recurring network scope: %v",err)
 }
 approved,err:=s.ApproveTCPRecurringConsent(ctx,c)
 if err!=nil{t.Fatal(err)}
 if approved.Status!="approved"||approved.EffectClass!="network_tcp_connect_recurring"||
  approved.TargetIPv4!=permit.TargetIPv4||approved.TargetPort!=permit.TargetPort||
  approved.TimeoutMS!=permit.TimeoutMS||approved.CadenceMinutes!=15||
  approved.ExpiresAt>permit.ExpiresAt{
  t.Fatalf("human recurring grant lost exact scope: %+v",approved)
 }
 goal,err=s.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 c.ExpectedGoalRevision=goal.Revision
 if _,err:=s.ApproveTCPRecurringConsent(ctx,c);!errors.Is(err,ErrInvalidTransition){
  t.Fatalf("duplicate active recurring grant accepted: %v",err)
 }
 for _,update:=range []string{
  "UPDATE project_goal_tcp_recurring_consents SET target_ipv4='192.168.3.12' WHERE id=?",
  "UPDATE project_goal_tcp_recurring_consents SET target_port=22 WHERE id=?",
  "UPDATE project_goal_tcp_recurring_consents SET expires_at=expires_at+60000 WHERE id=?",
  "UPDATE project_goal_tcp_recurring_consents SET cadence_minutes=15 WHERE id=?",
 }{
  if _,err:=db.SQL().ExecContext(ctx,update,approved.ID);err==nil{
   t.Fatalf("existing recurring grant mutated: %s",update)
  }
 }
 consents,err:=s.ListTCPRecurringConsents(ctx,p.ID,goal.ID)
 if err!=nil||len(consents)!=1||consents[0].ID!=approved.ID{
  t.Fatalf("recurring grant list missing: %+v %v",consents,err)
 }
 stop:=RevokeTCPRecurringConsentCommand{
  ProjectID:p.ID,GoalID:goal.ID,ConsentID:approved.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedConsentRevision:approved.Revision,
  ActorPrincipalID:"operator",
 }
 agent:=stop;agent.ActorPrincipalID="robot"
 if _,err:=s.RevokeTCPRecurringConsent(ctx,agent);!errors.Is(err,ErrPrincipalIneligible){
  t.Fatalf("Agent revoked recurring grant: %v",err)
 }
 revoked,err:=s.RevokeTCPRecurringConsent(ctx,stop)
 if err!=nil||revoked.Status!="revoked"||revoked.Revision!=approved.Revision+1{
  t.Fatalf("recurring revocation not persisted: %+v %v",revoked,err)
 }
 if _,err:=s.RevokeTCPRecurringConsent(ctx,stop);!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale recurring revoke replay accepted: %v",err)
 }
 if _,err:=db.SQL().ExecContext(ctx,
  "DELETE FROM project_goal_tcp_recurring_consents WHERE id=?",approved.ID);err==nil{
  t.Fatal("recurring consent audit deleted")
 }
 current,err:=s.ContinuousGoal(ctx,goal.ID);if err!=nil{t.Fatal(err)}
 if current.Health!="unobserved"||current.Status!="configured"||
  current.LastCheckedAt!=0||current.LastVerifiedAt!=0||current.NextDueAt!=0{
  t.Fatalf("recurring consent incorrectly marked goal protected: %+v",current)
 }
 var attempts int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_goal_tcp_probe_attempts").Scan(&attempts);err!=nil||attempts!=0{
  t.Fatalf("consent triggered unauthorised sockets: %d %v",attempts,err)
 }
}
