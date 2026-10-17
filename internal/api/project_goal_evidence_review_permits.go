package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// A review permit covers previously-attested receipt metadata only.
// These handlers cannot start a probe, recurring scheduler or remediation.
type goalEvidenceReviewService interface{
 ApproveGoalEvidenceReview(context.Context,projectworkspace.ApproveGoalEvidenceReviewCommand)(projectworkspace.GoalEvidenceReviewPermit,error)
 RevokeGoalEvidenceReview(context.Context,projectworkspace.RevokeGoalEvidenceReviewCommand)(projectworkspace.GoalEvidenceReviewPermit,error)
 ListGoalEvidenceReviews(context.Context,string,string)([]projectworkspace.GoalEvidenceReviewPermit,error)
}

func(s *Server)reviewPermitScope(w http.ResponseWriter,r *http.Request,capability string)(projectworkspace.Project,goalEvidenceReviewService,bool){
 identity,ok:=s.authenticate(w,r)
 if !ok{return projectworkspace.Project{},nil,false}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return projectworkspace.Project{},nil,false}
 project,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return projectworkspace.Project{},nil,false}
 if !s.authorize(w,r,identity,project.WorkspaceID,capability){return projectworkspace.Project{},nil,false}
 svc,ok:=s.projects.(goalEvidenceReviewService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"evidence review permits unavailable");return projectworkspace.Project{},nil,false}
 return project,svc,true
}

func(s *Server)listProjectGoalReviewPermits(w http.ResponseWriter,r *http.Request){
 p,svc,ok:=s.reviewPermitScope(w,r,"project.read")
 if !ok{return}
 result,err:=svc.ListGoalEvidenceReviews(r.Context(),p.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,result,err,http.StatusOK)
}

func(s *Server)approveProjectGoalReviewPermit(w http.ResponseWriter,r *http.Request){
 // Require explicit project.write and an actual human member at the domain
 // transaction boundary; an Agent's project.write cannot grant itself reviews.
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalEvidenceReviewService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"evidence review permits unavailable");return}
 var input struct{
  SourceID string `json:"source_id"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpiresAt int64 `json:"expires_at"`
 }
 if !decodeJSON(w,r,&input){return}
 result,err:=svc.ApproveGoalEvidenceReview(r.Context(),projectworkspace.ApproveGoalEvidenceReviewCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),SourceID:input.SourceID,
  ExpectedGoalRevision:input.ExpectedGoalRevision,ExpiresAt:input.ExpiresAt,
  ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,result,err,http.StatusCreated)
}

func(s *Server)revokeProjectGoalReviewPermit(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalEvidenceReviewService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"evidence review permits unavailable");return}
 var input struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPermitRevision int64 `json:"expected_permit_revision"`
 }
 if !decodeJSON(w,r,&input){return}
 result,err:=svc.RevokeGoalEvidenceReview(r.Context(),projectworkspace.RevokeGoalEvidenceReviewCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),PermitID:r.PathValue("permitID"),
  ExpectedGoalRevision:input.ExpectedGoalRevision,
  ExpectedPermitRevision:input.ExpectedPermitRevision,
  ActorPrincipalID:identity.PrincipalID,
 })
 respondDomain(w,result,err,http.StatusOK)
}
