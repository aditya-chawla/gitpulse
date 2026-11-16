package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// Sealer encrypts secrets at rest with AES-256-GCM.
type Sealer struct{ aead cipher.AEAD }

func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal returns ciphertext and a fresh random nonce. aad binds the ciphertext to
// its owner (e.g. the user ID) so rows can't be swapped between users.
func (s *Sealer) Seal(plaintext, aad []byte) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return s.aead.Seal(nil, nonce, plaintext, aad), nonce, nil
}

func (s *Sealer) Open(ciphertext, nonce, aad []byte) ([]byte, error) {
	return s.aead.Open(nil, nonce, ciphertext, aad)
}

// randomToken returns a URL-safe random string with n bytes of entropy.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}
