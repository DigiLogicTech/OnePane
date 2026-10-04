package webauth

import (
	"encoding/json"
	"testing"
)

func TestPBKDF2Vector(t *testing.T) {
	got := pbkdf2SHA256([]byte("password"), []byte("salt"), 1, 32)
	const want = "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"
	if fmtHex(got) != want {
		t.Fatalf("got %s", fmtHex(got))
	}
}
func fmtHex(b []byte) string {
	const h = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = h[v>>4]
		out[i*2+1] = h[v&15]
	}
	return string(out)
}
func TestPasswordCredentialRejectsWrongPassword(t *testing.T) {
	// Avoid the production iteration count in a unit test while still testing parser/compare.
	salt := []byte("0123456789abcdef")
	key := pbkdf2SHA256([]byte("correct horse battery staple"), salt, 100000, 32)
	raw, _ := json.Marshal(passwordCredential{KDF: passwordKDF, Iterations: 100000, Salt: "MDEyMzQ1Njc4OWFiY2RlZg", Hash: base64Raw(key)})
	if !verifyPassword(raw, "correct horse battery staple") {
		t.Fatal("valid password rejected")
	}
	if verifyPassword(raw, "wrong password") {
		t.Fatal("wrong password accepted")
	}
}
func base64Raw(b []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out []byte
	for i := 0; i < len(b); i += 3 {
		rem := len(b) - i
		v := uint(b[i]) << 16
		if rem > 1 {
			v |= uint(b[i+1]) << 8
		}
		if rem > 2 {
			v |= uint(b[i+2])
		}
		out = append(out, alphabet[(v>>18)&63], alphabet[(v>>12)&63])
		if rem > 1 {
			out = append(out, alphabet[(v>>6)&63])
		}
		if rem > 2 {
			out = append(out, alphabet[v&63])
		}
	}
	return string(out)
}
