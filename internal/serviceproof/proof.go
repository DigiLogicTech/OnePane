// Package serviceproof authenticates a new Windows service child's HTTP readiness.
package serviceproof

import (
 "crypto/hmac"
 "crypto/sha256"
 "encoding/hex"
)

const ChallengeHeader="X-OnePane-Service-Challenge"
const ProofHeader="X-OnePane-Service-Proof"
const ChallengeBytes=32

// Sign proves possession of the per-launch secret over a fresh nonce.
func Sign(token string, challenge []byte) string {
 if len(token)<32||len(challenge)!=ChallengeBytes{return ""}
 mac:=hmac.New(sha256.New,[]byte(token))
 _,_=mac.Write(challenge)
 return hex.EncodeToString(mac.Sum(nil))
}

func Verify(token string,challenge []byte,proof string) bool {
 if len(proof)!=sha256.Size*2{return false}
 got,err:=hex.DecodeString(proof)
 if err!=nil{return false}
 expected:=Sign(token,challenge)
 if expected==""{return false}
 want,_:=hex.DecodeString(expected)
 return hmac.Equal(want,got)
}
