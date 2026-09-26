package store_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

func seedProduct(t *testing.T, st *store.Store, slug string, active bool, prices ...int) int64 {
	t.Helper()
	ctx := context.Background()
	var vs []store.Variant
	for i, p := range prices {
		vs = append(vs, store.Variant{Name: []string{"250g", "500g", "1kg"}[i], Price: p, Stock: 5, Sort: i})
	}
	id, err := st.CreateProduct(ctx, store.Product{Slug: slug, Name: "Product " + slug, Description: "desc", Active: active, Featured: slug == "honey"}, vs)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProductsListAndGet(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	cat, _ := st.CreateCategory(ctx, store.Category{Slug: "honey", Name: "Honey"})
	honey := seedProduct(t, st, "honey", true, 900, 650)
	seedProduct(t, st, "mango", true, 1200)
	seedProduct(t, st, "hidden", false, 10)
	p, _ := st.GetProduct(ctx, honey)
	p.CategoryID = &cat
	if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
		t.Fatal(err)
	}
	st.AddImage(ctx, store.Image{ProductID: honey, File: "abc", Alt: "jar", Width: 1600, Height: 1200})

	cards, err := st.ListProductCards(ctx, store.ListOpts{})
	if err != nil || len(cards) != 2 {
		t.Fatalf("want 2 active cards, got %d err=%v", len(cards), err)
	}
	var h store.ProductCard
	for _, c := range cards {
		if c.Slug == "honey" {
			h = c
		}
	}
	if h.MinPrice != 650 || h.VariantCount != 2 || !h.InStock || h.ImageFile == nil || *h.ImageFile != "abc" {
		t.Fatalf("card wrong: %+v", h)
	}
	cards, _ = st.ListProductCards(ctx, store.ListOpts{CategorySlug: "honey"})
	if len(cards) != 1 {
		t.Fatalf("category filter: %d", len(cards))
	}
	cards, _ = st.ListProductCards(ctx, store.ListOpts{Query: "MANG"})
	if len(cards) != 1 || cards[0].Slug != "mango" {
		t.Fatalf("search: %+v", cards)
	}
	cards, _ = st.ListProductCards(ctx, store.ListOpts{FeaturedOnly: true})
	if len(cards) != 1 || cards[0].Slug != "honey" {
		t.Fatalf("featured: %+v", cards)
	}
	cards, _ = st.ListProductCards(ctx, store.ListOpts{CategoryID: cat, ExcludeID: honey, Limit: 4})
	if len(cards) != 0 {
		t.Fatalf("related should exclude self: %+v", cards)
	}

	full, err := st.GetProductBySlug(ctx, "honey", true)
	if err != nil || len(full.Variants) != 2 || full.Variants[0].Name != "250g" || len(full.Images) != 1 || full.Category == nil || full.Category.Slug != "honey" {
		t.Fatalf("full: %+v err=%v", full, err)
	}
	if _, err := st.GetProductBySlug(ctx, "hidden", true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("inactive should be not found for store, got %v", err)
	}
	if _, err := st.GetProductBySlug(ctx, "hidden", false); err != nil {
		t.Fatalf("inactive should be visible to admin: %v", err)
	}
	entries, _ := st.ActiveProductsForSitemap(ctx)
	if len(entries) != 2 {
		t.Fatalf("sitemap: %+v", entries)
	}
	rows, _ := st.ListProductsAdmin(ctx)
	if len(rows) != 3 {
		t.Fatalf("admin rows: %d", len(rows))
	}
}

func TestProductUpdateVariantsAndDelete(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	id := seedProduct(t, st, "honey", true, 900, 650)
	p, _ := st.GetProduct(ctx, id)
	// keep first (renamed), drop second, add third
	vs := []store.Variant{{ID: p.Variants[0].ID, Name: "250 g", Price: 950, Stock: 3, Sort: 0}, {Name: "2kg", Price: 2400, Stock: 1, Sort: 1}}
	p.Name = "Wild Honey"
	if err := st.UpdateProduct(ctx, p.Product, vs); err != nil {
		t.Fatal(err)
	}
	p2, _ := st.GetProduct(ctx, id)
	if p2.Name != "Wild Honey" || len(p2.Variants) != 2 || p2.Variants[0].Name != "250 g" || p2.Variants[0].Price != 950 || p2.Variants[1].Name != "2kg" {
		t.Fatalf("variants wrong: %+v", p2.Variants)
	}
	if !p2.UpdatedAt.After(p.UpdatedAt) {
		t.Fatal("updated_at not bumped")
	}
	st.AddImage(ctx, store.Image{ProductID: id, File: "img1", Width: 400, Height: 300})
	imgs, err := st.DeleteProduct(ctx, id)
	if err != nil || len(imgs) != 1 || imgs[0].File != "img1" {
		t.Fatalf("delete: %+v %v", imgs, err)
	}
	if _, err := st.GetProduct(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("product still exists")
	}
}

func TestImagesOrderAndCartLookup(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	id := seedProduct(t, st, "honey", true, 650)
	a, _ := st.AddImage(ctx, store.Image{ProductID: id, File: "a", Width: 400, Height: 400})
	b, _ := st.AddImage(ctx, store.Image{ProductID: id, File: "b", Width: 400, Height: 400})
	imgs, _ := st.ListImages(ctx, id)
	if imgs[0].ID != a || imgs[1].ID != b {
		t.Fatalf("initial order: %+v", imgs)
	}
	if _, err := st.MoveImage(ctx, b, true); err != nil {
		t.Fatal(err)
	}
	imgs, _ = st.ListImages(ctx, id)
	if imgs[0].ID != b {
		t.Fatalf("move up failed: %+v", imgs)
	}
	if movedProduct, err := st.MoveImage(ctx, b, true); err != nil {
		t.Fatalf("moving the first image up should be a no-op: %v", err)
	} else if movedProduct != id {
		t.Fatalf("MoveImage should return the image's own product id, got %d want %d", movedProduct, id)
	}
	st.UpdateImageAlt(ctx, a, "jar of honey")
	del, err := st.DeleteImage(ctx, a)
	if err != nil || del.Alt != "jar of honey" {
		t.Fatalf("delete image: %+v %v", del, err)
	}
	p, _ := st.GetProduct(ctx, id)
	cv, err := st.VariantsForCart(ctx, []int64{p.Variants[0].ID, 999999})
	if err != nil || len(cv) != 1 || cv[0].ProductSlug != "honey" || cv[0].Price != 650 || cv[0].ImageFile == nil || *cv[0].ImageFile != "b" {
		t.Fatalf("cart lookup: %+v %v", cv, err)
	}
}

// ---- Task 21 fix round 1: slug history is written inside UpdateProduct ----

// TestUpdateProductRecordsSlugHistoryOnRename asserts UpdateProduct itself —
// not a separate caller-side step — writes the retired slug into
// product_slugs when the slug actually changes.
func TestUpdateProductRecordsSlugHistoryOnRename(t *testing.T) {
	pool := storetest.Pool(t)
	st := store.New(pool)
	ctx := context.Background()
	id := seedProduct(t, st, "wild-honey", true, 650)

	p, _ := st.GetProduct(ctx, id)
	p.Slug = "wild-forest-honey"
	if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
		t.Fatal(err)
	}

	var slug string
	var productID int64
	if err := pool.QueryRow(ctx, `select slug, product_id from product_slugs where slug = $1`, "wild-honey").Scan(&slug, &productID); err != nil {
		t.Fatalf("expected a history row for the retired slug: %v", err)
	}
	if productID != id {
		t.Fatalf("history row points at product %d, want %d", productID, id)
	}
	got, err := st.ResolveSlugRedirect(ctx, "wild-honey")
	if err != nil || got != "wild-forest-honey" {
		t.Fatalf("ResolveSlugRedirect(wild-honey) = %q, %v; want wild-forest-honey, nil", got, err)
	}
}

// TestUpdateProductSkipsSlugHistoryWhenSlugUnchanged asserts a save that only
// touches price, name, or any other field never writes to product_slugs.
func TestUpdateProductSkipsSlugHistoryWhenSlugUnchanged(t *testing.T) {
	pool := storetest.Pool(t)
	st := store.New(pool)
	ctx := context.Background()
	id := seedProduct(t, st, "wild-honey", true, 650)

	p, _ := st.GetProduct(ctx, id)
	p.Name = "Wild Honey (Grade A)"
	if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := pool.QueryRow(ctx, `select count(*) from product_slugs where product_id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a save that doesn't change the slug must not write history, got %d rows", n)
	}
}

// TestUpdateProductConcurrentRenamesDoNotLoseSlugs is the Task 21 fix-round-1
// regression test. Two admins renaming the SAME product concurrently to
// different slugs must not silently drop one of the two retired values: the
// bug (fixed by reading the old slug under `select ... for update` inside
// UpdateProduct's own transaction, rather than trusting a value the caller
// read earlier) let whichever rename lost the last-write race vanish with no
// error and no history row, 404ing forever afterward.
//
// Reliability, not luck:
//   - Each iteration uses its own fresh product, so a bad ordering in one
//     iteration can't be masked or amplified by another.
//   - Both renames are released from a shared, closed-once channel so their
//     Begin/lock/read/write round trips to a real Postgres instance actually
//     overlap; pgxpool gives each goroutine its own connection, so there is
//     no client-side serialization forcing them apart.
//   - 20 independent iterations run, and the test asserts that BOTH
//     candidates win at least once across them. If one side deterministically
//     won every time, that would mean the two calls were never actually
//     concurrent (e.g. because of accidental sequencing in the test itself),
//     and the whole test would only be proving something true by construction
//     rather than exercising the race — the test fails outright in that case
//     rather than silently passing for the wrong reason.
//   - Every iteration checks the exact, not just the eventual, outcome:
//     product_slugs for that product must contain PRECISELY {s0, loser} —
//     s0 recorded by whichever transaction ran first (it read the original
//     slug), and loser recorded by whichever ran second (it read the first
//     transaction's committed new slug, under the row lock, as ITS old
//     slug). This is the serialization claim itself, not just its consequence.
func TestUpdateProductConcurrentRenamesDoNotLoseSlugs(t *testing.T) {
	const iterations = 20
	pool := storetest.Pool(t)
	st := store.New(pool)
	ctx := context.Background()

	var lost int
	var winsA, winsB int
	for i := 0; i < iterations; i++ {
		s0 := fmt.Sprintf("iter%d-s0", i)
		slugA := fmt.Sprintf("iter%d-a", i)
		slugB := fmt.Sprintf("iter%d-b", i)
		id := seedProduct(t, st, s0, true, 100)

		start := make(chan struct{})
		var wg sync.WaitGroup
		rename := func(newSlug string) {
			defer wg.Done()
			<-start
			p, err := st.GetProduct(ctx, id)
			if err != nil {
				t.Error(err)
				return
			}
			p.Slug = newSlug
			if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
				t.Error(err)
			}
		}
		wg.Add(2)
		go rename(slugA)
		go rename(slugB)
		close(start)
		wg.Wait()

		final, err := st.GetProduct(ctx, id)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		var winner, loser string
		switch final.Slug {
		case slugA:
			winner, loser = slugA, slugB
			winsA++
		case slugB:
			winner, loser = slugB, slugA
			winsB++
		default:
			t.Fatalf("iteration %d: final slug %q is neither candidate (a=%q b=%q)", i, final.Slug, slugA, slugB)
		}

		// Every slug that was ever live for this product must resolve to the
		// one now live.
		for _, old := range []string{s0, loser} {
			got, err := st.ResolveSlugRedirect(ctx, old)
			if err != nil || got != winner {
				lost++
				t.Logf("iteration %d: %q lost — ResolveSlugRedirect(%q) = %q, %v; want %q, nil", i, old, old, got, err, winner)
			}
		}

		// The serialization claim, checked directly: history holds exactly
		// {s0, loser}, nothing more and nothing less.
		rows, err := pool.Query(ctx, `select slug from product_slugs where product_id = $1 order by slug`, id)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		var got []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			got = append(got, s)
		}
		rows.Close()
		want := []string{s0, loser}
		sort.Strings(want)
		if !equalStrings(got, want) {
			lost++
			t.Logf("iteration %d: history = %v, want exactly %v", i, got, want)
		}
	}

	t.Logf("win split across %d iterations: a=%d b=%d (both non-zero proves genuine interleaving)", iterations, winsA, winsB)
	if winsA == 0 || winsB == 0 {
		t.Fatalf("both candidates must win at least once across %d iterations to prove genuine concurrency (a won %d, b won %d) — the test is not exercising the race", iterations, winsA, winsB)
	}
	if lost > 0 {
		t.Fatalf("%d slug(s) lost or mis-recorded across %d concurrent-rename iterations (see log lines above)", lost, iterations)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRegularPriceTaglineAndPromo(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	if _, err := st.GetPromo(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no promo yet: want ErrNotFound, got %v", err)
	}
	reg := 550
	trio, err := st.CreateProduct(ctx, store.Product{Slug: "trio", Name: "Trio", Tagline: "Three in one", Active: true, Promo: true},
		[]store.Variant{{Name: "3 × 100g", Price: 499, RegularPrice: &reg, Stock: 5}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.GetProduct(ctx, trio)
	if err != nil || p.Tagline != "Three in one" || !p.Promo || p.Variants[0].RegularPrice == nil || *p.Variants[0].RegularPrice != 550 {
		t.Fatalf("round trip lost fields: %+v %v", p, err)
	}
	got, err := st.GetPromo(ctx)
	if err != nil || got.ID != trio || len(got.Variants) != 1 {
		t.Fatalf("GetPromo = %+v %v", got, err)
	}

	// Clearing the regular price through UpdateProduct stores NULL.
	p.Variants[0].RegularPrice = nil
	if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
		t.Fatal(err)
	}
	if p, _ = st.GetProduct(ctx, trio); p.Variants[0].RegularPrice != nil {
		t.Fatal("regular price should be cleared")
	}

	// A second promo product takes the slot; the first loses it.
	other, err := st.CreateProduct(ctx, store.Product{Slug: "other", Name: "Other", Active: true, Promo: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetPromo(ctx); got.ID != other {
		t.Fatalf("promo should move to other, got %d", got.ID)
	}
	if p, _ = st.GetProduct(ctx, trio); p.Promo {
		t.Fatal("trio should have lost promo")
	}
	// ...and back again through UpdateProduct.
	p.Promo = true
	if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetPromo(ctx); got.ID != trio {
		t.Fatalf("promo should be trio again, got %d", got.ID)
	}

	// Review Focus 5: an inactive promo product is not returned.
	p.Active = false
	if err := st.UpdateProduct(ctx, p.Product, p.Variants); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPromo(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("inactive promo must be hidden, got %v", err)
	}
}

func TestProductCardCheapestVariantFields(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	reg := 150
	id, err := st.CreateProduct(ctx, store.Product{Slug: "onion", Name: "Onion", Tagline: "Fine-ground", Active: true},
		[]store.Variant{{Name: "200g", Price: 240, Stock: 1, Sort: 0}, {Name: "100g", Price: 130, RegularPrice: &reg, Stock: 1, Sort: 1}})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := st.GetProduct(ctx, id)
	cards, err := st.ListProductCards(ctx, store.ListOpts{})
	if err != nil || len(cards) != 1 {
		t.Fatalf("cards %+v %v", cards, err)
	}
	c := cards[0]
	if c.Tagline != "Fine-ground" || c.VariantName != "100g" || c.VariantID != p.Variants[1].ID || c.RegularPrice == nil || *c.RegularPrice != 150 || c.MinPrice != 130 {
		t.Fatalf("card wrong: %+v", c)
	}
}

// TestPromoConcurrentSaves is Review Focus 1: two saves that both set promo
// on different products at the same moment must both succeed and leave
// exactly one promo product.
func TestPromoConcurrentSaves(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	var ps []store.ProductFull
	for _, slug := range []string{"a", "b"} {
		id, err := st.CreateProduct(ctx, store.Product{Slug: slug, Name: slug, Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})
		if err != nil {
			t.Fatal(err)
		}
		p, _ := st.GetProduct(ctx, id)
		p.Promo = true
		ps = append(ps, p)
	}
	for round := 0; round < 10; round++ {
		var wg sync.WaitGroup
		errs := make([]error, len(ps))
		for i, p := range ps {
			wg.Add(1)
			go func() { defer wg.Done(); errs[i] = st.UpdateProduct(ctx, p.Product, p.Variants) }()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: concurrent promo save failed: %v", round, err)
			}
		}
		n := 0
		for _, p := range ps {
			if got, _ := st.GetProduct(ctx, p.ID); got.Promo {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("round %d: %d promo products, want 1", round, n)
		}
	}
}
