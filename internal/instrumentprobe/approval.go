package instrumentprobe

import (
 "bytes"
 "crypto/ed25519"
 "errors"
 "time"
)

// VerifyLocalReceiptAgainstApproval requires separately retained original
// human-approved OnePane challenge and private probe plan. A cryptographically
// genuine signature alone does not prove OnePane issued the challenge.
// Original approvals must be supplied from a trusted independent channel.
// This is offline historical evidence validation, never a new network grant,
// instrument hardware attestation, paired-Node vantage or incident vote.
func VerifyLocalReceiptAgainstApproval(r LocalProbeReceipt, public ed25519.PublicKey,
 issued Challenge, plan ProbePlan) error {
 if err:=VerifyLocalReceipt(r,public);err!=nil{return err}
 own:=r.Challenge
 if own.ID!=issued.ID||own.KeyID!=issued.KeyID||
  own.ProjectID!=issued.ProjectID||own.GoalID!=issued.GoalID||
  own.IssuedAt!=issued.IssuedAt||own.ExpiresAt!=issued.ExpiresAt||
  own.Status!=issued.Status||!bytes.Equal(own.Nonce,issued.Nonce)||
  !bytes.Equal(own.SigningPayload,issued.SigningPayload){
  return errors.New("local receipt differs from independently retained OnePane challenge")
 }
 target,err:=ValidateProbePlan(issued,plan,time.UnixMilli(r.ObservedAt))
 if err!=nil{return err}
 if r.Target!=target{
  return errors.New("local receipt differs from human-approved endpoint and timeout")
 }
 return nil
}
