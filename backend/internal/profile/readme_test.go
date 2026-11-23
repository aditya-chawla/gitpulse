package profile

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const readme = "# Hi, I'm Harshit\r\n\r\nIntro text.\r\n\r\n" + StartMarker + "\r\nold stuff\r\n" + EndMarker + "\r\n\r\n## Contact\r\nmail me\r\n"

func TestReplaceTouchesOnlyTheBlock(t *testing.T) {
	out, err := Replace(readme, "new block\nline 2")
	if err != nil {
		t.Fatal(err)
	}
	before, after, _ := strings.Cut(readme, StartMarker)
	_, tail, _ := strings.Cut(after, EndMarker)
	if !strings.HasPrefix(out, before+StartMarker) || !strings.HasSuffix(out, EndMarker+tail) {
		t.Fatalf("bytes outside the markers changed:\n%q", out)
	}
	// CRLF file: inserted block uses CRLF too.
	if !strings.Contains(out, StartMarker+"\r\nnew block\r\nline 2\r\n"+EndMarker) {
		t.Fatalf("block not inserted with CRLF:\n%q", out)
	}
	// Idempotent: replacing again with the same content is byte-identical.
	again, _ := Replace(out, "new block\nline 2")
	if again != out {
		t.Fatal("second replace changed the file")
	}
	cur, _ := Current(out)
	if !Same(cur, "new block\nline 2") {
		t.Fatalf("Same(%q) = false", cur)
	}
}

func TestReplaceRefusesBadMarkers(t *testing.T) {
	cases := map[string]error{
		"no markers":                           ErrNoMarkers,
		"only start " + StartMarker:            ErrBadMarkers,
		EndMarker + " reversed " + StartMarker: ErrBadMarkers,
		StartMarker + StartMarker + EndMarker:  ErrBadMarkers,
		StartMarker + EndMarker + EndMarker:    ErrBadMarkers,
	}
	for in, want := range cases {
		if _, err := Replace(in, "x"); !errors.Is(err, want) {
			t.Errorf("%q: err = %v want %v", in, err, want)
		}
	}
}

func TestRenderIsStableAndEscaped(t *testing.T) {
	d := Data{
		Languages: []Language{{"Go", 600}, {"TypeScript", 300}, {"Shell", 95}, {"Makefile", 5}},
		Projects:  []Project{{Name: "gitpulse", URL: "https://github.com/me/gitpulse", Description: "A [dash](board) <b>x</b> | y", Stars: 3}},
		Articles:  []Article{{Title: "Go *tips*", URL: "https://medium.com/@me/go-tips-1", Published: time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)}},
	}
	out := Render(d)
	for _, want := range []string{
		"**Go** 60% · **TypeScript** 30% · **Shell** 10%",
		"- [gitpulse](https://github.com/me/gitpulse): A \\[dash\\](board) &lt;b&gt;x&lt;/b&gt; \\| y ★ 3",
		"- [Go \\*tips\\*](https://medium.com/@me/go-tips-1) · Nov 20, 2026",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Makefile") {
		t.Error("languages under 1% should be left out")
	}
	if Render(d) != out {
		t.Error("render is not deterministic")
	}
}

func TestValidateFeedURL(t *testing.T) {
	ok := map[string]string{
		"https://medium.com/feed/@harshit":       "https://medium.com/feed/@harshit",
		"https://medium.com/feed/better-coding/": "https://medium.com/feed/better-coding",
		"https://harshit.medium.com/feed":        "https://harshit.medium.com/feed",
		"":                                       "",
	}
	for in, want := range ok {
		if got, err := ValidateFeedURL(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{
		"http://medium.com/feed/@me", "https://medium.com.evil.com/feed/@me", "https://evil.com/medium.com/feed",
		"https://medium.com/@me", "https://medium.com/feed/@me/extra", "https://a.b.medium.com/feed",
		"https://medium.com:8443/feed/@me", "https://user:pw@medium.com/feed/@me", "https://medium.com/feed/@me?x=1",
		"https://169.254.169.254/feed", "https://localhost/feed", "file:///etc/passwd",
	} {
		if _, err := ValidateFeedURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

const feed = `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>x</title>
<item><title><![CDATA[First post]]></title><link>https://medium.com/@me/first-post-abc?source=rss----1</link><pubDate>Thu, 20 Nov 2026 08:00:00 GMT</pubDate></item>
<item><title>Phishy</title><link>https://evil.example/p</link><pubDate>Thu, 20 Nov 2026 08:00:00 GMT</pubDate></item>
<item><title>Paren (1)</title><link>https://medium.com/@me/paren-(1)</link></item>
<item><title>3</title><link>https://medium.com/@me/3</link></item><item><title>4</title><link>https://medium.com/@me/4</link></item>
<item><title>5</title><link>https://medium.com/@me/5</link></item><item><title>6</title><link>https://medium.com/@me/6</link></item>
</channel></rss>`

func TestParseFeed(t *testing.T) {
	arts, err := ParseFeed([]byte(feed))
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 5 {
		t.Fatalf("got %d articles", len(arts))
	}
	if arts[0].Title != "First post" || arts[0].URL != "https://medium.com/@me/first-post-abc" || arts[0].Published.Day() != 20 {
		t.Errorf("first = %+v", arts[0])
	}
	for _, a := range arts {
		if strings.Contains(a.URL, "evil") {
			t.Error("non-Medium link kept")
		}
	}
	if arts[1].URL != "https://medium.com/@me/paren-%281%29" {
		t.Errorf("parens not encoded: %s", arts[1].URL)
	}
}

// rt serves canned responses for any request (lets tests "fetch" medium.com).
type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchArticlesLimits(t *testing.T) {
	serve := func(body string, status int) *http.Client {
		c := NewFeedClient()
		c.Transport = rt(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
		})
		return c
	}
	ctx := t.Context()
	if arts, err := FetchArticles(ctx, serve(feed, 200), "https://medium.com/feed/@me"); err != nil || len(arts) != 5 {
		t.Fatalf("ok feed: %d %v", len(arts), err)
	}
	if _, err := FetchArticles(ctx, serve(strings.Repeat("x", feedMaxBytes+10), 200), "https://medium.com/feed/@me"); err == nil {
		t.Error("oversized feed accepted")
	}
	if _, err := FetchArticles(ctx, serve(feed, 404), "https://medium.com/feed/@me"); err == nil {
		t.Error("404 accepted")
	}
	if _, err := FetchArticles(ctx, serve(feed, 200), "https://evil.example/feed"); !errors.Is(err, ErrFeedURL) {
		t.Error("disallowed URL fetched")
	}
	// Redirects off Medium are refused.
	c := NewFeedClient()
	c.Transport = rt(func(r *http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set("Location", "http://169.254.169.254/latest/meta-data")
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader("")), Header: h, Request: r}, nil
	})
	if _, err := FetchArticles(ctx, c, "https://medium.com/feed/@me"); err == nil {
		t.Error("redirect off Medium followed")
	}
}
