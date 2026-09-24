package app

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func variantIDs(t *testing.T, a *App, slug string) (int64, int64) {
	t.Helper()
	p, err := a.st.GetProductBySlug(context.Background(), slug, false)
	if err != nil {
		t.Fatal(err)
	}
	return p.Variants[0].ID, p.Variants[1].ID
}

func cookieHeader(w *httptest.ResponseRecorder, name string) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestCartAddUpdateRemove(t *testing.T) {
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	form := strings.NewReader("variant_id=" + itoa(v1) + "&qty=2")
	w := do(t, a, "POST", "/cart/items", form)
	if w.Code != 303 || w.Header().Get("Location") != "/cart" {
		t.Fatalf("plain add: %d %s", w.Code, w.Header().Get("Location"))
	}
	cart := cookieHeader(w, "cart")
	if cart == "" {
		t.Fatal("no cart cookie set")
	}
	// second add of the same variant increments
	w = do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=1"), "Cookie", "cart="+cart)
	cart = cookieHeader(w, "cart")
	w = do(t, a, "GET", "/cart", nil, "Cookie", "cart="+cart)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Wild Forest Honey") || !strings.Contains(body, `value="3"`) || !strings.Contains(body, "৳ 1,950") {
		t.Fatalf("cart page: %d\n%s", w.Code, body)
	}
	if !strings.Contains(body, `content="noindex"`) {
		t.Fatal("cart must be noindex")
	}
	// HTMX add returns the drawer plus an out-of-band badge
	w = do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=1"), "Cookie", "cart="+cart, "HX-Request", "true")
	body = w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `id="cart-count" class="badge" hx-swap-oob="true">4<`) || !strings.Contains(body, `href="/checkout"`) {
		t.Fatalf("htmx add: %d\n%s", w.Code, body)
	}
	cart = cookieHeader(w, "cart")
	// set qty to 0 removes the line
	w = do(t, a, "POST", "/cart/items/"+itoa(v1), strings.NewReader("qty=0"), "Cookie", "cart="+cart, "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Your cart is empty") {
		t.Fatalf("remove: %d\n%s", w.Code, w.Body.String())
	}
	// unknown variant → flash + redirect
	w = do(t, a, "POST", "/cart/items", strings.NewReader("variant_id=999999&qty=1"))
	if w.Code != 303 || w.Header().Get("Location") != "/products" || cookieHeader(w, "flash") == "" {
		t.Fatalf("unknown variant: %d %s", w.Code, w.Header().Get("Location"))
	}
	// drawer GET
	w = do(t, a, "GET", "/cart/drawer", nil, "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Your cart is empty") {
		t.Fatalf("drawer: %d", w.Code)
	}
}

func TestCartDropsInactiveAndFlagsShort(t *testing.T) {
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	v1, v2 := variantIDs(t, a, slug)
	// v2 has stock 0: adding it is refused
	w := do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v2)+"&qty=1"))
	if w.Code != 303 || cookieHeader(w, "cart") != "" {
		t.Fatalf("sold-out variant should not be added: %d", w.Code)
	}
	w = do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=9"))
	cart := cookieHeader(w, "cart")
	w = do(t, a, "GET", "/cart", nil, "Cookie", "cart="+cart)
	if !strings.Contains(w.Body.String(), "Only 5 left") {
		t.Fatalf("short line not flagged:\n%s", w.Body.String())
	}
	// deactivate the product: the line disappears and the cookie is rewritten
	p, _ := st.GetProductBySlug(context.Background(), slug, false)
	p.Active = false
	st.UpdateProduct(context.Background(), p.Product, p.Variants)
	w = do(t, a, "GET", "/cart", nil, "Cookie", "cart="+cart)
	if !strings.Contains(w.Body.String(), "Your cart is empty") {
		t.Fatal("inactive product should vanish from cart")
	}
	if c := w.Result().Cookies(); len(c) == 0 || c[0].Name != "cart" || c[0].MaxAge != -1 {
		t.Fatalf("cart cookie should be cleared: %v", c)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
