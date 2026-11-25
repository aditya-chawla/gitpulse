package api

import (
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
	"github.com/harshit3011/gitpulse/backend/internal/stats"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

func TestTrendsAndActionsEndpoints(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE users, scans, repos, action_items, repo_trend_snapshots CASCADE`); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool)
	_ = st.UpsertUser(ctx, store.User{ID: 1, NodeID: "U1", Login: "me"})

	tok := "test-session-token"
	sum := sha256.Sum256([]byte(tok))
	_ = st.CreateSession(ctx, sum[:], 1, time.Now().Add(time.Hour))

	var scanID int64
	_ = pool.QueryRow(ctx, `INSERT INTO scans (user_id, trigger, status, finished_at) VALUES (1, 'ui', 'succeeded', now()) RETURNING id`).Scan(&scanID)
	_, _ = pool.Exec(ctx, `INSERT INTO repos (id, user_id, full_name, last_seen_scan_id, source) VALUES (10, 1, 'me/app', $1, 'owned')`, scanID)
	_, _ = pool.Exec(ctx, `
		INSERT INTO repo_snapshots (scan_id, repo_id, name, visibility, is_fork, is_archived, stars, forks, size_kb, created_at,
		  has_readme, has_license, has_gitignore, has_ci, has_tests, open_prs, open_issues, health_score, health_breakdown, status)
		VALUES ($1, 10, 'app', 'public', false, false, 5, 1, 100, now(), true, true, true, false, false, 0, 0, 55, '[]', 'active')`, scanID)
	_, _ = pool.Exec(ctx, `
		INSERT INTO repo_trend_snapshots (repo_id, scan_id, scanned_at, health_score, open_prs, open_issues, stale_branches, failed_runs, commit_count_7d, commit_count_30d, unique_authors_30d, stars, forks)
		VALUES (10, $1, now(), 55, 0, 0, 0, 0, 6, 6, 1, 5, 1)`, scanID)
	var actionID int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO action_items (user_id, repo_id, rule, severity, title, detail, url, active, dismissed, last_scan_id)
		VALUES (1, 10, 'unprotected_code', 5, 'Active repo me/app has no CI or tests', '{}', 'https://github.com/me/app', true, false, $1)
		RETURNING id`, scanID).Scan(&actionID)

	authSvc := auth.NewService(auth.Config{AllowedUsers: map[string]bool{"me": true}}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := NewRouter(Deps{
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:    pool,
		Store: st,
		Auth:  authSvc,
		Stats: stats.NewService(pool),
	})

	doReq := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(&http.Cookie{Name: "gitpulse_session", Value: tok})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := doReq(http.MethodGet, "/api/trends"); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/trends = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodGet, "/api/trends/10"); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/trends/10 = %d: %s", rec.Code, rec.Body.String())
	}

	rec := doReq(http.MethodGet, "/api/actions")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/actions = %d: %s", rec.Code, rec.Body.String())
	}
	var actRes struct {
		Items []stats.ActionItem `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &actRes)
	if len(actRes.Items) != 1 || actRes.Items[0].ID != actionID {
		t.Fatalf("unexpected actions: %+v", actRes.Items)
	}

	if rec := doReq(http.MethodPost, "/api/actions/"+itoa(actionID)+"/dismiss"); rec.Code != http.StatusOK {
		t.Fatalf("POST dismiss = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doReq(http.MethodGet, "/api/actions")
	_ = json.Unmarshal(rec.Body.Bytes(), &actRes)
	if len(actRes.Items) != 0 {
		t.Fatalf("expected 0 actions after dismiss, got %+v", actRes.Items)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
