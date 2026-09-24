// Package app is the HTTP layer: storefront and admin handlers, rendering, middleware.
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"avalonshop/internal/config"
	"avalonshop/internal/mail"
	"avalonshop/internal/store"
	"avalonshop/internal/token"
)

type App struct {
	cfg           config.Config
	st            *store.Store
	mail          *mail.Mailer
	log           *slog.Logger
	tok           token.Signer
	tmpl          map[string]*template.Template
	static        fs.FS
	assetV        string
	dhaka         *time.Location
	loginLimit    *limiter
	forgotLimit   *limiter
	checkoutLimit *limiter
	mux           *http.ServeMux
}

func New(cfg config.Config, st *store.Store, m *mail.Mailer, templates, static fs.FS, log *slog.Logger) (*App, error) {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		return nil, err
	}
	a := &App{
		cfg: cfg, st: st, mail: m, log: log, tok: token.New(cfg.SessionSecret), static: sub,
		loginLimit:    newLimiter(5, 15*time.Minute),
		forgotLimit:   newLimiter(5, 15*time.Minute),
		checkoutLimit: newLimiter(10, time.Hour),
		mux:           http.NewServeMux(),
	}
	if a.dhaka, err = time.LoadLocation("Asia/Dhaka"); err != nil {
		return nil, err
	}
	a.assetV = assetVersion(sub)
	if a.tmpl, err = parseTemplates(templates, a.funcs()); err != nil {
		return nil, fmt.Errorf("templates: %w", err)
	}
	a.routes()
	return a, nil
}

// assetVersion is a short hash of the CSS and JS so their URLs change on deploy.
func assetVersion(static fs.FS) string {
	h := sha256.New()
	for _, name := range []string{"app.css", "app.js"} {
		b, _ := fs.ReadFile(static, name)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

func (a *App) routes() {
	m := a.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	})
	m.Handle("GET /static/", immutable(http.StripPrefix("/static/", http.FileServerFS(a.static))))
	m.Handle("GET /media/", immutable(http.StripPrefix("/media/", http.FileServer(http.Dir(a.cfg.UploadDir)))))
	m.HandleFunc("/", a.notFound)
	// Later tasks append their routes below this line.
}

func (a *App) Handler() http.Handler {
	csrf := http.NewCrossOriginProtection()
	return secureHeaders(gzipMiddleware(limitBody(a.withUser(csrf.Handler(a.mux)))))
}
