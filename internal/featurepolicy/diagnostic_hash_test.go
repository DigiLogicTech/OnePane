package featurepolicy

import (
 "crypto/sha256"
 "encoding/hex"
 "strings"
 "testing"
 "time"
)

func TestDiagnosticOpaqueCorrelationCannotExposeCredentialLikeSuffix(t *testing.T){
 for _,id:=range []string{"run:PasswordValue123","task:APIKeySecretValue","trace:PrivateAccount123"} {
  d:=DecisionDiagnostic(time.Unix(1,0),id,"files.read",false)
  if d.CorrelationID=="redacted"||d.CorrelationID==id||strings.Contains(d.CorrelationID,id[strings.IndexByte(id,':')+1:]){
   t.Fatalf("raw identifier exposed in correlation metadata: %q",d.CorrelationID)
  }
  if len(d.CorrelationID)<28||len(d.CorrelationID)>32{
   t.Fatalf("hash format invalid: %q",d.CorrelationID)
  }
  again:=DecisionDiagnostic(time.Unix(5,0),id,"files.read",true)
  if d.CorrelationID!=again.CorrelationID{
   t.Fatal("identical authorized correlation lost deterministic fingerprint")
  }
  rawDigest:=sha256.Sum256([]byte(id))
  if strings.HasSuffix(d.CorrelationID,hex.EncodeToString(rawDigest[:12])){
   t.Fatal("diagnostic identifier uses a guessable unkeyed digest")
  }
 }
}
