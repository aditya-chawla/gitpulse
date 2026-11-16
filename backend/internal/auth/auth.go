// Package auth implements GitHub OAuth sign-in, the username allowlist, and
// cookie sessions.
package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/harshit3011/gitpulse/backend/internal/store"
)

const (
	SessionCookie = "gitpulse_session"
	stateCookie   = "gitpulse_oauth_state"
	sessionTTL    = 30 * 24 * time.Hour
	// repo: private repo metadata, contents (manifests) and Actions runs.
	// OAuth apps have no read-only private-repo scope.
	oauthScopes = "read:user repo"
)

type Config struct {
	ClientID, ClientSecret, RedirectURL string
	AllowedUsers                        map[string]bool
	FrontendURL                         string
	CookieSecure                        bool
	CookieSameSite                      http.SameSite

	// Overridable for tests.
	AuthorizeURL, TokenURL, APIBaseURL string
}

type Service struct {
	cfg    Config
	store  *store.Store
	sealer *Sealer
	http   *http.Client
	log    *slog.Logger
}

func NewService(cfg Config, st *store.Store, sealer *Sealer, log *slog.Logger) *Service {
	if cfg.AuthorizeURL == "" {
		cfg.AuthorizeURL = "https://github.com/login/oauth/authorize"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://github.com/login/oauth/access_token"
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = "https://api.github.com"
	}
	return &Service{cfg: cfg, store: st, sealer: sealer, http: &http.Client{Timeout: 15 * time.Second}, log: log}
}

func (s *Service) Allowed(login string) bool { return s.cfg.AllowedUsers[strings.ToLower(login)] }

// Login redirects to GitHub with a CSRF state bound to a short-lived cookie.
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken(24)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: state, Path: "/api/auth", MaxAge: 600,
		HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
	q := url.Values{
		"client_id":    {s.cfg.ClientID},
		"redirect_uri": {s.cfg.RedirectURL},
		"scope":        {oauthScopes},
		"state":        {state},
		"allow_signup": {"false"},
	}
	http.Redirect(w, r, s.cfg.AuthorizeURL+"?"+q.Encode(), http.StatusFound)
}

// Callback exchanges the code, checks the allowlist, stores the encrypted
// token, and starts a session.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := r.Cookie(stateCookie)
	state := r.URL.Query().Get("state")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		s.fail(w, r, "invalid_state", errors.New("oauth state mismatch"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/api/auth", MaxAge: -1})

	if e := r.URL.Query().Get("error"); e != "" {
		s.fail(w, r, "denied", fmt.Errorf("github returned error: %s", e))
		return
	}
	token, scopes, err := s.exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		s.fail(w, r, "exchange_failed", err)
		return
	}
	gu, err := s.fetchUser(ctx, token)
	if err != nil {
		s.fail(w, r, "user_fetch_failed", err)
		return
	}
	if !s.Allowed(gu.Login) {
		s.log.Warn("login rejected: not in allowlist", "login", gu.Login, "github_id", gu.ID)
		s.redirectFrontend(w, r, "/login?error=not_allowed")
		return
	}

	aad := []byte(strconv.FormatInt(gu.ID, 10))
	ct, nonce, err := s.sealer.Seal([]byte(token), aad)
	if err != nil {
		s.fail(w, r, "internal", err)
		return
	}
	if err := s.store.UpsertUser(ctx, store.User{
		ID: gu.ID, NodeID: gu.NodeID, Login: gu.Login, Name: gu.Name, AvatarURL: gu.AvatarURL,
		TokenCiphertext: ct, TokenNonce: nonce, TokenScopes: scopes,
	}); err != nil {
		s.fail(w, r, "internal", err)
		return
	}

	sess, err := randomToken(32)
	if err != nil {
		s.fail(w, r, "internal", err)
		return
	}
	expires := time.Now().Add(sessionTTL)
	if err := s.store.CreateSession(ctx, hashToken(sess), gu.ID, expires); err != nil {
		s.fail(w, r, "internal", err)
		return
	}
	http.SetCookie(w, s.sessionCookie(sess, expires))
	s.log.Info("login ok", "login", gu.Login, "github_id", gu.ID)
	s.redirectFrontend(w, r, "/")
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		if err := s.store.DeleteSession(r.Context(), hashToken(c.Value)); err != nil {
			s.log.Error("delete session", "err", err)
		}
	}
	c := s.sessionCookie("", time.Unix(0, 0))
	c.MaxAge = -1
	http.SetCookie(w, c)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) sessionCookie(v string, exp time.Time) *http.Cookie {
	return &http.Cookie{
		Name: SessionCookie, Value: v, Path: "/", Expires: exp,
		HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: s.cfg.CookieSameSite,
	}
}

type ctxKey struct{}

// RequireUser rejects requests without a valid session; handlers read the user
// with UserFrom.
func (s *Service) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := s.userFromRequest(r)
		if u == nil {
			writeJSONError(w, http.StatusUnauthorized, "not signed in")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

// OptionalUser attaches the user if a valid session exists.
func (s *Service) OptionalUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := s.userFromRequest(r); u != nil {
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, u))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) userFromRequest(r *http.Request) *store.User {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.store.SessionUser(r.Context(), hashToken(c.Value))
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Error("session lookup", "err", err)
		}
		return nil
	}
	// Allowlist is re-checked on every request so removing a user from
	// ALLOWED_USERS takes effect immediately.
	if !s.Allowed(u.Login) {
		return nil
	}
	return u
}

func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(ctxKey{}).(*store.User)
	return u
}

// AccessToken decrypts a user's stored GitHub token.
func (s *Service) AccessToken(u *store.User) (string, error) {
	pt, err := s.sealer.Open(u.TokenCiphertext, u.TokenNonce, []byte(strconv.FormatInt(u.ID, 10)))
	if err != nil {
		return "", fmt.Errorf("decrypt token for user %d: %w", u.ID, err)
	}
	return string(pt), nil
}

// Me returns the signed-in user's public profile.
func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	u := UserFrom(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": u.ID, "login": u.Login, "name": u.Name, "avatarUrl": u.AvatarURL,
	})
}

func (s *Service) fail(w http.ResponseWriter, r *http.Request, code string, err error) {
	s.log.Error("oauth callback failed", "code", code, "err", err)
	s.redirectFrontend(w, r, "/login?error="+url.QueryEscape(code))
}

func (s *Service) redirectFrontend(w http.ResponseWriter, r *http.Request, path string) {
	base := s.cfg.FrontendURL
	if base == "/" {
		base = ""
	}
	http.Redirect(w, r, base+path, http.StatusFound)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// --- GitHub calls needed only for sign-in ---

func (s *Service) exchange(ctx context.Context, code string) (token, scopes string, err error) {
	if code == "" {
		return "", "", errors.New("missing code")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	form := url.Values{
		"client_id": {s.cfg.ClientID}, "client_secret": {s.cfg.ClientSecret},
		"code": {code}, "redirect_uri": {s.cfg.RedirectURL},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", "", fmt.Errorf("decode token response (status %d): %w", resp.StatusCode, err)
	}
	if body.Error != "" || body.AccessToken == "" {
		return "", "", fmt.Errorf("token exchange: %s %s", body.Error, body.ErrorDesc)
	}
	return body.AccessToken, body.Scope, nil
}

type githubUser struct {
	ID        int64  `json:"id"`
	NodeID    string `json:"node_id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (s *Service) fetchUser(ctx context.Context, token string) (*githubUser, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.APIBaseURL+"/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /user: status %d", resp.StatusCode)
	}
	var u githubUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	if u.ID == 0 || u.Login == "" {
		return nil, errors.New("GET /user: incomplete response")
	}
	return &u, nil
}
