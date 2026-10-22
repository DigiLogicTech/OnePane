package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalSignedObservationReadService interface{
 ListGoalSignedObservationProofs(context.Context,string,string)([]projectworkspace.SignedGoalObservationProof,error)
}

// The only public signed-source API in this checkpoint is a read-only,
// tenant-authorised metadata view. Source key approval and proof ingestion
// are NOT remotely exposed until a narrow, authenticated source-submit
// capability has been reviewed. A Project read permission grants neither.
func(s *Server)listProjectGoalSignedObservations(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{
  writeError(w,http.StatusServiceUnavailable,"Project service unavailable")
  return
 }
 project,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,project.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalSignedObservationReadService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"signed Observation proofs unavailable")
  return
 }
 proofs,err:=svc.ListGoalSignedObservationProofs(r.Context(),project.ID,
  strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,proofs,err,http.StatusOK)
}
