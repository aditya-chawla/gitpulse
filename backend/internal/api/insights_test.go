package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/db"
	"github.com/harshit3011/gitpulse/backend/internal/insights"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

func TestInsightsEndpoints(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	if err := db.Migrate(url); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE users, scans, repos, repo_items, repo_trend_snapshots CASCADE`); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool)
	_ = st.UpsertUser(ctx, store.User{ID: 1, NodeID: "U1", Login: "me"})

	tok := "insights-session-token"
	sum := sha256.Sum256([]byte(tok))
	_ = st.CreateSession(ctx, sum[:], 1, time.Now().Add(time.Hour))

	now := time.Now().UTC()
	var scanID int64
	for i, health := range []int{85, 80, 75, 70} {
		at := now.Add(time.Duration(-3+i) * 7 * 24 * time.Hour)
		_ = pool.QueryRow(ctx, `INSERT INTO scans (user_id, trigger, status, started_at, finished_at) VALUES (1, 'ui', 'succeeded', $1, $1) RETURNING id`, at).Scan(&scanID)
		if i == 0 {
			_, _ = pool.Exec(ctx, `INSERT INTO repos (id, user_id, full_name, last_seen_scan_id, source) VALUES (20, 1, 'oss/lib', $1, 'watched')`, scanID)
		} else {
			_, _ = pool.Exec(ctx, `UPDATE repos SET last_seen_scan_id = $1 WHERE id = 20`, scanID)
		}
		_, _ = pool.Exec(ctx, `
			INSERT INTO repo_snapshots (scan_id, repo_id, name, visibility, is_fork, is_archived, stars, forks, size_kb, created_at,
			  has_readme, has_license, has_gitignore, has_ci, has_tests, open_prs, open_issues, health_score, health_breakdown, status)
			VALUES ($1, 20, 'lib', 'public', false, false, 100, 10, 500, $2, true, true, true, true, true, $3, 5, $4, '[]', 'active')`,
			scanID, at.AddDate(-1, 0, 0), 5+i*5, health)
		_, _ = pool.Exec(ctx, `
			INSERT INTO repo_trend_snapshots (repo_id, scan_id, scanned_at, health_score, open_prs, open_issues, stale_branches, failed_runs, commit_count_7d, commit_count_30d, unique_authors_30d, top_author_login, top_author_pct, stars, forks)
			VALUES (20, $1, $2, $3, $4, 5, 0, 0, 10, 40, 8, 'alice', 30, 100, 10)`,
			scanID, at, health, 5+i*5)
	}

	// Open PRs on the latest scan for PERCENTILE_CONT.
	_, _ = pool.Exec(ctx, `
		INSERT INTO repo_items (scan_id, repo_id, kind, number, title, author, url, created_at, updated_at)
		VALUES
		  ($1, 20, 'pr', 1, 'PR 1', 'bob', 'https://github.com/oss/lib/pull/1', $2, $2),
		  ($1, 20, 'pr', 2, 'PR 2', 'carol', 'https://github.com/oss/lib/pull/2', $3, $3)`,
		scanID, now.Add(-2*24*time.Hour), now.Add(-20*24*time.Hour))

	authSvc := auth.NewService(auth.Config{AllowedUsers: map[string]bool{"me": true}}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := NewRouter(Deps{
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:    pool,
		Store: st,
		Auth:  authSvc,
		Stats: stats.NewService(pool),
	})

	doReq := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: "gitpulse_session", Value: tok})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := doReq("/api/insights?source=watched")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/insights = %d: %s", rec.Code, rec.Body.String())
	}
	var listRes struct {
		Repos []insights.RepoInsights `json:"repos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listRes); err != nil {
		t.Fatal(err)
	}
	if len(listRes.Repos) != 1 {
		t.Fatalf("expected 1 watched repo insight, got %+v", listRes.Repos)
	}
	got := listRes.Repos[0]
	if got.SampleCount != 4 || got.HealthSlopePerDay >= 0 || got.ProjectedHealth30d >= got.HealthScore {
		t.Fatalf("expected negative slope and lower projection, got %+v", got)
	}
	if got.PRMedianAgeDays < 10 || got.PRStalePct != 50 {
		t.Fatalf("unexpected PR percentiles/stalePct: %+v", got)
	}

	rec2 := doReq("/api/insights/20")
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /api/insights/20 = %d: %s", rec2.Code, rec2.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agent/ask", bytes.NewBufferString(`{"role":"contributor","repoId":20,"question":"Should I contribute?"}`))
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	req.AddCookie(&http.Cookie{Name: "gitpulse_session", Value: tok})
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req)
	if rec3.Code != http.StatusOK {
		t.Fatalf("POST /api/agent/ask = %d: %s", rec3.Code, rec3.Body.String())
	}
}
