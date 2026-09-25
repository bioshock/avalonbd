package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"avalonshop/internal/store"
)

var (
	imgTagRe    = regexp.MustCompile(`<img\b[^>]*>`)
	externalRe  = regexp.MustCompile(`(?:src|href)="(https?://[^"]+)"`)
	sitemapLocs = regexp.MustCompile(`<loc>([^<]+)</loc>`)
)

func gzipSize(b []byte) int {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Len()
}

// TestPerformanceBudget enforces the spec's page budget on every public page:
// no third-party requests, complete image attributes, one h1, and size caps.
//
// The image guard is deliberately stricter than the brief's original draft
// (task-19-decisions #1): every /media/ image must carry both srcset and
// sizes. The brief's width="64"/width="48" exemptions existed only because
// the cart, checkout, and gallery thumbnails had no srcset at the time it was
// written; Tasks 11 and 12 gave them one, so the exemption is dropped rather
// than carried forward as dead slack.
func TestPerformanceBudget(t *testing.T) {
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	st.CreateZone(context.Background(), store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	cart := cookieHeader(do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=1")), "cart")

	pages := []string{"/", "/products", "/products?category=honey", "/products/" + slug, "/cart", "/checkout", "/login", "/register"}
	for _, p := range pages {
		w := do(t, a, "GET", p, nil, "Cookie", "cart="+cart)
		if w.Code != 200 {
			t.Fatalf("%s: status %d", p, w.Code)
		}
		body := w.Body.Bytes()
		html := string(body)
		if n := gzipSize(body); n > 30*1024 {
			t.Errorf("%s: %d bytes gzipped, budget is 30 KB", p, n)
		}
		if strings.Count(html, "<h1") != 1 {
			t.Errorf("%s: expected exactly one <h1>", p)
		}
		for _, m := range externalRe.FindAllStringSubmatch(html, -1) {
			if !strings.HasPrefix(m[1], a.cfg.BaseURL) && !strings.HasPrefix(m[1], "https://schema.org") {
				t.Errorf("%s: external resource %s", p, m[1])
			}
		}
		for _, tag := range imgTagRe.FindAllString(html, -1) {
			for _, attr := range []string{`width="`, `height="`, `alt="`} {
				if !strings.Contains(tag, attr) {
					t.Errorf("%s: <img> missing %s: %s", p, attr, tag)
				}
			}
			if strings.Contains(tag, "/media/") && (!strings.Contains(tag, "srcset=") || !strings.Contains(tag, "sizes=")) {
				t.Errorf("%s: media image missing srcset and/or sizes: %s", p, tag)
			}
			if !strings.Contains(tag, `fetchpriority="high"`) && !strings.Contains(tag, `loading="lazy"`) && p == "/products/"+slug {
				t.Errorf("%s: non-hero image must be lazy: %s", p, tag)
			}
		}
		if !strings.Contains(html, `<link rel="preload" href="/static/fonts/manrope.woff2"`) {
			t.Errorf("%s: Manrope not preloaded", p)
		}
		if strings.Contains(html, "<script src=") && !strings.Contains(html, `defer></script>`) {
			t.Errorf("%s: scripts must be deferred", p)
		}
	}
	css, _ := os.ReadFile("../../static/app.css")
	if len(css) > 15*1024 {
		t.Errorf("app.css is %d bytes, budget 15 KB", len(css))
	}
	js, _ := os.ReadFile("../../static/app.js")
	htmx, _ := os.ReadFile("../../static/htmx.min.js")
	if n := gzipSize(append(js, htmx...)); n > 25*1024 {
		t.Errorf("JS is %d bytes gzipped, budget 25 KB", n)
	}
	// static assets carry the immutable cache header and gzip
	w := do(t, a, "GET", "/static/app.js?v=x", nil, "Accept-Encoding", "gzip")
	if w.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("static js headers: %v", w.Header())
	}
	zr, _ := gzip.NewReader(w.Body)
	if b, _ := io.ReadAll(zr); len(b) != len(js) {
		t.Error("gzipped js does not round-trip")
	}
}

// TestSEORedirectsAndSitemap covers Task 21's slug-history redirects
// (task-19-decisions #8): a renamed product's old URL must 301 to the live
// one and that destination must actually resolve; a renamed-then-deactivated
// product's old URL must 404, never 301, to a dead page; and the sitemap
// must never advertise a URL that itself redirects, since it is already
// restricted to active products' current slugs (ActiveProductsForSitemap).
func TestSEORedirectsAndSitemap(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)

	// Rename an active product: the old slug must 301, and the destination
	// the redirect names must itself resolve, not just look right.
	live := createSlugTestProduct(t, a, adm, "Rebranded Soap", "old-soap-name")
	live = renameSlugTestProduct(t, a, adm, live, "new-soap-name")

	w := do(t, a, "GET", "/products/old-soap-name", nil)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("renamed product's old slug: got %d, want 301", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/products/new-soap-name" {
		t.Fatalf("redirect Location = %q, want /products/new-soap-name", loc)
	}
	if w := do(t, a, "GET", loc, nil); w.Code != http.StatusOK {
		t.Fatalf("redirect destination %q: got %d, want 200 (a 301 to a URL that itself 404s is worse than a plain 404)", loc, w.Code)
	}

	// Rename then deactivate a second product: task-21-decisions #1 requires
	// a plain 404 here, never a 301 to a page that would then 404 anyway.
	dead := createSlugTestProduct(t, a, adm, "Discontinued Item", "old-discontinued-name")
	dead = renameSlugTestProduct(t, a, adm, dead, "new-discontinued-name")
	deactivateSlugTestProduct(t, a, adm, dead)

	if w := do(t, a, "GET", "/products/old-discontinued-name", nil); w.Code != http.StatusNotFound {
		t.Fatalf("old slug of a renamed-then-deactivated product: got %d, want 404, not a 301 to a dead page", w.Code)
	}
	if w := do(t, a, "GET", "/products/new-discontinued-name", nil); w.Code != http.StatusNotFound {
		t.Fatalf("current slug of a deactivated product: got %d, want 404", w.Code)
	}

	// Sitemap: only the current slug of the still-active renamed product,
	// never a retired slug and never the current slug of a deactivated one.
	w = do(t, a, "GET", "/sitemap.xml", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("sitemap.xml: %d", w.Code)
	}
	body := w.Body.String()
	for _, bad := range []string{"/products/old-soap-name", "/products/old-discontinued-name", "/products/new-discontinued-name"} {
		if strings.Contains(body, bad) {
			t.Errorf("sitemap must not advertise %s:\n%s", bad, body)
		}
	}
	if !strings.Contains(body, "/products/new-soap-name") {
		t.Errorf("sitemap missing the current slug of the renamed active product:\n%s", body)
	}

	// Directly assert no <loc> in the sitemap 301s (task-19-decisions #8: "a
	// sitemap advertising a URL that 301s is an SEO defect"). This walks every
	// entry the sitemap actually emits — home, /products, each category, and
	// each active product — not just the two products this test renamed.
	base := a.cfg.BaseURL
	locs := sitemapLocs.FindAllStringSubmatch(body, -1)
	if len(locs) == 0 {
		t.Fatal("sitemap has no <loc> entries to check")
	}
	for _, m := range locs {
		full := m[1]
		if !strings.HasPrefix(full, base) {
			t.Fatalf("sitemap loc %q does not start with base URL %q", full, base)
		}
		u, err := url.Parse(strings.TrimPrefix(full, base))
		if err != nil {
			t.Fatalf("sitemap loc %q: %v", full, err)
		}
		target := u.Path
		if u.RawQuery != "" {
			target += "?" + u.RawQuery
		}
		if target == "" {
			target = "/"
		}
		w := do(t, a, "GET", target, nil)
		if w.Code == http.StatusMovedPermanently {
			t.Errorf("sitemap advertises %s, which 301s to %s", full, w.Header().Get("Location"))
		}
	}
}
