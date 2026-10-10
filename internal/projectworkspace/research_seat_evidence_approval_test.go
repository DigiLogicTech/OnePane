package projectworkspace

import (
 "strings"
 "testing"
)

func TestResearchSeatIntentNeverAuthorizesUnfrozenOrManualSeats(t *testing.T){
 snapshot:=[]byte(`{"members":[
  {"id":"local","member_kind":"agent","status":"active","config":{}},
  {"id":"web","member_kind":"agent","status":"active","config":{"manual_web":{"enabled":true,"provider_id":"chatgpt"}}},
  {"id":"chair","member_kind":"agent","status":"active","config":{"council_chair_only":true}},
  {"id":"human","member_kind":"human","status":"active","config":{}},
  {"id":"suspended","member_kind":"agent","status":"disabled","config":{}},
  {"id":"supervisor","member_kind":"supervisor","status":"active","config":{}}
 ]}`)
 if !frozenResearchSeatsEligible(snapshot,[]string{"local"}){
  t.Fatal("active frozen local agent was rejected")
 }
 for _,seat:=range []string{"web","chair","human","suspended","supervisor","unknown"}{
  if frozenResearchSeatsEligible(snapshot,[]string{seat}){
   t.Fatalf("inadmissible frozen Research seat %q was approved",seat)
  }
  if frozenResearchSeatsEligible(snapshot,[]string{"local",seat}){
   t.Fatalf("mixed Research seat selection admitted %q",seat)
  }
 }
 if frozenResearchSeatsEligible([]byte("not JSON"),[]string{"local"}){
  t.Fatal("corrupt snapshot permitted sharing")
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
