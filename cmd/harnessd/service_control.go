package main

import (
 "crypto/subtle"
 "net"
 "net/http"
)

// serviceControlHandler installs token-authenticated loopback-only endpoints
// exclusively for Windows SCM launches, with an ephemeral per-launch secret.
// A normal /v1/health response is intentionally insufficient to establish
// ownership or readiness of the SCM-managed child process.
func serviceControlHandler(base http.Handler, token string, cancel func()) http.Handler {
 if base==nil{base=http.NotFoundHandler()}
 if len(token)<32{return base}
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  isReady:=r.URL.Path=="/__onepane/service/ready"
  isShutdown:=r.URL.Path=="/__onepane/service/shutdown"
  if !isReady&&!isShutdown{base.ServeHTTP(w,r);return}
  host,_,err:=net.SplitHostPort(r.RemoteAddr)
  expectedMethod:=http.MethodPost
  if isReady {expectedMethod=http.MethodGet}
  if err!=nil||net.ParseIP(host)==nil||!net.ParseIP(host).IsLoopback()||
   r.Method!=expectedMethod||
   subtle.ConstantTimeCompare([]byte(r.Header.Get("X-OnePane-Service-Token")),[]byte(token))!=1 {
   http.NotFound(w,r);return
  }
  w.Header().Set("Cache-Control","no-store")
  if isReady {w.WriteHeader(http.StatusNoContent);return}
  w.WriteHeader(http.StatusAccepted)
  if cancel!=nil{go cancel()}
 })
}
