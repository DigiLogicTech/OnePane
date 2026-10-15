package projectworkspace

import "testing"

// A real Ed25519 signer can issue multiple valid but contradictory
// self-reports. Counting signatures cannot create corroborating observers.
func TestSummarizeInstrumentSelfReportsNeverCountsIndependentVantages(t *testing.T){
 for _,tc:=range []struct{
  name string
  claims []GoalIncidentSignedEndpointClaim
  consistency string
  keys int
 }{
  {"empty",nil,"no_signed_claims",0},
  {"single",[]GoalIncidentSignedEndpointClaim{{InstrumentKeyID:"a",Outcome:"connected"}},"consistent_unverified_self_reports",1},
  {"same key consistent",[]GoalIncidentSignedEndpointClaim{{InstrumentKeyID:"a",Outcome:"connected"},{InstrumentKeyID:"a",Outcome:"connected"}},"consistent_unverified_self_reports",1},
  {"same key conflicts",[]GoalIncidentSignedEndpointClaim{{InstrumentKeyID:"a",Outcome:"connected"},{InstrumentKeyID:"a",Outcome:"not_connected"}},"conflicting_unverified_self_reports",1},
  {"distinct keys conflict",[]GoalIncidentSignedEndpointClaim{{InstrumentKeyID:"a",Outcome:"connected"},{InstrumentKeyID:"b",Outcome:"timed_out"}},"conflicting_unverified_self_reports",2},
  {"distinct keys still not attested",[]GoalIncidentSignedEndpointClaim{{InstrumentKeyID:"a",Outcome:"connected"},{InstrumentKeyID:"b",Outcome:"connected"}},"consistent_unverified_self_reports",2},
 }{
  t.Run(tc.name,func(t *testing.T){
   consistency,keys:=summarizeInstrumentSelfReports(tc.claims)
   if consistency!=tc.consistency||keys!=tc.keys{
    t.Fatalf("got (%s,%d) want (%s,%d)",consistency,keys,tc.consistency,tc.keys)
   }
  })
 }
}
