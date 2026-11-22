// Package config loads and validates runtime configuration from the environment.
package config

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/harshit3011/gitpulse/backend/internal/github"
)

type Config struct {
	Port               string
	DatabaseURL        string
	GitHubClientID     string // the GitHub App's client ID (also the JWT issuer)
	GitHubClientSecret string
	GitHubAppKey       *rsa.PrivateKey
	OAuthRedirectURL   string
	AllowedUsers       map[string]bool // lower-cased logins
	ScanSecret         string
	FrontendURL        string   // where the browser lands after login/logout
	CORSOrigins        []string // extra origins allowed to call the API with credentials
	CookieSecure       bool
	CookieSameSite     http.SameSite
	StaticDir          string // optional: serve built frontend from here
	LogLevel           slog.Level
}

func Load() (*Config, error) {
	var errs []error
	req := func(k string) string {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", k))
		}
		return v
	}
	opt := func(k, def string) string {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
		return def
	}

	c := &Config{
		Port:               opt("PORT", "8080"),
		DatabaseURL:        req("DATABASE_URL"),
		GitHubClientID:     req("GITHUB_CLIENT_ID"),
		GitHubClientSecret: req("GITHUB_CLIENT_SECRET"),
		OAuthRedirectURL:   req("OAUTH_REDIRECT_URL"),
		ScanSecret:         req("SCAN_SECRET"),
		FrontendURL:        strings.TrimRight(opt("FRONTEND_URL", "/"), "/"),
		CookieSecure:       opt("COOKIE_SECURE", "true") != "false",
		StaticDir:          opt("STATIC_DIR", ""),
		AllowedUsers:       map[string]bool{},
	}
	if c.FrontendURL == "" {
		c.FrontendURL = "/"
	}

	for _, u := range strings.Split(req("ALLOWED_USERS"), ",") {
		if u = strings.ToLower(strings.TrimSpace(u)); u != "" {
			c.AllowedUsers[u] = true
		}
	}

	if key := req("GITHUB_APP_PRIVATE_KEY"); key != "" {
		k, err := github.ParsePrivateKey(key)
		if err != nil {
			errs = append(errs, fmt.Errorf("GITHUB_APP_PRIVATE_KEY: %w", err))
		}
		c.GitHubAppKey = k
	}
	if c.ScanSecret != "" && len(c.ScanSecret) < 24 {
		errs = append(errs, errors.New("SCAN_SECRET must be at least 24 characters"))
	}

	for _, o := range strings.Split(opt("CORS_ALLOWED_ORIGINS", ""), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			c.CORSOrigins = append(c.CORSOrigins, o)
		}
	}

	switch strings.ToLower(opt("COOKIE_SAMESITE", "lax")) {
	case "lax":
		c.CookieSameSite = http.SameSiteLaxMode
	case "none":
		c.CookieSameSite = http.SameSiteNoneMode
		if !c.CookieSecure {
			errs = append(errs, errors.New("COOKIE_SAMESITE=none requires COOKIE_SECURE=true"))
		}
	case "strict":
		c.CookieSameSite = http.SameSiteStrictMode
	default:
		errs = append(errs, errors.New("COOKIE_SAMESITE must be lax, strict or none"))
	}

	if err := c.LogLevel.UnmarshalText([]byte(opt("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}
	return c, errors.Join(errs...)
}
