// Package config loads and validates runtime configuration from the environment.
package config

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/llm"
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

	LLM LLMConfig
}

// LLMConfig: AI is optional. With no API key the provider is "none" and every
// AI feature uses its deterministic fallback.
type LLMConfig struct {
	Provider         string // "none" | "anthropic" (effective value)
	DisabledReason   string // why Provider is none, for the UI
	AnthropicAPIKey  string
	Model            string
	Effort           string // low|medium|high|xhigh|max, "" = model default
	MonthlyBudgetUSD float64
	MaxInputTokens   int
	MaxOutputTokens  int
	Price            llm.Price
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
	c.LLM, errs = loadLLM(opt, errs)
	return c, errors.Join(errs...)
}

func loadLLM(opt func(k, def string) string, errs []error) (LLMConfig, []error) {
	num := func(k string, def, min, max float64) float64 {
		v := opt(k, "")
		if v == "" {
			return def
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < min || f > max {
			errs = append(errs, fmt.Errorf("%s must be a number between %g and %g", k, min, max))
			return def
		}
		return f
	}
	l := LLMConfig{
		AnthropicAPIKey:  opt("ANTHROPIC_API_KEY", ""),
		Model:            opt("LLM_MODEL", "claude-haiku-5-5"),
		Effort:           strings.ToLower(opt("LLM_EFFORT", "low")),
		MonthlyBudgetUSD: num("LLM_MONTHLY_BUDGET_USD", 5, 0, 10_000),
		MaxInputTokens:   int(num("LLM_MAX_INPUT_TOKENS", 24_000, 1_000, 900_000)),
		MaxOutputTokens:  int(num("LLM_MAX_OUTPUT_TOKENS", 4_000, 256, 128_000)),
	}
	switch l.Effort {
	case "low", "medium", "high", "xhigh", "max":
	case "default":
		l.Effort = ""
	default:
		errs = append(errs, errors.New("LLM_EFFORT must be low, medium, high, xhigh, max or default"))
	}

	switch p := strings.ToLower(opt("LLM_PROVIDER", "")); p {
	case "":
		// Unset: AI turns on automatically once a key exists.
		l.Provider = "none"
		if l.AnthropicAPIKey != "" {
			l.Provider = "anthropic"
		}
	case "none", "anthropic":
		l.Provider = p
	default:
		errs = append(errs, errors.New("LLM_PROVIDER must be none or anthropic"))
		l.Provider = "none"
	}
	switch {
	case l.Provider == "anthropic" && l.AnthropicAPIKey == "":
		// Not an error: the app runs fully without AI.
		l.Provider = "none"
		l.DisabledReason = "no API key"
	case l.Provider == "none" && l.AnthropicAPIKey != "":
		l.DisabledReason = "LLM_PROVIDER=none"
	case l.Provider == "none":
		l.DisabledReason = "no API key"
	}

	info, known := llm.LookupModel(l.Model)
	l.Price = info.Price
	in, out := opt("LLM_PRICE_INPUT_PER_MTOK", ""), opt("LLM_PRICE_OUTPUT_PER_MTOK", "")
	switch {
	case in != "" || out != "":
		// An override replaces the whole price, including any long-prompt tier.
		if in == "" || out == "" {
			errs = append(errs, errors.New("set both LLM_PRICE_INPUT_PER_MTOK and LLM_PRICE_OUTPUT_PER_MTOK"))
		}
		l.Price = llm.Price{
			InputPerMTok:  num("LLM_PRICE_INPUT_PER_MTOK", 0, 0, 1_000),
			OutputPerMTok: num("LLM_PRICE_OUTPUT_PER_MTOK", 0, 0, 1_000),
		}
	case !known && l.Provider != "none":
		errs = append(errs, fmt.Errorf("LLM_MODEL %q has no built-in price: set LLM_PRICE_INPUT_PER_MTOK and LLM_PRICE_OUTPUT_PER_MTOK so the budget cap works", l.Model))
	}
	return l, errs
}
