package api

import(
 "context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalInstrumentMeasurementService interface{
 PrepareGoalInstrumentMeasurement(context.Context,projectworkspace.SignedMeasurementCommand)(
  projectworkspace.InstrumentMeasurementSigningPayload,error)
 AcceptGoalInstrumentMeasurement(context.Context,projectworkspace.SignedMeasurementCommand)(
  projectworkspace.SignedMeasurementClaim,error)
 ListGoalInstrumentMeasurements(context.Context,string,string)(
  []projectworkspace.SignedMeasurementClaim,error)
 GetGoalInstrumentMeasurementBundle(context.Context,string,string,string)(
  projectworkspace.InstrumentMeasurementVerificationBundle,error)
}

func(s *Server)goalInstrumentMeasurementOperator(w http.ResponseWriter,r *http.Request,
 capability string)(projectworkspace.Project,goalInstrumentMeasurementService,Identity,bool){
 p,_,i,ok:=s.goalInstrumentOperator(w,r,capability)
 if !ok{return projectworkspace.Project{},nil,Identity{},false}
 svc,ok:=s.projects.(goalInstrumentMeasurementService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"instrument measurement unavailable")
  return projectworkspace.Project{},nil,Identity{},false
 }
 return p,svc,i,true
}
func(s *Server)prepareProjectGoalInstrumentMeasurement(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalInstrumentMeasurementOperator(w,r,"project.write")
 if !ok{return}
 var body struct{
  ChallengeID string `json:"challenge_id"`
  Outcome string `json:"outcome"`
  ObservedAt int64 `json:"observed_at"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&body){return}
 result,err:=svc.PrepareGoalInstrumentMeasurement(r.Context(),projectworkspace.SignedMeasurementCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),KeyID:r.PathValue("keyID"),
  ChallengeID:body.ChallengeID,Outcome:body.Outcome,
  ObservedAt:body.ObservedAt,ExpectedGoalRevision:body.ExpectedGoalRevision,
  ActorPrincipalID:i.PrincipalID,
 })
 respondDomain(w,result,err,http.StatusOK)
}
func(s *Server)acceptProjectGoalInstrumentMeasurement(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalInstrumentMeasurementOperator(w,r,"project.write")
 if !ok{return}
 var body struct{
  ChallengeID string `json:"challenge_id"`
  Outcome string `json:"outcome"`
  ObservedAt int64 `json:"observed_at"`
  Signature []byte `json:"signature"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&body){return}
 result,err:=svc.AcceptGoalInstrumentMeasurement(r.Context(),projectworkspace.SignedMeasurementCommand{
  ProjectID:p.ID,GoalID:r.PathValue("goalID"),KeyID:r.PathValue("keyID"),
  ChallengeID:body.ChallengeID,Outcome:body.Outcome,ObservedAt:body.ObservedAt,
  ExpectedGoalRevision:body.ExpectedGoalRevision,
  ActorPrincipalID:i.PrincipalID,Signature:body.Signature,
 })
 respondDomain(w,result,err,http.StatusCreated)
}
func(s *Server)listProjectGoalInstrumentMeasurements(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.goalInstrumentMeasurementOperator(w,r,"project.read")
 if !ok{return}
 out,err:=svc.ListGoalInstrumentMeasurements(r.Context(),p.ID,r.PathValue("goalID"))
 respondDomain(w,out,err,http.StatusOK)
}
func(s *Server)getProjectGoalInstrumentMeasurementBundle(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.goalInstrumentMeasurementOperator(w,r,"project.read")
 if !ok{return}
 out,err:=svc.GetGoalInstrumentMeasurementBundle(r.Context(),
  p.ID,r.PathValue("goalID"),r.PathValue("claimID"))
 respondDomain(w,out,err,http.StatusOK)
}
