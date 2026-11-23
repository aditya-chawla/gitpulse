package profile

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	feedTimeout  = 10 * time.Second
	feedMaxBytes = 2 << 20 // 2 MB
	maxArticles  = 5
)

var ErrFeedURL = errors.New("feed URL must be https://medium.com/feed/<@user or publication> or https://<name>.medium.com/feed")

// ValidateFeedURL allows only Medium feed URLs, so the server can't be
// pointed at internal or arbitrary hosts (SSRF). Returns the canonical URL.
func ValidateFeedURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrFeedURL
	}
	host := strings.ToLower(u.Hostname())
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	switch {
	case host == "medium.com":
		// /feed/@user or /feed/publication (one segment after /feed/)
		rest, ok := strings.CutPrefix(path, "/feed/")
		if !ok || rest == "" || strings.Contains(rest, "/") || strings.Contains(rest, "..") {
			return "", ErrFeedURL
		}
	case strings.HasSuffix(host, ".medium.com") && !strings.Contains(strings.TrimSuffix(host, ".medium.com"), "."):
		if path != "/feed" {
			return "", ErrFeedURL
		}
	default:
		return "", ErrFeedURL
	}
	return "https://" + host + path, nil
}

func allowedHost(h string) bool {
	h = strings.ToLower(h)
	return h == "medium.com" || strings.HasSuffix(h, ".medium.com")
}

// NewFeedClient returns an HTTP client that refuses redirects off Medium.
func NewFeedClient() *http.Client {
	return &http.Client{
		Timeout: feedTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || req.URL.Scheme != "https" || !allowedHost(req.URL.Hostname()) {
				return errors.New("redirect outside medium.com refused")
			}
			return nil
		},
	}
}

type rss struct {
	Channel struct {
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			PubDate string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

// FetchArticles reads the latest posts from a validated Medium feed.
func FetchArticles(ctx context.Context, client *http.Client, feedURL string) ([]Article, error) {
	u, err := ValidateFeedURL(feedURL)
	if err != nil || u == "" {
		return nil, ErrFeedURL
	}
	ctx, cancel := context.WithTimeout(ctx, feedTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "gitpulse (profile README updater)")
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch feed: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, feedMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read feed: %w", err)
	}
	if len(body) > feedMaxBytes {
		return nil, errors.New("feed is larger than 2 MB")
	}
	return ParseFeed(body)
}

// ParseFeed extracts up to 5 articles. Links must point at Medium over https;
// tracking query strings are dropped.
func ParseFeed(body []byte) ([]Article, error) {
	var f rss
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}
	var out []Article
	for _, it := range f.Channel.Items {
		link, err := url.Parse(strings.TrimSpace(it.Link))
		title := strings.TrimSpace(it.Title)
		if err != nil || link.Scheme != "https" || !allowedHost(link.Hostname()) || title == "" {
			continue
		}
		link.RawQuery, link.Fragment = "", ""
		// Parentheses would end the markdown link early.
		a := Article{Title: title, URL: strings.NewReplacer("(", "%28", ")", "%29").Replace(link.String())}
		if t, err := time.Parse(time.RFC1123, strings.TrimSpace(it.PubDate)); err == nil {
			a.Published = t
		} else if t, err := time.Parse(time.RFC1123Z, strings.TrimSpace(it.PubDate)); err == nil {
			a.Published = t
		}
		out = append(out, a)
		if len(out) == maxArticles {
			break
		}
	}
	return out, nil
}
