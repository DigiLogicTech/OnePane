package nodefederation

import (
 "crypto/tls"
 "crypto/x509"
 "errors"
 "testing"
 "time"
)

func TestPinnedNodeCertificateValidatesPurposeAndDates(t *testing.T){
 identity,err:=EnsureIdentity(t.TempDir(),"peer")
 if err!=nil{t.Fatal(err)}
 cert,err:=x509.ParseCertificate(identity.Certificate.Certificate[0])
 if err!=nil{t.Fatal(err)}
 now:=time.Now()
 state:=tls.ConnectionState{PeerCertificates:[]*x509.Certificate{cert}}
 check:=func(cs tls.ConnectionState,fp string)error{
  return validatePinnedPeerServerCertificate(cs,fp,now)
 }
 if err=check(state,identity.Fingerprint);err!=nil{
  t.Fatalf("valid pinned OnePane peer certificate was rejected: %v",err)
 }
 wrongFP:="sha256:0000000000000000000000000000000000000000000000000000000000000000"
 if err=check(state,wrongFP);!errors.Is(err,ErrPeerCertificate){
  t.Fatalf("untrusted peer certificate fingerprint passed: %v",err)
 }
 if err=check(tls.ConnectionState{},identity.Fingerprint);!errors.Is(err,ErrPeerCertificate){
  t.Fatalf("peer without TLS certificate passed: %v",err)
 }
 if err=check(tls.ConnectionState{PeerCertificates:[]*x509.Certificate{cert,cert}},
  identity.Fingerprint);!errors.Is(err,ErrPeerCertificate){
  t.Fatalf("ambiguous peer TLS leaf set passed: %v",err)
 }
 for name,mutate:=range map[string]func(*x509.Certificate){
  "not_yet_valid":func(c *x509.Certificate){c.NotBefore=now.Add(time.Minute)},
  "expired":func(c *x509.Certificate){c.NotAfter=now.Add(-time.Minute)},
  "wrong_purpose":func(c *x509.Certificate){c.ExtKeyUsage=[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}},
  "no_eku":func(c *x509.Certificate){c.ExtKeyUsage=nil},
  "no_digital_signature":func(c *x509.Certificate){c.KeyUsage=x509.KeyUsageKeyEncipherment},
  "certificate_authority":func(c *x509.Certificate){c.IsCA=true},
  "certificate_signing_key":func(c *x509.Certificate){c.KeyUsage|=x509.KeyUsageCertSign},
 }{
  t.Run(name,func(t *testing.T){
   bad:=*cert
   mutate(&bad)
   if err:=check(tls.ConnectionState{PeerCertificates:[]*x509.Certificate{&bad}},
    identity.Fingerprint);!errors.Is(err,ErrPeerCertificate){
    t.Fatalf("wrongly scoped/expired pinned certificate accepted: %v",err)
   }
  })
 }
}
