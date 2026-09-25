package app

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"avalonshop/internal/config"
	"avalonshop/internal/mail"
	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newApp builds an App against the real templates and static files. pool may be nil
// for tests that never touch the database.
func newApp(t *testing.T, pool *pgxpool.Pool) *App {
	t.Helper()
	cfg := config.Config{
		Addr: ":0", BaseURL: "http://localhost:8080", SessionSecret: bytes.Repeat([]byte("k"), 32),
		UploadDir: t.TempDir(), MailFrom: "shop@test.local", OrderNotifyEmail: "owner@test.local",
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := mail.New("", "587", "", "", cfg.MailFrom, os.DirFS("../.."), log)
	if err != nil {
		t.Fatal(err)
	}
	var st *store.Store
	if pool != nil {
		st = store.New(pool)
	}
	a, err := New(cfg, st, m, os.DirFS("../.."), os.DirFS("../.."), log)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func newDBApp(t *testing.T) (*App, *store.Store) {
	t.Helper()
	pool := storetest.Pool(t)
	a := newApp(t, pool)
	return a, a.st
}

func do(t *testing.T, a *App, method, path string, body io.Reader, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, body)
	if body != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestSecurityHeadersAndGzip404(t *testing.T) {
	a := newApp(t, nil)
	w := do(t, a, "GET", "/no-such-page", nil, "Accept-Encoding", "gzip")
	if w.Code != 404 {
		t.Fatalf("status %d", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("html not gzipped: %v", w.Header())
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers missing: %v", w.Header())
	}
	zr, _ := gzip.NewReader(w.Body)
	html, _ := io.ReadAll(zr)
	if !strings.Contains(string(html), "Page not found") || !strings.Contains(string(html), `<meta name="robots" content="noindex">`) {
		t.Fatalf("404 page wrong: %s", html)
	}
	if strings.Count(string(html), "<h1") != 1 {
		t.Fatal("exactly one h1 expected")
	}
}

func TestStaticAndMediaCaching(t *testing.T) {
	a := newApp(t, nil)
	w := do(t, a, "GET", "/static/app.css?v=abc", nil, "Accept-Encoding", "gzip")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("css: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("css headers: %v", w.Header())
	}
	os.WriteFile(a.cfg.UploadDir+"/x-400.webp", []byte("RIFF....WEBP"), 0o644)
	w = do(t, a, "GET", "/media/x-400.webp", nil, "Accept-Encoding", "gzip")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "" || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("media: %d %v", w.Code, w.Header())
	}
	if w := do(t, a, "GET", "/media/", nil); w.Code != 404 {
		t.Fatalf("directory listing should 404, got %d", w.Code)
	}
	if w := do(t, a, "GET", "/healthz", nil); w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("healthz: %d %q", w.Code, w.Body.String())
	}
}

func TestPlainResponseWithoutGzip(t *testing.T) {
	a := newApp(t, nil)
	w := do(t, a, "GET", "/no-such-page", nil) // no Accept-Encoding
	if w.Header().Get("Content-Encoding") != "" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("plain response wrong: %v", w.Header())
	}
}

// TestBodyLimits checks limitBody directly: ordinary routes are capped at 1 MB
// and reject a ~2 MB body, while the image upload route shape gets the 110 MB
// cap and must not reject the same ~2 MB body for size. No route in this task
// actually reads the body, so the middleware is exercised against a probe
// handler that drains r.Body, exactly as a real handler would.
func TestBodyLimits(t *testing.T) {
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				http.Error(w, "too large", http.StatusRequestEntityTooLarge)
				return
			}
			t.Fatalf("unexpected read error: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})
	h := limitBody(probe)
	body := bytes.Repeat([]byte("a"), 2<<20) // ~2 MB; well under the 110 MB cap, well over the 1 MB cap

	r := httptest.NewRequest("POST", "/checkout", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("normal route: want 413 for a 2 MB body, got %d", w.Code)
	}

	r2 := httptest.NewRequest("POST", "/admin/products/1/images", bytes.NewReader(body))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("upload route: 2 MB body must not be rejected for size, got %d", w2.Code)
	}
}

// TestSecurityInvariants is the M1 fix. The only prior CSP assertion in the
// branch checked one substring of one directive, and four separate
// mutations of shipped source (dropping frame-ancestors/form-action/base-uri
// from the CSP; flipping cookie HttpOnly, SameSite and Secure) all left the
// full suite green. This pins the exact CSP byte-for-byte and the exact
// session-cookie attributes under both http:// and https:// BASE_URL.
func TestSecurityInvariants(t *testing.T) {
	a := newApp(t, nil)
	w := do(t, a, "GET", "/no-such-page", nil) // any route: secureHeaders runs before routing
	const wantCSP = "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'self'"
	if got := w.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("CSP = %q, want %q", got, wantCSP)
	}

	cases := []struct {
		name       string
		baseURL    string
		wantSecure bool
	}{
		{"http", "http://localhost:8080", false},
		{"https", "https://avalonbd.com", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newApp(t, nil)
			a.cfg.BaseURL = c.baseURL
			rec := httptest.NewRecorder()
			a.login(rec, 1)
			var sess *http.Cookie
			for _, ck := range rec.Result().Cookies() {
				if ck.Name == "sess" {
					sess = ck
				}
			}
			if sess == nil {
				t.Fatal("sess cookie was not set")
			}
			if !sess.HttpOnly {
				t.Error("sess cookie must be HttpOnly")
			}
			if sess.SameSite != http.SameSiteLaxMode {
				t.Errorf("sess cookie SameSite = %v, want Lax", sess.SameSite)
			}
			if sess.Secure != c.wantSecure {
				t.Errorf("sess cookie Secure = %v, want %v for BASE_URL=%q", sess.Secure, c.wantSecure, c.baseURL)
			}
		})
	}
}

// TestRangeRequestSkipsGzip ensures a 206 Partial Content response (from the
// static file server answering a Range request) is never gzipped: gzipping it
// would leave Content-Range describing the identity byte range while the body
// no longer matches it.
func TestRangeRequestSkipsGzip(t *testing.T) {
	a := newApp(t, nil)
	w := do(t, a, "GET", "/static/app.css", nil, "Accept-Encoding", "gzip", "Range", "bytes=0-99")
	if w.Code != http.StatusPartialContent {
		t.Fatalf("status %d, want 206", w.Code)
	}
	if enc := w.Header().Get("Content-Encoding"); enc == "gzip" {
		t.Fatalf("range response must not be gzipped: %v", w.Header())
	}
	cr := w.Header().Get("Content-Range")
	if cr == "" {
		t.Fatal("missing Content-Range")
	}
	if !strings.HasPrefix(cr, "bytes 0-99/") {
		t.Fatalf("unexpected Content-Range: %q", cr)
	}
	if w.Body.Len() != 100 {
		t.Fatalf("body length %d does not match the requested 100-byte range (Content-Range=%s)", w.Body.Len(), cr)
	}
}
