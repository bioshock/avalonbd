package app

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"avalonshop/internal/store"
)

func adminSession(t *testing.T, a *App, st *store.Store) string {
	t.Helper()
	ctx := context.Background()
	if err := st.SeedAdmin(ctx, "admin@example.com", "admin-pass-123"); err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return sessionCookie(t, a, u.ID)
}

func TestAdminAccess(t *testing.T) {
	a, st := newDBApp(t)
	if w := do(t, a, "GET", "/admin", nil); w.Code != 404 {
		t.Fatalf("logged out should 404, got %d", w.Code)
	}
	cust, _ := st.CreateUser(context.Background(), "c@example.com", "x", "C", "customer")
	if w := do(t, a, "GET", "/admin", nil, "Cookie", sessionCookie(t, a, cust.ID)); w.Code != 404 {
		t.Fatalf("customer should 404, got %d", w.Code)
	}
	adm := adminSession(t, a, st)
	w := do(t, a, "GET", "/admin", nil, "Cookie", adm)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Dashboard") || !strings.Contains(w.Body.String(), `content="noindex"`) {
		t.Fatalf("admin dashboard: %d", w.Code)
	}
}

func TestAdminCategories(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	w := do(t, a, "POST", "/admin/categories", strings.NewReader(url.Values{"name": {"Wild Honey"}, "sort": {"2"}}.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("create: %d", w.Code)
	}
	c, err := st.GetCategoryBySlug(ctx, "wild-honey")
	if err != nil || c.Sort != 2 {
		t.Fatalf("category not created: %v", err)
	}
	do(t, a, "POST", "/admin/categories", strings.NewReader(url.Values{"name": {"Wild Honey"}}.Encode()), "Cookie", adm)
	if _, err := st.GetCategoryBySlug(ctx, "wild-honey-2"); err != nil {
		t.Fatal("duplicate name should get -2 slug")
	}
	w = do(t, a, "POST", "/admin/categories/"+itoa(c.ID), strings.NewReader(url.Values{"name": {"Honey"}, "slug": {"Honey!"}, "sort": {"1"}}.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("update: %d", w.Code)
	}
	c2, _ := st.GetCategory(ctx, c.ID)
	if c2.Name != "Honey" || c2.Slug != "honey" || c2.Sort != 1 {
		t.Fatalf("update lost: %+v", c2)
	}
	w = do(t, a, "GET", "/admin/categories", nil, "Cookie", adm)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `value="Honey"`) {
		t.Fatalf("list: %d", w.Code)
	}
	st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", CategoryID: &c.ID, Active: true}, nil)
	w = do(t, a, "POST", "/admin/categories/"+itoa(c.ID)+"/delete", nil, "Cookie", adm)
	if w.Code != 303 || !strings.Contains(cookieHeader(w, "flash"), "products") {
		t.Fatalf("in-use delete should flash: %d %q", w.Code, cookieHeader(w, "flash"))
	}
	c3, _ := st.GetCategoryBySlug(ctx, "wild-honey-2")
	do(t, a, "POST", "/admin/categories/"+itoa(c3.ID)+"/delete", nil, "Cookie", adm)
	if _, err := st.GetCategory(ctx, c3.ID); err == nil {
		t.Fatal("category not deleted")
	}
}

func TestAdminZones(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	w := do(t, a, "POST", "/admin/zones", strings.NewReader(url.Values{"name": {"Rajshahi"}, "fee": {"60"}, "active": {"on"}, "sort": {"1"}}.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("create: %d", w.Code)
	}
	zones, _ := st.ListZones(ctx, true)
	if len(zones) != 1 || zones[0].Fee != 60 {
		t.Fatalf("zone: %+v", zones)
	}
	z := zones[0]
	w = do(t, a, "POST", "/admin/zones/"+itoa(z.ID), strings.NewReader(url.Values{"name": {"Rajshahi City"}, "fee": {"70"}, "sort": {"1"}}.Encode()), "Cookie", adm)
	z2, _ := st.GetZone(ctx, z.ID)
	if w.Code != 303 || z2.Fee != 70 || z2.Active {
		t.Fatalf("update (unchecked active must become false): %d %+v", w.Code, z2)
	}
	w = do(t, a, "POST", "/admin/zones", strings.NewReader(url.Values{"name": {"X"}, "fee": {"-5"}}.Encode()), "Cookie", adm)
	if w.Code != 303 || !strings.Contains(cookieHeader(w, "flash"), "fee") {
		t.Fatalf("negative fee should be rejected with a flash: %d", w.Code)
	}
	do(t, a, "POST", "/admin/zones/"+itoa(z.ID)+"/delete", nil, "Cookie", adm)
	if _, err := st.GetZone(ctx, z.ID); err == nil {
		t.Fatal("zone not deleted")
	}
}
