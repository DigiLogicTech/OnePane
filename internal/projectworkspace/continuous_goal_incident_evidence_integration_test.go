//go:build integration

package projectworkspace

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/json"
 "strings"
 "testing"
 "time"

 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// Uses actual approved permits, actual source-key signing and real SQLite
// admission. No outbound network: the original TCP attempt uses fake dialer.
func TestIncidentEvidenceAssociatesOnlyHumanDeclaredHistoricalEndpoint(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/incident-signed-correlation.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 clk:=&mutableAlertTestClock{now:time.Now().UTC().Truncate(time.Second)}
 svc:=NewService(db.SQL(),db,clk)
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Historical instrument correlation",CreatedBy:"human",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Local",ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 g,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"human",
  Objective:"Review operator-declared endpoint only",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 permit,err:=svc.ApproveGoalTCPProbe(ctx,ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,SourceID:"router",
  TargetIPv4:"192.168.16.9",TargetPort:8443,TimeoutMS:300,
  ExpiresAt:clk.UnixMilli()+2*60*60*1000,
  ExpectedGoalRevision:g.Revision,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 _,err=svc.SetGoalTCPBaseline(ctx,SetGoalTCPBaselineCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedTCP:"not_connected",
  ExpectedBaselineRevision:0,ExpectedGoalRevision:g.Revision,
 })
 if err!=nil{t.Fatal(err)}
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 declared,err:=svc.DeclareGoalTCPTarget(ctx,DeclareGoalTCPTargetCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:permit.ID,
  DisplayLabel:"Router Uplink",ActorPrincipalID:"human",
  ExpectedGoalRevision:g.Revision,ExpectedPermitRevision:permit.Revision,
  ExpiresAt:clk.UnixMilli()+90*60*1000,
 })
 if err!=nil{t.Fatal(err)}
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 pin,err:=svc.ApproveGoalTLSPin(ctx,ApproveGoalTLSPinCommand{
  ProjectID:p.ID,GoalID:g.ID,DeclaredTargetID:declared.ID,
  CertificateSHA256:"sha256:"+strings.Repeat("a",64),
  ActorPrincipalID:"human",ExpectedGoalRevision:g.Revision,
  ExpectedTargetRevision:declared.Revision,ExpiresAt:clk.UnixMilli()+60*60*1000,
 })
 if err!=nil{t.Fatal(err)}
 publicKey,privateKey,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 key,err:=svc.ApproveGoalInstrumentKey(ctx,ApproveGoalInstrumentKeyCommand{
  ProjectID:p.ID,GoalID:g.ID,TLSPinID:pin.ID,PublicKey:publicKey,
  ActorPrincipalID:"human",ExpectedGoalRevision:g.Revision,
  ExpectedPinRevision:pin.Revision,ExpiresAt:clk.UnixMilli()+45*60*1000,
 })
 if err!=nil{t.Fatal(err)}
 // A declaration created before the TCP sample, not retroactively.
 clk.now=clk.now.Add(2*time.Second)
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 tcp,err:=svc.runManualGoalTCPProbe(ctx,RunGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:g.ID,PermitID:permit.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:g.Revision,
  ExpectedPermitRevision:permit.Revision,
 },&fakeGoalTCPDialer{})
 if err!=nil||tcp.Status!="connected"{t.Fatalf("TCP drift: %+v %v",tcp,err)}
 alerts,err:=svc.ListTCPReachabilityAlerts(ctx,p.ID,g.ID)
 if err!=nil||len(alerts)!=1{t.Fatalf("missing baseline drift: %+v %v",alerts,err)}
 caseReview,err:=svc.OpenGoalIncidentReview(ctx,OpenGoalIncidentReviewCommand{
  ProjectID:p.ID,GoalID:g.ID,TCPAlertID:alerts[0].ID,
  ExpectedAlertRevision:alerts[0].Revision,ActorPrincipalID:"human",
 })
 if err!=nil{t.Fatal(err)}
 before,err:=svc.GoalIncidentEvidence(ctx,p.ID,g.ID,caseReview.ID)
 if err!=nil||before.EndpointAssociation!="not_established"||
  len(before.SignedEndpointClaims)!=0{
  t.Fatalf("unaccepted instrument claim was inferred: %+v %v",before,err)
 }
 // Signed claim via the existing independently scoped source admission.
 clk.now=clk.now.Add(2*time.Second)
 g,err=svc.ContinuousGoal(ctx,g.ID);if err!=nil{t.Fatal(err)}
 challenge,err:=svc.IssueGoalInstrumentChallenge(ctx,IssueGoalInstrumentChallengeCommand{
  ProjectID:p.ID,GoalID:g.ID,KeyID:key.ID,ActorPrincipalID:"human",
  ExpectedGoalRevision:g.Revision,ExpectedKeyRevision:key.Revision,
 })
 if err!=nil{t.Fatal(err)}
 cmd:=SignedMeasurementCommand{
  ProjectID:p.ID,GoalID:g.ID,KeyID:key.ID,ChallengeID:challenge.ID,
  ActorPrincipalID:"human",ExpectedGoalRevision:g.Revision,
  Outcome:"not_connected",ObservedAt:clk.UnixMilli(),
 }
 payload,err:=svc.PrepareGoalInstrumentMeasurement(ctx,cmd)
 if err!=nil{t.Fatal(err)}
 cmd.Signature=ed25519.Sign(privateKey,payload.SigningPayload)
 claim,err:=svc.AcceptGoalInstrumentMeasurement(ctx,cmd)
 if err!=nil{t.Fatal(err)}
 evidence,err:=svc.GoalIncidentEvidence(ctx,p.ID,g.ID,caseReview.ID)
 if err!=nil{t.Fatal(err)}
 if evidence.EndpointAssociation!="historical_operator_declared_same_endpoint"||
  len(evidence.SignedEndpointClaims)!=1||
  evidence.SignedEndpointClaims[0].ClaimID!=claim.ID||
  evidence.SignedEndpointClaims[0].Outcome!="not_connected"||
  evidence.DeviceIdentityVerified||evidence.IncidentConfirmed||
  evidence.IndependentVantageVerified||evidence.RemediationAuthorized||
  evidence.Alert.Status!="open"{
  t.Fatalf("historical bounded evidence misclassified: %+v",evidence)
 }
 // The sensor's self-report of an opposite outcome is retained, not
 // automatically reconciled or promoted above an actual TCP socket record.
 latest,err:=svc.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 if latest.Health!="unobserved"||latest.LastVerifiedAt!=0||latest.NextDueAt!=0{
  t.Fatalf("historical claims activated monitoring: %+v",latest)
 }
 var tasks int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);
  err!=nil||tasks!=0{t.Fatalf("metadata view started Tasks: %d %v",tasks,err)}
}
