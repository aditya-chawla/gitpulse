package watchlist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/harshit3011/gitpulse/backend/internal/db"
	"github.com/harshit3011/gitpulse/backend/internal/github"
)

func TestParseRepoInput(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		name  string
		ok    bool
	}{
		{"torvalds/linux", "torvalds", "linux", true},
		{"  kubernetes/kubernetes/ ", "kubernetes", "kubernetes", true},
		{"https://github.com/golang/go", "golang", "go", true},
		{"https://github.com/golang/go.git", "golang", "go", true},
		{"github.com/facebook/react/", "facebook", "react", true},
		{"", "", "", false},
		{"onlyowner", "", "", false},
		{"a/b/c", "", "", false},
		{"https://gitlab.com/a/b", "", "", false},
		{"owner/..", "", "", false},
	}
	for _, tc := range cases {
		o, n, err := ParseRepoInput(tc.in)
		if tc.ok {
			if err != nil || o != tc.owner || n != tc.name {
				t.Errorf("ParseRepoInput(%q) = (%q, %q, %v), want (%q, %q, nil)", tc.in, o, n, err, tc.owner, tc.name)
			}
		} else if err == nil {
			t.Errorf("ParseRepoInput(%q) succeeded (%q/%q), want error", tc.in, o, n)
		}
	}
}

type fakeLookup struct {
	meta *RepoMeta
	err  error
}

func (f fakeLookup) LookupPublicRepo(_ context.Context, _ int64, owner, name string) (*RepoMeta, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.meta != nil {
		return f.meta, nil
	}
	return &RepoMeta{ID: 42, Owner: owner, Name: name}, nil
}

func testDB(t *testing.T, lookup RepoLookup) *Service {
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
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, watched_repos CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, node_id, login) VALUES (1, 'U1', 'me')`); err != nil {
		t.Fatal(err)
	}
	return New(pool, lookup)
}

func TestWatchlistCRUDAndLimits(t *testing.T) {
	svc := testDB(t, fakeLookup{})
	ctx := context.Background()

	w, err := svc.Add(ctx, 1, "https://github.com/torvalds/linux")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if w.FullName != "torvalds/linux" || w.GitHubID == nil || *w.GitHubID != 42 {
		t.Fatalf("unexpected watched repo: %+v", w)
	}

	if _, err := svc.Add(ctx, 1, "torvalds/linux"); !errors.Is(err, ErrAlreadyWatched) {
		t.Fatalf("duplicate Add: err = %v, want ErrAlreadyWatched", err)
	}

	list, err := svc.List(ctx, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: len=%d err=%v", len(list), err)
	}

	if err := svc.Remove(ctx, 1, w.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := svc.Remove(ctx, 1, w.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Remove: err = %v, want ErrNotFound", err)
	}

	for i := range MaxWatchedRepos {
		if _, err := svc.Add(ctx, 1, fmt.Sprintf("org/repo-%d", i)); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if _, err := svc.Add(ctx, 1, "org/overflow"); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("51st Add: err = %v, want ErrLimitReached", err)
	}
}

func TestWatchlistRejectsPrivateRepo(t *testing.T) {
	svc := testDB(t, fakeLookup{err: github.ErrPrivateRepo})
	if _, err := svc.Add(context.Background(), 1, "org/secret"); !errors.Is(err, ErrPrivateRepo) {
		t.Fatalf("private Add: err = %v, want ErrPrivateRepo", err)
	}
}
