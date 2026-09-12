package store_test

import (
	"context"
	"errors"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

func TestCategoriesCRUD(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	id, err := st.CreateCategory(ctx, store.Category{Slug: "honey", Name: "Honey", Sort: 2})
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	if _, err := st.CreateCategory(ctx, store.Category{Slug: "honey", Name: "Dup"}); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
	st.CreateCategory(ctx, store.Category{Slug: "mango", Name: "Mango", Sort: 1})
	list, _ := st.ListCategories(ctx)
	if len(list) != 2 || list[0].Slug != "mango" {
		t.Fatalf("order by sort wrong: %+v", list)
	}
	c, err := st.GetCategoryBySlug(ctx, "honey")
	if err != nil || c.ID != id {
		t.Fatal(err)
	}
	c.Name = "Raw Honey"
	if err := st.UpdateCategory(ctx, c); err != nil {
		t.Fatal(err)
	}
	c2, _ := st.GetCategory(ctx, id)
	if c2.Name != "Raw Honey" {
		t.Fatalf("update lost: %+v", c2)
	}
	if _, err := st.GetCategoryBySlug(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	slug, _ := st.UniqueSlug(ctx, "categories", "honey", 0)
	if slug != "honey-2" {
		t.Fatalf("UniqueSlug = %q", slug)
	}
	slug, _ = st.UniqueSlug(ctx, "categories", "honey", id)
	if slug != "honey" {
		t.Fatalf("UniqueSlug excluding self = %q", slug)
	}
	if err := st.DeleteCategory(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteCategory(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestZonesCRUD(t *testing.T) {
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	a, _ := st.CreateZone(ctx, store.Zone{Name: "Rajshahi", Fee: 60, Active: true, Sort: 1})
	b, _ := st.CreateZone(ctx, store.Zone{Name: "Rest of BD", Fee: 120, Active: false, Sort: 2})
	all, _ := st.ListZones(ctx, false)
	active, _ := st.ListZones(ctx, true)
	if len(all) != 2 || len(active) != 1 || active[0].ID != a {
		t.Fatalf("all=%d active=%d", len(all), len(active))
	}
	z, _ := st.GetZone(ctx, b)
	z.Active = true
	z.Fee = 130
	if err := st.UpdateZone(ctx, z); err != nil {
		t.Fatal(err)
	}
	z2, _ := st.GetZone(ctx, b)
	if !z2.Active || z2.Fee != 130 {
		t.Fatalf("update lost: %+v", z2)
	}
	if err := st.DeleteZone(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetZone(ctx, a); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
