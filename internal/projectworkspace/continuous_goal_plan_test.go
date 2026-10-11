package projectworkspace

import (
 "encoding/json"
 "strings"
 "testing"
)

func TestL5MonitoringPlanRejectsAmbiguousSourceAllowlists(t *testing.T){
 cases:=[]struct{name string; raw string}{
  {"duplicate sources",`{"version":1,"source_ids":["dns"],"source_ids":["uptime"]}`},
  {"escaped duplicate sources",`{"version":1,"source_ids":["dns"],"source\u005fids":["uptime"]}`},
  {"duplicate version",`{"version":1,"version":2,"source_ids":["dns"]}`},
  {"trailing object",`{"version":1,"source_ids":[]}{"version":1}`},
  {"unknown authority",`{"version":1,"source_ids":["dns"],"network":true}`},
  {"repeated source",`{"version":1,"source_ids":["dns","dns"]}`},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   if _,err:=canonicalGoalPlan(json.RawMessage(tc.raw));err==nil{
    t.Fatalf("ambiguous or expanded monitoring plan was accepted: %s",tc.raw)
   }
  })
 }
 if _,err:=canonicalGoalPlan(json.RawMessage(strings.Repeat(" ",8*1024+1)));err==nil{
  t.Fatal("oversized monitoring plan accepted")
 }
}

func TestL5MonitoringPlanCanonicalisesOnlyAuthorisedSources(t *testing.T){
 raw:=json.RawMessage(`{"source_ids":["dns","uptime"],"version":1}`)
 plan,err:=canonicalGoalPlan(raw)
 if err!=nil{t.Fatal(err)}
 var decoded goalMonitorPlan
 if err=json.Unmarshal(plan,&decoded);err!=nil{t.Fatal(err)}
 if decoded.Version!=1||len(decoded.SourceIDs)!=2||
  decoded.SourceIDs[0]!="dns"||decoded.SourceIDs[1]!="uptime"{
  t.Fatalf("approved monitoring allowlist changed: %s",plan)
 }
 empty,err:=canonicalGoalPlan(nil)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(empty),"network")||strings.Contains(string(empty),"secrets"){
  t.Fatalf("default monitoring plan introduced capabilities: %s",empty)
 }
}
