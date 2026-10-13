package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)
type goalResponseCandidateService interface {
 ListGoalResponseCandidates(context.Context,string,string)([]projectworkspace.GoalResponseCandidate,error)
 ProposeGoalResponseCandidate(context.Context,projectworkspace.ProposeGoalResponseCandidateCommand)(projectworkspace.GoalResponseCandidate,error)
 WithdrawGoalResponseCandidate(context.Context,projectworkspace.WithdrawGoalResponseCandidateCommand)(projectworkspace.GoalResponseCandidate,error)
 GoalResponseCandidateReadiness(context.Context,string,string,string)(projectworkspace.GoalResponseCandidateReadiness,error)
}

// Candidate APIs are human-only and cannot dispatch even with project.write.
// A separate future, individually approved Operation grant must never be
// inferred from this non-executable historical intent.
func(s *Server)goalResponseCandidateRequest(w http.ResponseWriter,r *http.Request,action string){
 identity,ok:=s.authenticate(w,r);if !ok{return}
 if identity.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human operator required");return
 }
 if s.projects==nil{
  writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return
 }
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 permission:="project.read"
 if action=="propose"||action=="withdraw"{permission="project.write"}
 if !s.authorize(w,r,identity,p.WorkspaceID,permission){return}
 svc,ok:=s.projects.(goalResponseCandidateService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"response candidates unavailable");return}
 goalID:=strings.TrimSpace(r.PathValue("goalID"))
 switch action{
 case "list":
  out,e:=svc.ListGoalResponseCandidates(r.Context(),p.ID,goalID)
  respondDomain(w,out,e,http.StatusOK)
 case "readiness":
  out,e:=svc.GoalResponseCandidateReadiness(r.Context(),p.ID,goalID,
   strings.TrimSpace(r.PathValue("candidateID")))
  respondDomain(w,out,e,http.StatusOK)
 case "propose":
  var body struct{
   IncidentReviewID string `json:"incident_review_id"`
   ActionCode string `json:"action_code"`
   ExpectedGoalRevision int64 `json:"expected_goal_revision"`
   ExpectedReviewRevision int64 `json:"expected_review_revision"`
   ExpiresAt int64 `json:"expires_at"`
  }
  if !decodeJSON(w,r,&body){return}
  out,e:=svc.ProposeGoalResponseCandidate(r.Context(),projectworkspace.ProposeGoalResponseCandidateCommand{
   ProjectID:p.ID,GoalID:goalID,IncidentReviewID:body.IncidentReviewID,
   ActionCode:body.ActionCode,ExpectedGoalRevision:body.ExpectedGoalRevision,
   ExpectedReviewRevision:body.ExpectedReviewRevision,ExpiresAt:body.ExpiresAt,
   ActorPrincipalID:identity.PrincipalID,
  })
  respondDomain(w,out,e,http.StatusCreated)
 case "withdraw":
  var body struct{ExpectedRevision int64 `json:"expected_revision"`}
  if !decodeJSON(w,r,&body){return}
  out,e:=svc.WithdrawGoalResponseCandidate(r.Context(),
   projectworkspace.WithdrawGoalResponseCandidateCommand{
    ProjectID:p.ID,GoalID:goalID,
    CandidateID:strings.TrimSpace(r.PathValue("candidateID")),
    ExpectedRevision:body.ExpectedRevision,ActorPrincipalID:identity.PrincipalID,
   })
  respondDomain(w,out,e,http.StatusOK)
 }
}
func(s *Server)listProjectGoalResponseCandidates(w http.ResponseWriter,r *http.Request){
 s.goalResponseCandidateRequest(w,r,"list")
}
func(s *Server)proposeProjectGoalResponseCandidate(w http.ResponseWriter,r *http.Request){
 s.goalResponseCandidateRequest(w,r,"propose")
}
func(s *Server)getProjectGoalResponseCandidateReadiness(w http.ResponseWriter,r *http.Request){
 s.goalResponseCandidateRequest(w,r,"readiness")
}
func(s *Server)withdrawProjectGoalResponseCandidate(w http.ResponseWriter,r *http.Request){
 s.goalResponseCandidateRequest(w,r,"withdraw")
}
