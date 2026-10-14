package main

import (
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/x509"
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
 seal,err:=nodefederation.SealHumanNodeProbeExecution(node,signed.Permit,time.Now())
 if err!=nil{t.Fatal(err)}
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
