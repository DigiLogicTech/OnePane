package localai

import (
 "io"
 "net/http"
 "strings"
 "testing"
)

func TestOmniRouteHealthResponseContract(t *testing.T) {
 cases:=[]struct{name string; code int; body string; want bool}{
  {"ready",200,"ok\\n",true},
  {"booting",503,"starting\\n",false},
  {"stopping",503,"stopping\\n",false},
  {"wrong body",200,`{"status":"ok"}`,false},
  {"service error",500,"ok\\n",false},
  {"not listening",0,"",false},
 }
 for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
  if tc.code==0 {if omniRouteHealthResponse(nil)!=tc.want {t.Fatal("nil response should be unready")};return}
  resp:=&http.Response{StatusCode:tc.code,Body:io.NopCloser(strings.NewReader(tc.body))}
  if got:=omniRouteHealthResponse(resp);got!=tc.want {t.Fatalf("health ready=%v, want %v",got,tc.want)}
 })}
}
