package app

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"avalonshop/internal/img"
	"avalonshop/internal/money"
	"avalonshop/internal/store"
)

// siteDescription is the default meta/OG description for pages that don't set
// their own. Declared once, here; other tasks reuse it rather than redeclaring it.
const siteDescription = "Natural products from Rajshahi, Bangladesh. Small batches, honest sourcing, cash on delivery nationwide."

// page is what every layout receives. Handlers fill the SEO fields and Data;
// render fills the rest.
type page struct {
	Title       string
	Description string
	Canonical   string
	OGImage     string
	JSONLD      template.JS
	NoIndex     bool
	User        *store.User
	CartCount   int
	Flash       string
	V           string
	BaseURL     string
	Path        string
	Data        any
}

func (a *App) funcs() template.FuncMap {
	return template.FuncMap{
		"taka":   money.Format,
		"imgURL": func(stem string, w int) string { return "/media/" + img.Filename(stem, w) },
		"srcset": func(stem string, width int) string {
			var parts []string
			for _, w := range img.WidthsFor(width) {
				parts = append(parts, fmt.Sprintf("/media/%s %dw", img.Filename(stem, w), w))
			}
			return strings.Join(parts, ", ")
		},
		"dhaka": func(t time.Time) string { return t.In(a.dhaka).Format("2 Jan 2006, 3:04 PM") },
		"deref": func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		},
		"derefInt": func(p *int) int {
			if p == nil {
				return 0
			}
			return *p
		},
		"add":       func(x, y int) int { return x + y },
		"mul":       func(x, y int) int { return x * y },
		"hasPrefix": strings.HasPrefix,
		"cardData": func(c store.ProductCard, i int) map[string]any { return map[string]any{"C": c, "Eager": i < 4} },
	}
}

func parseTemplates(fsys fs.FS, funcs template.FuncMap) (map[string]*template.Template, error) {
	out := map[string]*template.Template{}
	for _, set := range []struct{ dir, layout string }{{"store", "templates/layout.html"}, {"admin", "templates/admin/layout.html"}} {
		pages, err := fs.Glob(fsys, "templates/"+set.dir+"/*.html")
		if err != nil {
			return nil, err
		}
		for _, p := range pages {
			if p == set.layout {
				continue
			}
			t, err := template.New("layout.html").Funcs(funcs).ParseFS(fsys, set.layout, "templates/partials/*.html", p)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			out[set.dir+"/"+path.Base(p)] = t
		}
	}
	partials, err := fs.Glob(fsys, "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	for _, p := range partials {
		t, err := template.New(path.Base(p)).Funcs(funcs).ParseFS(fsys, "templates/partials/*.html")
		if err != nil {
			return nil, err
		}
		out["partials/"+path.Base(p)] = t
	}
	return out, nil
}

func (a *App) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	a.renderStatus(w, r, http.StatusOK, name, p)
}

// renderStatus executes into a buffer first so a template error yields a clean
// 500 instead of half a page.
func (a *App) renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, p page) {
	t, ok := a.tmpl[name]
	if !ok {
		a.serverError(w, r, fmt.Errorf("template %q not found", name))
		return
	}
	p.User = a.currentUser(r)
	p.CartCount = cartCount(a.cartLines(r))
	p.Flash = a.popFlash(w, r)
	p.V = a.assetV
	p.BaseURL = a.cfg.BaseURL
	p.Path = r.URL.Path
	if p.Canonical == "" {
		p.Canonical = a.cfg.BaseURL + r.URL.Path
	}
	if p.Description == "" {
		p.Description = siteDescription
	}
	switch {
	case p.Title == "":
		p.Title = "Avalon"
	case !strings.Contains(p.Title, "Avalon"):
		p.Title += " · Avalon"
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", p); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (a *App) renderPartial(w http.ResponseWriter, name string, data any) {
	t, ok := a.tmpl["partials/"+name]
	if !ok {
		http.Error(w, "partial not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		a.log.Error("partial", "name", name, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	buf.WriteTo(w)
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.renderStatus(w, r, http.StatusNotFound, "store/404.html", page{Title: "Page not found", NoIndex: true})
}

func (a *App) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// redirect sends 303 for browsers and HX-Redirect for HTMX so both land on url.
func (a *App) redirect(w http.ResponseWriter, r *http.Request, url string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}
