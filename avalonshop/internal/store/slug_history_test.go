package store_test

import (
	"context"
	"errors"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

func createSlugHistoryProduct(t *testing.T, st *store.Store, slug string, active bool) int64 {
	t.Helper()
	id, err := st.CreateProduct(context.Background(),
		store.Product{Slug: slug, Name: "Product " + slug, Active: active},
		[]store.Variant{{Name: "x", Price: 100, Stock: 1}})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func renameSlugHistoryProduct(t *testing.T, st *store.Store, id int64, newSlug string) {
	t.Helper()
	ctx := context.Background()
	p, err := st.GetProduct(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	old := p.Slug
	p.Slug = newSlug
	if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordOldSlug(ctx, id, old); err != nil {
		t.Fatal(err)
	}
}

// TestResolveSlugRedirect covers the basic rename case: the old slug resolves
// to the product's current slug.
func TestResolveSlugRedirect(t *testing.T) {
	st := store.New(storetest.Pool(t))
	id := createSlugHistoryProduct(t, st, "socks", true)
	renameSlugHistoryProduct(t, st, id, "wool-socks")

	got, err := st.ResolveSlugRedirect(context.Background(), "socks")
	if err != nil || got != "wool-socks" {
		t.Fatalf("ResolveSlugRedirect(socks) = %q, %v; want wool-socks, nil", got, err)
	}
}

// TestResolveSlugRedirectChain rules out an A->B->C chain resolving through
// an intermediate slug: both A and B must resolve straight to C, never to
// each other, because both history rows point at the same product_id and the
// join always reads that product's CURRENT slug.
func TestResolveSlugRedirectChain(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	id := createSlugHistoryProduct(t, st, "a", true)
	renameSlugHistoryProduct(t, st, id, "b")
	renameSlugHistoryProduct(t, st, id, "c")

	for _, old := range []string{"a", "b"} {
		got, err := st.ResolveSlugRedirect(ctx, old)
		if err != nil || got != "c" {
			t.Fatalf("ResolveSlugRedirect(%s) = %q, %v; want c, nil", old, got, err)
		}
	}
}

// TestResolveSlugRedirectUnknown asserts a slug that was never live and never
// retired reports ErrNotFound, not some other error or a zero value taken as
// success.
func TestResolveSlugRedirectUnknown(t *testing.T) {
	st := store.New(storetest.Pool(t))
	_, err := st.ResolveSlugRedirect(context.Background(), "never-existed")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// TestResolveSlugRedirectInactiveProduct404s is task-21-decisions #1: an old
// slug whose product has since been deactivated must resolve as not-found,
// exactly like that product's own current slug does, rather than pointing a
// 301 at a page that will then 404. This is the mutation the task flags as
// most likely to pass silently if the active filter is ever dropped from the
// join.
func TestResolveSlugRedirectInactiveProduct404s(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	id := createSlugHistoryProduct(t, st, "seasonal-old", true)
	renameSlugHistoryProduct(t, st, id, "seasonal-new")

	p, err := st.GetProduct(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	p.Active = false
	if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
		t.Fatal(err)
	}

	_, err = st.ResolveSlugRedirect(ctx, "seasonal-old")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old slug of a deactivated product must report ErrNotFound (404), got %v", err)
	}
}

// TestRecordOldSlugGuardsSelfReference asserts the write side never writes a
// product's own CURRENT slug into its history. A rename-and-rename-back
// sequence retires "b" while the live slug lands back on "a", which already
// has a history row from the first rename; RecordOldSlug must not disturb
// that, and a fresh call with the slug the product already currently has
// must be a silent no-op.
func TestRecordOldSlugGuardsSelfReference(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	id := createSlugHistoryProduct(t, st, "a", true)

	// Calling RecordOldSlug with the product's own current slug must not
	// create a row at all.
	if err := st.RecordOldSlug(ctx, id, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResolveSlugRedirect(ctx, "a"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a product's own current slug must never resolve out of history, got %v", err)
	}

	renameSlugHistoryProduct(t, st, id, "b") // records "a" -> id
	renameSlugHistoryProduct(t, st, id, "a") // records "b" -> id; live slug is "a" again

	// "b" now correctly resolves to the live slug "a".
	if got, err := st.ResolveSlugRedirect(ctx, "b"); err != nil || got != "a" {
		t.Fatalf("ResolveSlugRedirect(b) = %q, %v; want a, nil", got, err)
	}
	// The stale "a" history row from the first rename still exists, but it
	// can never shadow the live product: callers check products.slug first
	// (internal/app/store_pages.go), so a request for "a" is answered by the
	// live row and never reaches this history lookup at all.
}

// TestSlugHistoryUpsertOnReuse covers what happens when two different
// products race to claim the same retired slug: product A retires "socks",
// then product B is renamed to "socks" and later retires it too. The
// product_slugs primary key on slug alone would otherwise make the second
// retirement fail with a duplicate-key error; the upsert instead makes the
// newest owner (B) win the redirect.
func TestSlugHistoryUpsertOnReuse(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	a := createSlugHistoryProduct(t, st, "socks", true)
	renameSlugHistoryProduct(t, st, a, "wool-socks") // retires "socks" -> a

	b := createSlugHistoryProduct(t, st, "socks", true) // reuse is allowed, live uniqueness only
	renameSlugHistoryProduct(t, st, b, "cotton-socks")  // retires "socks" -> b, must upsert not error

	got, err := st.ResolveSlugRedirect(ctx, "socks")
	if err != nil {
		t.Fatalf("upsert on a reused-then-retired slug must not error: %v", err)
	}
	if got != "cotton-socks" {
		t.Fatalf("the newest owner must win: got %q, want cotton-socks", got)
	}
}

// TestSlugHistoryCascadeDelete asserts deleting a product removes its
// product_slugs rows, checked directly against the table rather than through
// the redirect (a join to a deleted product would also stop matching if the
// cascade were missing, which would make that assertion pass for the wrong
// reason).
func TestSlugHistoryCascadeDelete(t *testing.T) {
	pool := storetest.Pool(t)
	st := store.New(pool)
	ctx := context.Background()
	id := createSlugHistoryProduct(t, st, "p1", true)
	renameSlugHistoryProduct(t, st, id, "p2")

	var before int
	if err := pool.QueryRow(ctx, `select count(*) from product_slugs where product_id = $1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 1 {
		t.Fatalf("want 1 history row before delete, got %d", before)
	}

	if _, err := st.DeleteProduct(ctx, id); err != nil {
		t.Fatal(err)
	}

	var after int
	if err := pool.QueryRow(ctx, `select count(*) from product_slugs where product_id = $1`, id).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Fatalf("deleting a product must cascade-delete its slug history, got %d rows left", after)
	}
}
