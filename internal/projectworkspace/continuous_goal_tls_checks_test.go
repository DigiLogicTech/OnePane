package projectworkspace

import(
 "context"
 "crypto/sha256"
 "encoding/hex"
 "net"
 "net/http/httptest"
 "testing"
 "time"
)

// Only tests may redirect a preapproved literal IPv4:port to a local
// hermetic TLS listener. The production method uses net.Dialer directly.
type testTLSPinDialer struct{
 target string
 address string
 called int
}
func(d *testTLSPinDialer)DialContext(ctx context.Context,network,address string)(net.Conn,error){
 d.called++
 if network!="tcp4"||address!=d.target{return nil,ErrInvalidCommand}
 return (&net.Dialer{}).DialContext(ctx,"tcp",d.address)
}
func TestPinnedCertificateHandshakeRejectsWrongFingerprintAndUnsafeTargets(t *testing.T){
 server:=httptest.NewTLSServer(nil)
 defer server.Close()
 pin:=sha256.Sum256(server.Certificate().Raw)
 fingerprint:="sha256:"+hex.EncodeToString(pin[:])
 wrong:="sha256:"+hex.EncodeToString(make([]byte,32))
 hostport:=server.Listener.Addr().String()
 d:=&testTLSPinDialer{target:"192.168.42.77:443",address:hostport}
 run:=func(fp string)string{
  return tlsPinnedHandshake(context.Background(),d,"192.168.42.77",443,1300,fp)
 }
 if got:=run(fingerprint);got!="matched"{
  t.Fatalf("out-of-band exactly pinned server cert failed: %s",got)
 }
 if got:=run(wrong);got!="mismatched"{
  t.Fatalf("wrong cert was not a distinct untrusted result: %s",got)
 }
 if got:=run("sha256:INVALID");got!="tls_failed"{
  t.Fatalf("malformed operator pin admitted: %s",got)
 }
 if got:=tlsPinnedHandshake(context.Background(),d,"127.0.0.1",443,
  1300,fingerprint);got!="tls_failed"{
  t.Fatalf("loopback escaped private target restriction: %s",got)
 }
 if got:=tlsPinnedHandshake(context.Background(),d,"metadata.google.internal",443,
  1300,fingerprint);got!="tls_failed"{
  t.Fatalf("DNS hostname escaped private literal restriction: %s",got)
 }
 if got:=tlsPinnedHandshake(context.Background(),d,"192.168.42.77",443,
  5000,fingerprint);got!="tls_failed"{
  t.Fatalf("long socket timeout escaped hard cap: %s",got)
 }
 if d.called!=2{t.Fatalf("unsafe target caused a network call: %d",d.called)}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 result:=tlsPinnedHandshake(ctx,d,"192.168.42.77",443,1000,fingerprint)
 if result=="matched"{t.Fatalf("cancelled invocation forged TLS match: %s",result)}
}
func TestPinnedTLSCheckHasNonHealthResultDefaults(t *testing.T){
 check:=GoalPinnedTLSCheck{Status:"matched",CertificatePossessionMatched:true}
 if check.DeviceIdentityVerified||check.MonitoringActive{
  t.Fatal("certificate possession was promoted to physical-device trust")
 }
 _=time.Second
}
