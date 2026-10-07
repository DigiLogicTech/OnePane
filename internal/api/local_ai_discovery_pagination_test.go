package api

import (
 "encoding/base64"
 "net/http"
 "net/url"
 "strings"
 "testing"
)

func TestDiscoveryHuggingFacePaginationCursor(t *testing.T) {
 h:=http.Header{}
 h.Set("Link", `<https://huggingface.co/api/models?limit=40&cursor=opaque%2Btoken>; rel="next"`)
 if got:=nextHFCursor(h);got!="opaque+token"{t.Fatalf("unexpected next cursor: %q",got)}
 h.Set("Link", `<https://evil.example/api/models?cursor=untrusted>; rel="next"`)
 if got:=nextHFCursor(h);got!=""{t.Fatalf("must reject untrusted next-link origin: %q",got)}
 h.Del("Link")
 if got:=nextHFCursor(h);got!=""{t.Fatalf("end of source must have no cursor")}
}
func TestDiscoveryCompositeCursorRoundTrip(t *testing.T) {
 state:=map[string]string{"huggingface":"opaque:token","huggingbay":"80","llmfit":"-"}
 raw:=encodeDiscoveryCursor(state)
 if _,err:=base64.RawURLEncoding.DecodeString(raw);err!=nil{t.Fatal(err)}
 decoded,err:=decodeDiscoveryCursor(raw)
 if err!=nil{t.Fatal(err)}
 for key,want:=range state{if decoded[key]!=want{t.Fatalf("%s: %q != %q",key,decoded[key],want)}}
 for _,bad:=range []string{"not a cursor",strings.Repeat("x",8193)}{
   if _,err:=decodeDiscoveryCursor(bad);err==nil{t.Fatalf("accepted invalid cursor: %q",bad[:min(20,len(bad))])}
 }
}
func TestDiscoveryOffsetBounds(t *testing.T){
 for _,raw:=range []string{"-1","nan","1000001"}{if _,err:=discoveryOffset(raw);err==nil{t.Fatalf("accepted %q",raw)}}
 n,err:=discoveryOffset("123");if err!=nil||n!=123{t.Fatalf("offset: %d %v",n,err)}
 _=url.Values{}
}
