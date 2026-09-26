package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"avalonshop/internal/store"
)

// seedCatalog creates one category, one featured product (2 variants, one sold out, one image)
// and one inactive product. Returns the featured product's slug.
func seedCatalog(t *testing.T, st *store.Store) string {
	t.Helper()
	ctx := context.Background()
	cat, err := st.CreateCategory(ctx, store.Category{Slug: "honey", Name: "Honey"})
	if err != nil {
		t.Fatal(err)
	}
	sku := "HNY-500"
	id, err := st.CreateProduct(ctx, store.Product{Slug: "wild-forest-honey", Name: "Wild Forest Honey", Description: "Raw honey <b>from</b> the Sundarbans.\nDark and smoky.", CategoryID: &cat, Active: true, Featured: true},
		[]store.Variant{{Name: "500g", SKU: &sku, Price: 650, Stock: 5}, {Name: "1kg", Price: 1200, Stock: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddImage(ctx, store.Image{ProductID: id, File: "abcdef0123456789", Alt: "Jar of honey", Width: 1600, Height: 1200}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProduct(ctx, store.Product{Slug: "hidden", Name: "Hidden", Active: false}, []store.Variant{{Name: "x", Price: 1, Stock: 1}}); err != nil {
		t.Fatal(err)
	}
	return "wild-forest-honey"
}

var jsonldRe = regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)

func TestHomeAndListing(t *testing.T) {
	a, st := newDBApp(t)
	seedCatalog(t, st)
	w := do(t, a, "GET", "/", nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Wild Forest Honey") || strings.Count(body, "<h1") != 1 {
		t.Fatalf("home: %d\n%s", w.Code, body)
	}
	if !strings.Contains(body, `<link rel="canonical" href="http://localhost:8080/">`) || !strings.Contains(body, `<meta name="description"`) {
		t.Fatal("home missing canonical or description")
	}
	if !strings.Contains(body, `<meta property="og:image" content="http://localhost:8080/media/`) {
		t.Fatal("home missing og:image")
	}
	w = do(t, a, "GET", "/products?category=honey", nil)
	body = w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `class="chip on" href="/products?category=honey"`) || !strings.Contains(body, `href="http://localhost:8080/products?category=honey"`) {
		t.Fatalf("category listing: %d\n%s", w.Code, body)
	}
	if !strings.Contains(body, `<meta property="og:image" content="http://localhost:8080/media/`) {
		t.Fatal("category listing missing og:image")
	}
	if strings.Contains(body, "Hidden") {
		t.Fatal("inactive product listed")
	}
	if w := do(t, a, "GET", "/products?category=nope", nil); w.Code != 404 {
		t.Fatalf("unknown category should 404, got %d", w.Code)
	}
	w = do(t, a, "GET", "/products?q=forest", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `content="noindex"`) || !strings.Contains(w.Body.String(), "Wild Forest Honey") {
		t.Fatalf("search: %d", w.Code)
	}
}

func TestProductPageSEOAndSpeed(t *testing.T) {
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	w := do(t, a, "GET", "/products/"+slug, nil)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	for _, want := range []string{
		`<title>Wild Forest Honey · Avalon Foods</title>`,
		`<link rel="canonical" href="http://localhost:8080/products/wild-forest-honey">`,
		`<meta property="og:image" content="http://localhost:8080/media/abcdef0123456789-1600.webp">`,
		`<meta property="og:type" content="product">`,
		`fetchpriority="high"`,
		`srcset="/media/abcdef0123456789-400.webp 400w, /media/abcdef0123456789-900.webp 900w, /media/abcdef0123456789-1600.webp 1600w"`,
		`width="1600" height="1200"`,
		`sizes="(min-width: 900px) 50vw, 100vw"`,
		`value="`, // variant radios
		`disabled><span>1kg</span>`,
		`SKU HNY-500`,
		"Raw honey &lt;b&gt;from&lt;/b&gt;", // description escaped
	} {
		if !strings.Contains(body, want) {
			t.Errorf("product page missing %q", want)
		}
	}
	if strings.Count(body, "<h1") != 1 {
		t.Error("exactly one h1 expected")
	}
	m := jsonldRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no JSON-LD block")
	}
	var ld map[string]any
	if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
		t.Fatalf("JSON-LD not valid JSON: %v\n%s", err, m[1])
	}
	offers := ld["offers"].([]any)
	first := offers[0].(map[string]any)
	if ld["@type"] != "Product" || len(offers) != 2 || first["price"].(float64) != 650 || first["priceCurrency"] != "BDT" || first["sku"] != "HNY-500" {
		t.Fatalf("JSON-LD wrong: %v", ld)
	}
	if offers[1].(map[string]any)["availability"] != "https://schema.org/OutOfStock" {
		t.Fatal("sold-out variant should be OutOfStock")
	}
	if w := do(t, a, "GET", "/products/hidden", nil); w.Code != 404 {
		t.Fatalf("inactive product should 404, got %d", w.Code)
	}
	if w := do(t, a, "GET", "/products/nope", nil); w.Code != 404 {
		t.Fatalf("unknown slug should 404, got %d", w.Code)
	}
}

func TestTruncateAndSelectedVariant(t *testing.T) {
	if got := truncate("one two three four", 10); got != "one two…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate short = %q", got)
	}
	vs := []store.Variant{{ID: 1, Stock: 0}, {ID: 2, Stock: 3}}
	if selectedVariant(vs).ID != 2 {
		t.Fatal("should pick first in-stock variant")
	}
	if selectedVariant(vs[:1]).ID != 1 {
		t.Fatal("should fall back to first variant")
	}
}
