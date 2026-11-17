package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	userAgent = "gitpulse"
	// When remaining quota falls below this, wait for the reset instead of
	// burning the last requests.
	lowQuota = 50
	// Never wait longer than this for a quota reset; fail the scan instead.
	maxQuotaWait = 15 * time.Minute
	maxAttempts  = 4
	callTimeout  = 30 * time.Second
)

type HTTPClient struct {
	token      string
	apiBase    string
	graphqlURL string
	http       *http.Client
	cache      Cache
	log        *slog.Logger
	sleep      func(ctx context.Context, d time.Duration) error

	calls atomic.Int64
	mu    sync.Mutex
	rest  quota
	gql   quota
}

type quota struct {
	known     bool
	remaining int
	reset     time.Time
}

type Option func(*HTTPClient)

// WithBaseURL points the client at a fake server (tests).
func WithBaseURL(u string) Option {
	return func(c *HTTPClient) { c.apiBase = u; c.graphqlURL = u + "/graphql" }
}
func WithCache(cache Cache) Option        { return func(c *HTTPClient) { c.cache = cache } }
func WithLogger(l *slog.Logger) Option    { return func(c *HTTPClient) { c.log = l } }
func WithHTTPClient(h *http.Client) Option { return func(c *HTTPClient) { c.http = h } }
func withSleep(f func(context.Context, time.Duration) error) Option {
	return func(c *HTTPClient) { c.sleep = f }
}

func New(token string, opts ...Option) *HTTPClient {
	c := &HTTPClient{
		token:      token,
		apiBase:    "https://api.github.com",
		graphqlURL: "https://api.github.com/graphql",
		http:       &http.Client{},
		log:        slog.Default(),
		sleep:      sleepCtx,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *HTTPClient) Rate() RateInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return RateInfo{RESTRemaining: c.rest.remaining, GraphQLRemaining: c.gql.remaining, Calls: int(c.calls.Load())}
}

// APIError is a non-2xx response that survived retries.
type APIError struct {
	Status int
	URL    string
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github %s: status %d: %s", e.URL, e.Status, truncate(e.Body, 300))
}

func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

// waitForQuota blocks while the relevant bucket is nearly exhausted.
func (c *HTTPClient) waitForQuota(ctx context.Context, isGraphQL bool) error {
	c.mu.Lock()
	q := c.rest
	if isGraphQL {
		q = c.gql
	}
	c.mu.Unlock()
	if !q.known || q.remaining >= lowQuota {
		return nil
	}
	wait := time.Until(q.reset) + 2*time.Second
	if wait <= 0 {
		return nil
	}
	if wait > maxQuotaWait {
		return fmt.Errorf("github rate limit nearly exhausted (%d left), resets in %s", q.remaining, wait.Round(time.Second))
	}
	c.log.Warn("rate limit low, waiting for reset", "remaining", q.remaining, "wait", wait.Round(time.Second))
	return c.sleep(ctx, wait)
}

func (c *HTTPClient) recordQuota(h http.Header, isGraphQL bool) {
	rem, err1 := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	reset, err2 := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err1 != nil || err2 != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	q := quota{known: true, remaining: rem, reset: time.Unix(reset, 0)}
	if isGraphQL {
		c.gql = q
	} else {
		c.rest = q
	}
}

// retryDelay decides whether a response is retryable and how long to wait.
// attempt starts at 1.
func retryDelay(status int, h http.Header, attempt int) (time.Duration, bool) {
	switch {
	case status == http.StatusTooManyRequests,
		status == http.StatusForbidden && (h.Get("Retry-After") != "" || h.Get("X-RateLimit-Remaining") == "0"):
		// Secondary or primary rate limit.
		if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil {
			return time.Duration(s) * time.Second, true
		}
		if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if d := time.Until(time.Unix(reset, 0)) + time.Second; d > 0 && d <= maxQuotaWait {
				return d, true
			}
			return 0, false
		}
		return 60 * time.Second, true
	case status >= 500:
		return time.Duration(1<<attempt) * time.Second, true // 2s, 4s, 8s
	}
	return 0, false
}

// do sends a request with quota awareness, per-attempt timeouts and retries.
// The returned body is fully read.
func (c *HTTPClient) do(ctx context.Context, method, url string, body []byte, hdr http.Header, isGraphQL bool) (int, http.Header, []byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := c.waitForQuota(ctx, isGraphQL); err != nil {
			return 0, nil, nil, err
		}
		status, h, b, err := c.once(ctx, method, url, body, hdr, isGraphQL)
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		var wait time.Duration
		if err != nil {
			lastErr = err // network error or per-attempt timeout
			wait = time.Duration(1<<attempt) * time.Second
		} else {
			d, retry := retryDelay(status, h, attempt)
			if !retry {
				return status, h, b, nil
			}
			lastErr = &APIError{Status: status, URL: url, Body: string(b)}
			wait = d
		}
		if attempt == maxAttempts {
			break
		}
		c.log.Warn("github request retry", "url", url, "attempt", attempt, "wait", wait, "err", lastErr)
		if err := c.sleep(ctx, wait); err != nil {
			return 0, nil, nil, err
		}
	}
	return 0, nil, nil, lastErr
}

// once performs a single attempt with its own timeout.
func (c *HTTPClient) once(ctx context.Context, method, url string, body []byte, hdr http.Header, isGraphQL bool) (int, http.Header, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	c.calls.Add(1)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return 0, nil, nil, err
	}
	c.recordQuota(resp.Header, isGraphQL)
	return resp.StatusCode, resp.Header, b, nil
}

// restGet performs a conditional GET using the ETag cache. It returns the
// status (304 is translated to 200 with the cached body).
func (c *HTTPClient) restGet(ctx context.Context, path string) (int, []byte, error) {
	url := c.apiBase + path
	hdr := http.Header{}
	var cachedBody []byte
	if c.cache != nil {
		if etag, body, ok := c.cache.Get(ctx, url); ok {
			hdr.Set("If-None-Match", etag)
			cachedBody = body
		}
	}
	status, h, body, err := c.do(ctx, http.MethodGet, url, nil, hdr, false)
	if err != nil {
		return 0, nil, err
	}
	switch {
	case status == http.StatusNotModified && cachedBody != nil:
		return http.StatusOK, cachedBody, nil
	case status == http.StatusOK:
		if etag := h.Get("ETag"); etag != "" && c.cache != nil {
			c.cache.Put(ctx, url, etag, body)
		}
		return status, body, nil
	case status == http.StatusAccepted || status == http.StatusNoContent:
		return status, body, nil
	default:
		return status, nil, &APIError{Status: status, URL: url, Body: string(body)}
	}
}

type gqlError struct {
	Type    string   `json:"type"`
	Message string   `json:"message"`
	Path    []any    `json:"path"`
}

// GraphQLError carries errors returned in a GraphQL response body.
type GraphQLError struct{ Errors []gqlError }

func (e *GraphQLError) Error() string {
	if len(e.Errors) == 0 {
		return "graphql error"
	}
	return fmt.Sprintf("graphql: %s (%s)", e.Errors[0].Message, e.Errors[0].Type)
}

// graphql runs a query. Errors of type NOT_FOUND are tolerated when data is
// present (e.g. one aliased repo was deleted mid-scan).
func (c *HTTPClient) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	status, _, body, err := c.do(ctx, http.MethodPost, c.graphqlURL, payload, nil, true)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return &APIError{Status: status, URL: c.graphqlURL, Body: string(body)}
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decode graphql response: %w", err)
	}
	if len(env.Errors) > 0 {
		fatal := len(env.Data) == 0 || string(env.Data) == "null"
		for _, e := range env.Errors {
			if e.Type != "NOT_FOUND" {
				fatal = true
			}
		}
		if fatal {
			return &GraphQLError{Errors: env.Errors}
		}
		c.log.Warn("graphql partial result", "errors", len(env.Errors), "first", env.Errors[0].Message)
	}
	return json.Unmarshal(env.Data, out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
