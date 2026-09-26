package app

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"
)

func (a *App) sitemap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prods, err := a.st.ActiveProductsForSitemap(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cats, err := a.st.ListCategories(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	add := func(loc string, lastmod time.Time) {
		b.WriteString("<url><loc>")
		xml.EscapeText(&b, []byte(loc))
		b.WriteString("</loc>")
		if !lastmod.IsZero() {
			b.WriteString("<lastmod>" + lastmod.UTC().Format("2006-01-02") + "</lastmod>")
		}
		b.WriteString("</url>\n")
	}
	base := a.cfg.BaseURL
	add(base+"/", time.Time{})
	add(base+"/products", time.Time{})
	for _, cp := range contentPages {
		add(base+"/"+cp.Slug, time.Time{})
	}
	for _, c := range cats {
		add(base+"/products?category="+c.Slug, time.Time{})
	}
	for _, p := range prods {
		add(base+"/products/"+p.Slug, p.UpdatedAt)
	}
	b.WriteString("</urlset>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(b.String()))
}

func (a *App) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte("User-agent: *\nDisallow: /admin\nDisallow: /cart\nDisallow: /checkout\nDisallow: /account\nDisallow: /orders\nDisallow: /login\nDisallow: /register\nDisallow: /forgot\nDisallow: /reset\nSitemap: " + a.cfg.BaseURL + "/sitemap.xml\n"))
}
