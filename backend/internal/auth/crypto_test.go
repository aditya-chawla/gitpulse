package auth

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestSealerRoundTripAndAAD(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	s, err := NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	ct, nonce, err := s.Seal([]byte("gho_secret"), []byte("42"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("gho_secret")) {
		t.Fatal("ciphertext contains plaintext")
	}
	pt, err := s.Open(ct, nonce, []byte("42"))
	if err != nil || string(pt) != "gho_secret" {
		t.Fatalf("round trip failed: %q %v", pt, err)
	}
	if _, err := s.Open(ct, nonce, []byte("43")); err == nil {
		t.Fatal("expected failure when ciphertext is used for another user")
	}
	_, nonce2, _ := s.Seal([]byte("gho_secret"), []byte("42"))
	if bytes.Equal(nonce, nonce2) {
		t.Fatal("nonce reused")
	}
}

func TestNewSealerRejectsBadKey(t *testing.T) {
	if _, err := NewSealer(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}

func TestAllowedIsCaseInsensitive(t *testing.T) {
	s := &Service{cfg: Config{AllowedUsers: map[string]bool{"harshit3011": true}}}
	if !s.Allowed("Harshit3011") {
		t.Fatal("expected allowed")
	}
	if s.Allowed("someoneelse") {
		t.Fatal("expected rejected")
	}
}
