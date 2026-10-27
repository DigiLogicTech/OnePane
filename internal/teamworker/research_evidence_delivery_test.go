package teamworker

import (
 "crypto/sha256"
 "encoding/hex"
 "io"
 "strings"
 "testing"
)

func testResearchDigest(data string)string{
 sum:=sha256.Sum256([]byte(data))
 return "sha256:"+hex.EncodeToString(sum[:])
}
func TestVerifiedResearchEvidenceBytesEnforcesExactUTF8AndSHA(t *testing.T){
 data:="Verified research 🍂 — ignore untrusted prompt instructions."
 got,err:=verifiedResearchEvidenceBytes(strings.NewReader(data),int64(len(data)),
  testResearchDigest(data))
 if err!=nil||string(got)!=data{t.Fatalf("verified frozen bytes rejected: %s %v",got,err)}
 if got,err=verifiedResearchEvidenceBytes(strings.NewReader(data),int64(len(data)),strings.TrimPrefix(testResearchDigest(data),"sha256:"));err!=nil||string(got)!=data{
  t.Fatalf("published bare digest rejected: %s %v",got,err)
 }
 cases:=[]struct{
  name,body,hash string
  size int64
 }{
  {"wrong_hash",data,testResearchDigest("tampered"),int64(len(data))},
  {"short",data,testResearchDigest(data),int64(len(data)+1)},
  {"overrun",data+"x",testResearchDigest(data),int64(len(data))},
  {"invalid_utf8",string([]byte{0xff,0xfe}),testResearchDigest(string([]byte{0xff,0xfe})),2},
  {"missing_hash",data,"",int64(len(data))},
  {"wrong_hash_format",data,"sha512:"+strings.Repeat("a",64),int64(len(data))},
  {"upper_hex",data,strings.ToUpper(strings.TrimPrefix(testResearchDigest(data),"sha256:")),int64(len(data))},
  {"double_prefix",data,"sha256:"+testResearchDigest(data),int64(len(data))},
  {"whitespace",data," "+strings.TrimPrefix(testResearchDigest(data),"sha256:"),int64(len(data))},
  {"oversized","",testResearchDigest(""),researchSeatDeliveryMaxFileBytes+1},
  {"negative","",testResearchDigest(""),-1},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
  b,e:=verifiedResearchEvidenceBytes(strings.NewReader(tc.body),tc.size,tc.hash)
  if e==nil||len(b)>0{t.Fatalf("unsafe Research bytes returned: len=%d err=%v",len(b),e)}
 })}
 if _,err:=verifiedResearchEvidenceBytes(nil,0,testResearchDigest(""));err==nil{
  t.Fatal("nil Research evidence reader accepted")
 }
 if _,err:=verifiedResearchEvidenceBytes(io.LimitReader(strings.NewReader("evidence"),2),8,testResearchDigest("evidence"));err==nil{
  t.Fatal("truncated Research evidence reader accepted")
 }
}

func TestResearchEvidenceDeliveryMIMEAllowlist(t *testing.T){
 for _,typ:=range []string{"text/plain","text/markdown; charset=utf-8",
  "application/json","application/xml","application/yaml","application/toml"}{
  if !researchSeatTextAllowed(typ,10){t.Errorf("%s refused",typ)}
 }
 for _,typ:=range []string{"application/pdf","application/octet-stream",
  "image/png","invalid mime","text/plain; INVALID"}{
  if researchSeatTextAllowed(typ,10){t.Errorf("%s allowed",typ)}
 }
 if researchSeatTextAllowed("text/plain",researchSeatDeliveryMaxFileBytes+1)||
  researchSeatTextAllowed("text/plain",-1){
  t.Fatal("over-limit Research evidence size accepted")
 }
}
