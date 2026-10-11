package projectorchestrator

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
)

type orchestratorRouteCapture struct {
 calls int
 request scheduler.RouteRequest
}
func (m *orchestratorRouteCapture) Route(_ context.Context,req scheduler.RouteRequest)(scheduler.Decision,error){
 m.calls++
 m.request=req
 return scheduler.Decision{},scheduler.ErrNoEligibleCandidate
}

func TestProjectOrchestratorNeverSendsProjectSnapshotToUnapprovedRemoteOrUntestedModel(t *testing.T){
 recorder:=&orchestratorRouteCapture{}
 svc:=&Service{scheduler:recorder}
 _,_,_,err:=svc.reason(context.Background(),"tenant","operator",
  "inspect my Project",json.RawMessage(`{"private":"project-context"}`))
 if err==nil{t.Fatal("missing local qualified model should block reasoning")}
 if recorder.calls!=1 {t.Fatalf("unexpected route count %d",recorder.calls)}
 req:=recorder.request
 if !req.LocalOnly||!req.RequireZeroIncrementalCost||req.AllowUntested||
  req.AllowSubscriptionUsage||req.AllowPotentialMonetarySpend||
  !req.AllowLimited{
  t.Fatalf("unsafe route admitted for private Project context: %+v",req)
 }
 if req.WorkspaceID!="tenant"||req.DataLabel.WorkspaceID!="tenant"||
  req.DataLabel.Confidentiality!=policy.ConfidentialityInternal{
  t.Fatalf("Project context routing escaped tenant and confidentiality: %+v",req)
 }
 if len(req.AllowedKinds)!=1||req.AllowedKinds[0]!=scheduler.CandidateModel||
  req.CapabilityID!="inference.general"||req.RoleName!="project-orchestrator"||
  req.ProtocolLevel!="L1"{
  t.Fatalf("unexpected Project reasoning route: %+v",req)
 }
}
