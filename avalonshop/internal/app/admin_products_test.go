package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
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
