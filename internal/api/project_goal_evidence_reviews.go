package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalEvidenceReviewExecutionService interface{
 ReviewGoalEvidence(context.Context,projectworkspace.ReviewGoalEvidenceCommand)(projectworkspace.GoalEvidenceReviewRun,error)
 ListGoalEvidenceReviewRuns(context.Context,string,string)([]projectworkspace.GoalEvidenceReviewRun,error)
}

// The only review supported here is of already attached receipt timestamps;
// this API cannot invoke a probe, Task, hosted model or remediation.
func(s *Server)runProjectGoalEvidenceReview(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalEvidenceReviewExecutionService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"receipt evidence reviews unavailable");return}
 var input struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPermitRevision int64 `json:"expected_permit_revision"`
 }
 if !decodeJSON(w,r,&input){return}
 result,err:=svc.ReviewGoalEvidence(r.Context(),projectworkspace.ReviewGoalEvidenceCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),PermitID:r.PathValue("permitID"),
  ExpectedGoalRevision:input.ExpectedGoalRevision,
  ExpectedPermitRevision:input.ExpectedPermitRevision,
  ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,result,err,http.StatusOK)
}

func(s *Server)listProjectGoalEvidenceReviews(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalEvidenceReviewExecutionService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"receipt evidence reviews unavailable");return}
 result,err:=svc.ListGoalEvidenceReviewRuns(r.Context(),p.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,result,err,http.StatusOK)
}
