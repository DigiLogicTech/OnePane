package main

import (
 "testing"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestSealedLiveNodePermitAcceptsOnlyOmittedDisplayState(t *testing.T){
 sealed:=projectworkspace.GoalNodeProbeExecutionPermit{
  ID:"grant",ProjectID:"project",GoalID:"goal",ChallengeID:"nonce",
  OperatorPublicKeySHA256:"sha256:operator",PlanSHA256:"sha256:plan",
  PlanGeneration:1,PeerNodeID:"peer",Status:"approved",
  EffectiveExecutionState:"active",Revision:1,
 }
 fromPost:=sealed
 fromPost.EffectiveExecutionState=""
 if !sameSealedHumanProbeScope(sealed,fromPost){
  t.Fatal("original POST permit did not match separately sealed current inventory")
 }
 if !sameSealedHumanProbeScope(sealed,sealed){
  t.Fatal("current active signed inventory record rejected")
 }
 for label,mutate:=range map[string]func(*projectworkspace.GoalNodeProbeExecutionPermit){
  "wrong_node":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.PeerNodeID="different"},
  "wrong_operator":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.OperatorPublicKeySHA256="different"},
  "wrong_plan":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.PlanGeneration++},
  "wrong_challenge":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.ChallengeID="replayed"},
  "inactive":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.EffectiveExecutionState="revoked"},
 }{
  t.Run(label,func(t *testing.T){
   altered:=fromPost
   mutate(&altered)
   if sameSealedHumanProbeScope(sealed,altered){
    t.Fatal("operator-signed approval from another scope matched server seal")
   }
  })
 }
 revoked:=sealed;revoked.EffectiveExecutionState="revoked"
 if sameSealedHumanProbeScope(revoked,fromPost){
  t.Fatal("historical revoked server record was interpreted as active")
 }
}
