package projectworkspace

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"

 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalInstrumentProbePlan is an operator-authenticated, read-only snapshot
// of the currently approved one-shot TCP endpoint. The exact IP/port are
// intentionally available ONLY through an authenticated HUMAN API, not
// triage, machine proof history, model routing, or event payloads.
//
// This plan is NOT server-signed. Its delivery and transfer to an external
// instrument require a separately trusted channel; it is NOT an attestation
// of the physical node, executable, endpoint ownership or observation.
type GoalInstrumentProbePlan struct {
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 InstrumentKeyID string `json:"instrument_key_id"`
 ChallengeID string `json:"challenge_id"`
 DeclaredTargetID string `json:"declared_target_id"`
 TLSPinID string `json:"tls_pin_id"`
 SourceID string `json:"source_id"`
 TargetIPv4 string `json:"target_ipv4"`
 TargetPort int `json:"target_port"`
 TimeoutMS int `json:"timeout_ms"`
 OriginalChallengeSHA256 string `json:"original_challenge_sha256"`
 ExpiresAt int64 `json:"expires_at"`
 ObserverVantageVerified bool `json:"observer_vantage_verified"`
 TargetDeviceIdentityVerified bool `json:"target_device_identity_verified"`
 AttestationClass string `json:"attestation_class"`
}

// PrepareGoalInstrumentProbePlan does NOT open a socket, increment a
// challenge budget, or consume a challenge. Admission and signing still
// independently recheck all original human-granted authority.
func(s *Service)PrepareGoalInstrumentProbePlan(ctx context.Context,
 projectID,goalID,keyID,challengeID,actorID string,
 expectedGoalRevision int64)(GoalInstrumentProbePlan,error){
 if projectID==""||goalID==""||keyID==""||challengeID==""||
  actorID==""||expectedGoalRevision<1{
  return GoalInstrumentProbePlan{},ErrInvalidCommand
 }
 var plan GoalInstrumentProbePlan
 err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,projectID)
  if err!=nil{return err}
  if err=s.requireActor(ctx,tx,project.WorkspaceID,actorID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,actorID);err!=nil{return err}
  key,pin,err:=s.activeInstrumentKeyTx(ctx,tx,projectID,goalID,keyID,now)
  if err!=nil{return err}
  scope,err:=s.activePinnedTargetTx(ctx,tx,projectID,goalID,key.DeclaredTargetID,now)
  if err!=nil{return err}
  if scope.GoalRevision!=expectedGoalRevision{return ErrRevisionConflict}
  if scope.Target.SourceID!=scope.Permit.SourceID||
   scope.Target.ID!=key.DeclaredTargetID||
   scope.Target.ProjectWorkspaceID!=key.ProjectWorkspaceID||
   scope.Target.PlanHash!=key.PlanHash||
   scope.Target.PermitID!=pin.PermitID||
   scope.Target.PermitRevision!=pin.PermitRevision||
   pin.DeclaredTargetID!=key.DeclaredTargetID||
   pin.ProjectWorkspaceID!=key.ProjectWorkspaceID||
   pin.PlanHash!=key.PlanHash{
   return ErrInvalidTransition
  }
  var challenge GoalInstrumentChallenge
  challenge.ID=challengeID
  challenge.KeyID=keyID
  challenge.ProjectID=projectID
  challenge.GoalID=goalID
  err=tx.QueryRowContext(ctx,`SELECT nonce,signing_payload,issued_at,
   expires_at,status FROM project_goal_instrument_challenges
   WHERE id=? AND instrument_key_id=? AND project_id=? AND goal_id=?`,
   challengeID,keyID,projectID,goalID).
   Scan(&challenge.Nonce,&challenge.SigningPayload,
    &challenge.IssuedAt,&challenge.ExpiresAt,&challenge.Status)
  if err!=nil{return ErrInvalidTransition}
  if challenge.Status!="issued"||challenge.IssuedAt<key.ApprovedAt||
   challenge.IssuedAt>now||challenge.ExpiresAt<=now||
   challenge.ExpiresAt>key.ExpiresAt||
   challenge.ExpiresAt>pin.ExpiresAt{
   return ErrInvalidTransition
  }
  expected,err:=instrumentSigningBytes(key,pin,challenge)
  if err!=nil{return err}
  if !bytes.Equal(expected,challenge.SigningPayload){
   return ErrInvalidTransition
  }
  digest:=sha256.Sum256(challenge.SigningPayload)
  plan=GoalInstrumentProbePlan{
   ProjectID:projectID,GoalID:goalID,InstrumentKeyID:keyID,
   ChallengeID:challengeID,DeclaredTargetID:key.DeclaredTargetID,
   TLSPinID:pin.ID,SourceID:scope.Target.SourceID,
   TargetIPv4:scope.Permit.TargetIPv4,TargetPort:scope.Permit.TargetPort,
   TimeoutMS:scope.Permit.TimeoutMS,
   OriginalChallengeSHA256:"sha256:"+hex.EncodeToString(digest[:]),
   ExpiresAt:challenge.ExpiresAt,
   AttestationClass:"human_authorised_endpoint_snapshot_not_device_attestation",
  }
  return nil
 })
 if err!=nil{return GoalInstrumentProbePlan{},err}
 return plan,nil
}
