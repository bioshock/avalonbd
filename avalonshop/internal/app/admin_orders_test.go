package app

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"avalonshop/internal/mail"
	"avalonshop/internal/store"
)

func TestAdminOrdersFlow(t *testing.T) {
	a, st := newDBApp(t)
	adm := adminSession(t, a, st)
	ctx := context.Background()
	var logbuf bytes.Buffer
	a.mail, _ = mail.New("", "587", "", "", "shop@test.local", os.DirFS("../.."), slog.New(slog.NewTextHandler(&logbuf, nil)))

	slug := seedCatalog(t, st)
	v1, _ := variantIDs(t, a, slug)
	zone, _ := st.CreateZone(ctx, store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	o, err := st.PlaceOrder(ctx, store.NewOrder{Name: "Ana", Phone: "01712345678", Email: "ana@example.com", Address: "Road 1, Rajshahi", ZoneID: zone, Lines: []store.OrderLine{{VariantID: v1, Qty: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, a, "GET", "/admin/orders", nil, "Cookie", adm)
	if w.Code != 200 || !strings.Contains(w.Body.String(), o.Number) {
		t.Fatalf("list default (new): %d", w.Code)
	}
	if w := do(t, a, "GET", "/admin/orders?status=shipped", nil, "Cookie", adm); strings.Contains(w.Body.String(), o.Number) {
		t.Fatal("status filter not applied")
	}
	w = do(t, a, "GET", "/admin/orders/"+itoa(o.ID), nil, "Cookie", adm)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Ana") || !strings.Contains(body, `value="confirmed"`) || strings.Contains(body, `value="delivered"`) {
		t.Fatalf("detail should offer only allowed transitions: %d\n%s", w.Code, body)
	}
	w = do(t, a, "POST", "/admin/orders/"+itoa(o.ID)+"/status", strings.NewReader("status=delivered"), "Cookie", adm)
	if w.Code != 303 || !strings.Contains(cookieHeader(w, "flash"), "allowed") {
		t.Fatalf("bad transition should flash: %d %q", w.Code, cookieHeader(w, "flash"))
	}
	do(t, a, "POST", "/admin/orders/"+itoa(o.ID)+"/status", strings.NewReader("status=confirmed"), "Cookie", adm)
	do(t, a, "POST", "/admin/orders/"+itoa(o.ID)+"/status", strings.NewReader("status=shipped"), "Cookie", adm)
	a.mail.Wait()
	if !strings.Contains(logbuf.String(), "on its way") {
		t.Fatalf("shipped email not sent:\n%s", logbuf.String())
	}
	do(t, a, "POST", "/admin/orders/"+itoa(o.ID)+"/note", strings.NewReader("note=Left+with+neighbour"), "Cookie", adm)
	got, _ := st.GetOrder(ctx, o.ID)
	if got.Status != "shipped" || got.AdminNote != "Left with neighbour" {
		t.Fatalf("order state: %+v", got.Order)
	}
	if w := do(t, a, "GET", "/admin/orders?status=all", nil, "Cookie", adm); !strings.Contains(w.Body.String(), o.Number) {
		t.Fatal("all filter")
	}
	if w := do(t, a, "GET", "/admin/orders/999", nil, "Cookie", adm); w.Code != 404 {
		t.Fatal("missing order should 404")
	}
}
