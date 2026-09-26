package app

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestSiteHelpers(t *testing.T) {
	if got, want := waLink("Hi & bye? #1 I'd"), "https://wa.me/8801933309009?text=Hi%20%26%20bye%3F%20%231%20I%27d"; got != want {
		t.Fatalf("waLink = %q, want %q", got, want)
	}
	n := func(v int) *int { return &v }
	for _, c := range []struct {
		price   int
		regular *int
		want    int
	}{{130, nil, 0}, {130, n(130), 0}, {130, n(100), 0}, {499, n(550), 550}} {
		if got := was(c.price, c.regular); got != c.want {
			t.Errorf("was(%d, %v) = %d, want %d", c.price, c.regular, got, c.want)
		}
	}
	if got := lines(" a \n\n b\r\n"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("lines = %q", got)
	}
	if site.Address() != "Shaheb Bazar, Ghoramara, Rajshahi 6100, Bangladesh" {
		t.Fatalf("address = %q", site.Address())
	}
}

// TestImgURLClampsToStoredWidth: img.WidthsFor only generates widths at or
// below the stored image's actual width, so asking imgURL for a width wider
// than what's stored (e.g. requesting 400 for an image that was uploaded at
// 320px) must fall back to the stored width instead of naming a variant file
// that was never generated.
func TestImgURLClampsToStoredWidth(t *testing.T) {
	a := newApp(t, nil)
	imgURL, ok := a.funcs()["imgURL"].(func(string, int, int) string)
	if !ok {
		t.Fatal("imgURL func not found or wrong signature")
	}
	if got, want := imgURL("x", 400, 320), "/media/x-320.webp"; got != want {
		t.Fatalf("imgURL(x, 400, 320) = %q, want %q", got, want)
	}
}

// TestShellAndBrand renders a DB-free page (the 404) to check the shared
// header/footer, and scans every template for retired brand wording.
func TestShellAndBrand(t *testing.T) {
	a := newApp(t, nil)
	body := do(t, a, "GET", "/no-such-page", nil).Body.String()
	for _, want := range []string{
		`<meta property="og:site_name" content="Avalon Foods">`,
		`<meta property="og:type" content="website">`,
		`<title>Page not found · Avalon Foods</title>`,
		`src="/static/logo.svg"`,
		`href="/static/fonts/lora.woff2"`,
		// html/template always HTML-escapes '+' as "&#43;" for any URL-typed
		// value, including a template.URL (html/template/html.go's
		// htmlReplacementTable hard-codes '+' -> "&#43;" unconditionally).
		// This decodes back to a literal '+' in the browser's href DOM
		// property, so the tel: link dials correctly; only the raw HTML byte
		// sequence differs from a naive literal match.
		`href="tel:&#43;8801933309009"`,
		`href="https://wa.me/8801933309009?text=`,
		`href="mailto:shop@avalonbd.com"`,
		`href="https://www.facebook.com/share/1Lzv9YkGFs/"`,
		`href="/delivery-returns"`, `href="/privacy"`, `href="/terms"`, `href="/about"`, `href="/journey"`, `href="/contact"`,
		`<symbol id="i-leaf"`,
		"Good Food. A Better Tomorrow.",
		"© 2026 Avalon Foods. All rights reserved.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("shell missing %q", want)
		}
	}
	forbidden := []string{"Avalon Corporation", "2050", "Halal", "HACCP", "100% Natural", "Vision", "raceability"}
	// fs.WalkDir's error is checked (not ignored) and the scanned count is
	// asserted: silently ignoring the error would let a wrong path scan
	// nothing and pass vacuously.
	scanned := 0
	walkErr := fs.WalkDir(os.DirFS("../../templates"), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		scanned++
		b, _ := os.ReadFile("../../templates/" + p)
		for _, f := range forbidden {
			if strings.Contains(string(b), f) {
				t.Errorf("templates/%s contains %q", p, f)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if scanned < 20 {
		t.Fatalf("scanned only %d template files, want at least 20", scanned)
	}
}
