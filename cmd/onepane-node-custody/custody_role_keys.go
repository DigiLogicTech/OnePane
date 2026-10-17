package main

import (
 "bytes"
 "crypto/ed25519"
 "crypto/x509"
 "errors"
)

// requireDistinctCustodyRoles validates all FOUR independently enrolled
// SOFTWARE signing identities for a Node probe. Four signatures made by
// only three actual private keys must not be presented as four-party
// custody. Key separation does not attest hardware, location or honesty.
func requireDistinctCustodyRoles(serverDER,nodeDER []byte,
 human,instrument ed25519.PublicKey)error{
 serverCert,err:=x509.ParseCertificate(serverDER)
 if err!=nil{return err}
 nodeCert,err:=x509.ParseCertificate(nodeDER)
 if err!=nil{return err}
 server,ok:=serverCert.PublicKey.(ed25519.PublicKey)
 if !ok||len(server)!=ed25519.PublicKeySize{
  return errors.New("OnePane server requires an enrolled Ed25519 identity")
 }
 node,ok:=nodeCert.PublicKey.(ed25519.PublicKey)
 if !ok||len(node)!=ed25519.PublicKeySize||
  len(human)!=ed25519.PublicKeySize||
  len(instrument)!=ed25519.PublicKeySize{
  return errors.New("all four custody roles require independently enrolled Ed25519 public keys")
 }
 keys:=[][]byte{server,node,human,instrument}
 for i:=0;i<len(keys);i++{
  for j:=i+1;j<len(keys);j++{
   if bytes.Equal(keys[i],keys[j]){
    return errors.New("OnePane server, Node, human and instrument signing roles must use distinct keys")
   }
  }
 }
 return nil
}
