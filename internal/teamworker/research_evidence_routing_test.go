package teamworker

import (
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
)

func TestResearchEvidenceRoutingOnlyAllowsOriginModel(t *testing.T){
 local:=scheduler.Candidate{ID:"local",Kind:scheduler.CandidateModel,Local:true,
  CostClass:scheduler.CostLocal,HardZeroIncrementalCost:true,
  NodeID:"origin-node",ComputeMode:"cpu",
  Destination:policy.FlowDestination{Kind:policy.DestinationOriginNode}}
 if !researchEvidenceCandidateAllowed(local){t.Fatal("origin model rejected")}
 cases:=[]struct{name string;mutate func(*scheduler.Candidate)}{
  {"remote",func(c *scheduler.Candidate){c.ComputeMode="remote"}},
  {"cloud_compute",func(c *scheduler.Candidate){c.ComputeMode="cloud"}},
  {"cloud_provider",func(c *scheduler.Candidate){c.Provider="cloud"}},
  {"not_local",func(c *scheduler.Candidate){c.Local=false}},
  {"paid",func(c *scheduler.Candidate){c.CostClass=scheduler.CostPaid}},
  {"free_but_network",func(c *scheduler.Candidate){c.CostClass=scheduler.CostFree}},
  {"nonhard_cost",func(c *scheduler.Candidate){c.HardZeroIncrementalCost=false}},
  {"remote_node",func(c *scheduler.Candidate){c.Destination.Kind=policy.DestinationTrustedNode}},
  {"cloud_destination",func(c *scheduler.Candidate){c.Destination.Kind=policy.DestinationCloud}},
  {"runtime",func(c *scheduler.Candidate){c.Kind=scheduler.CandidateAgentRuntime}},
  {"unknown_node",func(c *scheduler.Candidate){c.NodeID=""}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){c:=local;tc.mutate(&c)
   if researchEvidenceCandidateAllowed(c){t.Fatal("nonlocal evidence destination accepted")}
  })
 }
 for _,mode:=range []string{"gpu","hybrid"}{
  c:=local;c.ComputeMode=mode
  if !researchEvidenceCandidateAllowed(c){t.Fatalf("origin %s model rejected",mode)}
 }
}

func TestResearchEvidenceRouteFenceOverridesCloudAndPaidPolicy(t *testing.T){
 route:=scheduler.RouteRequest{
  AllowSubscriptionUsage:true,AllowPotentialMonetarySpend:true,
  LocalOnly:false,RequireZeroIncrementalCost:false}
 applyResearchEvidenceRouteFence(&route)
 if !route.LocalOnly||!route.RequireZeroIncrementalCost||
  route.AllowPotentialMonetarySpend||route.AllowSubscriptionUsage||
  !route.PreferZeroIncrementalCost||len(route.AllowedKinds)!=1||
  route.AllowedKinds[0]!=scheduler.CandidateModel{
  t.Fatalf("evidence sharing did not fence risky routes: %+v",route)
 }
}
