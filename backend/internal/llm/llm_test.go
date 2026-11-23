package llm

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harshit3011/gitpulse/backend/internal/db"
)

// fakeProvider records calls and returns a canned reply.
type fakeProvider struct {
	calls   atomic.Int32
	reply   string
	in, out int
	refuse  bool
	err     error
	delay   time.Duration
}

func (f *fakeProvider) Name() string  { return "fake" }
func (f *fakeProvider) Model() string { return "fake-model" }
func (f *fakeProvider) Complete(ctx context.Context, _ ProviderRequest) (ProviderResponse, error) {
	f.calls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return ProviderResponse{Text: f.reply, InputTokens: f.in, OutputTokens: f.out, Refused: f.refuse}, f.err
}

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
	t.Cleanup(pool.Close)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE users, llm_cache CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, node_id, login) VALUES (1, 'U1', 'me')`); err != nil {
		t.Fatal(err)
	}
	return pool
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// $1 per MTok in and out keeps the arithmetic obvious.
var flat = Price{InputPerMTok: 1, OutputPerMTok: 1}

func task(input string) Task {
	return Task{Feature: "pr_review", UserID: 1, RepoID: 5, PromptVersion: "v1", System: "sys",
		Input: input, Schema: map[string]any{"type": "object"}}
}

type out struct {
	Summary string `json:"summary"`
}

func TestDisabledMakesNoCallsAndNoRows(t *testing.T) {
	pool := testDB(t)
	s := New(nil, pool, Config{MonthlyBudgetUSD: 5}, quiet)
	var o out
	if _, err := s.Run(context.Background(), task("x"), &o); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v", err)
	}
	var n int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM llm_calls`).Scan(&n)
	if n != 0 {
		t.Fatalf("disabled mode wrote %d rows", n)
	}
	u, err := s.Usage(context.Background(), 1)
	if err != nil || u.Enabled || u.DisabledReason != "no API key" || u.SpentUSD != 0 {
		t.Fatalf("usage = %+v err=%v", u, err)
	}
}

func TestCacheNeverSendsSameInputTwice(t *testing.T) {
	pool := testDB(t)
	p := &fakeProvider{reply: `{"summary":"hi"}`, in: 1000, out: 200}
	s := New(p, pool, Config{MonthlyBudgetUSD: 5, Price: flat}, quiet)
	ctx := context.Background()

	var a, b, c out
	m1, err := s.Run(ctx, task("same diff"), &a)
	if err != nil || m1.Cached || a.Summary != "hi" {
		t.Fatalf("first: %+v %v %+v", m1, err, a)
	}
	if want := 1200.0 / 1e6; m1.CostUSD != want {
		t.Fatalf("cost = %v want %v", m1.CostUSD, want)
	}
	m2, err := s.Run(ctx, task("same diff"), &b)
	if err != nil || !m2.Cached || b.Summary != "hi" || m2.CostUSD != 0 {
		t.Fatalf("second: %+v %v %+v", m2, err, b)
	}
	if _, err := s.Run(ctx, task("different diff"), &c); err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want 2", p.calls.Load())
	}
	// Changing the prompt version must miss the cache.
	t2 := task("same diff")
	t2.PromptVersion = "v2"
	_, _ = s.Run(ctx, t2, &c)
	if p.calls.Load() != 3 {
		t.Fatal("prompt version is not part of the cache key")
	}

	var statuses []string
	rows, _ := pool.Query(ctx, `SELECT status FROM llm_calls ORDER BY id`)
	for rows.Next() {
		var st string
		_ = rows.Scan(&st)
		statuses = append(statuses, st)
	}
	if len(statuses) != 4 || statuses[1] != "cache_hit" || statuses[0] != "ok" {
		t.Fatalf("logged = %v", statuses)
	}
}

func TestBudgetCapSkipsWithoutCalling(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	// Each call costs $0.001 (1000 tokens at $1/MTok). Worst case reserved per
	// call is a bit more (~2 input tokens + 1000 output).
	p := &fakeProvider{reply: `{"summary":"x"}`, in: 500, out: 500}
	s := New(p, pool, Config{MonthlyBudgetUSD: 0.0026, MaxOutputTokens: 1000, Price: flat}, quiet)

	// Spend recorded earlier this month counts; last month's doesn't.
	_, _ = pool.Exec(ctx, `INSERT INTO llm_calls (user_id, feature, provider, model, status, cost_usd, created_at)
		VALUES (1,'pr_review','fake','m','ok',0.0005, now()), (1,'pr_review','fake','m','ok',100, now() - interval '40 days')`)

	var o out
	if _, err := s.Run(ctx, task("a"), &o); err != nil { // 0.0005 + 0.001 reserved: fits
		t.Fatalf("first call should fit: %v", err)
	}
	if _, err := s.Run(ctx, task("b"), &o); err != nil { // 0.0015 + 0.001: fits
		t.Fatalf("second call should fit: %v", err)
	}
	if _, err := s.Run(ctx, task("c"), &o); !errors.Is(err, ErrOverBudget) { // 0.0025 + 0.001: over
		t.Fatalf("expected over budget, got %v", err)
	}
	calls := p.calls.Load()
	if _, err := s.Run(ctx, task("f"), &o); !errors.Is(err, ErrOverBudget) || p.calls.Load() != calls {
		t.Fatal("provider called while over budget")
	}
	// Cached answers are still served when the budget is exhausted.
	if m, err := s.Run(ctx, task("a"), &o); err != nil || !m.Cached {
		t.Fatalf("cache hit over budget: %+v %v", m, err)
	}
	u, _ := s.Usage(ctx, 1)
	if !u.Capped || u.ByFeature[0].Skipped != 2 {
		t.Fatalf("usage = %+v", u)
	}
}

func TestConcurrentReservationsRespectCap(t *testing.T) {
	pool := testDB(t)
	// Actual cost ≈ the reservation (~$0.001), so at most 3 calls fit in
	// $0.0035 no matter how the goroutines interleave.
	p := &fakeProvider{reply: `{"summary":"x"}`, in: 2, out: 1000, delay: 50 * time.Millisecond}
	s := New(p, pool, Config{MonthlyBudgetUSD: 0.0035, MaxOutputTokens: 1000, Price: flat}, quiet)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			var o out
			_, _ = s.Run(context.Background(), task(string(rune('a'+i))), &o)
		})
	}
	wg.Wait()
	if n := p.calls.Load(); n != 3 {
		t.Fatalf("%d calls ran; the cap admits exactly 3", n)
	}
}

func TestTooLargeIsRejectedBeforeCalling(t *testing.T) {
	pool := testDB(t)
	p := &fakeProvider{reply: `{}`}
	s := New(p, pool, Config{MonthlyBudgetUSD: 5, MaxInputTokens: 1000, Price: flat}, quiet)
	big := make([]byte, 4000)
	for i := range big {
		big[i] = 'x'
	}
	var o out
	if _, err := s.Run(context.Background(), task(string(big)), &o); !errors.Is(err, ErrTooLarge) || p.calls.Load() != 0 {
		t.Fatalf("err=%v calls=%d", err, p.calls.Load())
	}
}

func TestRefusalAndBadOutputAreRecorded(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	s := New(&fakeProvider{refuse: true, in: 50, out: 5}, pool, Config{MonthlyBudgetUSD: 5, Price: flat}, quiet)
	var o out
	if _, err := s.Run(ctx, task("r"), &o); !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v", err)
	}
	s = New(&fakeProvider{reply: "not json", in: 50, out: 5}, pool, Config{MonthlyBudgetUSD: 5, Price: flat}, quiet)
	if _, err := s.Run(ctx, task("b"), &o); !errors.Is(err, ErrBadOutput) {
		t.Fatalf("err = %v", err)
	}
	var refused, errored, cached int
	_ = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='refused'), count(*) FILTER (WHERE status='error' AND cost_usd > 0) FROM llm_calls`).Scan(&refused, &errored)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM llm_cache`).Scan(&cached)
	if refused != 1 || errored != 1 || cached != 0 {
		t.Fatalf("refused=%d errored(billed)=%d cached=%d", refused, errored, cached)
	}
}

func TestStaleReservationStopsCounting(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	// A crashed call left a big reservation 20 minutes ago.
	_, _ = pool.Exec(ctx, `INSERT INTO llm_calls (user_id, feature, provider, model, status, estimated_cost_usd, created_at)
		VALUES (1,'pr_review','fake','m','reserved', 4.99, now() - interval '20 minutes')`)
	s := New(&fakeProvider{reply: `{}`, in: 1, out: 1}, pool, Config{MonthlyBudgetUSD: 5, Price: flat}, quiet)
	var o out
	if _, err := s.Run(ctx, task("z"), &o); err != nil {
		t.Fatalf("stale reservation still blocks: %v", err)
	}
}
