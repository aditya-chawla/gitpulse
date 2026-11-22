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

	"github.com/harshit3011/gitpulse/backend/internal/api"
	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/config"
	"github.com/harshit3011/gitpulse/backend/internal/db"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/install"
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
			Log: log, DB: pool, Store: st, Auth: authSvc, Install: installs, Scanner: scn, Stats: stats.NewService(pool), ScanSecret: cfg.ScanSecret,
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
	return err
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
