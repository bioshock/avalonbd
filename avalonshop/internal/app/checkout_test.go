package app

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"avalonshop/internal/store"
)

func checkoutFixture(t *testing.T) (*App, *store.Store, string, int64) {
	t.Helper()
	a, st := newDBApp(t)
	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	zone, err := st.CreateZone(context.Background(), store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, a, "POST", "/cart/items", strings.NewReader("variant_id="+itoa(v1)+"&qty=2"))
	return a, st, cookieHeader(w, "cart"), zone
}

func checkoutBody(zone int64, phone string) string {
	v := url.Values{"name": {"Ana"}, "phone": {phone}, "email": {"ana@example.com"}, "address": {"House 1, Road 2, Rajshahi"}, "zone_id": {itoa(zone)}, "note": {"ring the bell"}}
	return v.Encode()
}

func TestCheckoutHappyPath(t *testing.T) {
	a, st, cart, zone := checkoutFixture(t)
	w := do(t, a, "GET", "/checkout", nil, "Cookie", "cart="+cart)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Rajshahi") || !strings.Contains(w.Body.String(), "৳ 1,300") {
		t.Fatalf("checkout page: %d\n%s", w.Code, w.Body.String())
	}
	w = do(t, a, "GET", "/checkout/totals?zone_id="+itoa(zone), nil, "Cookie", "cart="+cart, "HX-Request", "true")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "৳ 1,360") {
		t.Fatalf("totals partial: %d\n%s", w.Code, w.Body.String())
	}
	w = do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "+88 01712-345678")), "Cookie", "cart="+cart)
	loc := w.Header().Get("Location")
	if w.Code != 303 || !strings.HasPrefix(loc, "/orders/AV-001001?t=") {
		t.Fatalf("place order: %d %s\n%s", w.Code, loc, w.Body.String())
	}
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "cart" && c.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("cart cookie not cleared after order")
	}
	w = do(t, a, "GET", loc, nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Thank you, Ana") || !strings.Contains(body, "৳ 1,360") || !strings.Contains(body, `content="noindex"`) {
		t.Fatalf("order page: %d\n%s", w.Code, body)
	}
	if w := do(t, a, "GET", "/orders/AV-001001", nil); w.Code != 404 {
		t.Fatalf("order page without token should 404, got %d", w.Code)
	}
	if w := do(t, a, "GET", "/orders/AV-001001?t=0000000000000000", nil); w.Code != 404 {
		t.Fatalf("bad token should 404, got %d", w.Code)
	}
	o, err := st.GetOrderByNumber(context.Background(), "AV-001001")
	if err != nil || o.Phone != "01712345678" || o.Note != "ring the bell" || o.Total != 1360 {
		t.Fatalf("stored order: %+v %v", o.Order, err)
	}
	a.mail.Wait()
}

func TestCheckoutValidationAndOversell(t *testing.T) {
	a, _, cart, zone := checkoutFixture(t)
	w := do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "12345")), "Cookie", "cart="+cart)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "valid Bangladeshi mobile") {
		t.Fatalf("bad phone: %d", w.Code)
	}
	w = do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(0, "01712345678")), "Cookie", "cart="+cart)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Choose a delivery area") {
		t.Fatalf("missing zone: %d", w.Code)
	}
	// bump qty to 9 (stock is 5) then try to order
	slug := "wild-forest-honey"
	v1, _ := variantIDs(t, a, slug)
	w = do(t, a, "POST", "/cart/items/"+itoa(v1), strings.NewReader("qty=9"), "Cookie", "cart="+cart)
	cart = cookieHeader(w, "cart")
	w = do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "01712345678")), "Cookie", "cart="+cart)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Only 5 of") {
		t.Fatalf("oversell: %d\n%s", w.Code, w.Body.String())
	}
	if w := do(t, a, "GET", "/checkout", nil); w.Code != 303 || w.Header().Get("Location") != "/cart" {
		t.Fatalf("empty cart should redirect: %d", w.Code)
	}
}

// TestCheckoutInactiveZone covers the finding from the Task 13 review: a
// zone_id that names a real but inactive zone must 422 with the customer's
// other fields and cart intact, not silently drop the order to /cart.
func TestCheckoutInactiveZone(t *testing.T) {
	a, st, cart, _ := checkoutFixture(t)
	inactive, err := st.CreateZone(context.Background(), store.Zone{Name: "Sylhet", Fee: 80, Active: false})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(inactive, "01712345678")), "Cookie", "cart="+cart)
	body := w.Body.String()
	if w.Code != 422 || !strings.Contains(body, "no longer available") {
		t.Fatalf("inactive zone: %d\n%s", w.Code, body)
	}
	if !strings.Contains(body, "Ana") || !strings.Contains(body, "House 1, Road 2, Rajshahi") {
		t.Fatalf("submitted name/address should survive the re-render:\n%s", body)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "cart" && c.MaxAge == -1 {
			t.Fatal("cart cookie must not be cleared when the zone is rejected")
		}
	}
	orders, err := st.ListOrders(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 0 {
		t.Fatalf("no order should have been created, got %d", len(orders))
	}
}

// TestCheckoutTotalsNoJS covers decision 1: without the HX-Request header,
// GET /checkout/totals redirects to the full checkout page instead of
// returning a bare fragment.
func TestCheckoutTotalsNoJS(t *testing.T) {
	a, _, cart, zone := checkoutFixture(t)
	w := do(t, a, "GET", "/checkout/totals?zone_id="+itoa(zone), nil, "Cookie", "cart="+cart)
	if w.Code != 303 || w.Header().Get("Location") != "/checkout" {
		t.Fatalf("no-JS totals should redirect to /checkout: %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestCheckoutRateLimit(t *testing.T) {
	a, _, cart, zone := checkoutFixture(t)
	a.checkoutLimit = newLimiter(1, time.Hour)
	do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "bad")), "Cookie", "cart="+cart)
	w := do(t, a, "POST", "/checkout", strings.NewReader(checkoutBody(zone, "01712345678")), "Cookie", "cart="+cart)
	if w.Code != 429 {
		t.Fatalf("second attempt should be rate limited, got %d", w.Code)
	}
}

func TestPhoneAndEmail(t *testing.T) {
	ok := map[string]string{"01712345678": "01712345678", "+8801712345678": "01712345678", "01712 345 678": "01712345678", "8801912345678": "01912345678"}
	for in, want := range ok {
		if got, valid := normalizePhone(in); !valid || got != want {
			t.Errorf("normalizePhone(%q) = %q,%v", in, got, valid)
		}
	}
	for _, bad := range []string{"", "0171234567", "02712345678", "017123456789", "abc"} {
		if _, valid := normalizePhone(bad); valid {
			t.Errorf("normalizePhone(%q) should be invalid", bad)
		}
	}
	if !validEmail("a@b.co") || validEmail("nope") || validEmail("a b@c.d") || validEmail("a@b") {
		t.Error("validEmail wrong")
	}
}

// TestTotalsFor exercises the totals arithmetic directly with no App and no
// database, so it must pass even with TEST_DATABASE_URL unset.
func TestTotalsFor(t *testing.T) {
	zones := []store.Zone{{ID: 1, Name: "Dhaka", Fee: 0, Active: true}, {ID: 2, Name: "Rajshahi", Fee: 60, Active: true}}
	cases := []struct {
		name   string
		zoneID int64
		want   totalsView
	}{
		{"valid zone", 2, totalsView{Subtotal: 1300, Fee: 60, Total: 1360, HasZone: true}},
		{"unknown zone", 99, totalsView{Subtotal: 1300, Fee: 0, Total: 1300, HasZone: false}},
		{"zero zone", 0, totalsView{Subtotal: 1300, Fee: 0, Total: 1300, HasZone: false}},
	}
	for _, c := range cases {
		if got := totalsFor(1300, zones, c.zoneID); got != c.want {
			t.Errorf("%s: totalsFor(1300, zones, %d) = %+v, want %+v", c.name, c.zoneID, got, c.want)
		}
	}
}
