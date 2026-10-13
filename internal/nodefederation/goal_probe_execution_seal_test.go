package nodefederation

import (
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestPinnedOriginalOnePaneSignerSealsOnlyLiveHumanScope(t *testing.T){
 now:=time.Now().UTC().Truncate(time.Millisecond)
 original,err:=EnsureIdentity(t.TempDir(),"onepane-hub")
 if err!=nil{t.Fatal(err)}
 peer,err:=EnsureIdentity(t.TempDir(),"other-hub")
 if err!=nil{t.Fatal(err)}
 p:=projectworkspace.GoalNodeProbeExecutionPermit{
  ID:"grant1",ProjectID:"project1",GoalID:"goal1",
  ProjectWorkspaceID:"workspace1",InstrumentKeyID:"instrument",
  ChallengeID:"challenge1",SourceID:"router",DeclaredTargetID:"target1",
  TLSPinID:"pin1",PeerNodeID:"peer1",PeerTLSFingerprint:"sha256:"+strings.Repeat("a",64),
  OriginalChallengeSHA256:"sha256:"+strings.Repeat("b",64),
  InstrumentPublicKeySHA256:"sha256:"+strings.Repeat("c",64),
  OperatorPublicKeySHA256:"sha256:"+strings.Repeat("d",64),
  PlanSHA256:"sha256:"+strings.Repeat("e",64),PlanGeneration:1,
  Status:"approved",EffectiveExecutionState:"active",
  Revision:1,ApprovedBy:"human1",ApprovedAt:now.Add(-time.Second).UnixMilli(),
  ExpiresAt:now.Add(time.Minute).UnixMilli(),
 }
 s,err:=SealHumanNodeProbeExecution(original,p,now)
 if err!=nil{t.Fatalf("live human scope not signed by original server: %v",err)}
 assert:=func(w SealedNodeProbeAuthority,cert []byte,id,pin string)error{
  _,err:=VerifySealedNodeProbeExecution(w,cert,id,pin);return err
 }
 if err=assert(s,original.Certificate.Certificate[0],"onepane-hub",original.Fingerprint);err!=nil{
  t.Fatalf("separately enrolled original OnePane cert rejected: %v",err)
 }
 if err=assert(s,peer.Certificate.Certificate[0],"other-hub",peer.Fingerprint);err==nil{
  t.Fatal("unrelated OnePane issuer verified authority")
 }
 if err=assert(s,original.Certificate.Certificate[0],"onepane-hub",peer.Fingerprint);err==nil{
  t.Fatal("untrusted server certificate pin verified OnePane approval")
 }
 wrong:=s;wrong.Signature=append([]byte(nil),s.Signature...)
 wrong.Signature[0]^=1
 if err=assert(wrong,original.Certificate.Certificate[0],"onepane-hub",original.Fingerprint);err==nil{
  t.Fatal("altered original server signature verified")
 }
 wrong=s;wrong.SigningPayload=append([]byte(nil),s.SigningPayload...)
 wrong.SigningPayload=append(wrong.SigningPayload,' ')
 if err=assert(wrong,original.Certificate.Certificate[0],"onepane-hub",original.Fingerprint);err==nil{
  t.Fatal("noncanonical server envelope admitted")
 }
 cases:=map[string]func(*projectworkspace.GoalNodeProbeExecutionPermit){
  "revoked":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.Status="revoked"},
  "inactive":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.EffectiveExecutionState="scope_inactive"},
  "unsigned-operator":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.OperatorPublicKeySHA256=""},
  "old-plan":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.PlanGeneration=0},
  "expired":func(x *projectworkspace.GoalNodeProbeExecutionPermit){x.ExpiresAt=now.Add(-time.Second).UnixMilli()},
 }
 for name,change:=range cases{t.Run(name,func(t *testing.T){
  altered:=p;change(&altered)
  if _,err:=SealHumanNodeProbeExecution(original,altered,now);err==nil{
   t.Fatal("original OnePane server signed unavailable execution authority")
  }
 })}
}
