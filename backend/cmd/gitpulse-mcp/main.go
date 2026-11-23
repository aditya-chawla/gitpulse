// Command gitpulse-mcp is a small stdio MCP server that proxies every tool to
// a remote gitpulse /mcp endpoint. Use it with MCP clients that only launch
// local (stdio) servers, such as Claude Desktop.
//
//	GITPULSE_MCP_URL=https://<api-host>/mcp GITPULSE_MCP_TOKEN=... gitpulse-mcp
//
// The remote tool list is fetched at start-up. Logs go to stderr; stdout
// carries the protocol.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "1.0.0"

// bearer adds the token to every request to the remote server.
type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

// remote holds the client session and reconnects when it breaks.
type remote struct {
	url, token string
	log        *slog.Logger
	client     *mcp.Client

	mu      sync.Mutex
	session *mcp.ClientSession
}

// connect retries for a while: a free Render instance can take a minute or
// more to wake up.
func (r *remote) connect(ctx context.Context) (*mcp.ClientSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != nil {
		return r.session, nil
	}
	deadline := time.Now().Add(3 * time.Minute)
	wait := 2 * time.Second
	for {
		t := &mcp.StreamableClientTransport{
			Endpoint:             r.url,
			HTTPClient:           &http.Client{Timeout: 120 * time.Second, Transport: bearer{r.token, http.DefaultTransport}},
			DisableStandaloneSSE: true, // the server is stateless: request/response only
		}
		s, err := r.client.Connect(ctx, t, nil)
		if err == nil {
			r.session = s
			return s, nil
		}
		if strings.Contains(err.Error(), "401") || time.Now().After(deadline) || ctx.Err() != nil {
			return nil, fmt.Errorf("connect to %s: %w", r.url, err)
		}
		r.log.Warn("gitpulse not reachable yet, retrying (it may be waking up)", "err", err, "in", wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, 20*time.Second)
	}
}

func (r *remote) reset(s *mcp.ClientSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == s {
		_ = s.Close()
		r.session = nil
	}
}

// call forwards one tool call, reconnecting once on a transport failure.
func (r *remote) call(ctx context.Context, name string, args any) (*mcp.CallToolResult, error) {
	for attempt := 0; ; attempt++ {
		s, err := r.connect(ctx)
		if err != nil {
			return nil, err
		}
		res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err == nil || attempt == 1 || ctx.Err() != nil {
			return res, err
		}
		r.log.Warn("tool call failed; reconnecting", "tool", name, "err", err)
		r.reset(s)
	}
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("gitpulse-mcp", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	url, token := strings.TrimSpace(os.Getenv("GITPULSE_MCP_URL")), strings.TrimSpace(os.Getenv("GITPULSE_MCP_TOKEN"))
	if url == "" || token == "" {
		return errors.New("set GITPULSE_MCP_URL (e.g. https://<api-host>/mcp) and GITPULSE_MCP_TOKEN")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	r := &remote{url: url, token: token, log: log,
		client: mcp.NewClient(&mcp.Implementation{Name: "gitpulse-mcp-proxy", Version: version}, nil)}
	s, err := r.connect(ctx)
	if err != nil {
		return err
	}
	list, err := s.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	instructions := ""
	if ir := s.InitializeResult(); ir != nil {
		instructions = ir.Instructions
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "gitpulse", Title: "gitpulse", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	for _, tool := range list.Tools {
		name := tool.Name
		srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return r.call(ctx, name, req.Params.Arguments)
		})
	}
	log.Info("proxying gitpulse MCP over stdio", "remote", url, "tools", len(list.Tools))
	return srv.Run(ctx, &mcp.StdioTransport{})
}
