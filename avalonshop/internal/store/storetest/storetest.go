// Package storetest opens a throwaway test database. Not imported by the binary.
package storetest

import (
	"context"
	"os"
	"testing"

	"avalonshop/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool skips the test unless TEST_DATABASE_URL is set, then DROPS the public
// schema, recreates it, and runs migrations. Never point it at real data.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `drop schema public cascade; create schema public`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, pool, os.DirFS(root(t))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// root walks up from the test's working directory to the directory holding go.mod.
func root(t testing.TB) string {
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			return dir
		}
		dir += "/.."
	}
	t.Fatal("go.mod not found above test dir")
	return ""
}
