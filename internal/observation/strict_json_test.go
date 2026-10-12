package observation

import (
 "encoding/json"
 "strings"
 "testing"
)

func TestObservationJSONStrictlyRejectsDuplicateAndAmbiguousFields(t *testing.T){
 cases:=[]struct{name,raw string;valid bool}{
  {"simple object",`{"status":"ok","nested":{"up":true}}`,true},
  {"array of objects",`[{"id":1},{"id":2}]`,true},
  {"scalar",`false`,true},
  {"duplicate root",`{"status":"ok","status":"spoof"}`,false},
  {"unicode equivalent duplicate",`{"source_id":"ok","source\\u005fid":"spoof"}`,false},
  {"nested duplicate",`{"data":[{"ok":true,"ok":false}]}`,false},
  {"multiple roots",`{"ok":true}{"ok":false}`,false},
  {"empty",``,false},
  {"too deeply nested",strings.Repeat("[",66)+"0"+strings.Repeat("]",66),false},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   err:=validateUnambiguousObservationJSON(json.RawMessage(tc.raw))
   if (err==nil)!=tc.valid{t.Fatalf("valid=%v err=%v",tc.valid,err)}
  })
 }
 secret:=`{"SUPERSECRET_SOURCE_FIELD":"a","SUPERSECRET_SOURCE_FIELD":"b"}`
 if err:=validateUnambiguousObservationJSON(json.RawMessage(secret));
  err==nil||strings.Contains(err.Error(),"SUPERSECRET_SOURCE_FIELD"){
  t.Fatalf("ambiguous input was admitted or secret key name leaked: %v",err)
 }
}
