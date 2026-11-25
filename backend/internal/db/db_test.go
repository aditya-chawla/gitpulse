package db

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

func TestMigrateUpDownRoundtrip(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	if err := Migrate(url); err != nil {
		t.Fatalf("Migrate up: %v", err)
	}

	pool, err := Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.Close()

	for _, table := range []string{"watched_repos", "repo_trend_snapshots", "action_items"} {
		var exists bool
		err := pool.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
			table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("expected table %s to exist after Migrate", table)
		}
	}

	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, toMigrateURL(url))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if err := m.Steps(-3); err != nil {
		t.Fatalf("migrate down 3 steps: %v", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate re-up: %v", err)
	}
}
