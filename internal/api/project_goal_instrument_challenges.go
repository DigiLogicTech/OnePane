package api

import(
 "context"
 "net/http"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type goalInstrumentChallengeService interface{
 ApproveGoalInstrumentKey(context.Context,projectworkspace.ApproveGoalInstrumentKeyCommand)(projectworkspace.GoalInstrumentKey,error)
 RevokeGoalInstrumentKey(context.Context,projectworkspace.RevokeGoalInstrumentKeyCommand)(projectworkspace.GoalInstrumentKey,error)
 IssueGoalInstrumentChallenge(context.Context,projectworkspace.IssueGoalInstrumentChallengeCommand)(projectworkspace.GoalInstrumentChallenge,error)
 AcceptGoalInstrumentChallenge(context.Context,projectworkspace.AcceptGoalInstrumentChallengeCommand)(projectworkspace.GoalInstrumentProof,error)
 ListGoalInstrumentKeys(context.Context,string,string)([]projectworkspace.GoalInstrumentKey,error)
 ListGoalInstrumentProofs(context.Context,string,string)([]projectworkspace.GoalInstrumentProof,error)
}

func(s *Server)goalInstrumentOperator(w http.ResponseWriter,r *http.Request,
 capability string)(projectworkspace.Project,goalInstrumentChallengeService,Identity,bool){
 p,_,i,ok:=s.goalTargetOperator(w,r,capability)
 if !ok{return projectworkspace.Project{},nil,Identity{},false}
 svc,ok:=s.projects.(goalInstrumentChallengeService)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"instrument key service unavailable")
  return projectworkspace.Project{},nil,Identity{},false
 }
 return p,svc,i,true
}
func(s *Server)approveProjectGoalInstrumentKey(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalInstrumentOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  TLSPinID string `json:"tls_pin_id"`
  PublicKey []byte `json:"public_key"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedPinRevision int64 `json:"expected_pin_revision"`
  ExpiresAt int64 `json:"expires_at"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 result,err:=svc.ApproveGoalInstrumentKey(r.Context(),
  projectworkspace.ApproveGoalInstrumentKeyCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),TLSPinID:in.TLSPinID,
   PublicKey:in.PublicKey,ActorPrincipalID:i.PrincipalID,
   ExpectedGoalRevision:in.ExpectedGoalRevision,
   ExpectedPinRevision:in.ExpectedPinRevision,ExpiresAt:in.ExpiresAt,
  })
 respondDomain(w,result,err,http.StatusCreated)
}
func(s *Server)revokeProjectGoalInstrumentKey(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalInstrumentOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedKeyRevision int64 `json:"expected_key_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 result,err:=svc.RevokeGoalInstrumentKey(r.Context(),
  projectworkspace.RevokeGoalInstrumentKeyCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),KeyID:r.PathValue("keyID"),
   ActorPrincipalID:i.PrincipalID,ExpectedGoalRevision:in.ExpectedGoalRevision,
   ExpectedKeyRevision:in.ExpectedKeyRevision,
  })
 respondDomain(w,result,err,http.StatusOK)
}
func(s *Server)issueProjectGoalInstrumentChallenge(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalInstrumentOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedKeyRevision int64 `json:"expected_key_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 result,err:=svc.IssueGoalInstrumentChallenge(r.Context(),
  projectworkspace.IssueGoalInstrumentChallengeCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),KeyID:r.PathValue("keyID"),
   ActorPrincipalID:i.PrincipalID,ExpectedGoalRevision:in.ExpectedGoalRevision,
   ExpectedKeyRevision:in.ExpectedKeyRevision,
  })
 respondDomain(w,result,err,http.StatusCreated)
}
func(s *Server)acceptProjectGoalInstrumentChallenge(w http.ResponseWriter,r *http.Request){
 p,svc,i,ok:=s.goalInstrumentOperator(w,r,"project.write")
 if !ok{return}
 var in struct{
  ChallengeID string `json:"challenge_id"`
  Signature []byte `json:"signature"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&in){return}
 result,err:=svc.AcceptGoalInstrumentChallenge(r.Context(),
  projectworkspace.AcceptGoalInstrumentChallengeCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),KeyID:r.PathValue("keyID"),
   ChallengeID:in.ChallengeID,ActorPrincipalID:i.PrincipalID,
   ExpectedGoalRevision:in.ExpectedGoalRevision,Signature:in.Signature,
  })
 respondDomain(w,result,err,http.StatusCreated)
}
func(s *Server)listProjectGoalInstrumentKeys(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.goalInstrumentOperator(w,r,"project.read")
 if !ok{return}
 result,err:=svc.ListGoalInstrumentKeys(r.Context(),p.ID,r.PathValue("goalID"))
 respondDomain(w,result,err,http.StatusOK)
}
func(s *Server)listProjectGoalInstrumentProofs(w http.ResponseWriter,r *http.Request){
 p,svc,_,ok:=s.goalInstrumentOperator(w,r,"project.read")
 if !ok{return}
 result,err:=svc.ListGoalInstrumentProofs(r.Context(),p.ID,r.PathValue("goalID"))
 respondDomain(w,result,err,http.StatusOK)
}
