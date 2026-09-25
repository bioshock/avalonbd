package store_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

// countMigrationFiles counts the *.sql files in migrations/ the same way
// store.Migrate does, so this test's expectation tracks the embedded
// directory instead of a hard-coded literal that the next migration file
// would otherwise break again.
func countMigrationFiles(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			n++
		}
	}
	return n
}

func TestMigrateIdempotent(t *testing.T) {
	db := storetest.Pool(t)
	ctx := context.Background()
	if err := store.Migrate(ctx, db, os.DirFS("../..")); err != nil {
		t.Fatalf("second run: %v", err)
	}
	want := countMigrationFiles(t)
	var n int
	if err := db.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&n); err != nil || n != want {
		t.Fatalf("want %d migrations recorded, got %d err=%v", want, n, err)
	}
	for _, tbl := range []string{"categories", "products", "variants", "product_images", "delivery_zones", "users", "orders", "order_items"} {
		var ok bool
		db.QueryRow(ctx, `select to_regclass($1) is not null`, tbl).Scan(&ok)
		if !ok {
			t.Fatalf("table %s missing", tbl)
		}
	}
}
