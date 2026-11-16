package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func setBase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("GITHUB_CLIENT_ID", "id")
	t.Setenv("GITHUB_CLIENT_SECRET", "secret")
	t.Setenv("OAUTH_REDIRECT_URL", "http://localhost:8080/api/auth/callback")
	t.Setenv("ALLOWED_USERS", " Harshit3011 , other ")
	t.Setenv("TOKEN_ENC_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("SCAN_SECRET", strings.Repeat("s", 32))
}

func TestLoadOK(t *testing.T) {
	setBase(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://a.example/, https://b.example")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.AllowedUsers["harshit3011"] || !c.AllowedUsers["other"] || len(c.AllowedUsers) != 2 {
		t.Errorf("allowlist = %v", c.AllowedUsers)
	}
	if len(c.CORSOrigins) != 2 || c.CORSOrigins[0] != "https://a.example" {
		t.Errorf("cors = %v", c.CORSOrigins)
	}
	if !c.CookieSecure {
		t.Error("cookies must default to Secure")
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	setBase(t)
	t.Setenv("TOKEN_ENC_KEY", "short")
	t.Setenv("SCAN_SECRET", "tiny")
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("COOKIE_SAMESITE", "none")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"TOKEN_ENC_KEY", "SCAN_SECRET", "COOKIE_SAMESITE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %s: %v", want, err)
		}
	}
}
