package app

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
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
