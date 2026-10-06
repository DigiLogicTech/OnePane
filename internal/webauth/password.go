package webauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	passwordKDF        = "pbkdf2-hmac-sha256"
	passwordIterations = 600000
	passwordSaltBytes  = 32
	passwordKeyBytes   = 32
)

type passwordCredential struct {
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Hash       string `json:"hash"`
}

func validatePassword(password string) error {
	if len(password) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	if len(password) > 1024 {
		return errors.New("password is too long")
	}
	return nil
}

func hashPassword(password string) (json.RawMessage, error) {
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key := pbkdf2SHA256([]byte(password), salt, passwordIterations, passwordKeyBytes)
	c := passwordCredential{KDF: passwordKDF, Iterations: passwordIterations, Salt: base64.RawURLEncoding.EncodeToString(salt), Hash: base64.RawURLEncoding.EncodeToString(key)}
	for i := range key {
		key[i] = 0
	}
	return json.Marshal(c)
}

func verifyPassword(raw json.RawMessage, password string) bool {
	var c passwordCredential
	if json.Unmarshal(raw, &c) != nil || c.KDF != passwordKDF || c.Iterations < 100000 || c.Iterations > 2000000 {
		return false
	}
	salt, err1 := base64.RawURLEncoding.DecodeString(c.Salt)
	want, err2 := base64.RawURLEncoding.DecodeString(c.Hash)
	if err1 != nil || err2 != nil || len(salt) < 16 || len(want) != passwordKeyBytes {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, c.Iterations, len(want))
	ok := subtle.ConstantTimeCompare(got, want) == 1
	for i := range got {
		got[i] = 0
	}
	return ok
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	if iterations <= 0 || keyLen <= 0 {
		panic("invalid PBKDF2 parameters")
	}
	hLen := sha256.Size
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func normalizeUsername(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) < 3 || len(v) > 128 {
		return "", fmt.Errorf("username must be 3-128 characters")
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' || r == '@' {
			continue
		}
		return "", fmt.Errorf("username contains unsupported characters")
	}
	return v, nil
}
