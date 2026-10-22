package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalNodeMeasurementGrantsService interface{
 ApproveGoalNodeMeasurementGrant(context.Context,
  projectworkspace.ApproveGoalNodeMeasurementGrantCommand)(
   projectworkspace.GoalNodeMeasurementGrant,error)
 RevokeGoalNodeMeasurementGrant(context.Context,
  projectworkspace.RevokeGoalNodeMeasurementGrantCommand)(
   projectworkspace.GoalNodeMeasurementGrant,error)
 ListGoalNodeMeasurementGrants(context.Context,string,string)(
  []projectworkspace.GoalNodeMeasurementGrant,error)
 ListGoalNodeMeasurementReceipts(context.Context,string,string)(
  []projectworkspace.GoalNodeMeasurementReceipt,error)
}

func(s *Server)nodeMeasurementOperator(w http.ResponseWriter,r *http.Request,
 capability string)(projectworkspace.Project,goalNodeMeasurementGrantsService,Identity,bool){
 i,ok:=s.authenticate(w,r)
 if !ok{return projectworkspace.Project{},nil,Identity{},false}
 if i.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human operator required")
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
 svc,ok:=s.projects.(goalNodeMeasurementGrantsService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"Node measurement grants unavailable")
  return projectworkspace.Project{},nil,Identity{},false
 }
 return p,svc,i,true
}

func(s *Server)approveProjectGoalNodeMeasurement(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.nodeMeasurementOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  PeerNodeID string `json:"peer_node_id"`
  SourceID string `json:"source_id"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpiresAt int64 `json:"expires_at"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 grant,err:=svc.ApproveGoalNodeMeasurementGrant(r.Context(),
  projectworkspace.ApproveGoalNodeMeasurementGrantCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),SourceID:in.SourceID,
   PeerNodeID:in.PeerNodeID,ActorPrincipalID:i.PrincipalID,
   ExpectedGoalRevision:in.ExpectedGoalRevision,ExpiresAt:in.ExpiresAt,
  })
 respondDomain(w,grant,err,http.StatusCreated)
}
func(s *Server)listProjectGoalNodeMeasurements(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.nodeMeasurementOperator(w,r,"project.read")
 if !ok{return}
 out,err:=svc.ListGoalNodeMeasurementGrants(r.Context(),p.ID,r.PathValue("goalID"))
 respondDomain(w,out,err,http.StatusOK)
}
func(s *Server)revokeProjectGoalNodeMeasurement(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.nodeMeasurementOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedGrantRevision int64 `json:"expected_grant_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 out,err:=svc.RevokeGoalNodeMeasurementGrant(r.Context(),
  projectworkspace.RevokeGoalNodeMeasurementGrantCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),GrantID:r.PathValue("grantID"),
   ExpectedGoalRevision:in.ExpectedGoalRevision,
   ExpectedGrantRevision:in.ExpectedGrantRevision,
   ActorPrincipalID:i.PrincipalID,
  })
 respondDomain(w,out,err,http.StatusOK)
}

func(s *Server)listProjectGoalNodeMeasurementReceipts(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.nodeMeasurementOperator(w,r,"project.read")
 if !ok{return}
 results,err:=svc.ListGoalNodeMeasurementReceipts(r.Context(),p.ID,
  r.PathValue("goalID"))
 respondDomain(w,results,err,http.StatusOK)
}
