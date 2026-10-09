package api

import (
 "database/sql"
 "errors"
 "net/http"
 "strings"
)

// This read-only route deliberately mirrors the existing, deployment-specific
// model.read authority check. It does not grant access to another user's
// managed deployment or to the global model-testbed inventory.
func (s *Server) qaAgentCheckEvidence(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r)
 if !ok{return}
 dep:=strings.TrimSpace(r.PathValue("deploymentID"))
 if dep==""||len(dep)>192{
  writeError(w,http.StatusBadRequest,"valid managed deployment is required")
  return
 }
 if !s.authorizeManagedDeployment(w,r,i,dep,"model.read"){return}
 if s.attentionDB==nil{
  writeError(w,http.StatusServiceUnavailable,"QA model evidence store unavailable")
  return
 }
 evidence,err:=loadQAModelEvidence(r.Context(),s.attentionDB,dep)
 if errors.Is(err,sql.ErrNoRows){
  writeError(w,http.StatusNotFound,"managed deployment evidence not found")
  return
 }
 if err!=nil{
  writeError(w,http.StatusServiceUnavailable,"Agent Check evidence unavailable")
  return
 }
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,evidence)
}
