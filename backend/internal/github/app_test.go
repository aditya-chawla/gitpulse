package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestJWTIsVerifiableRS256(t *testing.T) {
	k := testKey(t)
	now := time.Unix(1_800_000_000, 0)
	a := NewApp("Iv1.abc", k, withAppClock(func() time.Time { return now }))
	tok, err := a.JWT()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("parts = %d", len(parts))
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&k.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature: %v", err)
	}
	var claims struct {
		Iat, Exp int64
		Iss      string
	}
	b, _ := base64.RawURLEncoding.DecodeString(parts[1])
	_ = json.Unmarshal(b, &claims)
	if claims.Iss != "Iv1.abc" || claims.Iat != now.Unix()-60 || claims.Exp-now.Unix() > 600 {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestParsePrivateKeyFormats(t *testing.T) {
	k := testKey(t)
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	p8, _ := x509.MarshalPKCS8PrivateKey(k)
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: p8})
	for name, in := range map[string]string{
		"pkcs1":   string(pkcs1),
		"pkcs8":   string(pkcs8),
		"base64":  base64.StdEncoding.EncodeToString(pkcs1),
		"escaped": strings.ReplaceAll(string(pkcs1), "\n", `\n`),
	} {
		got, err := ParsePrivateKey(in)
		if err != nil || !got.Equal(k) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if _, err := ParsePrivateKey("garbage"); err == nil {
		t.Error("garbage accepted")
	}
}

func TestInstallationTokenCachedPerScope(t *testing.T) {
	var mints atomic.Int32
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ey") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			n := mints.Add(1)
			lastBody = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&lastBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "ghs_" + string(rune('a'+n)), "expires_at": time.Now().Add(time.Hour),
			})
		case "/app/installations/7/access_tokens":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer srv.Close()
	a := NewApp("cid", testKey(t), WithAppBaseURL(srv.URL))
	ctx := context.Background()

	t1, _ := a.InstallationToken(ctx, 42, ReadOnly)
	t2, _ := a.InstallationToken(ctx, 42, ReadOnly)
	if t1 != t2 || mints.Load() != 1 {
		t.Fatalf("read token not cached: %q %q mints=%d", t1, t2, mints.Load())
	}
	if p := lastBody["permissions"].(map[string]any); p["contents"] != "read" || len(p) != 5 {
		t.Fatalf("read-only request asked for %v", p)
	}

	w, _ := a.InstallationToken(ctx, 42, TokenRequest{RepositoryIDs: []int64{9}, Permissions: map[string]string{"contents": "write"}})
	if w == t1 || mints.Load() != 2 {
		t.Fatal("write token must be minted separately from the read token")
	}
	if ids := lastBody["repository_ids"].([]any); len(ids) != 1 || ids[0].(float64) != 9 {
		t.Fatalf("write token not scoped to one repo: %v", lastBody)
	}

	if _, err := a.InstallationToken(ctx, 7, ReadOnly); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("uninstalled: err = %v", err)
	}
}

func TestInstallationTokenRefreshesNearExpiry(t *testing.T) {
	var mints atomic.Int32
	now := time.Unix(1_800_000_000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mints.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_x", "expires_at": now.Add(time.Hour)})
	}))
	defer srv.Close()
	clock := now
	a := NewApp("cid", testKey(t), WithAppBaseURL(srv.URL), withAppClock(func() time.Time { return clock }))
	_, _ = a.InstallationToken(context.Background(), 1, ReadOnly)
	clock = now.Add(50 * time.Minute)
	_, _ = a.InstallationToken(context.Background(), 1, ReadOnly)
	if mints.Load() != 1 {
		t.Fatal("token refreshed too early")
	}
	clock = now.Add(56 * time.Minute) // inside the 5-minute slack
	_, _ = a.InstallationToken(context.Background(), 1, ReadOnly)
	if mints.Load() != 2 {
		t.Fatal("token not refreshed before expiry")
	}
}

func TestInstallationForMatchesAccountID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":1,"account":{"id":100,"login":"someone"},"repository_selection":"all"},
			{"id":2,"account":{"id":200,"login":"renamed-me"},"repository_selection":"all","html_url":"https://github.com/settings/installations/2"}]`))
	}))
	defer srv.Close()
	a := NewApp("cid", testKey(t), WithAppBaseURL(srv.URL))
	in, err := a.InstallationFor(context.Background(), 200)
	if err != nil || in.ID != 2 || in.AccountLogin != "renamed-me" {
		t.Fatalf("in=%+v err=%v", in, err)
	}
	if _, err := a.InstallationFor(context.Background(), 300); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("err = %v", err)
	}
}

func TestMissingFor(t *testing.T) {
	in := &Installation{Permissions: map[string]string{"metadata": "read", "contents": "read", "issues": "write"}}
	got := in.MissingFor(ReadOnly)
	want := []string{"actions:read", "pull_requests:read"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("missing = %v", got)
	}
	if m := in.MissingFor(TokenRequest{Permissions: map[string]string{"contents": "write"}}); len(m) != 1 {
		t.Fatalf("read must not satisfy write: %v", m)
	}
	if m := in.MissingFor(TokenRequest{Permissions: map[string]string{"issues": "read"}}); len(m) != 0 {
		t.Fatalf("write satisfies read: %v", m)
	}
}
