package util

import (
	"crypto/sha256"
	"encoding/base64"
	"math/rand"
	"time"
)

const (
	codeVerifierLength = 128
	stateLength        = 32
	charset            = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// GenerateCodeVerifier generates a random code verifier for PKCE
func GenerateCodeVerifier() string {
	b := make([]byte, codeVerifierLength)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// GenerateCodeChallengeS256 generates a code challenge from verifier using S256 method
func GenerateCodeChallengeS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// GenerateState generates a random state parameter for OAuth
func GenerateState() string {
	b := make([]byte, stateLength)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
