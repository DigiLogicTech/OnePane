package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalTCPDeclaredTargetService interface{
 DeclareGoalTCPTarget(context.Context,projectworkspace.DeclareGoalTCPTargetCommand)(
  projectworkspace.GoalTCPDeclaredTarget,error)
 RevokeGoalTCPTarget(context.Context,projectworkspace.RevokeGoalTCPTargetCommand)(
  projectworkspace.GoalTCPDeclaredTarget,error)
 ListGoalTCPTargets(context.Context,string,string)(
  []projectworkspace.GoalTCPDeclaredTarget,error)
}

func(s *Server)goalTargetOperator(w http.ResponseWriter,r *http.Request,
 capability string)(projectworkspace.Project,goalTCPDeclaredTargetService,Identity,bool){
 i,ok:=s.authenticate(w,r)
 if !ok{return projectworkspace.Project{},nil,Identity{},false}
 if i.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human target reviewer required")
  return projectworkspace.Project{},nil,Identity{},false
 }
 if s.projects==nil{
  writeError(w,http.StatusServiceUnavailable,"Project service unavailable")
  return projectworkspace.Project{},nil,Identity{},false
 }
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return projectworkspace.Project{},nil,Identity{},false}
 if !s.authorize(w,r,i,p.WorkspaceID,capability){
  return projectworkspace.Project{},nil,Identity{},false
 }
 svc,ok:=s.projects.(goalTCPDeclaredTargetService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"declared target service unavailable")
  return projectworkspace.Project{},nil,Identity{},false
 }
 return p,svc,i,true
}

func(s *Server)declareProjectGoalTCPTarget(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalTargetOperator(w,r,"project.write")
 if !ok{return}
 var input struct{
  PermitID string `json:"permit_id"`
  DisplayLabel string `json:"display_label"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPermitRevision int64 `json:"expected_permit_revision"`
  ExpiresAt int64 `json:"expires_at"`
 }
 if !decodeSignedSourceJSON(w,r,&input){return}
 result,err:=svc.DeclareGoalTCPTarget(r.Context(),
  projectworkspace.DeclareGoalTCPTargetCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),
   PermitID:input.PermitID,DisplayLabel:input.DisplayLabel,
   ActorPrincipalID:i.PrincipalID,
   ExpectedGoalRevision:input.ExpectedGoalRevision,
   ExpectedPermitRevision:input.ExpectedPermitRevision,ExpiresAt:input.ExpiresAt,
  })
 respondDomain(w,result,err,http.StatusCreated)
}

func(s *Server)listProjectGoalTCPTargets(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.goalTargetOperator(w,r,"project.read")
 if !ok{return}
 result,err:=svc.ListGoalTCPTargets(r.Context(),p.ID,r.PathValue("goalID"))
 respondDomain(w,result,err,http.StatusOK)
}

func(s *Server)revokeProjectGoalTCPTarget(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalTargetOperator(w,r,"project.write")
 if !ok{return}
 var input struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedTargetRevision int64 `json:"expected_target_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&input){return}
 result,err:=svc.RevokeGoalTCPTarget(r.Context(),
  projectworkspace.RevokeGoalTCPTargetCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),
   TargetID:r.PathValue("targetID"),
   ActorPrincipalID:i.PrincipalID,
   ExpectedGoalRevision:input.ExpectedGoalRevision,
   ExpectedTargetRevision:input.ExpectedTargetRevision,
  })
 respondDomain(w,result,err,http.StatusOK)
}
