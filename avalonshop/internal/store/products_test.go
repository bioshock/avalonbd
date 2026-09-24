package store_test

import (
	"context"
	"errors"
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
