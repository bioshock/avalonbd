package app

import (
	"context"
	"html"
	"strings"
	"testing"

	"avalonshop/internal/store"
)

func TestContentPages(t *testing.T) {
	a, st := newDBApp(t)
	ctx := context.Background()
	st.CreateZone(ctx, store.Zone{Name: "Inside Rajshahi city", Fee: 60, Active: true})
	st.CreateZone(ctx, store.Zone{Name: "Retired zone", Fee: 999, Active: false})
	sitemap := do(t, a, "GET", "/sitemap.xml", nil).Body.String()

	if len(contentPages) != 6 {
		t.Fatalf("want 6 content pages, got %d", len(contentPages))
	}
	for _, cp := range contentPages {
		w := do(t, a, "GET", "/"+cp.Slug, nil)
		body := w.Body.String()
		if w.Code != 200 {
			t.Fatalf("/%s: %d", cp.Slug, w.Code)
		}
		for _, want := range []string{
			"<title>" + html.EscapeString(cp.Title) + " · Avalon Foods</title>",
			`<link rel="canonical" href="http://localhost:8080/` + cp.Slug + `">`,
			`<meta name="description" content="` + html.EscapeString(cp.Description) + `">`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("/%s missing %q", cp.Slug, want)
			}
		}
		if strings.Count(body, "<h1") != 1 {
			t.Errorf("/%s: exactly one h1", cp.Slug)
		}
		if !strings.Contains(sitemap, "<loc>http://localhost:8080/"+cp.Slug+"</loc>") {
			t.Errorf("sitemap missing /%s", cp.Slug)
		}
	}

	d := do(t, a, "GET", "/delivery-returns", nil).Body.String()
	if !strings.Contains(d, "Inside Rajshahi city") || !strings.Contains(d, "৳ 60") || strings.Contains(d, "Retired zone") {
		t.Error("delivery page must list active zones from the DB only")
	}
	c := do(t, a, "GET", "/contact", nil).Body.String()
	for _, want := range []string{"Shaheb Bazar, Ghoramara, Rajshahi 6100, Bangladesh", "Sat–Thu, 10am–8pm", `href="tel:&#43;8801933309009"`, `href="mailto:shop@avalonbd.com"`} {
		if !strings.Contains(c, want) {
			t.Errorf("contact missing %q", want)
		}
	}
	if j := do(t, a, "GET", "/journey", nil).Body.String(); !strings.Contains(j, "Follow our story on Facebook") {
		t.Error("journey page must link to Facebook")
	}
}
