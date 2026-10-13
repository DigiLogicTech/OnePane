package api

import (
 "context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalNodeProbeExecutionService interface{
 ApproveGoalNodeProbeExecutionPermit(context.Context,
  projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand)(
   projectworkspace.GoalNodeProbeExecutionPermit,error)
 RevokeGoalNodeProbeExecutionPermit(context.Context,
  projectworkspace.RevokeGoalNodeProbeExecutionPermitCommand)(
   projectworkspace.GoalNodeProbeExecutionPermit,error)
 ListGoalNodeProbeExecutionPermits(context.Context,string,string)(
  []projectworkspace.GoalNodeProbeExecutionPermit,error)
}

// Reuses the authenticated HUMAN target reviewer gate. project.read only
// sees bounded permission metadata; project.write is required to authorise
// or revoke the one-shot Node-local network action.
func(s *Server)goalNodeProbeExecutionOperator(w http.ResponseWriter,r *http.Request,
 capability string)(projectworkspace.Project,goalNodeProbeExecutionService,Identity,bool){
 p,_,i,ok:=s.goalTargetOperator(w,r,capability)
 if !ok{return projectworkspace.Project{},nil,Identity{},false}
 svc,ok:=s.projects.(goalNodeProbeExecutionService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"Node one-shot permit service unavailable")
  return projectworkspace.Project{},nil,Identity{},false
 }
 return p,svc,i,true
}
func(s *Server)approveProjectGoalNodeProbeExecution(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalNodeProbeExecutionOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  InstrumentKeyID string `json:"instrument_key_id"`
  ChallengeID string `json:"challenge_id"`
  PeerNodeID string `json:"peer_node_id"`
  OperatorPublicKey []byte `json:"operator_public_key"`
  OperatorProofSignature []byte `json:"operator_proof_signature"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 result,err:=svc.ApproveGoalNodeProbeExecutionPermit(r.Context(),
  projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),
   InstrumentKeyID:in.InstrumentKeyID,ChallengeID:in.ChallengeID,
   PeerNodeID:in.PeerNodeID,OperatorPublicKey:in.OperatorPublicKey,
   OperatorProofSignature:in.OperatorProofSignature,
   ActorPrincipalID:i.PrincipalID,
   ExpectedGoalRevision:in.ExpectedGoalRevision,
  })
 respondDomain(w,result,err,http.StatusCreated)
}
func(s *Server)revokeProjectGoalNodeProbeExecution(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalNodeProbeExecutionOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPermitRevision int64 `json:"expected_permit_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 result,err:=svc.RevokeGoalNodeProbeExecutionPermit(r.Context(),
  projectworkspace.RevokeGoalNodeProbeExecutionPermitCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),
   PermitID:r.PathValue("permitID"),ActorPrincipalID:i.PrincipalID,
   ExpectedGoalRevision:in.ExpectedGoalRevision,
   ExpectedPermitRevision:in.ExpectedPermitRevision,
  })
 respondDomain(w,result,err,http.StatusOK)
}
func(s *Server)listProjectGoalNodeProbeExecutions(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.goalNodeProbeExecutionOperator(w,r,"project.read")
 if !ok{return}
 result,err:=svc.ListGoalNodeProbeExecutionPermits(r.Context(),
  p.ID,r.PathValue("goalID"))
 respondDomain(w,result,err,http.StatusOK)
}
