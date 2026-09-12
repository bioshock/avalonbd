package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"avalonshop/internal/store"
	"avalonshop/internal/store/storetest"
)

func orderFixture(t *testing.T) (*store.Store, store.NewOrder, int64) {
	t.Helper()
	st := store.New(storetest.Pool(t))
	ctx := context.Background()
	pid := seedProduct(t, st, "honey", true, 650)
	p, _ := st.GetProduct(ctx, pid)
	vid := p.Variants[0].ID
	zid, _ := st.CreateZone(ctx, store.Zone{Name: "Rajshahi", Fee: 60, Active: true})
	return st, store.NewOrder{Name: "Ana", Phone: "01712345678", Email: "ana@example.com", Address: "Road 1", ZoneID: zid,
		Lines: []store.OrderLine{{VariantID: vid, Qty: 2}}}, vid
}

func stock(t *testing.T, st *store.Store, vid int64) int {
	t.Helper()
	cv, _ := st.VariantsForCart(context.Background(), []int64{vid})
	return cv[0].Stock
}

func TestPlaceOrderDecrementsAndSnapshots(t *testing.T) {
	st, in, vid := orderFixture(t)
	ctx := context.Background()
	o, err := st.PlaceOrder(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if o.Number != "AV-001001" || o.Subtotal != 1300 || o.DeliveryFee != 60 || o.Total != 1360 || o.Status != "new" || o.ZoneName != "Rajshahi" {
		t.Fatalf("order wrong: %+v", o.Order)
	}
	if len(o.Items) != 1 || o.Items[0].ProductName != "Product honey" || o.Items[0].VariantName != "250g" || o.Items[0].UnitPrice != 650 || o.Items[0].Qty != 2 {
		t.Fatalf("items wrong: %+v", o.Items)
	}
	if stock(t, st, vid) != 3 {
		t.Fatalf("stock = %d, want 3", stock(t, st, vid))
	}
	o2, _ := st.PlaceOrder(ctx, in)
	if o2.Number != "AV-001002" {
		t.Fatalf("second number %q", o2.Number)
	}
	got, err := st.GetOrderByNumber(ctx, "AV-001001")
	if err != nil || got.ID != o.ID || len(got.Items) != 1 {
		t.Fatalf("get by number: %v", err)
	}
	list, _ := st.ListOrders(ctx, "new", 10)
	if len(list) != 2 || list[0].ID != o2.ID {
		t.Fatalf("list newest first: %+v", list)
	}
}

func TestPlaceOrderOversell(t *testing.T) {
	st, in, vid := orderFixture(t)
	in.Lines[0].Qty = 6 // stock is 5
	_, err := st.PlaceOrder(context.Background(), in)
	var oos store.ErrOutOfStock
	if !errors.As(err, &oos) || oos.Available != 5 || oos.VariantID != vid {
		t.Fatalf("want ErrOutOfStock{Available:5}, got %v", err)
	}
	if stock(t, st, vid) != 5 {
		t.Fatal("stock changed on failed order")
	}
}

func TestPlaceOrderConcurrentLastUnit(t *testing.T) {
	st, in, vid := orderFixture(t)
	in.Lines[0].Qty = 5
	var wg sync.WaitGroup
	okCount := 0
	var mu sync.Mutex
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.PlaceOrder(context.Background(), in); err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if okCount != 1 || stock(t, st, vid) != 0 {
		t.Fatalf("ok=%d stock=%d", okCount, stock(t, st, vid))
	}
}

func TestStatusTransitionsAndCancelRestores(t *testing.T) {
	st, in, vid := orderFixture(t)
	ctx := context.Background()
	o, _ := st.PlaceOrder(ctx, in)
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "delivered"); !errors.Is(err, store.ErrTransition) {
		t.Fatalf("new→delivered should fail, got %v", err)
	}
	o2, err := st.UpdateOrderStatus(ctx, o.ID, "confirmed")
	if err != nil || o2.Status != "confirmed" {
		t.Fatal(err)
	}
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "shipped"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if stock(t, st, vid) != 5 {
		t.Fatalf("stock after cancel = %d", stock(t, st, vid))
	}
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "confirmed"); !errors.Is(err, store.ErrTransition) {
		t.Fatal("cancelled is terminal")
	}
	if err := st.SetAdminNote(ctx, o.ID, "called, wrong number"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetOrder(ctx, o.ID)
	if got.AdminNote != "called, wrong number" {
		t.Fatal("note lost")
	}
}

func TestCanTransitionTable(t *testing.T) {
	ok := [][2]string{{"new", "confirmed"}, {"new", "cancelled"}, {"confirmed", "shipped"}, {"confirmed", "cancelled"}, {"shipped", "delivered"}, {"shipped", "cancelled"}}
	bad := [][2]string{{"new", "shipped"}, {"new", "delivered"}, {"delivered", "cancelled"}, {"cancelled", "new"}, {"confirmed", "new"}, {"new", "bogus"}}
	for _, c := range ok {
		if !store.CanTransition(c[0], c[1]) {
			t.Errorf("%s→%s should be allowed", c[0], c[1])
		}
	}
	for _, c := range bad {
		if store.CanTransition(c[0], c[1]) {
			t.Errorf("%s→%s should be rejected", c[0], c[1])
		}
	}
}

func TestDashboard(t *testing.T) {
	st, in, _ := orderFixture(t)
	ctx := context.Background()
	st.PlaceOrder(ctx, in) // leaves stock at 3 (<= 5 → low)
	d, err := st.Dashboard(ctx)
	if err != nil || d.NewOrders != 1 || len(d.Recent) != 1 || len(d.LowStock) != 1 || d.LowStock[0].Stock != 3 {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestPlaceOrderDuplicateLinesMergeAndCancel(t *testing.T) {
	st, in, vid := orderFixture(t)
	ctx := context.Background()
	// Place order with duplicate variant lines: [A qty 2, A qty 1]
	in.Lines = []store.OrderLine{{VariantID: vid, Qty: 2}, {VariantID: vid, Qty: 1}}
	o, err := st.PlaceOrder(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// Order should have exactly one item with merged qty of 3
	if len(o.Items) != 1 || o.Items[0].Qty != 3 {
		t.Fatalf("want 1 item with qty 3, got %+v", o.Items)
	}
	// Stock should be 5 - 3 = 2
	if stock(t, st, vid) != 2 {
		t.Fatalf("stock = %d, want 2", stock(t, st, vid))
	}
	// Cancel the order
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "confirmed"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateOrderStatus(ctx, o.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	// Stock should be restored to 5
	if stock(t, st, vid) != 5 {
		t.Fatalf("stock after cancel = %d, want 5", stock(t, st, vid))
	}
}

func TestPlaceOrderInvalidZeroQty(t *testing.T) {
	st, in, vid := orderFixture(t)
	ctx := context.Background()
	// Try to place order with qty 0
	in.Lines = []store.OrderLine{{VariantID: vid, Qty: 0}}
	_, err := st.PlaceOrder(ctx, in)
	if err == nil {
		t.Fatal("want error for qty 0, got nil")
	}
	// Stock should be unchanged
	if stock(t, st, vid) != 5 {
		t.Fatalf("stock = %d, want 5", stock(t, st, vid))
	}
}
