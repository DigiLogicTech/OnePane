package api

import (
 "database/sql"
 "errors"
 "net/http"
 "strings"
)

// Only administrators with Node operation privileges can inspect local
// control-plane Node QA evidence. No direct remote network call occurs,
// and no underlying Node/OS diagnostic privileges are expanded.
func(s *Server)qaNodeEvidenceHandler(w http.ResponseWriter,r *http.Request){
 if _,ok:=s.requireNodeOperator(w,r);!ok{return}
 nodeID:=strings.TrimSpace(r.PathValue("nodeID"))
 if nodeID==""||len(nodeID)>192{
  writeError(w,http.StatusBadRequest,"valid selected Node is required")
  return
 }
 if s.attentionDB==nil{
  writeError(w,http.StatusServiceUnavailable,"Node QA database unavailable")
  return
 }
 report,err:=loadQANodeEvidence(r.Context(),s.attentionDB,nodeID)
 if errors.Is(err,sql.ErrNoRows){
  writeError(w,http.StatusNotFound,"Node evidence unavailable")
  return
 }
 if err!=nil{
  writeError(w,http.StatusServiceUnavailable,"Node diagnostics unavailable")
  return
 }
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,report)
}
