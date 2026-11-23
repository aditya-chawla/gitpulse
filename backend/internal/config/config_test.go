package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"sync"
	"testing"
)

var testKeyPEM = sync.OnceValue(func() []byte {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
})

func setBase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("GITHUB_CLIENT_ID", "id")
	t.Setenv("GITHUB_CLIENT_SECRET", "secret")
	t.Setenv("OAUTH_REDIRECT_URL", "http://localhost:8080/api/auth/callback")
	t.Setenv("ALLOWED_USERS", " Harshit3011 , other ")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", base64.StdEncoding.EncodeToString(testKeyPEM()))
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
	if c.GitHubAppKey == nil {
		t.Error("app key not parsed")
	}
}

func TestPrivateKeyAcceptsEscapedPEM(t *testing.T) {
	setBase(t)
	// Some dashboards only take one line: PEM with literal "\n" escapes.
	t.Setenv("GITHUB_APP_PRIVATE_KEY", strings.ReplaceAll(string(testKeyPEM()), "\n", `\n`))
	if c, err := Load(); err != nil || c.GitHubAppKey == nil {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	setBase(t)
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "not a key")
	t.Setenv("SCAN_SECRET", "tiny")
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("COOKIE_SAMESITE", "none")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"GITHUB_APP_PRIVATE_KEY", "SCAN_SECRET", "COOKIE_SAMESITE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %s: %v", want, err)
		}
	}
}

func TestLLMModes(t *testing.T) {
	cases := []struct {
		name, provider, key, model string
		wantProvider, wantReason   string
		wantErr                    bool
	}{
		{"no key: off, no error", "", "", "", "none", "no API key", false},
		{"key added later: on automatically", "", "sk-test", "", "anthropic", "", false},
		{"explicit anthropic without key: off, no error", "anthropic", "", "", "none", "no API key", false},
		{"explicit none wins over a key", "none", "sk-test", "", "none", "LLM_PROVIDER=none", false},
		{"unknown model needs prices", "", "sk-test", "claude-future-9", "anthropic", "", true},
		{"bad provider", "openai", "", "", "none", "no API key", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setBase(t)
			t.Setenv("LLM_PROVIDER", c.provider)
			t.Setenv("ANTHROPIC_API_KEY", c.key)
			t.Setenv("LLM_MODEL", c.model)
			cfg, err := Load()
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v", err)
			}
			if cfg.LLM.Provider != c.wantProvider || cfg.LLM.DisabledReason != c.wantReason {
				t.Fatalf("provider=%q reason=%q", cfg.LLM.Provider, cfg.LLM.DisabledReason)
			}
		})
	}
}

func TestLLMDefaultsAndOverrides(t *testing.T) {
	setBase(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	l := c.LLM
	if l.Model != "claude-haiku-5-5" || l.Effort != "low" || l.MonthlyBudgetUSD != 5 ||
		l.MaxInputTokens != 24_000 || l.MaxOutputTokens != 4_000 || l.Price.InputPerMTok != 0.10 || l.Price.OutputPerMTok != 0.50 {
		t.Fatalf("defaults = %+v", l)
	}
	t.Setenv("LLM_PRICE_INPUT_PER_MTOK", "0.2")
	t.Setenv("LLM_PRICE_OUTPUT_PER_MTOK", "1")
	t.Setenv("LLM_MONTHLY_BUDGET_USD", "0")
	t.Setenv("LLM_EFFORT", "default")
	c, err = Load()
	if err != nil || c.LLM.Price.InputPerMTok != 0.2 || c.LLM.Price.LongThreshold != 0 || c.LLM.MonthlyBudgetUSD != 0 || c.LLM.Effort != "" {
		t.Fatalf("overrides: %+v err=%v", c.LLM, err)
	}
	t.Setenv("LLM_MONTHLY_BUDGET_USD", "-3")
	if _, err := Load(); err == nil {
		t.Fatal("negative budget accepted")
	}
}
