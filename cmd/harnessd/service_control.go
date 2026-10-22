package main

import (
 "encoding/hex"
 "net"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/serviceproof"
)

// serviceControlHandler installs non-bearer, loopback-only service endpoints.
// Readiness is observable but proves possession of the launch secret to SCM.
// Shutdown requires a DIFFERENT purpose-bound HMAC; reading the readiness
// endpoint cannot yield a valid shutdown authorisation.
func serviceControlHandler(base http.Handler, token string, cancel func()) http.Handler {
 if base==nil{base=http.NotFoundHandler()}
 if len(token)<32{return base}
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  ready:=r.URL.Path=="/__onepane/service/ready"
  shutdown:=r.URL.Path=="/__onepane/service/shutdown"
  if !ready&&!shutdown{base.ServeHTTP(w,r);return}
  host,_,err:=net.SplitHostPort(r.RemoteAddr)
  method:=http.MethodPost
  if ready{method=http.MethodGet}
  if err!=nil||net.ParseIP(host)==nil||!net.ParseIP(host).IsLoopback()||r.Method!=method{
   http.NotFound(w,r);return
  }
  challenge,err:=hex.DecodeString(r.Header.Get(serviceproof.ChallengeHeader))
  if err!=nil||len(challenge)!=serviceproof.ChallengeBytes{
   http.NotFound(w,r);return
  }
  w.Header().Set("Cache-Control","no-store")
  if ready{
   w.Header().Set(serviceproof.ProofHeader,serviceproof.Sign(token,challenge))
   w.WriteHeader(http.StatusNoContent)
   return
  }
  if !serviceproof.VerifyShutdown(token,challenge,r.Header.Get(serviceproof.ProofHeader)){
   http.NotFound(w,r);return
  }
  w.WriteHeader(http.StatusAccepted)
  if cancel!=nil{go cancel()}
 })
}
