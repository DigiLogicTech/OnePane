package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)
type goalTCPObserverFenceService interface{
 GetGoalTCPObservationFence(context.Context,string,string)(projectworkspace.GoalTCPObservationFence,error)
 SetGoalTCPObservationFence(context.Context,projectworkspace.SetGoalTCPObservationFenceCommand)(projectworkspace.GoalTCPObservationFence,error)
}

func(s *Server)goalTCPObserverFenceRequest(w http.ResponseWriter,r *http.Request,write bool){
 identity,ok:=s.authenticate(w,r);if !ok{return}
 if identity.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human observer control required");return
 }
 if s.projects==nil{
  writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return
 }
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 permission:="project.read"
 if write{permission="project.write"}
 if !s.authorize(w,r,identity,p.WorkspaceID,permission){return}
 svc,ok:=s.projects.(goalTCPObserverFenceService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"observer control unavailable");return
 }
 goal:=strings.TrimSpace(r.PathValue("goalID"))
 if !write{
  out,e:=svc.GetGoalTCPObservationFence(r.Context(),p.ID,goal)
  respondDomain(w,out,e,http.StatusOK)
  return
 }
 var req struct {
  TargetStatus string `json:"target_status"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedFenceRevision int64 `json:"expected_fence_revision"`
 }
 if !decodeJSON(w,r,&req){return}
 out,e:=svc.SetGoalTCPObservationFence(r.Context(),
  projectworkspace.SetGoalTCPObservationFenceCommand{
   ProjectID:p.ID,GoalID:goal,ActorPrincipalID:identity.PrincipalID,
   TargetStatus:req.TargetStatus,ExpectedGoalRevision:req.ExpectedGoalRevision,
   ExpectedFenceRevision:req.ExpectedFenceRevision,
  })
 respondDomain(w,out,e,http.StatusOK)
}
func(s *Server)getProjectGoalTCPObserverFence(w http.ResponseWriter,r *http.Request){
 s.goalTCPObserverFenceRequest(w,r,false)
}
func(s *Server)setProjectGoalTCPObserverFence(w http.ResponseWriter,r *http.Request){
 s.goalTCPObserverFenceRequest(w,r,true)
}
