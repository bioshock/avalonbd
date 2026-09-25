package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"avalonshop/internal/img"
	"avalonshop/internal/store"
)

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	featured, err := a.st.ListProductCards(ctx, store.ListOpts{FeaturedOnly: true, Limit: 8})
	if err == nil && len(featured) == 0 {
		featured, err = a.st.ListProductCards(ctx, store.ListOpts{Limit: 8})
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cats, err := a.st.ListCategories(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := page{
		Title:       "Avalon Shop · Fresh from Rajshahi",
		Description: siteDescription,
		OGImage:     a.cardOGImage(featured),
		Data:        map[string]any{"Featured": featured, "Categories": cats},
	}
	a.render(w, r, "store/home.html", p)
}

func (a *App) products(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	catSlug := r.URL.Query().Get("category")
	cats, err := a.st.ListCategories(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var current *store.Category
	if catSlug != "" {
		c, err := a.st.GetCategoryBySlug(ctx, catSlug)
		if errors.Is(err, store.ErrNotFound) {
			a.notFound(w, r)
			return
		}
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		current = &c
	}
	cards, err := a.st.ListProductCards(ctx, store.ListOpts{CategorySlug: catSlug, Query: q})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := page{
		Title:       "All products",
		Description: "Browse every product from Avalon. " + siteDescription,
		OGImage:     a.cardOGImage(cards),
		Data:        map[string]any{"Cards": cards, "Categories": cats, "Current": current, "Query": q},
	}
	switch {
	case q != "":
		p.Title = "Search: " + q
		p.NoIndex = true
	case current != nil:
		p.Title = current.Name
		p.Description = current.Name + " from Avalon. " + siteDescription
		p.Canonical = a.cfg.BaseURL + "/products?category=" + current.Slug
	}
	a.render(w, r, "store/products.html", p)
}

func (a *App) product(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")
	p, err := a.st.GetProductBySlug(ctx, slug, true)
	if errors.Is(err, store.ErrNotFound) {
		// The live product always wins: only fall back to slug history once
		// the current-slug lookup above has already missed (task-21-brief
		// #3). ResolveSlugRedirect itself only resolves through active
		// products, so an old slug of an inactive or deleted product falls
		// through to the plain 404 below rather than a 301 to a dead page
		// (task-21-decisions #1).
		switch target, herr := a.st.ResolveSlugRedirect(ctx, slug); {
		case herr == nil:
			dest := "/products/" + target
			if r.URL.RawQuery != "" {
				dest += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, dest, http.StatusMovedPermanently)
		case errors.Is(herr, store.ErrNotFound):
			a.notFound(w, r)
		default:
			a.serverError(w, r, herr)
		}
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var related []store.ProductCard
	if p.CategoryID != nil {
		related, err = a.st.ListProductCards(ctx, store.ListOpts{CategoryID: *p.CategoryID, ExcludeID: p.ID, Limit: 4})
		if err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	desc := p.MetaDescription
	if desc == "" {
		desc = truncate(strings.ReplaceAll(p.Description, "\n", " "), 155)
	}
	pg := page{
		Title:       p.Name,
		Description: desc,
		JSONLD:      a.productJSONLD(p, desc),
		Data:        map[string]any{"Product": p, "Related": related, "Selected": selectedVariant(p.Variants)},
	}
	if len(p.Images) > 0 {
		pg.OGImage = a.cfg.BaseURL + "/media/" + img.Filename(p.Images[0].File, p.Images[0].Width)
	}
	a.render(w, r, "store/product.html", pg)
}

// cardOGImage returns an absolute URL for the first card that has an image, or "".
func (a *App) cardOGImage(cards []store.ProductCard) string {
	for _, c := range cards {
		if c.ImageFile != nil && c.ImageWidth != nil {
			return a.cfg.BaseURL + "/media/" + img.Filename(*c.ImageFile, *c.ImageWidth)
		}
	}
	return ""
}

// selectedVariant is the variant shown first: the first one in stock, else the first.
func selectedVariant(vs []store.Variant) store.Variant {
	for _, v := range vs {
		if v.Stock > 0 {
			return v
		}
	}
	if len(vs) > 0 {
		return vs[0]
	}
	return store.Variant{}
}

// truncate cuts s to at most n runes at a word boundary and appends an ellipsis.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)[:n]
	cut := string(runes)
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

func (a *App) productJSONLD(p store.ProductFull, desc string) template.JS {
	url := a.cfg.BaseURL + "/products/" + p.Slug
	images := []string{}
	for _, im := range p.Images {
		images = append(images, a.cfg.BaseURL+"/media/"+img.Filename(im.File, im.Width))
	}
	offers := []map[string]any{}
	for _, v := range p.Variants {
		avail := "https://schema.org/InStock"
		if v.Stock == 0 {
			avail = "https://schema.org/OutOfStock"
		}
		o := map[string]any{"@type": "Offer", "name": v.Name, "price": v.Price, "priceCurrency": "BDT", "availability": avail, "url": url}
		if v.SKU != nil {
			o["sku"] = *v.SKU
		}
		offers = append(offers, o)
	}
	sort.SliceStable(offers, func(i, j int) bool { return offers[i]["price"].(int) < offers[j]["price"].(int) })
	data := map[string]any{
		"@context": "https://schema.org", "@type": "Product",
		"name": p.Name, "description": desc, "image": images, "url": url,
		"brand":  map[string]any{"@type": "Brand", "name": "Avalon"},
		"offers": offers,
	}
	b, err := json.Marshal(data)
	if err != nil {
		return template.JS(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return template.JS(b)
}
