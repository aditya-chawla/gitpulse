package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memCache struct {
	mu sync.Mutex
	m  map[string][2]string
}

func (c *memCache) Get(_ context.Context, k string) (string, []byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v[0], []byte(v[1]), ok
}
func (c *memCache) Put(_ context.Context, k, etag string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = [2]string{etag, string(body)}
}

func newTest(t *testing.T, h http.Handler) (*HTTPClient, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	var slept []time.Duration
	c := New("tok", WithBaseURL(srv.URL), WithCache(&memCache{m: map[string][2]string{}}),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		withSleep(func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }))
	return c, &slept
}

var repo = RepoRef{Owner: "me", Name: "r"}

func TestRetriesServerErrors(t *testing.T) {
	var n atomic.Int32
	c, slept := newTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":1,"name":"ci","conclusion":"failure"}]}`))
	}))
	runs, err := c.FailedRuns(context.Background(), repo, time.Now())
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%v err=%v", runs, err)
	}
	if n.Load() != 3 || len(*slept) != 2 {
		t.Fatalf("calls=%d sleeps=%v", n.Load(), *slept)
	}
}

func TestGivesUpOnClientErrors(t *testing.T) {
	var n atomic.Int32
	c, _ := newTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	if _, err := c.FailedRuns(context.Background(), repo, time.Now()); err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 1 {
		t.Fatalf("401 must not be retried, got %d calls", n.Load())
	}
}

func TestSecondaryRateLimitHonoursRetryAfter(t *testing.T) {
	var n atomic.Int32
	c, slept := newTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
	}))
	if _, err := c.FailedRuns(context.Background(), repo, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(*slept) != 1 || (*slept)[0] != 7*time.Second {
		t.Fatalf("sleeps = %v", *slept)
	}
}

func TestConditionalRequestUsesCachedBody(t *testing.T) {
	var sawINM atomic.Bool
	c, _ := newTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			sawINM.Store(true)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`[{"days":[0,1,0,0,0,0,0],"total":1,"week":1700352000}]`))
	}))
	for i := 0; i < 2; i++ {
		weeks, ok, err := c.CommitActivity(context.Background(), repo)
		if err != nil || !ok || len(weeks) != 1 || weeks[0].Total != 1 {
			t.Fatalf("iteration %d: weeks=%v ok=%v err=%v", i, weeks, ok, err)
		}
	}
	if !sawINM.Load() {
		t.Fatal("second request was not conditional")
	}
}

func TestCommitActivityWaitsWhileComputing(t *testing.T) {
	var n atomic.Int32
	c, _ := newTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	_, ok, err := c.CommitActivity(context.Background(), repo)
	if err != nil || !ok || n.Load() != 3 {
		t.Fatalf("ok=%v err=%v calls=%d", ok, err, n.Load())
	}
}

func TestWaitsWhenQuotaLow(t *testing.T) {
	reset := time.Now().Add(30 * time.Second).Unix()
	c, slept := newTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "3")
		w.Header().Set("X-RateLimit-Reset", jsonNum(reset))
		_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
	}))
	_, _ = c.FailedRuns(context.Background(), repo, time.Now()) // learns quota
	_, _ = c.FailedRuns(context.Background(), repo, time.Now()) // must wait first
	if len(*slept) != 1 || (*slept)[0] < 25*time.Second {
		t.Fatalf("sleeps = %v", *slept)
	}
}

func jsonNum(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestOwnedReposShrinksPageOnTimeout(t *testing.T) {
	var sizes []int
	c, _ := newTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				N int `json:"n"`
			} `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		sizes = append(sizes, req.Variables.N)
		if req.Variables.N > 5 {
			_, _ = w.Write([]byte(`{"errors":[{"message":"timedout","type":"TIMEOUT"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"user":{"repositories":{"pageInfo":{"hasNextPage":false},"nodes":[
			{"databaseId":9,"id":"R_9","name":"r","owner":{"login":"me"},"visibility":"PUBLIC",
			 "createdAt":"2024-01-01T00:00:00Z","licenseInfo":{"spdxId":"MIT"},
			 "languages":{"edges":[{"size":100,"node":{"name":"Go"}}]},
			 "pullRequests":{"totalCount":1,"nodes":[{"number":3,"title":"t","author":null,
			   "reviewRequests":{"nodes":[{"requestedReviewer":{"login":"me"}},{"requestedReviewer":{}}]}}]},
			 "refs":{"totalCount":2,"nodes":[{"name":"main","target":{"committedDate":"2024-02-01T00:00:00Z"}}]}}
		]}}}}`))
	}))
	repos, err := c.OwnedRepos(context.Background(), "me")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != "[20 10 5]" {
		t.Errorf("page sizes = %v", sizes)
	}
	r := repos[0]
	if r.ID != 9 || r.Visibility != "public" || r.LicenseSPDX != "MIT" || r.Languages[0].Bytes != 100 {
		t.Errorf("repo = %+v", r)
	}
	if !r.BranchesTruncated || len(r.OpenPRs) != 1 || r.OpenPRs[0].Author != "" ||
		len(r.OpenPRs[0].ReviewRequests) != 1 {
		t.Errorf("nested = %+v", r)
	}
}

func TestTreeFragmentAndFlatten(t *testing.T) {
	if got := treeFragment(2); got != "fragment T on Tree { entries { name type object { ... on Tree { entries { name type } } } } }" {
		t.Fatalf("fragment = %s", got)
	}
	var tr gqlTree
	_ = json.Unmarshal([]byte(`{"entries":[{"name":"README.md","type":"blob"},
		{"name":"backend","type":"tree","object":{"entries":[{"name":"go.mod","type":"blob"}]}}]}`), &tr)
	var out []TreeEntry
	flatten("", &tr, &out)
	if len(out) != 3 || out[2].Path != "backend/go.mod" || !out[1].IsDir {
		t.Fatalf("flatten = %+v", out)
	}
}
