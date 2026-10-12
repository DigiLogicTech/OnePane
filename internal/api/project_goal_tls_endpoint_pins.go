package api

import(
 "context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalTLSPinService interface{
 ApproveGoalTLSPin(context.Context,projectworkspace.ApproveGoalTLSPinCommand)(projectworkspace.GoalTLSPin,error)
 RevokeGoalTLSPin(context.Context,projectworkspace.RevokeGoalTLSPinCommand)(projectworkspace.GoalTLSPin,error)
 ListGoalTLSPins(context.Context,string,string)([]projectworkspace.GoalTLSPin,error)
 RunGoalPinnedTLSCheck(context.Context,projectworkspace.RunGoalPinnedTLSCheckCommand)(projectworkspace.GoalPinnedTLSCheck,error)
 ListGoalPinnedTLSChecks(context.Context,string,string)([]projectworkspace.GoalPinnedTLSCheck,error)
}

func(s *Server)goalTLSPinOperator(w http.ResponseWriter,r *http.Request,
 capability string)(projectworkspace.Project,goalTLSPinService,Identity,bool){
 p,_,identity,ok:=s.goalTargetOperator(w,r,capability)
 if !ok{return projectworkspace.Project{},nil,Identity{},false}
 svc,ok:=s.projects.(goalTLSPinService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"pinned TLS verification unavailable")
  return projectworkspace.Project{},nil,Identity{},false
 }
 return p,svc,identity,true
}
func(s *Server)approveProjectGoalTLSPin(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalTLSPinOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  DeclaredTargetID string `json:"declared_target_id"`
  CertificateSHA256 string `json:"certificate_sha256"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedTargetRevision int64 `json:"expected_target_revision"`
  ExpiresAt int64 `json:"expires_at"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 result,err:=svc.ApproveGoalTLSPin(r.Context(),projectworkspace.ApproveGoalTLSPinCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),
  DeclaredTargetID:in.DeclaredTargetID,CertificateSHA256:in.CertificateSHA256,
  ActorPrincipalID:i.PrincipalID,ExpectedGoalRevision:in.ExpectedGoalRevision,
  ExpectedTargetRevision:in.ExpectedTargetRevision,ExpiresAt:in.ExpiresAt,
 })
 respondDomain(w,result,err,http.StatusCreated)
}
func(s *Server)revokeProjectGoalTLSPin(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalTLSPinOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPinRevision int64 `json:"expected_pin_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 result,err:=svc.RevokeGoalTLSPin(r.Context(),projectworkspace.RevokeGoalTLSPinCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),PinID:r.PathValue("pinID"),
  ActorPrincipalID:i.PrincipalID,ExpectedGoalRevision:in.ExpectedGoalRevision,
  ExpectedPinRevision:in.ExpectedPinRevision,
 })
 respondDomain(w,result,err,http.StatusOK)
}
func(s *Server)listProjectGoalTLSPins(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.goalTLSPinOperator(w,r,"project.read")
 if !ok{return}
 result,err:=svc.ListGoalTLSPins(r.Context(),p.ID,r.PathValue("goalID"))
 respondDomain(w,result,err,http.StatusOK)
}
func(s *Server)runProjectGoalTLSPinCheck(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalTLSPinOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPinRevision int64 `json:"expected_pin_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 result,err:=svc.RunGoalPinnedTLSCheck(r.Context(),projectworkspace.RunGoalPinnedTLSCheckCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),PinID:r.PathValue("pinID"),
  ActorPrincipalID:i.PrincipalID,ExpectedGoalRevision:in.ExpectedGoalRevision,
  ExpectedPinRevision:in.ExpectedPinRevision,
 })
 respondDomain(w,result,err,http.StatusCreated)
}
func(s *Server)listProjectGoalTLSPinChecks(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.goalTLSPinOperator(w,r,"project.read")
 if !ok{return}
 result,err:=svc.ListGoalPinnedTLSChecks(r.Context(),p.ID,r.PathValue("goalID"))
 respondDomain(w,result,err,http.StatusOK)
}
