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

// adminRoutes is every route requireAdmin must wrap. Kept table-driven, not a
// single spot-check, so that an unwrapped route (this task's own mistake, or
// one of Tasks 17/18's nine more hand-typed adm(...) calls) fails a test
// instead of shipping silently: a single-route check like the one above
// stayed green when a reviewer removed adm(...) from the category delete
// route and left it reachable by a logged-out curl.
//
// Path placeholders ({cid}, {zid}, {pid}, {iid}) are substituted with real
// row ids by TestAdminRoutesRequireAdmin before probing, rather than a
// literal "1". Several id-taking handlers correctly return a genuine 404 for
// store.ErrNotFound (task-17-decisions #3), which makes a nonexistent id
// indistinguishable from "not an admin": with a literal "1" and no such row,
// removing adm(...) from POST /admin/images/{id}/delete still 404'd (via the
// handler's own DeleteImage -> ErrNotFound path) and this test stayed green.
// Pointing every placeholder at a row that actually exists means a 404 here
// can only come from authorization.
var adminRoutes = [][2]string{
	{"GET", "/admin"},
	{"GET", "/admin/categories"}, {"POST", "/admin/categories"},
	{"POST", "/admin/categories/{cid}"}, {"POST", "/admin/categories/{cid}/delete"},
	{"GET", "/admin/zones"}, {"POST", "/admin/zones"},
	{"POST", "/admin/zones/{zid}"}, {"POST", "/admin/zones/{zid}/delete"},
	{"GET", "/admin/products"},
	{"GET", "/admin/products/new"}, {"POST", "/admin/products/new"},
	{"GET", "/admin/products/variant-row"},
	{"GET", "/admin/products/{pid}"}, {"POST", "/admin/products/{pid}"},
	{"POST", "/admin/products/{pid}/delete"}, {"POST", "/admin/products/{pid}/images"},
	{"POST", "/admin/images/{iid}"}, {"POST", "/admin/images/{iid}/move"}, {"POST", "/admin/images/{iid}/delete"},
	{"GET", "/admin/orders"},
	{"GET", "/admin/orders/{oid}"},
	{"POST", "/admin/orders/{oid}/status"}, {"POST", "/admin/orders/{oid}/note"},
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	a, st := newDBApp(t)
	ctx := context.Background()
	cust, _ := st.CreateUser(ctx, "cust@example.com", "x", "C", "customer")
	custCookie := sessionCookie(t, a, cust.ID)

	// Seed one real row behind every placeholder above. None of these probes
	// should ever reach a handler body when adm(...) is present (every one
	// 404s at the wrapper), so this data is never mutated by a passing run.
	cid, err := st.CreateCategory(ctx, store.Category{Name: "Cat", Slug: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	zid, err := st.CreateZone(ctx, store.Zone{Name: "Zone", Fee: 0, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	pid, err := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})
	if err != nil {
		t.Fatal(err)
	}
	iid, err := st.AddImage(ctx, store.Image{ProductID: pid, File: "seed", Width: 10, Height: 10})
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.GetProduct(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	o, err := st.PlaceOrder(ctx, store.NewOrder{Name: "Ana", Phone: "01712345678", Email: "ana@example.com", Address: "Road 1, Rajshahi", ZoneID: zid, Lines: []store.OrderLine{{VariantID: p.Variants[0].ID, Qty: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	replace := strings.NewReplacer(
		"{cid}", itoa(cid),
		"{zid}", itoa(zid),
		"{pid}", itoa(pid),
		"{iid}", itoa(iid),
		"{oid}", itoa(o.ID),
	)

	for _, rt := range adminRoutes {
		method, path := rt[0], replace.Replace(rt[1])
		body := url.Values{"name": {"pwn"}, "fee": {"0"}}.Encode()
		if w := do(t, a, method, path, strings.NewReader(body)); w.Code != 404 {
			t.Errorf("logged out %s %s -> %d, want 404", method, path, w.Code)
		}
		if w := do(t, a, method, path, strings.NewReader(body), "Cookie", custCookie); w.Code != 404 {
			t.Errorf("customer %s %s -> %d, want 404", method, path, w.Code)
		}
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
	flash, _ := url.QueryUnescape(cookieHeader(w, "flash"))
	// Assert on our own validation message, not just a substring ("fee") that
	// a raw Postgres constraint-violation error would also happen to contain
	// if the `fee < 0` check were ever deleted.
	if w.Code != 303 || !strings.Contains(flash, "0 or more") {
		t.Fatalf("negative fee should be rejected with a flash: %d %q", w.Code, flash)
	}
	// A blank fee must not silently save as ৳0: it's a field error, and the
	// existing fee is left untouched.
	w = do(t, a, "POST", "/admin/zones/"+itoa(z.ID), strings.NewReader(url.Values{"name": {"Rajshahi City"}, "fee": {""}, "sort": {"1"}}.Encode()), "Cookie", adm)
	flash, _ = url.QueryUnescape(cookieHeader(w, "flash"))
	if w.Code != 303 || !strings.Contains(flash, "required") {
		t.Fatalf("blank fee should be rejected with a flash: %d %q", w.Code, flash)
	}
	z3, _ := st.GetZone(ctx, z.ID)
	if z3.Fee != 70 {
		t.Fatalf("blank fee must not change the existing fee: %+v", z3)
	}
	do(t, a, "POST", "/admin/zones/"+itoa(z.ID)+"/delete", nil, "Cookie", adm)
	if _, err := st.GetZone(ctx, z.ID); err == nil {
		t.Fatal("zone not deleted")
	}
}
