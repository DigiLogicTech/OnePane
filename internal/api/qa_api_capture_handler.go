package api

import (
 "errors"
 "net/http"
 "time"
)

var (
 errQAInvalidCapture=errors.New("invalid diagnostic capture options")
 errQAAlreadyActive=errors.New("diagnostic capture already active")
)

// All capture controls require the already established Node Admin boundary.
// Credentials/session IDs and user identities are used only as private
// in-memory owner checks; they never appear in exported diagnostic reports.
func timeNowQA()time.Time{return time.Now().UTC()}
func(s *Server)qaAPICaptureStart(w http.ResponseWriter,r *http.Request){
 i,ok:=s.requireNodeOperator(w,r);if !ok{return}
 var in struct{
  Level string `json:"level"`
  Seconds int `json:"seconds"`
 }
 if !decodeJSON(w,r,&in){return}
 view,err:=s.apiCapture.start(timeNowQA(),i.PrincipalID,i.CredentialID,in.Level,in.Seconds)
 if err!=nil{
  if errors.Is(err,errQAAlreadyActive){writeError(w,http.StatusConflict,"a diagnostic capture is already running");return}
  if errors.Is(err,errQAInvalidCapture){writeError(w,http.StatusBadRequest,"level normal/verbose and duration 30-300 seconds required");return}
  writeError(w,http.StatusServiceUnavailable,"diagnostic capture could not start");return
 }
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusCreated,view)
}
func(s *Server)qaAPICaptureStatus(w http.ResponseWriter,r *http.Request){
 i,ok:=s.requireNodeOperator(w,r);if !ok{return}
 view,exists:=s.apiCapture.view(timeNowQA(),i.PrincipalID,i.CredentialID)
 if !exists{writeError(w,http.StatusNotFound,"no owned diagnostic capture available");return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,view)
}
func(s *Server)qaAPICaptureStop(w http.ResponseWriter,r *http.Request){
 i,ok:=s.requireNodeOperator(w,r);if !ok{return}
 view,exists:=s.apiCapture.stop(timeNowQA(),i.PrincipalID,i.CredentialID)
 if !exists{writeError(w,http.StatusNotFound,"no owned diagnostic capture available");return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,view)
}
