// Package app is the HTTP layer: storefront and admin handlers, rendering, middleware.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
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
	apiLimit      *limiter
	mux           *http.ServeMux
	// hidden mirrors the "noindex" setting so every response can check it for free.
	// ponytail: in-memory copy, loaded at boot; with several app instances a toggle
	// only reaches the others on restart — re-read per request if we ever scale out.
	hidden atomic.Bool
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
		apiLimit:      newLimiter(300, time.Minute),
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

// LoadSettings reads the owner's site-wide switches from the database.
func (a *App) LoadSettings(ctx context.Context) error {
	v, err := a.st.Setting(ctx, "noindex")
	a.hidden.Store(v == "on")
	return err
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
	m.HandleFunc("GET /{$}", a.home)
	m.HandleFunc("GET /products", a.products)
	m.HandleFunc("GET /products/{slug}", a.product)
	m.HandleFunc("GET /cart", a.cartPage)
	m.HandleFunc("GET /cart/drawer", a.cartDrawer)
	m.HandleFunc("POST /cart/items", a.cartAdd)
	m.HandleFunc("POST /cart/items/{id}", a.cartUpdate)
	m.HandleFunc("GET /checkout", a.checkoutGet)
	m.HandleFunc("POST /checkout", a.checkoutPost)
	m.HandleFunc("GET /checkout/totals", a.checkoutTotals)
	m.HandleFunc("GET /orders/{number}", a.orderPage)
	m.HandleFunc("GET /login", a.loginGet)
	m.HandleFunc("POST /login", a.loginPost)
	m.HandleFunc("POST /logout", a.logoutPost)
	m.HandleFunc("GET /register", a.registerGet)
	m.HandleFunc("POST /register", a.registerPost)
	m.HandleFunc("GET /forgot", a.forgotGet)
	m.HandleFunc("POST /forgot", a.forgotPost)
	m.HandleFunc("GET /reset/{token}", a.resetGet)
	m.HandleFunc("POST /reset/{token}", a.resetPost)
	m.HandleFunc("GET /account", a.requireUser(a.accountGet))
	m.HandleFunc("POST /account", a.requireUser(a.accountPost))
	m.HandleFunc("POST /account/password", a.requireUser(a.accountPasswordPost))
	m.HandleFunc("GET /sitemap.xml", a.sitemap)
	m.HandleFunc("GET /robots.txt", a.robots)
	adm := a.requireAdmin
	m.HandleFunc("GET /admin", adm(a.adminDashboard))
	m.HandleFunc("GET /admin/categories", adm(a.adminCategories))
	m.HandleFunc("POST /admin/categories", adm(a.adminCategoryCreate))
	m.HandleFunc("POST /admin/categories/{id}", adm(a.adminCategoryUpdate))
	m.HandleFunc("POST /admin/categories/{id}/delete", adm(a.adminCategoryDelete))
	m.HandleFunc("POST /admin/settings/noindex", adm(a.adminSetNoIndex))
	m.HandleFunc("GET /admin/zones", adm(a.adminZones))
	m.HandleFunc("POST /admin/zones", adm(a.adminZoneCreate))
	m.HandleFunc("POST /admin/zones/{id}", adm(a.adminZoneUpdate))
	m.HandleFunc("POST /admin/zones/{id}/delete", adm(a.adminZoneDelete))
	m.HandleFunc("GET /admin/products", adm(a.adminProducts))
	m.HandleFunc("GET /admin/products/new", adm(a.adminProductNew))
	m.HandleFunc("POST /admin/products/new", adm(a.adminProductCreate))
	m.HandleFunc("GET /admin/products/variant-row", adm(a.adminVariantRow))
	m.HandleFunc("GET /admin/products/{id}", adm(a.adminProductEdit))
	m.HandleFunc("POST /admin/products/{id}", adm(a.adminProductUpdate))
	m.HandleFunc("POST /admin/products/{id}/delete", adm(a.adminProductDelete))
	m.HandleFunc("POST /admin/products/{id}/images", adm(a.adminImagesUpload))
	m.HandleFunc("POST /admin/images/{id}", adm(a.adminImageAlt))
	m.HandleFunc("POST /admin/images/{id}/move", adm(a.adminImageMove))
	m.HandleFunc("POST /admin/images/{id}/delete", adm(a.adminImageDelete))
	m.HandleFunc("GET /admin/orders", adm(a.adminOrders))
	m.HandleFunc("GET /admin/orders/{id}", adm(a.adminOrder))
	m.HandleFunc("POST /admin/orders/{id}/status", adm(a.adminOrderStatus))
	m.HandleFunc("POST /admin/orders/{id}/note", adm(a.adminOrderNote))
	for _, cp := range contentPages {
		m.HandleFunc("GET /"+cp.Slug, a.contentPage(cp.Slug, cp.Title, cp.Description))
	}
	m.HandleFunc("/", a.notFound)
	// Later tasks append their routes below this line.
}

// Handler routes /api/admin/ around the session and CSRF layers: the API
// authenticates with a bearer token and never reads cookies.
func (a *App) Handler() http.Handler {
	csrf := http.NewCrossOriginProtection()
	web := a.withUser(csrf.Handler(a.mux))
	api := a.apiHandler()
	return secureHeaders(gzipMiddleware(limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.hidden.Load() {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		if strings.HasPrefix(r.URL.Path, "/api/admin/") {
			api.ServeHTTP(w, r)
			return
		}
		web.ServeHTTP(w, r)
	}))))
}
