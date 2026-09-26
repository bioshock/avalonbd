package app

import (
	"compress/gzip"
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"avalonshop/internal/store"
)

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self' https://analytics.c14.cloud; connect-src 'self' https://analytics.c14.cloud; frame-ancestors 'none'; form-action 'self'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

var gzPool = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}

type gzipWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	compress    bool
}

func compressible(ct string) bool {
	for _, p := range []string{"text/html", "text/css", "text/plain", "application/javascript", "text/javascript", "application/json", "application/xml", "text/xml", "image/svg+xml"} {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	return false
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	h := g.Header()
	if code != http.StatusNoContent && code != http.StatusNotModified && code != http.StatusPartialContent &&
		h.Get("Content-Range") == "" && compressible(h.Get("Content-Type")) {
		g.compress = true
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		h.Del("Content-Length")
		g.gz.Reset(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.compress {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// gzipMiddleware compresses text responses. Images are already compressed and pass through.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzPool.Get().(*gzip.Writer)
		defer gzPool.Put(gz)
		gw := &gzipWriter{ResponseWriter: w, gz: gz}
		defer func() {
			if gw.compress {
				gw.gz.Close()
			}
		}()
		next.ServeHTTP(gw, r)
	})
}

// limitBody caps request bodies; the image upload route gets its own larger cap.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			if (strings.HasPrefix(r.URL.Path, "/admin/products/") || strings.HasPrefix(r.URL.Path, "/api/admin/products/")) && strings.HasSuffix(r.URL.Path, "/images") {
				r.Body = http.MaxBytesReader(w, r.Body, 110<<20)
			} else {
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			}
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

// withUser loads the session user once per request. Static and media paths skip it.
func (a *App) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/static/") && !strings.HasPrefix(r.URL.Path, "/media/") {
			if c, err := r.Cookie("sess"); err == nil {
				var u store.User
				if _, ok := a.tok.DecodeSession(c.Value, time.Now(), func(id int64) (string, bool) {
					var err error
					u, err = a.st.GetUser(r.Context(), id)
					return u.PasswordHash, err == nil
				}); ok {
					r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &u))
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxKey{}).(*store.User)
	return u
}

func immutable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}
