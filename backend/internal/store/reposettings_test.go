package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/harshit3011/gitpulse/backend/internal/db"
)

func testStore(t *testing.T) *Store {
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
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, scans, repos CASCADE`); err != nil {
		t.Fatal(err)
	}
	return New(pool)
}

func TestRepoSettingsDefaultOffAndOwnerOnly(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	for _, u := range []User{{ID: 1, NodeID: "U1", Login: "me"}, {ID: 2, NodeID: "U2", Login: "other"}} {
		if err := st.UpsertUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO repos (id, user_id, full_name) VALUES (10, 1, 'me/a'), (20, 2, 'other/b')`); err != nil {
		t.Fatal(err)
	}

	got, err := st.RepoSettingFor(ctx, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.PRMode != PRModeDashboard || got.ReleasesEnabled {
		t.Fatalf("defaults must not write anything: %+v", got)
	}

	saved, err := st.SaveRepoSetting(ctx, 1, RepoSetting{RepoID: 10, PRMode: PRModeComment, ReleasesEnabled: true})
	if err != nil || saved.PRMode != PRModeComment || !saved.ReleasesEnabled || saved.FullName != "me/a" {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}

	// Another user's repo is invisible and cannot be switched on.
	if _, err := st.SaveRepoSetting(ctx, 1, RepoSetting{RepoID: 20, PRMode: PRModeComment}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign repo: err = %v", err)
	}
	if _, err := st.RepoSettingFor(ctx, 1, 20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign repo read: err = %v", err)
	}
	if _, err := st.SaveRepoSetting(ctx, 1, RepoSetting{RepoID: 10, PRMode: "yolo"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad mode: err = %v", err)
	}
	list, _ := st.ListRepoSettings(ctx, 1)
	if len(list) != 1 || list[0].RepoID != 10 {
		t.Fatalf("list = %+v", list)
	}
}

func TestWatchedRepoReadOnly(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	if err := st.UpsertUser(ctx, User{ID: 1, NodeID: "U1", Login: "me"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO repos (id, user_id, full_name, source) VALUES (99, 1, 'torvalds/linux', 'watched')`); err != nil {
		t.Fatal(err)
	}
	got, err := st.RepoSettingFor(ctx, 1, 99)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "watched" || got.PRMode != PRModeOff || got.ReleasesEnabled {
		t.Fatalf("watched repo must default to off/false: %+v", got)
	}
	if _, err := st.SaveRepoSetting(ctx, 1, RepoSetting{RepoID: 99, PRMode: PRModeComment, ReleasesEnabled: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("saving watched repo setting: err = %v, want ErrInvalid", err)
	}
}
