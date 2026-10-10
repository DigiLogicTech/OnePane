package projectworkspace

import (
 "strings"
 "testing"
)

func TestResearchSeatIntentRejectsMixedOrPartiallyApprovedCouncils(t *testing.T){
 safe:=[]byte(`{"research_mode":true,"execution_mode":"council","members":[
  {"id":"localA","member_kind":"agent","status":"active","config":{}},
  {"id":"localB","member_kind":"agent","status":"active","config":{}},
  {"id":"disabled","member_kind":"agent","status":"disabled","config":{}}
 ]}`)
 if !frozenResearchSeatsEligible(safe,[]string{"localA","localB"}){
  t.Fatal("fully covered Research Council denied")
 }
 for _,ids:=range [][]string{{"localA"},{"localB"},{"localA","disabled"},
  {"localA","localB","unknown"}}{
  if frozenResearchSeatsEligible(safe,ids){
   t.Fatalf("partial or foreign Research seat approval accepted: %q",ids)
  }
 }
 for _,tc:=range []struct{name,cfg string}{
  {"manual_web",`{"manual_web":{"enabled":true}}`},
  {"chair",`{"council_chair_only":true}`},
 }{
  sample:=[]byte(`{"research_mode":true,"execution_mode":"council","members":[
   {"id":"localA","member_kind":"agent","status":"active","config":{}},
   {"id":"localB","member_kind":"agent","status":"active","config":`+tc.cfg+`}
  ]}`)
  if frozenResearchSeatsEligible(sample,[]string{"localA","localB"}){
   t.Fatalf("%s could receive private Research critique outputs",tc.name)
  }
 }
 for _,invalid:=range []string{
  `{"research_mode":false,"execution_mode":"council","members":[]}`,
  `{"research_mode":true,"execution_mode":"team","members":[]}`,
  `{"research_mode":true,"execution_mode":"council","research":{"chair_mode":"manual"},"members":[{"id":"a","member_kind":"agent","status":"active","config":{}}]}`,
  `not JSON`,
 }{
  if frozenResearchSeatsEligible([]byte(invalid),[]string{"a"}){
   t.Fatalf("invalid Research snapshot accepted: %s",invalid)
  }
 }
}

func TestCanonicalResearchSeatIntentIsBoundedUniqueAndDeterministic(t *testing.T){
 ids,err:=canonicalResearchSeats([]string{"b","a"})
 if err!=nil||len(ids)!=2||ids[0]!="a"||ids[1]!="b"{
  t.Fatalf("canonical seat IDs not stable: %q %v",ids,err)
 }
 for _,input:=range [][]string{
  nil,{},{"duplicate","duplicate"},{" leading"},{"trailing "},
  {strings.Repeat("x",129)},
  {"1","2","3","4","5","6","7","8","9"},
 }{
  if _,err:=canonicalResearchSeats(input);err==nil{
   t.Fatalf("unsafe seat list accepted: %q",input)
  }
 }
}

func TestResearchSeatTextIntentRequiresSmallApprovedText(t *testing.T){
 valid:=[]WorkspaceEvidenceEntry{{
  AssetID:"asset",Version:1,ContentHash:"sha256:hash",
  SizeBytes:64<<10,MIMEType:"text/markdown",
 }}
 if !researchTextSelectionBounded(valid){t.Fatal("small text was rejected")}
 for _,tc:=range []struct{name string;entries []WorkspaceEvidenceEntry}{
  {"binary",[]WorkspaceEvidenceEntry{{SizeBytes:10,MIMEType:"application/pdf"}}},
  {"negative",[]WorkspaceEvidenceEntry{{SizeBytes:-1,MIMEType:"text/plain"}}},
  {"oversized",[]WorkspaceEvidenceEntry{{SizeBytes:(64<<10)+1,MIMEType:"text/plain"}}},
  {"nine",make([]WorkspaceEvidenceEntry,9)},
 }{
  if researchTextSelectionBounded(tc.entries){
   t.Fatalf("%s Research selection incorrectly eligible",tc.name)
  }
 }
}
