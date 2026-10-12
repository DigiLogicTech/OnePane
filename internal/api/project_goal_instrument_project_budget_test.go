package api

import (
 "net/http"
 "net/http/httptest"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestInstrumentProjectChallengeBudgetsReturnBoundedRetryWindows(t *testing.T){
 for _,tc:=range []struct{
  name string
  err error
  retry string
 }{
  {"five minute",projectworkspace.ErrInstrumentProjectFiveMinuteRateLimited,"300"},
  {"daily",projectworkspace.ErrInstrumentProjectDailyRateLimited,"86400"},
 }{
  t.Run(tc.name,func(t *testing.T){
   w:=httptest.NewRecorder()
   respondDomain(w,nil,tc.err,http.StatusCreated)
   if w.Code!=http.StatusTooManyRequests{
    t.Fatalf("instrument Project quota produced HTTP %d instead of 429",w.Code)
   }
   if v:=w.Header().Get("Retry-After");v!=tc.retry{
    t.Fatalf("incorrect server retry time: %q, expected %s",v,tc.retry)
   }
  })
 }
}
