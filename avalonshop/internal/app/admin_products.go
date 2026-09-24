package app

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"avalonshop/internal/img"
	"avalonshop/internal/store"
)

const maxUploadBytes = 10 << 20
const maxUploadFiles = 10

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
func parseProductForm(r *http.Request) (store.Product, []store.Variant, map[string]string) {
	errs := map[string]string{}
	if err := r.ParseForm(); err != nil {
		errs["form"] = "Could not read the form."
		return store.Product{}, nil, errs
	}
	p := store.Product{
		Name:            strings.TrimSpace(r.FormValue("name")),
		Slug:            strings.TrimSpace(r.FormValue("slug")),
		Description:     strings.TrimSpace(r.FormValue("description")),
		MetaDescription: strings.TrimSpace(r.FormValue("meta_description")),
		Active:          r.FormValue("active") == "on",
		Featured:        r.FormValue("featured") == "on",
	}
	if cid, _ := strconv.ParseInt(r.FormValue("category_id"), 10, 64); cid > 0 {
		p.CategoryID = &cid
	}
	if p.Name == "" {
		errs["name"] = "Name is required."
	}
	if utf8.RuneCountInString(p.MetaDescription) > 160 {
		errs["meta_description"] = "Keep the meta description under 160 characters; search engines cut it there."
	}
	if p.Slug == "" {
		p.Slug = p.Name
	}
	p.Slug = store.Slugify(p.Slug)

	f := r.Form
	var vs []store.Variant
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
		if err1 != nil || err2 != nil || price < 0 || stock < 0 {
			errs["variants"] = "Price and stock must be whole numbers, 0 or more."
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
		vs = append(vs, store.Variant{ID: id, Name: name, SKU: &sku, Price: price, Stock: stock, Sort: sort})
	}
	if len(vs) == 0 {
		errs["variants"] = "Add at least one variant (even a single default size)."
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
			errs = append(errs, fh.Filename+": over 10 MB.")
			continue
		}
		f, err := fh.Open()
		if err != nil {
			errs = append(errs, fh.Filename+": could not read.")
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			errs = append(errs, fh.Filename+": could not read.")
			continue
		}
		// img.Process names the files itself (a random hex stem); the
		// untrusted fh.Filename from the request is never used as a path.
		res, err := img.Process(data, a.cfg.UploadDir)
		if err != nil {
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}
		if _, err := a.st.AddImage(r.Context(), store.Image{ProductID: id, File: res.Stem, Width: res.Width, Height: res.Height}); err != nil {
			img.Remove(a.cfg.UploadDir, res.Stem, res.Width)
			errs = append(errs, fh.Filename+": could not save.")
		}
	}
	a.imagesResponse(w, r, id, errs)
}

func (a *App) adminImageAlt(w http.ResponseWriter, r *http.Request) {
	productID, _ := strconv.ParseInt(r.FormValue("product_id"), 10, 64)
	if err := a.st.UpdateImageAlt(r.Context(), pathID(r), strings.TrimSpace(r.FormValue("alt"))); err != nil {
		a.imagesResponse(w, r, productID, []string{"Could not save alt text."})
		return
	}
	a.imagesResponse(w, r, productID, nil)
}

func (a *App) adminImageMove(w http.ResponseWriter, r *http.Request) {
	productID, _ := strconv.ParseInt(r.FormValue("product_id"), 10, 64)
	if err := a.st.MoveImage(r.Context(), pathID(r), r.FormValue("dir") == "up"); err != nil {
		a.imagesResponse(w, r, productID, []string{"Could not reorder."})
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
