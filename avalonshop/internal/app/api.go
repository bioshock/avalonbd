package app

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"avalonshop/internal/img"
	"avalonshop/internal/store"
)

// The admin JSON API (docs/api.md). Bearer-token auth, no cookies, so the
// session and CSRF middleware are skipped for /api/admin/ (see Handler).

type apiCategory struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type apiVariant struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	SKU          *string `json:"sku"`
	Price        int     `json:"price"`
	RegularPrice *int    `json:"regular_price"`
	Stock        int     `json:"stock"`
}

type apiImage struct {
	ID     int64  `json:"id"`
	File   string `json:"file"`
	Alt    string `json:"alt"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// toAPIImage converts a store image row to its JSON shape. Task 4's image
// upload/delete routes reuse this so the literal isn't duplicated.
func toAPIImage(im store.Image) apiImage {
	return apiImage{ID: im.ID, File: im.File, Alt: im.Alt, Width: im.Width, Height: im.Height}
}

type apiProduct struct {
	ID              int64        `json:"id"`
	Slug            string       `json:"slug"`
	Name            string       `json:"name"`
	Tagline         string       `json:"tagline"`
	Description     string       `json:"description"`
	MetaDescription string       `json:"meta_description"`
	CategoryID      *int64       `json:"category_id"`
	Active          bool         `json:"active"`
	Featured        bool         `json:"featured"`
	Promo           bool         `json:"promo"`
	Variants        []apiVariant `json:"variants"`
	Images          []apiImage   `json:"images"`
}

func toAPIProduct(p store.ProductFull) apiProduct {
	out := apiProduct{
		ID: p.ID, Slug: p.Slug, Name: p.Name, Tagline: p.Tagline, Description: p.Description,
		MetaDescription: p.MetaDescription, CategoryID: p.CategoryID, Active: p.Active,
		Featured: p.Featured, Promo: p.Promo, Variants: []apiVariant{}, Images: []apiImage{},
	}
	for _, v := range p.Variants {
		out.Variants = append(out.Variants, apiVariant{ID: v.ID, Name: v.Name, SKU: v.SKU, Price: v.Price, RegularPrice: v.RegularPrice, Stock: v.Stock})
	}
	for _, im := range p.Images {
		out.Images = append(out.Images, toAPIImage(im))
	}
	return out
}

// toStore converts request JSON. Variant order in the array is the sort order.
func (in apiProduct) toStore() (store.Product, []store.Variant) {
	p := store.Product{
		Slug: in.Slug, Name: in.Name, Tagline: strings.TrimSpace(in.Tagline), Description: strings.TrimSpace(in.Description),
		MetaDescription: strings.TrimSpace(in.MetaDescription), CategoryID: in.CategoryID,
		Active: in.Active, Featured: in.Featured, Promo: in.Promo,
	}
	vs := make([]store.Variant, 0, len(in.Variants))
	for i, v := range in.Variants {
		vs = append(vs, store.Variant{ID: v.ID, Name: strings.TrimSpace(v.Name), SKU: v.SKU, Price: v.Price, RegularPrice: v.RegularPrice, Stock: v.Stock, Sort: i})
	}
	return p, vs
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, msg string, fields map[string]string) {
	body := map[string]any{"error": msg}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	writeJSON(w, status, body)
}

func (a *App) apiServerError(w http.ResponseWriter, r *http.Request, err error) {
	a.log.Error("api request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	apiError(w, http.StatusInternalServerError, "internal error", nil)
}

// decodeJSON rejects unknown fields so a typo like "regularPrice" is an
// error instead of a silently unset value.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			apiError(w, http.StatusRequestEntityTooLarge, "request body too large", nil)
			return false
		}
		apiError(w, http.StatusBadRequest, "bad JSON: "+err.Error(), nil)
		return false
	}
	return true
}

func (a *App) apiHandler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/admin/categories", a.apiCategories)
	m.HandleFunc("POST /api/admin/categories", a.apiCategoryCreate)
	m.HandleFunc("GET /api/admin/products", a.apiProducts)
	m.HandleFunc("POST /api/admin/products", a.apiProductCreate)
	m.HandleFunc("GET /api/admin/products/{id}", a.apiProductGet)
	m.HandleFunc("PUT /api/admin/products/{id}", a.apiProductUpdate)
	m.HandleFunc("DELETE /api/admin/products/{id}", a.apiProductDelete)
	// Task 4 adds the image routes here.
	m.HandleFunc("/api/admin/", func(w http.ResponseWriter, r *http.Request) {
		apiError(w, http.StatusNotFound, "no such endpoint", nil)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := a.cfg.AdminAPIToken
		if tok == "" { // API disabled: look exactly like any other missing page
			a.notFound(w, r)
			return
		}
		if !a.apiLimit.Allow(clientIP(r)) {
			apiError(w, http.StatusTooManyRequests, "too many requests, slow down", nil)
			return
		}
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(tok)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			apiError(w, http.StatusUnauthorized, "missing or wrong token", nil)
			return
		}
		m.ServeHTTP(w, r)
	})
}

// ---- categories ----

func (a *App) apiCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := a.st.ListCategories(r.Context())
	if err != nil {
		a.apiServerError(w, r, err)
		return
	}
	out := []apiCategory{}
	for _, c := range cats {
		out = append(out, apiCategory{ID: c.ID, Slug: c.Slug, Name: c.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) apiCategoryCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		apiError(w, http.StatusBadRequest, "validation failed", map[string]string{"name": "is required"})
		return
	}
	slug := strings.TrimSpace(in.Slug)
	if slug == "" {
		slug = in.Name
	}
	slug = store.Slugify(slug)
	id, err := a.st.CreateCategory(r.Context(), store.Category{Slug: slug, Name: in.Name})
	if errors.Is(err, store.ErrDuplicate) {
		apiError(w, http.StatusConflict, "a category with slug "+slug+" already exists", nil)
		return
	}
	if err != nil {
		a.apiServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "slug": slug})
}

// ---- products ----

func (a *App) apiProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.st.ListProductsAdmin(r.Context())
	if err != nil {
		a.apiServerError(w, r, err)
		return
	}
	out := []apiProduct{}
	for _, row := range rows { // ponytail: one query per product; fine for a catalog of dozens
		p, err := a.st.GetProduct(r.Context(), row.ID)
		if err != nil {
			a.apiServerError(w, r, err)
			return
		}
		out = append(out, toAPIProduct(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// apiLoad fetches the {id} product or writes 404/500 and returns false.
func (a *App) apiLoad(w http.ResponseWriter, r *http.Request) (store.ProductFull, bool) {
	p, err := a.st.GetProduct(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		apiError(w, http.StatusNotFound, "no such product", nil)
		return p, false
	}
	if err != nil {
		a.apiServerError(w, r, err)
		return p, false
	}
	return p, true
}

func (a *App) apiProductGet(w http.ResponseWriter, r *http.Request) {
	if p, ok := a.apiLoad(w, r); ok {
		writeJSON(w, http.StatusOK, toAPIProduct(p))
	}
}

// apiSaveError maps store errors from CreateProduct/UpdateProduct.
func (a *App) apiSaveError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrDuplicate):
		apiError(w, http.StatusConflict, "slug or SKU already in use by another product", nil)
	case errors.Is(err, store.ErrInUse): // foreign key: category_id points nowhere
		apiError(w, http.StatusBadRequest, "validation failed", map[string]string{"category_id": "no such category"})
	default:
		a.apiServerError(w, r, err)
	}
}

func (a *App) apiProductCreate(w http.ResponseWriter, r *http.Request) {
	var in apiProduct
	if !decodeJSON(w, r, &in) {
		return
	}
	p, vs := in.toStore()
	for i := range vs {
		vs[i].ID = 0 // create never updates existing rows
	}
	if errs := validateProduct(&p, vs); len(errs) > 0 {
		apiError(w, http.StatusBadRequest, "validation failed", errs)
		return
	}
	id, err := a.st.CreateProduct(r.Context(), p, vs)
	if err != nil {
		a.apiSaveError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "slug": p.Slug})
}

func (a *App) apiProductUpdate(w http.ResponseWriter, r *http.Request) {
	existing, ok := a.apiLoad(w, r)
	if !ok {
		return
	}
	var in apiProduct
	if !decodeJSON(w, r, &in) {
		return
	}
	p, vs := in.toStore()
	p.ID = existing.ID
	errs := validateProduct(&p, vs)
	// Review Focus 2: a variant id from another product would make
	// syncVariants update nothing and then delete this product's real rows.
	own := map[int64]int{} // variant id -> its live stock, for the SkipStockUpdate check below
	for _, v := range existing.Variants {
		own[v.ID] = v.Stock
	}
	for i, v := range vs {
		if v.ID == 0 {
			continue
		}
		stock, isOwn := own[v.ID]
		if !isOwn {
			errs[fmt.Sprintf("variants[%d].id", i)] = "not a variant of this product (use 0 for a new variant)"
			continue
		}
		// C2, ported from the admin form (products.go's SkipStockUpdate):
		// the documented GET -> edit -> PUT round-trip resubmits whatever
		// stock number the earlier GET returned for any field the client
		// didn't mean to touch. Unlike the admin form, the JSON API has no
		// hidden "rendered with" field distinct from the live value, so it
		// cannot tell "untouched" apart from "stale". Treating a mismatch
		// against the row's live stock as staleness and keeping the
		// database's value is the safe side of that ambiguity: an order's
		// decrement must never be silently reverted by an unrelated PUT
		// (e.g. a price change) that carries a stale, pre-decrement number.
		// A PUT that resubmits the same stock the row already has behaves
		// identically either way.
		if v.Stock != stock {
			vs[i].SkipStockUpdate = true
		}
	}
	if len(errs) > 0 {
		apiError(w, http.StatusBadRequest, "validation failed", errs)
		return
	}
	if err := a.st.UpdateProduct(r.Context(), p, vs); err != nil {
		a.apiSaveError(w, r, err)
		return
	}
	full, err := a.st.GetProduct(r.Context(), p.ID)
	if err != nil {
		a.apiServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIProduct(full))
}

func (a *App) apiProductDelete(w http.ResponseWriter, r *http.Request) {
	imgs, err := a.st.DeleteProduct(r.Context(), pathID(r))
	switch {
	case errors.Is(err, store.ErrNotFound):
		apiError(w, http.StatusNotFound, "no such product", nil)
		return
	case errors.Is(err, store.ErrInUse):
		apiError(w, http.StatusConflict, "this product appears in orders; set active to false instead", nil)
		return
	case err != nil:
		a.apiServerError(w, r, err)
		return
	}
	for _, im := range imgs {
		img.Remove(a.cfg.UploadDir, im.File, im.Width)
	}
	w.WriteHeader(http.StatusNoContent)
}
