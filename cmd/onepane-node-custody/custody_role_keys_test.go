package main

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/x509"
 "encoding/json"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "path/filepath"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
 "github.com/DigiLogicTech/OnePane/internal/nodefederation"
)

func TestCustodyRoleKeysRejectAllSixPairCollisions(t *testing.T){
 issuer,err:=nodefederation.EnsureIdentity(t.TempDir(),"issuer")
 if err!=nil{t.Fatal(err)}
 worker,err:=nodefederation.EnsureIdentity(t.TempDir(),"worker")
 if err!=nil{t.Fatal(err)}
 serverCert:=issuer.Certificate.Certificate[0]
 nodeCert:=worker.Certificate.Certificate[0]
 serverLeaf,err:=x509.ParseCertificate(serverCert)
 if err!=nil{t.Fatal(err)}
 nodeLeaf,err:=x509.ParseCertificate(nodeCert)
 if err!=nil{t.Fatal(err)}
 serverKey:=serverLeaf.PublicKey.(ed25519.PublicKey)
 nodeKey:=nodeLeaf.PublicKey.(ed25519.PublicKey)
 human,_,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 instrument,_,err:=ed25519.GenerateKey(rand.Reader)
 if err!=nil{t.Fatal(err)}
 if err:=requireDistinctCustodyRoles(serverCert,nodeCert,human,instrument);err!=nil{
  t.Fatalf("four distinct role keys rejected: %v",err)
 }
 cases:=[]struct{
  name string
  serverDER,nodeDER []byte
  human,instrument ed25519.PublicKey
 }{
  {"server_node",nodeCert,nodeCert,human,instrument},
  {"server_human",serverCert,nodeCert,serverKey,instrument},
  {"server_instrument",serverCert,nodeCert,human,serverKey},
  {"node_human",serverCert,nodeCert,nodeKey,instrument},
  {"node_instrument",serverCert,nodeCert,human,nodeKey},
  {"human_instrument",serverCert,nodeCert,human,human},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
  if err:=requireDistinctCustodyRoles(tc.serverDER,tc.nodeDER,tc.human,tc.instrument);err==nil{
   t.Fatal("duplicate signing key accepted as independent custody role")
  }
 })}
}

func TestNodeExecutionRejectsClonedServerKeyBeforeNetworkAndNonce(t *testing.T){
 c,ch,_:=nodeProbeFixture(t)
 node,err:=loadExistingNodeIdentity(c.NodeDataDir,c.NodeID)
 if err!=nil{t.Fatal(err)}
 var signed SignedNodeExecutionPermit
 if err:=readCanonical(c.ExecutionPermitFile,&signed,true);err!=nil{t.Fatal(err)}
 // The attacker uses the EXISTING enrolled Node key as the original
 // OnePane issuer as well. Both signatures verify individually, but four
 // independent software identities do not exist.
 if _,err:=nodefederation.SealHumanNodeProbeExecution(node,signed.Permit,time.Now());err==nil{
  t.Fatal("OnePane issuer accepted its own paired Node signing identity")
 }
 // Simulate a hostile detached file bypassing the normal issuer endpoint:
 // signatures are genuine but server/Node roles share one private key.
 seal:=forgeClonedIssuerSeal(t,node,signed.Permit,time.Now())
 c.ServerSealFile=writeCanonicalTest(t,t.TempDir(),"cloned-issuer-seal.json",seal)
 c.ServerCertificateFile=filepath.Join(c.NodeDataDir,"keys","federation.crt")
 c.ServerNodeID=c.NodeID
 c.ServerFingerprint=node.Fingerprint
 dial:=&recordingNodeDialer{}
 if err:=ExecuteExplicitNodeProbe(context.Background(),c,dial,time.Now);err==nil{
  t.Fatal("reused Node key passed OnePane server custody gate")
 }
 if dial.calls!=0{t.Fatalf("denied duplicate key opened %d socket(s)",dial.calls)}
 if err:=instrumentprobe.ClaimLocalProbeOnce(c.InstrumentPrivateKeyFile,ch);err!=nil{
  t.Fatalf("denied duplicate key spent the one-shot nonce: %v",err)
 }
}

func forgeClonedIssuerSeal(t *testing.T,
 node nodefederation.Identity,p projectworkspace.GoalNodeProbeExecutionPermit,
 at time.Time)nodefederation.SealedNodeProbeAuthority{
 t.Helper()
 cert,err:=x509.ParseCertificate(node.Certificate.Certificate[0])
 if err!=nil{t.Fatal(err)}
 env:=struct{
  Domain string `json:"domain"`
  ServerNodeID string `json:"server_node_id"`
  ServerCertificateSHA256 string `json:"server_certificate_sha256"`
  SignedAt int64 `json:"signed_at"`
  Permit projectworkspace.GoalNodeProbeExecutionPermit `json:"permit"`
 }{
  Domain:"OnePane/server-node-probe-authority/ed25519/v1",
  ServerNodeID:cert.Subject.CommonName,
  ServerCertificateSHA256:node.Fingerprint,
  SignedAt:at.UnixMilli(),Permit:p,
 }
 raw,err:=json.Marshal(env);if err!=nil{t.Fatal(err)}
 key,ok:=node.Certificate.PrivateKey.(ed25519.PrivateKey)
 if !ok{t.Fatal("test requires the original Node Ed25519 signer")}
 return nodefederation.SealedNodeProbeAuthority{
  SigningPayload:raw,Signature:ed25519.Sign(key,raw),
 }
}
