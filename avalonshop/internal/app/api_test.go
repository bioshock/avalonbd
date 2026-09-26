package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"avalonshop/internal/store"
)

const testToken = "0123456789abcdef0123456789abcdef"

func apiApp(t *testing.T) (*App, *store.Store) {
	t.Helper()
	a, st := newDBApp(t)
	a.cfg.AdminAPIToken = testToken
	return a, st
}

// apiDo sends body as JSON (a string is sent verbatim) with the test token.
func apiDo(t *testing.T, a *App, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		j, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(j)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not JSON (%d): %v\n%s", w.Code, err, w.Body.String())
	}
	return v
}

func onionJSON(catID int64) map[string]any {
	return map[string]any{
		"name": "Avalon Onion Powder", "tagline": "Fine-ground onion.", "description": "Line one\nLine two",
		"meta_description": "Onion powder", "category_id": catID, "active": true, "featured": true, "promo": false,
		"variants": []map[string]any{{"name": "100g", "sku": "AV-ONION-100", "price": 130, "regular_price": nil, "stock": 100}},
	}
}

func TestAPIDisabledWithoutToken(t *testing.T) {
	a := newApp(t, nil) // AdminAPIToken empty
	for _, p := range []string{"/api/admin/products", "/api/admin/categories"} {
		if w := apiDo(t, a, "GET", p, nil); w.Code != 404 {
			t.Fatalf("%s with API disabled: got %d, want 404", p, w.Code)
		}
	}
}

func TestAPIAuth(t *testing.T) {
	a := newApp(t, nil)
	a.cfg.AdminAPIToken = testToken
	for _, h := range []string{"", "Bearer wrong-token-wrong-token-wrong-token", "Basic " + testToken, testToken, "Bearer " + testToken + "x"} {
		r := httptest.NewRequest("GET", "/api/admin/products", nil)
		if h != "" {
			r.Header.Set("Authorization", h)
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 401 || !strings.Contains(w.Body.String(), `"error"`) {
			t.Fatalf("Authorization %q: got %d %s, want 401 JSON", h, w.Code, w.Body.String())
		}
	}
}

func TestAPICategories(t *testing.T) {
	a, _ := apiApp(t)
	if w := apiDo(t, a, "GET", "/api/admin/categories", nil); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty list must be [] not null: %d %s", w.Code, w.Body.String())
	}
	w := apiDo(t, a, "POST", "/api/admin/categories", map[string]any{"name": "Spices & Powders"})
	got := decodeBody[map[string]any](t, w)
	if w.Code != 201 || got["slug"] != "spices-powders" || got["id"].(float64) <= 0 {
		t.Fatalf("create: %d %v", w.Code, got)
	}
	if w := apiDo(t, a, "POST", "/api/admin/categories", map[string]any{"slug": "spices-powders", "name": "Again"}); w.Code != 409 {
		t.Fatalf("duplicate slug: %d", w.Code)
	}
	if w := apiDo(t, a, "POST", "/api/admin/categories", map[string]any{"name": " "}); w.Code != 400 {
		t.Fatalf("blank name: %d", w.Code)
	}
	list := decodeBody[[]apiCategory](t, apiDo(t, a, "GET", "/api/admin/categories", nil))
	if len(list) != 1 || list[0].Name != "Spices & Powders" {
		t.Fatalf("list: %+v", list)
	}
}

func TestAPIProductLifecycle(t *testing.T) {
	a, st := apiApp(t)
	ctx := context.Background()
	cat, _ := st.CreateCategory(ctx, store.Category{Slug: "spices", Name: "Spices"})

	w := apiDo(t, a, "POST", "/api/admin/products", onionJSON(cat))
	created := decodeBody[map[string]any](t, w)
	if w.Code != 201 || created["slug"] != "avalon-onion-powder" {
		t.Fatalf("create: %d %v", w.Code, created)
	}
	id := int64(created["id"].(float64))

	p := decodeBody[apiProduct](t, apiDo(t, a, "GET", "/api/admin/products/"+itoa(id), nil))
	if p.Name != "Avalon Onion Powder" || p.Tagline != "Fine-ground onion." || !p.Featured || *p.CategoryID != cat || len(p.Variants) != 1 || p.Variants[0].Price != 130 || p.Variants[0].RegularPrice != nil {
		t.Fatalf("get: %+v", p)
	}

	// PUT: reprice the existing variant, add a regular price, add a second variant.
	reg := 150
	p.Variants[0].Price, p.Variants[0].RegularPrice = 120, &reg
	p.Variants = append(p.Variants, apiVariant{Name: "250g", Price: 280, Stock: 10})
	if w := apiDo(t, a, "PUT", "/api/admin/products/"+itoa(id), p); w.Code != 200 {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	full, _ := st.GetProduct(ctx, id)
	if len(full.Variants) != 2 || full.Variants[0].Price != 120 || *full.Variants[0].RegularPrice != 150 || full.Variants[1].Name != "250g" {
		t.Fatalf("after put: %+v", full.Variants)
	}

	// PUT without the second variant removes it.
	p.Variants = p.Variants[:1]
	apiDo(t, a, "PUT", "/api/admin/products/"+itoa(id), p)
	if full, _ = st.GetProduct(ctx, id); len(full.Variants) != 1 {
		t.Fatalf("variant not removed: %+v", full.Variants)
	}

	list := decodeBody[[]apiProduct](t, apiDo(t, a, "GET", "/api/admin/products", nil))
	if len(list) != 1 || list[0].ID != id || len(list[0].Variants) != 1 {
		t.Fatalf("list: %+v", list)
	}

	if w := apiDo(t, a, "DELETE", "/api/admin/products/"+itoa(id), nil); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	for _, m := range []string{"GET", "PUT", "DELETE"} {
		if w := apiDo(t, a, m, "/api/admin/products/"+itoa(id), p); w.Code != 404 {
			t.Fatalf("%s deleted product: %d", m, w.Code)
		}
	}
}

func TestAPIProductValidation(t *testing.T) {
	a, st := apiApp(t)
	ctx := context.Background()

	bad := onionJSON(0)
	delete(bad, "category_id")
	bad["variants"] = []map[string]any{{"name": "100g", "price": -1, "stock": 1}}
	w := apiDo(t, a, "POST", "/api/admin/products", bad)
	e := decodeBody[map[string]any](t, w)
	if w.Code != 400 || e["fields"].(map[string]any)["variants[0].price"] != "must be 0 or more" {
		t.Fatalf("validation: %d %v", w.Code, e)
	}
	// Review Focus 3: a misspelled field is an error, not silently dropped.
	typo := onionJSON(0)
	delete(typo, "category_id")
	typo["variants"] = []map[string]any{{"name": "100g", "price": 1, "regularPrice": 5, "stock": 1}}
	if w := apiDo(t, a, "POST", "/api/admin/products", typo); w.Code != 400 || !strings.Contains(w.Body.String(), "regularPrice") {
		t.Fatalf("unknown field: %d %s", w.Code, w.Body.String())
	}
	if w := apiDo(t, a, "POST", "/api/admin/products", `{"name":`); w.Code != 400 {
		t.Fatalf("broken JSON: %d", w.Code)
	}
	unknownCat := onionJSON(999999)
	if w := apiDo(t, a, "POST", "/api/admin/products", unknownCat); w.Code != 400 || !strings.Contains(w.Body.String(), "category_id") {
		t.Fatalf("unknown category: %d %s", w.Code, w.Body.String())
	}

	ok := onionJSON(0)
	delete(ok, "category_id")
	if w := apiDo(t, a, "POST", "/api/admin/products", ok); w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if w := apiDo(t, a, "POST", "/api/admin/products", ok); w.Code != 409 {
		t.Fatalf("duplicate slug: %d", w.Code)
	}

	// Review Focus 2: PUT with another product's variant id must not touch either product.
	otherID, _ := st.CreateProduct(ctx, store.Product{Slug: "other", Name: "Other", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})
	other, _ := st.GetProduct(ctx, otherID)
	target, _ := st.GetProductBySlug(ctx, "avalon-onion-powder", false)
	in := toAPIProduct(target)
	in.Variants[0].ID = other.Variants[0].ID
	w = apiDo(t, a, "PUT", "/api/admin/products/"+itoa(target.ID), in)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "variants[0].id") {
		t.Fatalf("foreign variant id: %d %s", w.Code, w.Body.String())
	}
	if got, _ := st.GetProduct(ctx, target.ID); len(got.Variants) != 1 || got.Variants[0].ID != target.Variants[0].ID {
		t.Fatalf("target variants changed: %+v", got.Variants)
	}
}

// TestAPIProductUpdatePreservesStockDecrement is the C2 fix for the API: a
// client that does the documented GET -> edit price -> PUT round-trip must
// not clobber a stock decrement (e.g. an order) that landed in between.
func TestAPIProductUpdatePreservesStockDecrement(t *testing.T) {
	a, st := apiApp(t)
	ctx := context.Background()

	body := onionJSON(0)
	delete(body, "category_id")
	created := decodeBody[map[string]any](t, apiDo(t, a, "POST", "/api/admin/products", body))
	id := int64(created["id"].(float64))
	p := decodeBody[apiProduct](t, apiDo(t, a, "GET", "/api/admin/products/"+itoa(id), nil))
	if p.Variants[0].Stock != 100 {
		t.Fatalf("expected initial stock 100, got %+v", p.Variants[0])
	}

	// An order lands in between: decrements stock the same way checkout does.
	zone, _ := st.CreateZone(ctx, store.Zone{Name: "Z", Fee: 0, Active: true})
	if _, err := st.PlaceOrder(ctx, store.NewOrder{Name: "A", Phone: "01712345678", Email: "a@b.co", Address: "somewhere far", ZoneID: zone, Lines: []store.OrderLine{{VariantID: p.Variants[0].ID, Qty: 7}}}); err != nil {
		t.Fatal(err)
	}

	// The client only edits price and PUTs back the stale (pre-decrement) body.
	p.Variants[0].Price = 140
	if w := apiDo(t, a, "PUT", "/api/admin/products/"+itoa(id), p); w.Code != 200 {
		t.Fatalf("put: %d %s", w.Code, w.Body.String())
	}
	full, _ := st.GetProduct(ctx, id)
	if full.Variants[0].Stock != 93 {
		t.Fatalf("stock decrement clobbered by stale PUT: got %d, want 93", full.Variants[0].Stock)
	}
	if full.Variants[0].Price != 140 {
		t.Fatalf("price change was lost: got %d, want 140", full.Variants[0].Price)
	}
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.NRGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAPIImages(t *testing.T) {
	a, st := apiApp(t)
	ctx := context.Background()
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("alt", "Jar of onion powder")
	fw, _ := mw.CreateFormFile("images", "jar.png")
	fw.Write(testPNG(t, 1200, 900))
	mw.Close()
	r := httptest.NewRequest("POST", "/api/admin/products/"+itoa(id)+"/images", &buf)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	imgs := decodeBody[[]apiImage](t, w)
	if w.Code != 201 || len(imgs) != 1 || imgs[0].ID == 0 || imgs[0].Width != 1200 || imgs[0].Alt != "Jar of onion powder" {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	p, _ := st.GetProduct(ctx, id)
	if len(p.Images) != 1 || p.Images[0].File != imgs[0].File {
		t.Fatalf("image not on product: %+v", p.Images)
	}

	// Not multipart → 400; unknown product → 404.
	if w := apiDo(t, a, "POST", "/api/admin/products/"+itoa(id)+"/images", `{}`); w.Code != 400 {
		t.Fatalf("json body to upload: %d", w.Code)
	}
	if w := apiDo(t, a, "POST", "/api/admin/products/999999/images", `{}`); w.Code != 404 {
		t.Fatalf("unknown product: %d", w.Code)
	}

	if w := apiDo(t, a, "DELETE", "/api/admin/images/"+itoa(imgs[0].ID), nil); w.Code != 204 {
		t.Fatalf("delete image: %d", w.Code)
	}
	if p, _ = st.GetProduct(ctx, id); len(p.Images) != 0 {
		t.Fatal("image row still there")
	}
	if w := apiDo(t, a, "DELETE", "/api/admin/images/"+itoa(imgs[0].ID), nil); w.Code != 404 {
		t.Fatalf("delete twice: %d", w.Code)
	}
}

func TestAPIDeleteOrderedProductConflicts(t *testing.T) {
	a, st := apiApp(t)
	ctx := context.Background()
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 5}})
	p, _ := st.GetProduct(ctx, id)
	zone, _ := st.CreateZone(ctx, store.Zone{Name: "Z", Fee: 0, Active: true})
	if _, err := st.PlaceOrder(ctx, store.NewOrder{Name: "A", Phone: "01712345678", Email: "a@b.co", Address: "somewhere far", ZoneID: zone, Lines: []store.OrderLine{{VariantID: p.Variants[0].ID, Qty: 1}}}); err != nil {
		t.Fatal(err)
	}
	if w := apiDo(t, a, "DELETE", "/api/admin/products/"+itoa(id), nil); w.Code != 409 || !strings.Contains(w.Body.String(), "active") {
		t.Fatalf("ordered product delete: %d %s", w.Code, w.Body.String())
	}
}
