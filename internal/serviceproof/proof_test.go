package serviceproof
import "testing"

func TestChallengeResponseRejectsStaleAndForgedProofs(t *testing.T){
 token:="secret-secret-secret-secret-secret-secret"
 old:="older-older-older-older-older-older"
 nonce:=make([]byte,ChallengeBytes)
 for i:=range nonce{nonce[i]=byte(i)}
 sig:=Sign(token,nonce)
 if len(sig)!=64||!Verify(token,nonce,sig){t.Fatal("valid proof rejected")}
 different:=append([]byte(nil),nonce...)
 different[0]^=0xff
 if Verify(old,nonce,sig)||Verify(token,different,sig)||Verify(token,nonce,"invalid")||
  Verify(token,nonce,sig[:63])||Verify("short",nonce,sig) {
  t.Fatal("invalid/stale service proof accepted")
 }
 if Sign(token,nonce[:31])!=""{t.Fatal("short nonce signed")}
}
