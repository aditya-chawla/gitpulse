package github

import (
	"bytes"
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
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNotInstalled means the GitHub App has no installation on the account.
var ErrNotInstalled = errors.New("github app is not installed on this account")

// App authenticates as a GitHub App: it signs JWTs with the app's private key
// and exchanges them for short-lived installation tokens.
type App struct {
	clientID string // JWT issuer (GitHub recommends the client ID over the app ID)
	key      *rsa.PrivateKey
	apiBase  string
	http     *http.Client
	now      func() time.Time

	mu     sync.Mutex
	tokens map[string]cachedToken
	info   *AppInfo
}

type cachedToken struct {
	token   string
	expires time.Time
}

type AppOption func(*App)

func WithAppBaseURL(u string) AppOption         { return func(a *App) { a.apiBase = u } }
func withAppClock(f func() time.Time) AppOption { return func(a *App) { a.now = f } }

func NewApp(clientID string, key *rsa.PrivateKey, opts ...AppOption) *App {
	a := &App{
		clientID: clientID, key: key, apiBase: "https://api.github.com",
		http: &http.Client{Timeout: 20 * time.Second}, now: time.Now, tokens: map[string]cachedToken{},
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// ParsePrivateKey accepts the app's PEM key either as-is (literal "\n"
// escapes allowed, for env vars) or base64-encoded.
func ParsePrivateKey(s string) (*rsa.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "-----BEGIN") {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, errors.New("private key is neither PEM nor base64-encoded PEM")
		}
		s = string(b)
	}
	s = strings.ReplaceAll(s, `\n`, "\n")
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, errors.New("private key: no PEM block found")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return rk, nil
}

// JWT returns an RS256 app token valid for ~9 minutes. iat is backdated to
// tolerate clock drift, as GitHub recommends.
func (a *App) JWT() (string, error) {
	now := a.now()
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": a.clientID,
	})
	signing := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

func (a *App) call(ctx context.Context, method, path, bearer string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.apiBase+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return &APIError{Status: resp.StatusCode, URL: a.apiBase + path, Body: string(b)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

func (a *App) appCall(ctx context.Context, method, path string, body, out any) error {
	jwt, err := a.JWT()
	if err != nil {
		return err
	}
	return a.call(ctx, method, path, jwt, body, out)
}

type AppInfo struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

// InstallURL is where a user installs (or re-installs) the app.
func (i *AppInfo) InstallURL() string {
	return "https://github.com/apps/" + i.Slug + "/installations/new"
}

// Info returns the app's own metadata, cached for the process lifetime.
func (a *App) Info(ctx context.Context) (*AppInfo, error) {
	a.mu.Lock()
	if a.info != nil {
		defer a.mu.Unlock()
		return a.info, nil
	}
	a.mu.Unlock()
	var info AppInfo
	if err := a.appCall(ctx, http.MethodGet, "/app", nil, &info); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.info = &info
	a.mu.Unlock()
	return &info, nil
}

type Installation struct {
	ID                  int64
	AccountID           int64
	AccountLogin        string
	RepositorySelection string            // all | selected
	Permissions         map[string]string // what the account accepted, e.g. contents=write
	HTMLURL             string
	SuspendedAt         *time.Time
}

type ghInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"account"`
	RepositorySelection string            `json:"repository_selection"`
	Permissions         map[string]string `json:"permissions"`
	HTMLURL             string            `json:"html_url"`
	SuspendedAt         *time.Time        `json:"suspended_at"`
}

func (g ghInstallation) toInstallation() *Installation {
	return &Installation{
		ID: g.ID, AccountID: g.Account.ID, AccountLogin: g.Account.Login,
		RepositorySelection: g.RepositorySelection, HTMLURL: g.HTMLURL, SuspendedAt: g.SuspendedAt,
		Permissions: g.Permissions,
	}
}

// InstallationFor finds the app's installation on the account with this ID.
// Matching on the numeric ID (not the login) survives username changes.
func (a *App) InstallationFor(ctx context.Context, accountID int64) (*Installation, error) {
	for page := 1; page <= 20; page++ {
		var list []ghInstallation
		if err := a.appCall(ctx, http.MethodGet, "/app/installations?per_page=100&page="+strconv.Itoa(page), nil, &list); err != nil {
			return nil, fmt.Errorf("list app installations: %w", err)
		}
		for _, in := range list {
			if in.Account.ID == accountID {
				return in.toInstallation(), nil
			}
		}
		if len(list) < 100 {
			break
		}
	}
	return nil, ErrNotInstalled
}

// TokenRequest narrows an installation token. GitHub caps it at what the
// installation was granted; asking for less is always allowed.
type TokenRequest struct {
	RepositoryIDs []int64           `json:"repository_ids,omitempty"`
	Permissions   map[string]string `json:"permissions,omitempty"`
}

func (r TokenRequest) key(installationID int64) string {
	ids := slices.Clone(r.RepositoryIDs)
	slices.Sort(ids)
	perms := make([]string, 0, len(r.Permissions))
	for k, v := range r.Permissions {
		perms = append(perms, k+"="+v)
	}
	slices.Sort(perms)
	return fmt.Sprintf("%d|%v|%s", installationID, ids, strings.Join(perms, ","))
}

// ReadOnly is what scans need: no write permission is ever requested.
var ReadOnly = TokenRequest{Permissions: map[string]string{
	"metadata": "read", "contents": "read", "pull_requests": "read", "issues": "read", "actions": "read",
}}

// MissingFor lists the permissions in want that the installation lacks or
// grants at a lower level (read < write). An empty result means a token for
// want can be minted.
func (in *Installation) MissingFor(want TokenRequest) []string {
	var missing []string
	for k, need := range want.Permissions {
		have := in.Permissions[k]
		if have == "" || (need == "write" && have != "write") {
			missing = append(missing, k+":"+need)
		}
	}
	slices.Sort(missing)
	return missing
}

// tokenSlack refreshes a cached token this long before GitHub expires it
// (tokens live one hour), so a request never starts with a dying token.
const tokenSlack = 5 * time.Minute

// InstallationToken returns a cached or freshly minted installation token.
func (a *App) InstallationToken(ctx context.Context, installationID int64, req TokenRequest) (string, error) {
	k := req.key(installationID)
	a.mu.Lock()
	if t, ok := a.tokens[k]; ok && a.now().Add(tokenSlack).Before(t.expires) {
		a.mu.Unlock()
		return t.token, nil
	}
	a.mu.Unlock()

	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	path := "/app/installations/" + strconv.FormatInt(installationID, 10) + "/access_tokens"
	if err := a.appCall(ctx, http.MethodPost, path, req, &out); err != nil {
		if IsNotFound(err) {
			return "", ErrNotInstalled
		}
		return "", fmt.Errorf("mint installation token: %w", err)
	}
	if out.Token == "" {
		return "", errors.New("mint installation token: empty token")
	}
	a.mu.Lock()
	a.tokens[k] = cachedToken{token: out.Token, expires: out.ExpiresAt}
	a.mu.Unlock()
	return out.Token, nil
}

// TokenFunc lets long scans outlive a single one-hour token.
func (a *App) TokenFunc(installationID int64, req TokenRequest) TokenFunc {
	return func(ctx context.Context) (string, error) { return a.InstallationToken(ctx, installationID, req) }
}

// RevokeUserToken deletes a user-to-server token once sign-in no longer needs
// it. Uses the app's client credentials (basic auth), per GitHub's API.
func RevokeUserToken(ctx context.Context, apiBase, clientID, clientSecret, token string) error {
	b, _ := json.Marshal(map[string]string{"access_token": token})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiBase+"/applications/"+clientID+"/token", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("revoke user token: status %d", resp.StatusCode)
	}
	return nil
}
