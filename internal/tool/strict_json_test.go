package tool

import (
 "encoding/json"
 "strings"
 "testing"
)

func TestToolCanonicalJSONRejectsConflictingKeysBeforeAuthority(t *testing.T){
 invalid:=[]struct{name,raw string}{
  {"duplicate top level", "{\"mode\":\"read\",\"mode\":\"execute\"}"},
  {"escaped same key", "{\"action\":1,\"\\u0061ction\":2}"},
  {"nested duplicate", "{\"scope\":{\"workspace\":\"one\",\"workspace\":\"two\"}}"},
  {"duplicate inside array", "[{\"path\":\"a\",\"path\":\"b\"}]"},
  {"multiple roots", "{\"safe\":true} {\"unsafe\":true}"},
  {"trailing garbage", "{\"safe\":true} garbage"},
  {"not closed", "{\"path\":1"},
  {"very deep",strings.Repeat("[",66)+"0"+strings.Repeat("]",66)},
 }
 for _,tc:=range invalid{
  t.Run(tc.name,func(t *testing.T){
   if canonical,err:=canonicalJSON(json.RawMessage(tc.raw));err==nil{
    t.Fatalf("ambiguous command was canonicalized as %s",canonical)
   }
  })
 }
 valid:=[]struct{input,expected string}{
  {"{\"z\":1,\"a\":2}","{\"a\":2,\"z\":1}"},
  {"{\"outer\":{\"z\":1,\"a\":2}}","{\"outer\":{\"a\":2,\"z\":1}}"},
  {"[1,true,null,\"hello\"]","[1,true,null,\"hello\"]"},
  {"{\"value\":1.2300001}","{\"value\":1.2300001}"},
  {"null","null"},
 }
 for _,tc:=range valid{
  normalized,err:=canonicalJSON(json.RawMessage(tc.input))
  if err!=nil||string(normalized)!=tc.expected{
   t.Fatalf("valid JSON not preserved: %q => %q error=%v want=%q",
    tc.input,normalized,err,tc.expected)
  }
 }
}

// Inputs may include substantial base64 text for bounded Workspace file
// operations. Duplicate detection must not impose an arbitrary 64KiB body
// cap or require a second full-memory JSON decoding pass.
func TestToolCanonicalJSONRetainsLargeSingleValue(t *testing.T){
 raw:=json.RawMessage("{\"content\":\""+strings.Repeat("A",128*1024)+"\"}")
 canonical,err:=canonicalJSON(raw)
 if err!=nil||len(canonical)!=len(raw){
  t.Fatalf("legitimate large Tool payload denied: bytes=%d error=%v",len(canonical),err)
 }
}
