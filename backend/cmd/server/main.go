package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // distroless has no zoneinfo; user timezones need it

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/api"
	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/config"
	"github.com/harshit3011/gitpulse/backend/internal/db"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/install"
	"github.com/harshit3011/gitpulse/backend/internal/llm"
	"github.com/harshit3011/gitpulse/backend/internal/prreview"
	"github.com/harshit3011/gitpulse/backend/internal/releases"
	"github.com/harshit3011/gitpulse/backend/internal/scanner"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Neon may be cold; give migrations and the first connection some room.
	startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := db.Connect(startCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	st := store.New(pool)
	authSvc := auth.NewService(auth.Config{
		ClientID: cfg.GitHubClientID, ClientSecret: cfg.GitHubClientSecret,
		RedirectURL: cfg.OAuthRedirectURL, AllowedUsers: cfg.AllowedUsers,
		FrontendURL: cfg.FrontendURL, CookieSecure: cfg.CookieSecure, CookieSameSite: cfg.CookieSameSite,
	}, st, log)
	installs := install.New(github.NewApp(cfg.GitHubClientID, cfg.GitHubAppKey), pool, log)
	ai := newLLM(cfg.LLM, pool, log)
	prs := prreview.New(pool, st, prAccess{installs, log}, ai, log)
	prs.Start(ctx)
	rels := releases.New(pool, st, relAccess{installs, log}, ai, log)
	if cfg.WebhookSecret == "" {
		log.Info("GITHUB_WEBHOOK_SECRET not set: webhooks disabled; PRs are picked up by /api/prs/sync")
	}

	go sweepSessions(ctx, st, log)

	if n, err := scanner.RecoverStale(startCtx, pool); err != nil {
		return err
	} else if n > 0 {
		log.Warn("marked interrupted scans as failed", "count", n)
	}
	scn := scanner.New(ctx, pool, st, func(ctx context.Context, u *store.User, cache github.Cache) (github.Client, error) {
		return installs.ReadClient(ctx, u, github.WithCache(cache), github.WithLogger(log))
	}, log)

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.NewRouter(api.Deps{
			Log: log, DB: pool, Store: st, Auth: authSvc, Install: installs, LLM: ai, PRs: prs, Releases: rels, WebhookSecret: cfg.WebhookSecret, Scanner: scn, Stats: stats.NewService(pool), ScanSecret: cfg.ScanSecret,
			CORSOrigins: cfg.CORSOrigins, StaticDir: cfg.StaticDir,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelShutdown()
	err = srv.Shutdown(shutdownCtx)
	// ctx is cancelled, so running scans are stopping; let them record their
	// final status before the pool closes.
	scn.Wait(10 * time.Second)
	prs.Wait(10 * time.Second)
	return err
}

// prAccess gives the PR assistant read-only clients for all repos, and a
// comment-only token scoped to one repo for writes.
type prAccess struct {
	inst *install.Service
	log  *slog.Logger
}

func (a prAccess) Read(ctx context.Context, u *store.User) (prreview.GitHub, error) {
	return a.inst.ReadClient(ctx, u, github.WithLogger(a.log))
}

func (a prAccess) Writer(ctx context.Context, u *store.User, repoID int64) (prreview.Commenter, error) {
	tok, err := a.inst.RepoToken(ctx, u, repoID, map[string]string{"pull_requests": "write"})
	if err != nil {
		return nil, err
	}
	return github.New(tok, github.WithLogger(a.log)), nil
}

func (a prAccess) BotLogin(ctx context.Context) (string, error) { return a.inst.BotLogin(ctx) }

// relAccess: read-only clients, and a contents:write token scoped to one repo
// for creating draft releases.
type relAccess struct {
	inst *install.Service
	log  *slog.Logger
}

func (a relAccess) Read(ctx context.Context, u *store.User) (releases.GitHub, error) {
	return a.inst.ReadClient(ctx, u, github.WithLogger(a.log))
}

func (a relAccess) ReleaseWriter(ctx context.Context, u *store.User, repoID int64) (releases.Writer, error) {
	tok, err := a.inst.RepoToken(ctx, u, repoID, map[string]string{"contents": "write"})
	if err != nil {
		return nil, err
	}
	return github.New(tok, github.WithLogger(a.log)), nil
}

// newLLM builds the AI service. With no key it is disabled and makes no calls.
func newLLM(c config.LLMConfig, pool *pgxpool.Pool, log *slog.Logger) *llm.Service {
	cfg := llm.Config{
		MonthlyBudgetUSD: c.MonthlyBudgetUSD, MaxInputTokens: c.MaxInputTokens,
		MaxOutputTokens: c.MaxOutputTokens, Price: c.Price, DisabledReason: c.DisabledReason,
	}
	var p llm.Provider
	if c.Provider == "anthropic" {
		p = llm.NewAnthropic(llm.AnthropicConfig{APIKey: c.AnthropicAPIKey, Model: c.Model, Effort: c.Effort})
		log.Info("AI enabled", "provider", "anthropic", "model", c.Model, "budget_usd", c.MonthlyBudgetUSD)
	} else {
		log.Info("AI disabled; features use deterministic output", "reason", c.DisabledReason)
	}
	return llm.New(p, pool, cfg, log)
}

func sweepSessions(ctx context.Context, st *store.Store, log *slog.Logger) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		if n, err := st.DeleteExpiredSessions(c); err != nil && ctx.Err() == nil {
			log.Error("sweep sessions", "err", err)
		} else if n > 0 {
			log.Info("swept expired sessions", "count", n)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
