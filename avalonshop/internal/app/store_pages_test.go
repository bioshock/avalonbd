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
	if !strings.Contains(body, `<meta property="og:image" content="http://localhost:8080/static/img/og.jpg">`) {
		t.Fatal("home og:image should be the brand share card")
	}
	if !strings.Contains(body, `<link rel="preload" as="image" href="/static/img/hero-960.webp"`) || !strings.Contains(body, `fetchpriority="high"`) {
		t.Fatal("hero image must be preloaded and high priority")
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

func TestHomeSpicesPromoAndWhatsApp(t *testing.T) {
	a, st := newDBApp(t)
	ctx := context.Background()
	w := do(t, a, "GET", "/", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `class="promo"`) || strings.Contains(w.Body.String(), "Our Essential Spices") {
		t.Fatalf("empty shop: promo and spices sections must be absent (%d)", w.Code)
	}

	reg := 150
	onion, _ := st.CreateProduct(ctx, store.Product{Slug: "onion", Name: "Avalon Onion Powder", Tagline: "Fine-ground onion.", Active: true, Featured: true},
		[]store.Variant{{Name: "100g", Price: 130, RegularPrice: &reg, Stock: 5}})
	same := 180
	garlic, _ := st.CreateProduct(ctx, store.Product{Slug: "garlic", Name: "Avalon Garlic Powder", Active: true, Featured: true},
		[]store.Variant{{Name: "100g", Price: 180, RegularPrice: &same, Stock: 0}}) // sold out; regular == price
	trioReg := 550
	trio, _ := st.CreateProduct(ctx, store.Product{Slug: "trio", Name: "Essential Spice Trio", Tagline: "Three in one.", Description: "Onion Powder — 100g\nGarlic Powder — 100g\n", Active: true, Promo: true},
		[]store.Variant{{Name: "3 × 100g", Price: 499, RegularPrice: &trioReg, Stock: 3}})
	on, _ := st.GetProduct(ctx, onion)
	ga, _ := st.GetProduct(ctx, garlic)
	tr, _ := st.GetProduct(ctx, trio)

	body := do(t, a, "GET", "/", nil).Body.String()
	for _, want := range []string{
		"Our Essential Spices", "Fine-ground onion.", "100g",
		`<s class="was">৳ 150</s>`, "Save ৳ 20", // onion card
		`name="variant_id" value="` + itoa(on.Variants[0].ID) + `"`, // quick add for in-stock single variant
		`class="promo"`, "Essential Spice Trio", "Three in one.",
		"Garlic Powder — 100g", // checklist from description lines
		"<s>৳ 550</s>", "৳ 499", "Save ৳ 51",
		`name="variant_id" value="` + itoa(tr.Variants[0].ID) + `"`,
		`href="https://wa.me/8801933309009?text=Hi%20Avalon%20Foods%2C%20I%27d%20like%20to%20order%3A%20Essential%20Spice%20Trio"`,
		"Why Avalon Foods?", `href="/journey"`, "Explore All Products",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home missing %q", want)
		}
	}
	// Review Focus 4: sold-out single-variant card shows Sold out and no quick
	// add. Asserted on the "variant_id" input specifically (not a bare
	// `value="<id>"`), which would collide with every quick-add form's own
	// `name="qty" value="1"` whenever the sold-out variant's id happens to be 1.
	if strings.Contains(body, `name="variant_id" value="`+itoa(ga.Variants[0].ID)+`"`) || !strings.Contains(body, "Sold out") {
		t.Error("sold-out garlic must not have a quick add form")
	}
	// regular == price: nothing crossed out for garlic.
	if strings.Contains(body, "৳ 180</s>") {
		t.Error("regular price equal to price must not be struck through")
	}
	if strings.Count(body, "<h1") != 1 {
		t.Error("exactly one h1")
	}

	m := jsonldRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no JSON-LD on home")
	}
	var ld map[string]any
	if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
		t.Fatalf("home JSON-LD invalid: %v\n%s", err, m[1])
	}
	addr, _ := ld["address"].(map[string]any)
	same2, _ := ld["sameAs"].([]any)
	if ld["@type"] != "LocalBusiness" || ld["telephone"] != "+8801933309009" || addr["addressLocality"] != "Rajshahi" || addr["postalCode"] != "6100" || len(same2) != 1 || same2[0] != "https://www.facebook.com/share/1Lzv9YkGFs/" {
		t.Fatalf("LocalBusiness wrong: %v", ld)
	}
	if !strings.Contains(body, `<meta property="og:type" content="website">`) {
		t.Error("home og:type must stay website")
	}

	// Review Focus 5: an inactive promo product removes the banner.
	tr.Active = false
	if err := st.UpdateProduct(ctx, tr.Product, tr.Variants); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(do(t, a, "GET", "/", nil).Body.String(), `class="promo"`) {
		t.Error("inactive promo must not render")
	}
}

func TestStruckPriceAndProductWhatsApp(t *testing.T) {
	a, st := newDBApp(t)
	ctx := context.Background()
	reg := 550
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "trio", Name: "Spice Trio & Co", Active: true},
		[]store.Variant{{Name: "Set", Price: 499, RegularPrice: &reg, Stock: 3, Sort: 0}, {Name: "Big set", Price: 900, Stock: 3, Sort: 1}}) // Set is cheapest, so the listing card shows its struck price
	p, _ := st.GetProduct(ctx, id)

	body := do(t, a, "GET", "/products/trio", nil).Body.String()
	for _, want := range []string{
		`<s class="was" id="was">৳ 550</s>`,
		`<span class="save" id="save">Save ৳ 51</span>`,
		`data-was-text="৳ 550" data-save-text="Save ৳ 51"`, // first variant
		`value="` + itoa(p.Variants[1].ID) + `" data-price-text="৳ 900" data-was-text="" data-save-text=""`,
		`class="btn outline wa"`,
		`href="https://wa.me/8801933309009?text=Hi%20Avalon%20Foods%2C%20I%27d%20like%20to%20order%3A%20Spice%20Trio%20%26%20Co"`,
		`<meta property="og:type" content="product">`,
		`"brand":{"@type":"Brand","name":"Avalon Foods"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("product page missing %q", want)
		}
	}
	list := do(t, a, "GET", "/products", nil).Body.String()
	if !strings.Contains(list, `<s class="was">৳ 550</s>`) {
		t.Error("listing card missing struck regular price")
	}
}
