package store_test

import (
	"context"
	"os"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

func TestMigrateIdempotent(t *testing.T) {
	db := storetest.Pool(t)
	ctx := context.Background()
	if err := store.Migrate(ctx, db, os.DirFS("../..")); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var n int
	if err := db.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("want 1 migration recorded, got %d err=%v", n, err)
	}
	for _, tbl := range []string{"categories", "products", "variants", "product_images", "delivery_zones", "users", "orders", "order_items"} {
		var ok bool
		db.QueryRow(ctx, `select to_regclass($1) is not null`, tbl).Scan(&ok)
		if !ok {
			t.Fatalf("table %s missing", tbl)
		}
	}
}
