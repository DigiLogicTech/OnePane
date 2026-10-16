package nodefederation

import (
 "context"
 "crypto/x509"
 "fmt"
 "net/http"
 "net/http/httptest"
 "sync/atomic"
 "testing"
)

func TestPinnedFederationClientRefusesCrossOriginPOSTRedirect(t *testing.T){
 var leaked atomic.Int64
 receiver:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  leaked.Add(1)
  w.WriteHeader(http.StatusNoContent)
 }))
 defer receiver.Close()
 for _,status:=range []int{http.StatusTemporaryRedirect,http.StatusPermanentRedirect}{
  t.Run(fmt.Sprint(status),func(t *testing.T){
   peer:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
    if r.Method!=http.MethodPost{t.Errorf("unexpected peer HTTP method %q",r.Method)}
    http.Redirect(w,r,receiver.URL+"/capture",status)
   }))
   defer peer.Close()
   leaf,err:=x509.ParseCertificate(peer.TLS.Certificates[0].Certificate[0])
   if err!=nil{t.Fatal(err)}
   fp:=CertificateFingerprint(leaf.Raw)
   svc:=&Service{}
   client:=svc.pinnedClient(fp,false)
   defer client.CloseIdleConnections()
   // This payload is deliberately only a marker, but in production it
   // can be a complete private inference prompt or management approval.
   err=postJSON(context.Background(),client,peer.URL+"/federation/v1/inference",
    map[string]any{"prompt":"federation-redirect-secret-marker"},nil)
   if err==nil{t.Fatal("redirect response counted as successful federation operation")}
   if got:=leaked.Load();got!=0{
    t.Fatalf("client forwarded a private federated request across origin %d times",got)
   }
  })
 }
}
