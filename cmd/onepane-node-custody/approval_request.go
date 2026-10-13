package main

import (
 "bytes"
 "crypto/ed25519"
 "encoding/base64"
 "errors"
 "flag"
 "io"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// HumanNodeApprovalRequest is the bounded body for the authenticated human
// OnePane API. Its operator private key never leaves the operator's host.
// The server determines the actor principal from its authenticated session.
type HumanNodeApprovalRequest struct {
 InstrumentKeyID string `json:"instrument_key_id"`
 ChallengeID string `json:"challenge_id"`
 PeerNodeID string `json:"peer_node_id"`
 OperatorPublicKey []byte `json:"operator_public_key"`
 OperatorProofSignature []byte `json:"operator_proof_signature"`
 ExpectedGoalRevision int64 `json:"expected_goal_revision"`
}

func makeHumanNodeApprovalRequest(
 c projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand,
 priv ed25519.PrivateKey)(HumanNodeApprovalRequest,error){
 if len(priv)!=ed25519.PrivateKeySize||
  !bytes.Equal(priv.Public().(ed25519.PublicKey),c.OperatorPublicKey){
  return HumanNodeApprovalRequest{},errors.New("human operator key is not separately enrolled")
 }
 payload,err:=projectworkspace.NodeOperatorExecutionApprovalSigningBytes(c)
 if err!=nil{return HumanNodeApprovalRequest{},err}
 signature:=ed25519.Sign(priv,payload)
 return HumanNodeApprovalRequest{
  InstrumentKeyID:c.InstrumentKeyID,ChallengeID:c.ChallengeID,
  PeerNodeID:c.PeerNodeID,ExpectedGoalRevision:c.ExpectedGoalRevision,
  OperatorPublicKey:c.OperatorPublicKey,OperatorProofSignature:signature,
 },nil
}

// prepare-approval-request runs ONLY on the human operator's trusted host.
// It does no API call or networking, and never creates a signing key.
func runPrepareHumanApproval(args []string)error{
 fs:=flag.NewFlagSet("prepare-approval-request",flag.ContinueOnError)
 fs.SetOutput(io.Discard)
 project:=fs.String("project-id","","original Project ID")
 goal:=fs.String("goal-id","","original Goal ID")
 instrument:=fs.String("instrument-key-id","","original instrument enrollment ID")
 challenge:=fs.String("challenge-id","","original issued random challenge ID")
 node:=fs.String("peer-node-id","","exact approved paired Node ID")
 actor:=fs.String("operator-principal-id","","currently authenticated human principal ID")
 revision:=fs.Int64("goal-revision",0,"current optimistic Goal revision")
 keyFile:=fs.String("operator-private-key","","separately held 0600 base64 Ed25519 operator private key")
 output:=fs.String("output","","new exclusive owner-only human API request JSON")
 if err:=fs.Parse(args);err!=nil{return err}
 if fs.NArg()!=0||*project==""||*goal==""||*instrument==""||
  *challenge==""||*node==""||*actor==""||*revision<1||
  *keyFile==""||*output==""||
  strings.TrimSpace(*actor)!=*actor{
  return errors.New("complete Project, Goal, instrument, challenge, Node, actor, revision, private key and output required")
 }
 raw,err:=readBounded(*keyFile,256,true)
 if err!=nil{return err}
 material,err:=base64.StdEncoding.DecodeString(string(bytes.TrimSpace(raw)))
 if err!=nil||len(material)!=ed25519.PrivateKeySize{
  return errors.New("expected protected base64 64-byte Ed25519 human operator key")
 }
 priv:=ed25519.PrivateKey(material)
 request,err:=makeHumanNodeApprovalRequest(
  projectworkspace.ApproveGoalNodeProbeExecutionPermitCommand{
   ProjectID:*project,GoalID:*goal,InstrumentKeyID:*instrument,
   ChallengeID:*challenge,PeerNodeID:*node,ActorPrincipalID:*actor,
   ExpectedGoalRevision:*revision,
   OperatorPublicKey:priv.Public().(ed25519.PublicKey),
  },priv)
 if err!=nil{return err}
 return exclusivePrivateJSON(*output,request)
}
