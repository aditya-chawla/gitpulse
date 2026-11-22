package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// randomToken returns n random bytes, URL-safe base64 encoded.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken is how session tokens are stored: a DB leak doesn't leak sessions.
func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}
