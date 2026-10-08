package webui

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)
func TestWebUIAssetsNeverCachedAcrossDesktopUpgrades(t *testing.T) {
 for _,path:=range []string{"/","/index.html","/app.js","/style.css","/web-chat-session-tabs.js","/deep-link-fallback"} {
  req:=httptest.NewRequest(http.MethodGet,path,nil)
  out:=httptest.NewRecorder()
  Handler().ServeHTTP(out,req)
  if out.Code!=http.StatusOK {t.Errorf("%s returned %d",path,out.Code);continue}
  if !strings.Contains(out.Header().Get("Cache-Control"),"no-store") {
   t.Errorf("%s can return stale webview assets: %s",path,out.Header().Get("Cache-Control"))
  }
 }
}
