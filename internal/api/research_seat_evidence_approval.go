package api

import (
 "context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type researchSeatEvidenceApprover interface{
 ApproveResearchSeatEvidence(context.Context,string,string,[]string)(
  projectworkspace.ResearchSeatEvidenceApproval,error)
 RevokeResearchSeatEvidence(context.Context,string,string)(
  projectworkspace.ResearchSeatEvidenceApproval,error)
 ResearchSeatEvidenceApproval(context.Context,string,string)(
  projectworkspace.ResearchSeatEvidenceApproval,error)
}

func(s *Server) approveResearchSeatEvidence(w http.ResponseWriter,r *http.Request){
 human,ss,ok:=s.sessionFor(w,r,"team.write")
 if !ok{return}
 if !s.authorize(w,r,human,ss.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(researchSeatEvidenceApprover)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Research seat approval unavailable");return}
 var in struct{
  SeatIDs []string `json:"seat_ids"`
  ConfirmLocalOnly bool `json:"confirm_local_only"`
 }
 if !decodeJSON(w,r,&in){return}
 if !in.ConfirmLocalOnly{
  writeError(w,http.StatusBadRequest,"Explicit local-only seat consent required")
  return
 }
 out,err:=svc.ApproveResearchSeatEvidence(r.Context(),ss.ID,human.PrincipalID,in.SeatIDs)
 respondDomain(w,out,err,http.StatusCreated)
}

func(s *Server) readResearchSeatEvidenceApproval(w http.ResponseWriter,r *http.Request){
 human,ss,ok:=s.sessionFor(w,r,"team.read")
 if !ok{return}
 if !s.authorize(w,r,human,ss.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(researchSeatEvidenceApprover)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Research seat approval unavailable");return}
 result,err:=svc.ResearchSeatEvidenceApproval(r.Context(),ss.ID,human.PrincipalID)
 if err!=nil{respondDomain(w,nil,err,0);return}
 w.Header().Set("Cache-Control","no-store, private")
 writeJSON(w,http.StatusOK,result)
}

func(s *Server) revokeResearchSeatEvidence(w http.ResponseWriter,r *http.Request){
 human,ss,ok:=s.sessionFor(w,r,"team.write")
 if !ok{return}
 if !s.authorize(w,r,human,ss.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(researchSeatEvidenceApprover)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Research seat approval unavailable");return}
 result,err:=svc.RevokeResearchSeatEvidence(r.Context(),ss.ID,human.PrincipalID)
 respondDomain(w,result,err,http.StatusOK)
}
