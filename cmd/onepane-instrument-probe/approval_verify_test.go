package main
import (
 "crypto/ed25519"
 "crypto/rand"
 "encoding/base64"
 "strings"
 "testing"
)
func TestReceiptOfflineVerifierRequiresIndependentServerApprovals(t *testing.T){
 pub,_,err:=ed25519.GenerateKey(rand.Reader);if err!=nil{t.Fatal(err)}
 err=runVerifyReceipt([]string{"--receipt","report.json",
  "--public-key",base64.StdEncoding.EncodeToString(pub)})
 if err==nil||!strings.Contains(err.Error(),"trusted --challenge"){
  t.Fatalf("self-signed report without server approvals admitted: %v",err)
 }
}
