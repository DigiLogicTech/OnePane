// Package serviceproof generates purpose-separated per-launch SCM proofs.
// Neither readiness nor shutdown transmits the random launch secret over HTTP.
package serviceproof

import (
 "crypto/hmac"
 "crypto/sha256"
 "encoding/hex"
)

const ChallengeHeader = "X-OnePane-Service-Challenge"
const ProofHeader = "X-OnePane-Service-Proof"
const ChallengeBytes = 32

func Sign(token string, challenge []byte) string {
 return sign(token,challenge,"onepane/scm/readiness/v1")
}
func SignShutdown(token string, challenge []byte) string {
 return sign(token,challenge,"onepane/scm/shutdown/v1")
}
func sign(token string, challenge []byte, purpose string) string {
 if len(token)<32||len(challenge)!=ChallengeBytes{return ""}
 mac:=hmac.New(sha256.New,[]byte(token))
 _,_=mac.Write([]byte(purpose))
 _,_=mac.Write([]byte{0})
 _,_=mac.Write(challenge)
 return hex.EncodeToString(mac.Sum(nil))
}
func Verify(token string, challenge []byte, proof string) bool {
 return verify(token,challenge,proof,"onepane/scm/readiness/v1")
}
func VerifyShutdown(token string, challenge []byte, proof string) bool {
 return verify(token,challenge,proof,"onepane/scm/shutdown/v1")
}
func verify(token string, challenge []byte, proof, purpose string) bool {
 if len(proof)!=sha256.Size*2{return false}
 got,err:=hex.DecodeString(proof)
 if err!=nil{return false}
 value:=sign(token,challenge,purpose)
 if value==""{return false}
 expected,_:=hex.DecodeString(value)
 return hmac.Equal(expected,got)
}
