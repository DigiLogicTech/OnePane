package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalIncidentReviewService interface {
 ListGoalIncidentReviews(context.Context,string,string)([]projectworkspace.GoalIncidentReview,error)
 OpenGoalIncidentReview(context.Context,projectworkspace.OpenGoalIncidentReviewCommand)(projectworkspace.GoalIncidentReview,error)
 CompleteGoalIncidentReview(context.Context,projectworkspace.CompleteGoalIncidentReviewCommand)(projectworkspace.GoalIncidentReview,error)
}

func(s *Server)incidentReviewRequest(w http.ResponseWriter,r *http.Request,
 action string){
 identity,ok:=s.authenticate(w,r);if !ok{return}
 if identity.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human incident reviewer required");return
 }
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 project,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 required:="project.read"
 if action!="list"{required="project.write"}
 if !s.authorize(w,r,identity,project.WorkspaceID,required){return}
 svc,ok:=s.projects.(goalIncidentReviewService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"incident reviews unavailable");return}
 goalID:=strings.TrimSpace(r.PathValue("goalID"))
 switch action{
 case "list":
  out,err:=svc.ListGoalIncidentReviews(r.Context(),project.ID,goalID)
  respondDomain(w,out,err,http.StatusOK)
 case "open":
  var body struct{
   TCPAlertID string `json:"tcp_alert_id"`
   ExpectedAlertRevision int64 `json:"expected_alert_revision"`
  }
  if !decodeJSON(w,r,&body){return}
  out,err:=svc.OpenGoalIncidentReview(r.Context(),projectworkspace.OpenGoalIncidentReviewCommand{
   ProjectID:project.ID,GoalID:goalID,TCPAlertID:body.TCPAlertID,
   ExpectedAlertRevision:body.ExpectedAlertRevision,ActorPrincipalID:identity.PrincipalID,
  })
  respondDomain(w,out,err,http.StatusCreated)
 case "review":
  var body struct{
   Decision string `json:"decision"`
   ExpectedRevision int64 `json:"expected_revision"`
  }
  if !decodeJSON(w,r,&body){return}
  out,err:=svc.CompleteGoalIncidentReview(r.Context(),
   projectworkspace.CompleteGoalIncidentReviewCommand{
    ProjectID:project.ID,GoalID:goalID,ReviewID:r.PathValue("reviewID"),
    ActorPrincipalID:identity.PrincipalID,ExpectedRevision:body.ExpectedRevision,
    Decision:body.Decision,
   })
  respondDomain(w,out,err,http.StatusOK)
 }
}
func(s *Server)listProjectGoalIncidentReviews(w http.ResponseWriter,r *http.Request){
 s.incidentReviewRequest(w,r,"list")
}
func(s *Server)openProjectGoalIncidentReview(w http.ResponseWriter,r *http.Request){
 s.incidentReviewRequest(w,r,"open")
}
func(s *Server)completeProjectGoalIncidentReview(w http.ResponseWriter,r *http.Request){
 s.incidentReviewRequest(w,r,"review")
}
