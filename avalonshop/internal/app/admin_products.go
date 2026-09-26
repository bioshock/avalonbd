package app

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"avalonshop/internal/img"
	"avalonshop/internal/store"
)

const maxUploadBytes = 10 << 20
const maxUploadFiles = 10

// maxFilenameInMessage caps how much of an uploaded filename lands in a flash
// message. The filename is attacker-controlled and never touches the
// filesystem (img.Process names files itself), but it was being echoed
// unbounded into a Set-Cookie flash: ten ~5000-character filenames put
// ~50 KB into one response header, which browsers and proxies can drop or
// reject outright (task-17-review Minor 5).
const maxFilenameInMessage = 60

func truncateFilename(name string) string {
	r := []rune(name)
	if len(r) <= maxFilenameInMessage {
		return name
	}
	return string(r[:maxFilenameInMessage]) + "…"
}

type productFormData struct {
	Product    store.Product
	Variants   []store.Variant
	Images     []store.Image
	Categories []store.Category
	Errors     map[string]string
	IsNew      bool
}

func (a *App) adminProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.st.ListProductsAdmin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/products.html", page{Title: "Products", NoIndex: true, Data: rows})
}

func (a *App) productFormPage(w http.ResponseWriter, r *http.Request, status int, d productFormData) {
	cats, err := a.st.ListCategories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	d.Categories = cats
	if d.Errors == nil {
		d.Errors = map[string]string{}
	}
	title := "Edit product"
	if d.IsNew {
		title = "New product"
	}
	a.renderStatus(w, r, status, "admin/product_form.html", page{Title: title, NoIndex: true, Data: d})
}

func (a *App) adminProductNew(w http.ResponseWriter, r *http.Request) {
	a.productFormPage(w, r, http.StatusOK, productFormData{IsNew: true, Product: store.Product{Active: true}, Variants: []store.Variant{{}}})
}

func (a *App) adminProductEdit(w http.ResponseWriter, r *http.Request) {
	p, err := a.st.GetProduct(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.productFormPage(w, r, http.StatusOK, productFormData{Product: p.Product, Variants: p.Variants, Images: p.Images})
}

// parseProductForm reads the product fields and the repeated variant_* fields.
// Rows with an empty name are ignored so blank template rows are harmless.
// Rules shared with the API live in validateProduct; this only adds the
// form-specific parsing (numbers typed as text, the C2 stock guard).
func parseProductForm(r *http.Request) (store.Product, []store.Variant, map[string]string) {
	if err := r.ParseForm(); err != nil {
		return store.Product{}, nil, map[string]string{"form": "Could not read the form."}
	}
	p := store.Product{
		Name:            r.FormValue("name"),
		Slug:            r.FormValue("slug"),
		Description:     strings.TrimSpace(r.FormValue("description")),
		MetaDescription: strings.TrimSpace(r.FormValue("meta_description")),
		Tagline:         strings.TrimSpace(r.FormValue("tagline")),
		Active:          r.FormValue("active") == "on",
		Featured:        r.FormValue("featured") == "on",
		Promo:           r.FormValue("promo") == "on",
	}
	if cid, _ := strconv.ParseInt(r.FormValue("category_id"), 10, 64); cid > 0 {
		p.CategoryID = &cid
	}

	f := r.Form
	var vs []store.Variant
	badNumber := false
	for i := range f["variant_name"] {
		name := strings.TrimSpace(f["variant_name"][i])
		if name == "" {
			continue
		}
		get := func(key string) string {
			if i < len(f[key]) {
				return strings.TrimSpace(f[key][i])
			}
			return ""
		}
		id, _ := strconv.ParseInt(get("variant_id"), 10, 64)
		price, err1 := strconv.Atoi(get("variant_price"))
		stock, err2 := strconv.Atoi(get("variant_stock"))
		if err1 != nil || err2 != nil {
			badNumber = true
		}
		var regular *int
		if s := get("variant_regular_price"); s != "" {
			if n, err := strconv.Atoi(s); err != nil {
				badNumber = true
			} else {
				regular = &n
			}
		}
		// C2: variant_stock_was carries the stock value this row was
		// rendered with (a hidden field in variant_row.html). If the
		// submitted stock still matches it, the admin never touched that
		// field, so the save must not overwrite whatever stock decrements
		// landed since the form was opened. A missing or unparseable
		// variant_stock_was (an old cached form, or a brand-new row) falls
		// back to the pre-fix behaviour of always writing the submitted
		// stock.
		skipStock := false
		if id > 0 {
			if was, err := strconv.Atoi(get("variant_stock_was")); err == nil && was == stock {
				skipStock = true
			}
		}
		// variant_sort is optional: fall back to row order when it's missing,
		// blank, or not a number (task-17-decisions #6).
		sort := len(vs)
		if s := get("variant_sort"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				sort = n
			}
		}
		sku := get("variant_sku")
		vs = append(vs, store.Variant{ID: id, Name: name, SKU: &sku, Price: price, RegularPrice: regular, Stock: stock, Sort: sort, SkipStockUpdate: skipStock})
	}
	errs := validateProduct(&p, vs)
	// The form shows one message for every per-row problem.
	for k := range errs {
		if strings.HasPrefix(k, "variants[") {
			delete(errs, k)
			badNumber = true
		}
	}
	if badNumber {
		errs["variants"] = "Price and stock must be whole numbers, 0 or more."
	}
	return p, vs, errs
}

// markDuplicateSKU records the shared field error for store.ErrDuplicate from
// either CreateProduct or UpdateProduct (task-17-decisions #5).
func markDuplicateSKU(errs map[string]string) {
	errs["variants"] = "One of those SKUs is already used by another product."
}

func (a *App) adminProductCreate(w http.ResponseWriter, r *http.Request) {
	p, vs, errs := parseProductForm(r)
	if len(errs) > 0 {
		a.productFormPage(w, r, http.StatusUnprocessableEntity, productFormData{IsNew: true, Product: p, Variants: vs, Errors: errs})
		return
	}
	var err error
	if p.Slug, err = a.st.UniqueSlug(r.Context(), "products", p.Slug, 0); err != nil {
		a.serverError(w, r, err)
		return
	}
	id, err := a.st.CreateProduct(r.Context(), p, vs)
	if errors.Is(err, store.ErrDuplicate) {
		markDuplicateSKU(errs)
		a.productFormPage(w, r, http.StatusUnprocessableEntity, productFormData{IsNew: true, Product: p, Variants: vs, Errors: errs})
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.flashBack(w, r, "Product created. Now add some photos.", "/admin/products/"+strconv.FormatInt(id, 10))
}

func (a *App) adminProductUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	existing, err := a.st.GetProduct(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p, vs, errs := parseProductForm(r)
	p.ID = id
	if len(errs) > 0 {
		a.productFormPage(w, r, http.StatusUnprocessableEntity, productFormData{Product: p, Variants: vs, Images: existing.Images, Errors: errs})
		return
	}
	if p.Slug, err = a.st.UniqueSlug(r.Context(), "products", p.Slug, id); err != nil {
		a.serverError(w, r, err)
		return
	}
	err = a.st.UpdateProduct(r.Context(), p, vs)
	if errors.Is(err, store.ErrDuplicate) {
		markDuplicateSKU(errs)
		a.productFormPage(w, r, http.StatusUnprocessableEntity, productFormData{Product: p, Variants: vs, Images: existing.Images, Errors: errs})
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	// Recording the retired slug happens inside UpdateProduct itself, under
	// the same row lock and transaction as the rename (Task 21 fix round 1):
	// this handler no longer needs to do it as a second, separate call.
	a.flashBack(w, r, "Saved.", "/admin/products/"+strconv.FormatInt(id, 10))
}

func (a *App) adminProductDelete(w http.ResponseWriter, r *http.Request) {
	imgs, err := a.st.DeleteProduct(r.Context(), pathID(r))
	switch {
	case errors.Is(err, store.ErrInUse):
		a.flashBack(w, r, "This product appears in orders, so it can't be deleted. Mark it inactive instead.", "/admin/products")
		return
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	for _, im := range imgs {
		img.Remove(a.cfg.UploadDir, im.File, im.Width)
	}
	a.flashBack(w, r, "Product deleted.", "/admin/products")
}

func (a *App) adminVariantRow(w http.ResponseWriter, r *http.Request) {
	a.renderPartial(w, "variant_row.html", store.Variant{})
}

// ---- images ----

func (a *App) imagesResponse(w http.ResponseWriter, r *http.Request, productID int64, errs []string) {
	back := "/admin/products/" + strconv.FormatInt(productID, 10)
	if !isHTMX(r) {
		if len(errs) > 0 {
			a.setFlash(w, strings.Join(errs, " "))
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	imgs, err := a.st.ListImages(r.Context(), productID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderPartial(w, "image_list.html", map[string]any{"ProductID": productID, "Images": imgs, "Errors": errs})
}

func (a *App) adminImagesUpload(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if _, err := a.st.GetProduct(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFound(w, r)
			return
		}
		a.serverError(w, r, err)
		return
	}
	// Task 10's limitBody middleware already applies this same 110 MB cap to
	// this route by path shape; setting it again here keeps the handler
	// correct on its own even if that routing detail ever changes
	// (task-17-decisions #2 — the spec's "110 MB for 10 files" body limit).
	r.Body = http.MaxBytesReader(w, r.Body, 110<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		a.imagesResponse(w, r, id, []string{"Upload too large: at most 10 images of 10 MB each per upload."})
		return
	}
	defer r.MultipartForm.RemoveAll()
	var errs []string
	files := r.MultipartForm.File["images"]
	if len(files) > maxUploadFiles {
		files = files[:maxUploadFiles]
		errs = append(errs, "Only the first 10 files were processed.")
	}
	for _, fh := range files {
		if fh.Size > maxUploadBytes {
			errs = append(errs, truncateFilename(fh.Filename)+": over 10 MB.")
			continue
		}
		f, err := fh.Open()
		if err != nil {
			errs = append(errs, truncateFilename(fh.Filename)+": could not read.")
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			errs = append(errs, truncateFilename(fh.Filename)+": could not read.")
			continue
		}
		// img.Process names the files itself (a random hex stem); the
		// untrusted fh.Filename from the request is never used as a path.
		res, err := img.Process(data, a.cfg.UploadDir)
		if err != nil {
			errs = append(errs, truncateFilename(fh.Filename)+": "+err.Error())
			continue
		}
		if _, err := a.st.AddImage(r.Context(), store.Image{ProductID: id, File: res.Stem, Width: res.Width, Height: res.Height}); err != nil {
			img.Remove(a.cfg.UploadDir, res.Stem, res.Width)
			errs = append(errs, truncateFilename(fh.Filename)+": could not save.")
		}
	}
	a.imagesResponse(w, r, id, errs)
}

// adminImageAlt and adminImageMove take the product id to redirect back to
// from the store, not from the form's product_id field: that field is
// unauthenticated attacker input, and trusting it let a request for one
// image re-render a different product's photo list, or redirect to
// /admin/products/0 when omitted entirely (task-17-review Minor 6).
// adminImageDelete already got this right; these two now match it.
func (a *App) adminImageAlt(w http.ResponseWriter, r *http.Request) {
	productID, err := a.st.UpdateImageAlt(r.Context(), pathID(r), strings.TrimSpace(r.FormValue("alt")))
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.imagesResponse(w, r, productID, nil)
}

func (a *App) adminImageMove(w http.ResponseWriter, r *http.Request) {
	dir := r.FormValue("dir")
	if dir != "up" && dir != "down" {
		// Any value other than the two the UI ever sends is rejected outright
		// rather than silently treated as "down" (task-17-review Minor 7).
		a.notFound(w, r)
		return
	}
	productID, err := a.st.MoveImage(r.Context(), pathID(r), dir == "up")
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.imagesResponse(w, r, productID, nil)
}

func (a *App) adminImageDelete(w http.ResponseWriter, r *http.Request) {
	im, err := a.st.DeleteImage(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	img.Remove(a.cfg.UploadDir, im.File, im.Width)
	a.imagesResponse(w, r, im.ProductID, nil)
}
