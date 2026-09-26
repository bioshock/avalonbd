package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"avalonshop/internal/img"
	"avalonshop/internal/store"
)

func productForm(name string, variants ...[]string) url.Values {
	v := url.Values{"name": {name}, "description": {"A fine product"}, "meta_description": {"Buy " + name}, "active": {"on"}}
	for _, row := range variants { // id, name, sku, price, stock
		v.Add("variant_id", row[0])
		v.Add("variant_name", row[1])
		v.Add("variant_sku", row[2])
		v.Add("variant_price", row[3])
		v.Add("variant_stock", row[4])
	}
	return v
}

func pngUpload(t *testing.T, w, h int) (io.Reader, string) {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.NRGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, m)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("images", "photo.png")
	fw.Write(pngBuf.Bytes())
	mw.Close()
	return &body, mw.FormDataContentType()
}

func TestAdminProductCreateEditDelete(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	if w := do(t, a, "GET", "/admin/products/new", nil, "Cookie", adm); w.Code != 200 || !strings.Contains(w.Body.String(), `name="variant_name"`) {
		t.Fatalf("new form: %d", w.Code)
	}
	form := productForm("Wild Forest Honey", []string{"", "500g", "HNY-500", "650", "5"}, []string{"", "", "", "", ""}, []string{"", "1kg", "", "1200", "2"})
	w := do(t, a, "POST", "/admin/products/new", strings.NewReader(form.Encode()), "Cookie", adm)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/admin/products/") {
		t.Fatalf("create: %d %s\n%s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	p, err := st.GetProductBySlug(ctx, "wild-forest-honey", false)
	if err != nil || len(p.Variants) != 2 || p.Variants[0].SKU == nil || *p.Variants[0].SKU != "HNY-500" || p.Variants[1].SKU != nil {
		t.Fatalf("created product wrong: %+v %v", p, err)
	}
	// blank variant rows are skipped; a product needs at least one
	w = do(t, a, "POST", "/admin/products/new", strings.NewReader(productForm("Empty").Encode()), "Cookie", adm)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "at least one variant") {
		t.Fatalf("no variants: %d", w.Code)
	}
	// duplicate name gets a -2 slug
	do(t, a, "POST", "/admin/products/new", strings.NewReader(productForm("Wild Forest Honey", []string{"", "x", "", "1", "1"}).Encode()), "Cookie", adm)
	if _, err := st.GetProductBySlug(ctx, "wild-forest-honey-2", false); err != nil {
		t.Fatal("expected -2 slug")
	}
	// edit: reprice first, drop second, add third
	edit := productForm("Wild Honey", []string{itoa(p.Variants[0].ID), "500 g", "HNY-500", "700", "4"}, []string{"", "2kg", "", "2400", "1"})
	edit.Set("slug", "wild honey")
	w = do(t, a, "POST", "/admin/products/"+itoa(p.ID), strings.NewReader(edit.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("edit: %d\n%s", w.Code, w.Body.String())
	}
	p2, _ := st.GetProduct(ctx, p.ID)
	if p2.Name != "Wild Honey" || p2.Slug != "wild-honey" || len(p2.Variants) != 2 || p2.Variants[0].Price != 700 || p2.Variants[1].Name != "2kg" {
		t.Fatalf("edit lost: %+v", p2)
	}
	if w := do(t, a, "GET", "/admin/products", nil, "Cookie", adm); w.Code != 200 || !strings.Contains(w.Body.String(), "Wild Honey") {
		t.Fatalf("list: %d", w.Code)
	}
	if w := do(t, a, "GET", "/admin/products/variant-row", nil, "Cookie", adm, "HX-Request", "true"); w.Code != 200 || !strings.Contains(w.Body.String(), `name="variant_name"`) {
		t.Fatalf("variant row: %d", w.Code)
	}
	w = do(t, a, "POST", "/admin/products/"+itoa(p.ID)+"/delete", nil, "Cookie", adm)
	if w.Code != 303 || w.Header().Get("Location") != "/admin/products" {
		t.Fatalf("delete: %d", w.Code)
	}
	if _, err := st.GetProduct(ctx, p.ID); err == nil {
		t.Fatal("product still exists")
	}
}

// TestAdminProductVariantSortIsHonoured is the C1 fix: decision 6 added a
// variant_sort field so an admin can reorder variants without retyping them.
// parseProductForm read it correctly, but syncVariants (internal/store) was
// writing the loop index instead of Variant.Sort, so the typed order was
// silently discarded and storage order always matched submitted row order.
// Posting three variants out of numeric-sort order must come back in sort
// order, not row order.
func TestAdminProductVariantSortIsHonoured(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	v := url.Values{"name": {"Sorted"}, "description": {"d"}, "meta_description": {"d"}, "active": {"on"}}
	rows := []struct{ name, sort string }{{"A", "30"}, {"B", "20"}, {"C", "10"}}
	for _, row := range rows {
		v.Add("variant_id", "")
		v.Add("variant_name", row.name)
		v.Add("variant_sku", "")
		v.Add("variant_price", "1")
		v.Add("variant_stock", "1")
		v.Add("variant_sort", row.sort)
	}
	w := do(t, a, "POST", "/admin/products/new", strings.NewReader(v.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("create: %d\n%s", w.Code, w.Body.String())
	}
	p, err := st.GetProductBySlug(ctx, "sorted", false)
	if err != nil || len(p.Variants) != 3 {
		t.Fatalf("product: %+v %v", p, err)
	}
	got := []string{p.Variants[0].Name, p.Variants[1].Name, p.Variants[2].Name}
	if got[0] != "C" || got[1] != "B" || got[2] != "A" {
		t.Fatalf("variant_sort not honoured: stored order %v, want [C B A] (sort 10,20,30)", got)
	}
}

// TestAdminProductSaveDoesNotResetStockUnderConcurrentOrder is the C2 fix. It
// reproduces the reviewer's exact measured sequence: stock 10 -> a customer
// orders 3 (stock 7) -> the admin saves the edit form they already had open
// before the order landed, changing only the product name -> before the fix,
// syncVariants wrote the rendered stock (10) back absolutely, and a later
// cancel then credited the order's qty on top of that, reaching 13 units of
// stock that don't exist. After the fix the admin's save must leave the
// decremented stock alone, and cancelling must land back on exactly 10.
func TestAdminProductSaveDoesNotResetStockUnderConcurrentOrder(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()

	id, err := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "Original Name", Active: true}, []store.Variant{{Name: "x", Price: 100, Stock: 10}})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := st.GetProduct(ctx, id)
	variantID := p.Variants[0].ID

	// Admin opens the edit form while stock is still 10 — this is the
	// rendered value the hidden variant_stock_was field carries.
	w := do(t, a, "GET", "/admin/products/"+itoa(id), nil, "Cookie", adm)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `name="variant_stock_was" value="10"`) {
		t.Fatalf("edit form should carry the rendered stock as variant_stock_was: %d\n%s", w.Code, w.Body.String())
	}

	// Customer orders 3 while the admin's tab is still open.
	zone, _ := st.CreateZone(ctx, store.Zone{Name: "Z", Fee: 0, Active: true})
	o, err := st.PlaceOrder(ctx, store.NewOrder{Name: "C", Phone: "01712345678", Email: "c@example.com", Address: "somewhere far enough", ZoneID: zone, Lines: []store.OrderLine{{VariantID: variantID, Qty: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := st.GetProduct(ctx, id); p.Variants[0].Stock != 7 {
		t.Fatalf("stock after order should be 7, got %d", p.Variants[0].Stock)
	}

	// Admin submits the form they already had open — same stock (10) as
	// rendered, only the name changed. This must NOT reset stock to 10.
	form := productForm("Renamed Product", []string{itoa(variantID), "x", "", "100", "10"})
	form.Add("variant_stock_was", "10")
	w = do(t, a, "POST", "/admin/products/"+itoa(id), strings.NewReader(form.Encode()), "Cookie", adm)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("save: %d\n%s", w.Code, w.Body.String())
	}
	p2, _ := st.GetProduct(ctx, id)
	if p2.Name != "Renamed Product" {
		t.Fatalf("name should have been updated: %+v", p2)
	}
	if p2.Variants[0].Stock != 7 {
		t.Fatalf("admin save must not reset stock: got %d, want 7 (the current, decremented value)", p2.Variants[0].Stock)
	}

	// Cancelling the order restores 3 onto the correct base (7), not onto a
	// re-inflated 10.
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	p3, _ := st.GetProduct(ctx, id)
	if p3.Variants[0].Stock != 10 {
		t.Fatalf("stock after cancel should be 10 (7+3), got %d — the shop must not believe it has more stock than it does", p3.Variants[0].Stock)
	}
}

func TestAdminProductImages(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})
	body, ct := pngUpload(t, 700, 500)
	w := do(t, a, "POST", "/admin/products/"+itoa(id)+"/images", body, "Cookie", adm, "Content-Type", ct, "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<img src="/media/`) {
		t.Fatalf("upload: %d\n%s", w.Code, w.Body.String())
	}
	imgs, _ := st.ListImages(ctx, id)
	if len(imgs) != 1 || imgs[0].Width != 700 || imgs[0].Height != 500 {
		t.Fatalf("image row: %+v", imgs)
	}
	for _, wd := range img.WidthsFor(700) {
		if _, err := os.Stat(filepath.Join(a.cfg.UploadDir, img.Filename(imgs[0].File, wd))); err != nil {
			t.Fatalf("variant %d missing: %v", wd, err)
		}
	}
	// a non-image is reported, not fatal
	var bad bytes.Buffer
	mw := multipart.NewWriter(&bad)
	fw, _ := mw.CreateFormFile("images", "notes.txt")
	fw.Write([]byte("hello, not an image"))
	mw.Close()
	w = do(t, a, "POST", "/admin/products/"+itoa(id)+"/images", &bad, "Cookie", adm, "Content-Type", mw.FormDataContentType(), "HX-Request", "true")
	// Task 8 changed img.ErrUnsupported to the spec's exact wording, and this
	// handler surfaces err.Error(), so we assert on the real message rather
	// than the brief's stale "unsupported image type" (task-17-decisions #1).
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Please upload JPEG, PNG, WebP or GIF") {
		t.Fatalf("bad upload: %d\n%s", w.Code, w.Body.String())
	}
	// second image, move it up, set alt, delete first
	body2, ct2 := pngUpload(t, 500, 500)
	do(t, a, "POST", "/admin/products/"+itoa(id)+"/images", body2, "Cookie", adm, "Content-Type", ct2)
	imgs, _ = st.ListImages(ctx, id)
	second := imgs[1]
	w = do(t, a, "POST", "/admin/images/"+itoa(second.ID)+"/move", strings.NewReader("dir=up&product_id="+itoa(id)), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("move: %d", w.Code)
	}
	imgs, _ = st.ListImages(ctx, id)
	if imgs[0].ID != second.ID {
		t.Fatal("move up failed")
	}
	do(t, a, "POST", "/admin/images/"+itoa(second.ID), strings.NewReader("alt=Jar+of+honey&product_id="+itoa(id)), "Cookie", adm)
	imgs, _ = st.ListImages(ctx, id)
	if imgs[0].Alt != "Jar of honey" {
		t.Fatal("alt not saved")
	}
	first := imgs[1]
	do(t, a, "POST", "/admin/images/"+itoa(first.ID)+"/delete", nil, "Cookie", adm)
	if _, err := os.Stat(filepath.Join(a.cfg.UploadDir, img.Filename(first.File, 400))); !os.IsNotExist(err) {
		t.Fatal("deleted image files remain")
	}
	// deleting the product removes remaining files
	do(t, a, "POST", "/admin/products/"+itoa(id)+"/delete", nil, "Cookie", adm)
	if _, err := os.Stat(filepath.Join(a.cfg.UploadDir, img.Filename(second.File, 400))); !os.IsNotExist(err) {
		t.Fatal("product delete left files")
	}
}

// TestAdminProductImagePerFileSizeCap test-locks spec §8's per-file 10 MB
// cap: it works today, but nothing failed when the review deleted the check
// entirely (task-17-review Minor 11).
func TestAdminProductImagePerFileSizeCap(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("images", "big.png")
	fw.Write(bytes.Repeat([]byte{0}, maxUploadBytes+1)) // one byte over the 10 MB per-file cap
	mw.Close()

	w := do(t, a, "POST", "/admin/products/"+itoa(id)+"/images", &body, "Cookie", adm, "Content-Type", mw.FormDataContentType(), "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "over 10 MB") {
		t.Fatalf("oversized file: %d\n%s", w.Code, w.Body.String())
	}
	if imgs, _ := st.ListImages(ctx, id); len(imgs) != 0 {
		t.Fatalf("a file over the per-file cap must not be processed: %d rows", len(imgs))
	}
}

// TestAdminProductImageFileCountCap test-locks spec §8's 10-file cap: also
// verified working but unpinned (task-17-review Minor 11).
func TestAdminProductImageFileCountCap(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	m := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, m)
	for i := 0; i < 14; i++ {
		fw, _ := mw.CreateFormFile("images", "p.png")
		fw.Write(pngBuf.Bytes())
	}
	mw.Close()

	w := do(t, a, "POST", "/admin/products/"+itoa(id)+"/images", &body, "Cookie", adm, "Content-Type", mw.FormDataContentType(), "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Only the first 10 files were processed.") {
		t.Fatalf("14 files: %d\n%s", w.Code, w.Body.String())
	}
	imgs, _ := st.ListImages(ctx, id)
	if len(imgs) != 10 {
		t.Fatalf("want exactly 10 rows for 14 uploaded files, got %d", len(imgs))
	}
}

// TestAdminImageMoveRejectsInvalidDirection is the fix for review Minor 7:
// any dir other than exactly "up" or "down" used to fall through to "down".
func TestAdminImageMoveRejectsInvalidDirection(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})
	first, _ := st.AddImage(ctx, store.Image{ProductID: id, File: "a", Width: 10, Height: 10})
	st.AddImage(ctx, store.Image{ProductID: id, File: "b", Width: 10, Height: 10})

	w := do(t, a, "POST", "/admin/images/"+itoa(first)+"/move", strings.NewReader("dir=sideways&product_id="+itoa(id)), "Cookie", adm)
	if w.Code != 404 {
		t.Fatalf("invalid dir should be rejected, got %d", w.Code)
	}
	imgs, _ := st.ListImages(ctx, id)
	if imgs[0].ID != first {
		t.Fatalf("order must be unchanged by a rejected move: %+v", imgs)
	}
}

// TestAdminImageProductIDIsDerivedNotTrusted is the fix for review Minor 6:
// adminImageAlt and adminImageMove used to trust product_id from the form.
// Posting a wrong (or missing) product_id must not change which product the
// response redirects to, or which product's list gets re-rendered.
func TestAdminImageProductIDIsDerivedNotTrusted(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	id1, _ := st.CreateProduct(ctx, store.Product{Slug: "p1", Name: "P1", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})
	id2, _ := st.CreateProduct(ctx, store.Product{Slug: "p2", Name: "P2", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 1}})
	imgID, _ := st.AddImage(ctx, store.Image{ProductID: id1, File: "a", Width: 10, Height: 10})

	// Claiming the image belongs to product 2: the redirect must still go to
	// product 1, the image's real owner, not the claimed product_id.
	w := do(t, a, "POST", "/admin/images/"+itoa(imgID), strings.NewReader("alt=x&product_id="+itoa(id2)), "Cookie", adm)
	if w.Code != 303 || w.Header().Get("Location") != "/admin/products/"+itoa(id1) {
		t.Fatalf("alt should redirect to the image's real product (%d), got %d %q", id1, w.Code, w.Header().Get("Location"))
	}

	// Omitting product_id entirely must not redirect to /admin/products/0.
	w = do(t, a, "POST", "/admin/images/"+itoa(imgID)+"/move", strings.NewReader("dir=up"), "Cookie", adm)
	if w.Code != 303 || w.Header().Get("Location") != "/admin/products/"+itoa(id1) {
		t.Fatalf("move with no product_id should still redirect to %d, got %d %q", id1, w.Code, w.Header().Get("Location"))
	}
}

func TestAdminProductDeleteRefusedWhenOrdered(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	id, _ := st.CreateProduct(ctx, store.Product{Slug: "p", Name: "P", Active: true}, []store.Variant{{Name: "x", Price: 1, Stock: 5}})
	p, _ := st.GetProduct(ctx, id)
	zone, _ := st.CreateZone(ctx, store.Zone{Name: "Z", Fee: 0, Active: true})
	if _, err := st.PlaceOrder(ctx, store.NewOrder{Name: "A", Phone: "01712345678", Email: "a@b.co", Address: "somewhere far", ZoneID: zone, Lines: []store.OrderLine{{VariantID: p.Variants[0].ID, Qty: 1}}}); err != nil {
		t.Fatal(err)
	}
	w := do(t, a, "POST", "/admin/products/"+itoa(id)+"/delete", nil, "Cookie", adm)
	if w.Code != 303 || !strings.Contains(cookieHeader(w, "flash"), "orders") {
		t.Fatalf("should refuse: %d %q", w.Code, cookieHeader(w, "flash"))
	}
}

// ---- Task 21: product slug history and 301 redirects ----

// createSlugTestProduct creates a one-variant active product through the real
// admin form (so the created row looks exactly like an admin-authored one)
// and returns it loaded back from the store.
func createSlugTestProduct(t *testing.T, a *App, adm, name, slug string) store.ProductFull {
	t.Helper()
	form := productForm(name, []string{"", "Default", "", "100", "5"})
	form.Set("slug", slug)
	w := do(t, a, "POST", "/admin/products/new", strings.NewReader(form.Encode()), "Cookie", adm)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create %q: %d\n%s", slug, w.Code, w.Body.String())
	}
	p, err := a.st.GetProductBySlug(context.Background(), slug, false)
	if err != nil {
		t.Fatalf("fetch created product %q: %v", slug, err)
	}
	return p
}

// renameSlugTestProduct posts an update through the real admin handler,
// changing only the slug, so the RecordOldSlug wiring in
// adminProductUpdate runs exactly as it would for a real rename. Returns the
// product reloaded after the rename.
func renameSlugTestProduct(t *testing.T, a *App, adm string, p store.ProductFull, newSlug string) store.ProductFull {
	t.Helper()
	v := p.Variants[0]
	form := productForm(p.Name, []string{itoa(v.ID), v.Name, "", itoa(int64(v.Price)), itoa(int64(v.Stock))})
	form.Set("slug", newSlug)
	w := do(t, a, "POST", "/admin/products/"+itoa(p.ID), strings.NewReader(form.Encode()), "Cookie", adm)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("rename %q -> %q: %d\n%s", p.Slug, newSlug, w.Code, w.Body.String())
	}
	got, err := a.st.GetProductBySlug(context.Background(), newSlug, false)
	if err != nil {
		t.Fatalf("fetch renamed product %q: %v", newSlug, err)
	}
	return got
}

// deactivateSlugTestProduct posts an update through the real admin handler
// that unchecks "active" while leaving the slug untouched.
func deactivateSlugTestProduct(t *testing.T, a *App, adm string, p store.ProductFull) {
	t.Helper()
	v := p.Variants[0]
	form := productForm(p.Name, []string{itoa(v.ID), v.Name, "", itoa(int64(v.Price)), itoa(int64(v.Stock))})
	form.Set("slug", p.Slug)
	form.Del("active")
	w := do(t, a, "POST", "/admin/products/"+itoa(p.ID), strings.NewReader(form.Encode()), "Cookie", adm)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("deactivate %q: %d\n%s", p.Slug, w.Code, w.Body.String())
	}
}

// TestAdminProductRenameRedirects301 is task-21-brief's core behaviour:
// renaming a product through the admin form records the old slug, and
// GET /products/{old-slug} then 301s to the product's current URL with the
// query string preserved, instead of 404ing search ranking and shared links
// into oblivion.
func TestAdminProductRenameRedirects301(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	createSlugTestProduct(t, a, adm, "Wild Honey", "wild-honey")
	p, err := st.GetProductBySlug(context.Background(), "wild-honey", false)
	if err != nil {
		t.Fatal(err)
	}
	renameSlugTestProduct(t, a, adm, p, "wild-forest-honey")

	w := do(t, a, "GET", "/products/wild-honey", nil)
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/products/wild-forest-honey" {
		t.Fatalf("old slug: got %d %q, want 301 to /products/wild-forest-honey", w.Code, w.Header().Get("Location"))
	}

	w = do(t, a, "GET", "/products/wild-honey?ref=newsletter&utm_source=x", nil)
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/products/wild-forest-honey?ref=newsletter&utm_source=x" {
		t.Fatalf("query string must survive the redirect: got %d %q", w.Code, w.Header().Get("Location"))
	}

	if w := do(t, a, "GET", "/products/wild-forest-honey", nil); w.Code != http.StatusOK {
		t.Fatalf("current slug should serve 200 directly, got %d", w.Code)
	}
}

// TestAdminProductRenameChainRedirectsToLatest is the chain case: rename
// A->B->C must send both the A and B URLs to C, never to each other, and
// never through an intermediate 404.
func TestAdminProductRenameChainRedirectsToLatest(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	createSlugTestProduct(t, a, adm, "Chain Product", "a")
	p, err := st.GetProductBySlug(context.Background(), "a", false)
	if err != nil {
		t.Fatal(err)
	}
	p = renameSlugTestProduct(t, a, adm, p, "b")
	renameSlugTestProduct(t, a, adm, p, "c")

	for _, old := range []string{"a", "b"} {
		w := do(t, a, "GET", "/products/"+old, nil)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/products/c" {
			t.Fatalf("%q: got %d %q, want 301 to /products/c", old, w.Code, w.Header().Get("Location"))
		}
	}
}

// TestAdminProductUnknownSlugStill404s asserts a slug that was never live and
// never retired still 404s (it must not be mistaken for a redirect target).
func TestAdminProductUnknownSlugStill404s(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	createSlugTestProduct(t, a, adm, "Real Product", "real-product")

	if w := do(t, a, "GET", "/products/never-existed", nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown slug: got %d, want 404", w.Code)
	}
}

// TestAdminProductRenameAwayAndBackServes200 is the loop-prevention case
// (task-21-brief #trap 2): renaming a product back to a slug it used to have
// must serve 200 directly from the live products row, never a 301 (which
// would otherwise redirect the URL to itself).
func TestAdminProductRenameAwayAndBackServes200(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	createSlugTestProduct(t, a, adm, "Loop Product", "loop-a")
	p, err := st.GetProductBySlug(context.Background(), "loop-a", false)
	if err != nil {
		t.Fatal(err)
	}
	p = renameSlugTestProduct(t, a, adm, p, "loop-b")
	renameSlugTestProduct(t, a, adm, p, "loop-a")

	w := do(t, a, "GET", "/products/loop-a", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("rename-away-and-back must serve 200 directly, not a redirect loop: got %d Location=%q", w.Code, w.Header().Get("Location"))
	}
}

// TestAdminProductOldSlugOfDeactivatedProduct404s is task-21-decisions #1,
// the trap most likely to pass silently: an old slug whose product has since
// been deactivated must 404, not 301 to a page that will itself then 404.
func TestAdminProductOldSlugOfDeactivatedProduct404s(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	createSlugTestProduct(t, a, adm, "Seasonal Item", "seasonal-old")
	p, err := st.GetProductBySlug(context.Background(), "seasonal-old", false)
	if err != nil {
		t.Fatal(err)
	}
	p = renameSlugTestProduct(t, a, adm, p, "seasonal-new")
	deactivateSlugTestProduct(t, a, adm, p)

	w := do(t, a, "GET", "/products/seasonal-old", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("old slug of a deactivated product: got %d Location=%q, want 404", w.Code, w.Header().Get("Location"))
	}
	// The current slug of the deactivated product must also still 404 —
	// unchanged behaviour, asserted here so a broken guard can't be masked by
	// this test only ever checking the OLD slug.
	if w := do(t, a, "GET", "/products/seasonal-new", nil); w.Code != http.StatusNotFound {
		t.Fatalf("current slug of a deactivated product: got %d, want 404", w.Code)
	}
}

func TestAdminProductTaglinePromoRegularPrice(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	form := productForm("Essential Spice Trio", []string{"", "3 × 100g", "AV-TRIO-300", "499", "5"})
	form.Set("tagline", "Three essential kitchen ingredients.")
	form.Set("promo", "on")
	form.Add("variant_regular_price", "550")
	w := do(t, a, "POST", "/admin/products/new", strings.NewReader(form.Encode()), "Cookie", adm)
	if w.Code != 303 {
		t.Fatalf("create: %d\n%s", w.Code, w.Body.String())
	}
	p, err := st.GetProductBySlug(ctx, "essential-spice-trio", false)
	if err != nil || p.Tagline != "Three essential kitchen ingredients." || !p.Promo || p.Variants[0].RegularPrice == nil || *p.Variants[0].RegularPrice != 550 {
		t.Fatalf("fields lost: %+v %v", p, err)
	}
	// The edit form shows them back.
	body := do(t, a, "GET", "/admin/products/"+itoa(p.ID), nil, "Cookie", adm).Body.String()
	for _, want := range []string{`name="tagline" value="Three essential kitchen ingredients."`, `name="promo" checked`, `name="variant_regular_price" min="0" value="550"`} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form missing %q", want)
		}
	}
	// Blank regular price clears it; a non-number is rejected.
	edit := productForm("Essential Spice Trio", []string{itoa(p.Variants[0].ID), "3 × 100g", "AV-TRIO-300", "499", "5"})
	edit.Add("variant_regular_price", "")
	if w := do(t, a, "POST", "/admin/products/"+itoa(p.ID), strings.NewReader(edit.Encode()), "Cookie", adm); w.Code != 303 {
		t.Fatalf("edit: %d", w.Code)
	}
	if p, _ = st.GetProduct(ctx, p.ID); p.Variants[0].RegularPrice != nil || p.Promo {
		t.Fatalf("blank regular price should clear it and unchecked promo should clear promo: %+v", p)
	}
	edit.Set("variant_regular_price", "abc")
	if w := do(t, a, "POST", "/admin/products/"+itoa(p.ID), strings.NewReader(edit.Encode()), "Cookie", adm); w.Code != 422 || !strings.Contains(w.Body.String(), "whole numbers") {
		t.Fatalf("bad regular price: %d", w.Code)
	}
}

func TestValidateProduct(t *testing.T) {
	neg := -1
	p := store.Product{Name: "  Onion Powder ", Slug: ""}
	errs := validateProduct(&p, []store.Variant{{Name: "100g", Price: -5, Stock: -1, RegularPrice: &neg}, {Name: " ", Price: 1}})
	if p.Name != "Onion Powder" || p.Slug != "onion-powder" {
		t.Fatalf("normalize: %+v", p)
	}
	for _, k := range []string{"variants[0].price", "variants[0].stock", "variants[0].regular_price", "variants[1].name"} {
		if errs[k] == "" {
			t.Errorf("missing error %s in %v", k, errs)
		}
	}
	if errs := validateProduct(&store.Product{Name: ""}, nil); errs["name"] == "" || errs["variants"] == "" {
		t.Fatalf("empty product: %v", errs)
	}
	ok := store.Product{Name: "X"}
	if errs := validateProduct(&ok, []store.Variant{{Name: "a", Price: 0}}); len(errs) != 0 {
		t.Fatalf("valid product flagged: %v", errs)
	}
}
