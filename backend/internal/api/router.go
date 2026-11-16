// Package api wires HTTP routes and middleware.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
)

type Deps struct {
	Log         *slog.Logger
	DB          *pgxpool.Pool
	Auth        *auth.Service
	CORSOrigins []string
	StaticDir   string
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, requestLogger(d.Log), middleware.Recoverer)

	// Liveness: no dependencies, so Render's health check passes during a DB blip.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(r chi.Router) {
		r.Use(cors(d.CORSOrigins), originCheck(d.CORSOrigins), middleware.Timeout(30*time.Second))

		r.Get("/auth/login", d.Auth.Login)
		r.Get("/auth/callback", d.Auth.Callback)
		r.Post("/auth/logout", d.Auth.Logout)

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.RequireUser)
			r.Get("/me", d.Auth.Me)
		})

		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		})
	})

	if d.StaticDir != "" {
		r.Handle("/*", spaHandler(d.StaticDir))
	}
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			if r.URL.Path == "/healthz" {
				return
			}
			log.Info("http",
				"method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"bytes", ww.BytesWritten(), "dur_ms", time.Since(start).Milliseconds(),
				"req_id", middleware.GetReqID(r.Context()))
		})
	}
}

// cors allows credentialed requests from the configured origins only. In the
// default same-origin deployment no origins are configured and this is a no-op.
func cors(origins []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Add("Vary", "Origin")
				if r.Method == http.MethodOptions {
					h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					h.Set("Access-Control-Allow-Headers", "Content-Type")
					h.Set("Access-Control-Max-Age", "600")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// originCheck is CSRF defence for cookie-authenticated writes: a browser
// request that changes state must come from our own host or an allowed origin.
// Requests without an Origin header (curl, GitHub Actions) pass through and
// must authenticate some other way (e.g. the scan secret).
func originCheck(origins []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" || allowed[origin] || sameHost(origin, r) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request rejected"})
		})
	}
}

func sameHost(origin string, r *http.Request) bool {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	if o == r.Host {
		return true
	}
	// Behind a rewrite/proxy the original host arrives as X-Forwarded-Host.
	return o == r.Header.Get("X-Forwarded-Host")
}

// spaHandler serves built frontend assets and falls back to index.html so
// client-side routes work. Used only in the single-service fallback setup.
func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fs.ServeHTTP(w, r)
	})
}
