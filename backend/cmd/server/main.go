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

	"github.com/harshit3011/gitpulse/backend/internal/api"
	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/config"
	"github.com/harshit3011/gitpulse/backend/internal/db"
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
	sealer, err := auth.NewSealer(cfg.TokenEncKey)
	if err != nil {
		return err
	}
	authSvc := auth.NewService(auth.Config{
		ClientID: cfg.GitHubClientID, ClientSecret: cfg.GitHubClientSecret,
		RedirectURL: cfg.OAuthRedirectURL, AllowedUsers: cfg.AllowedUsers,
		FrontendURL: cfg.FrontendURL, CookieSecure: cfg.CookieSecure, CookieSameSite: cfg.CookieSameSite,
	}, st, sealer, log)

	go sweepSessions(ctx, st, log)

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.NewRouter(api.Deps{
			Log: log, DB: pool, Auth: authSvc, CORSOrigins: cfg.CORSOrigins, StaticDir: cfg.StaticDir,
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
	return srv.Shutdown(shutdownCtx)
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
