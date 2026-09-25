package app

import (
	"strings"
	"testing"
)

func TestSitemapAndRobots(t *testing.T) {
	a, st := newDBApp(t)
	seedCatalog(t, st)
	w := do(t, a, "GET", "/sitemap.xml", nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/xml") || w.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatalf("sitemap headers: %d %v", w.Code, w.Header())
	}
	for _, want := range []string{
		`<loc>http://localhost:8080/</loc>`,
		`<loc>http://localhost:8080/products</loc>`,
		`<loc>http://localhost:8080/products?category=honey</loc>`,
		`<loc>http://localhost:8080/products/wild-forest-honey</loc>`,
		`<lastmod>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "hidden") {
		t.Error("inactive product in sitemap")
	}
	w = do(t, a, "GET", "/robots.txt", nil)
	body = w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Disallow: /admin") || !strings.Contains(body, "Sitemap: http://localhost:8080/sitemap.xml") {
		t.Fatalf("robots: %d\n%s", w.Code, body)
	}
}
