//go:build integration

package nodefederation

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "errors"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestInboundPairedNodeClientCertificateRequiresCurrentLeafClientIdentity(t *testing.T){
 identity,err:=EnsureIdentity(t.TempDir(),"peer")
 if err!=nil{t.Fatal(err)}
 cert,err:=x509.ParseCertificate(identity.Certificate.Certificate[0])
 if err!=nil{t.Fatal(err)}
 now:=time.Now()
 if err:=validatePairedNodeClientCertificate(cert,now);err!=nil{
  t.Fatalf("normal OnePane client leaf rejected: %v",err)
 }
 for name,mutate:=range map[string]func(*x509.Certificate){
  "expired":func(c *x509.Certificate){c.NotAfter=now.Add(-time.Second)},
  "not yet valid":func(c *x509.Certificate){c.NotBefore=now.Add(time.Hour)},
  "client EKU missing":func(c *x509.Certificate){c.ExtKeyUsage=[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}},
  "no EKU":func(c *x509.Certificate){c.ExtKeyUsage=nil},
  "CA certificate":func(c *x509.Certificate){c.IsCA=true},
  "cert signer":func(c *x509.Certificate){c.KeyUsage|=x509.KeyUsageCertSign},
  "no digital signature":func(c *x509.Certificate){c.KeyUsage=x509.KeyUsageKeyEncipherment},
 }{
  t.Run(name,func(t *testing.T){
   altered:=*cert
   mutate(&altered)
   if err:=validatePairedNodeClientCertificate(&altered,now);!errors.Is(err,ErrPeerCertificate){
    t.Fatalf("invalid pinned client certificate accepted: %v",err)
   }
  })
 }
}

func TestInboundTLSRejectsExpiredPairedPeerBeforeDatabaseAuthority(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDBForPeerCertificate(t)
 defer db.Close()
 ident,err:=EnsureIdentity(t.TempDir(),"peer")
 if err!=nil{t.Fatal(err)}
 cert,err:=x509.ParseCertificate(ident.Certificate.Certificate[0])
 if err!=nil{t.Fatal(err)}
 for _,q:=range []struct{sql string;args []any}{
  {`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,
    trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
    VALUES('peer','Peer',0,'peer','paired','PAIRED_MTLS','{}','{}',?,1,?,?)`,[]any{now,now,now}},
  {`INSERT INTO node_pairings(
    id,peer_node_id,direction,status,pairing_token,pairing_token_hash,
    pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,
    peer_tls_fingerprint,peer_certificate_pem,expires_at,paired_at,
    revision,created_at,updated_at)
    VALUES('pair','peer','inbound','paired','t','h','123456','h',1,1,?,'',?,?,1,?,?)`,
    []any{ident.Fingerprint,now+60000,now,now,now}},
 }{
  if _,err=db.SQL().ExecContext(ctx,q.sql,q.args...);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"origin",Local:true},Identity{},"",30*time.Second)
 req:=httptest.NewRequest(http.MethodPost,"https://localhost/federation/v1/inference",nil)
 req.TLS=&tls.ConnectionState{PeerCertificates:[]*x509.Certificate{cert}}
 if peer,err:=svc.peerFromTLS(req);err!=nil||peer!="peer"{
  t.Fatalf("valid paired Node client certificate denied: peer=%q err=%v",peer,err)
 }
 invalid:=*cert
 invalid.NotAfter=time.Now().Add(-time.Second)
 req.TLS=&tls.ConnectionState{PeerCertificates:[]*x509.Certificate{&invalid}}
 if _,err:=svc.peerFromTLS(req);!errors.Is(err,ErrPeerCertificate){
  t.Fatalf("expired paired peer retained federation execution authority: %v",err)
 }
}

func seedNodeDBForPeerCertificate(t *testing.T)(*sqliteStore.DB,int64){return seedNodeDB(t,"origin","origin-fp")}
