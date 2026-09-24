package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"avalonshop/internal/store"
)

type checkoutForm struct {
	Name, Phone, Email, Address, Note string
	ZoneID                            int64
	Errors                            map[string]string
}

type totalsView struct {
	Subtotal, Fee, Total int
	HasZone              bool
}

type checkoutData struct {
	Totals totalsView
	Cart   cartView
	Zones  []store.Zone
	Form   checkoutForm
	Error  string
}

// normalizePhone accepts Bangladeshi mobile numbers in any common spelling and
// returns the 11-digit local form (01XXXXXXXXX).
func normalizePhone(s string) (string, bool) {
	var d strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			d.WriteRune(r)
		}
	}
	p := d.String()
	if len(p) == 13 && strings.HasPrefix(p, "88") {
		p = p[2:]
	}
	if len(p) == 11 && p[0] == '0' && p[1] == '1' && p[2] >= '3' && p[2] <= '9' {
		return p, true
	}
	return "", false
}

func validEmail(s string) bool {
	at := strings.LastIndex(s, "@")
	return len(s) >= 6 && len(s) <= 254 && at > 0 && strings.Contains(s[at:], ".") && !strings.ContainsAny(s, " \t\n\r")
}

func parseCheckout(r *http.Request) checkoutForm {
	f := checkoutForm{
		Name: strings.TrimSpace(r.FormValue("name")), Phone: strings.TrimSpace(r.FormValue("phone")),
		Email: strings.TrimSpace(strings.ToLower(r.FormValue("email"))), Address: strings.TrimSpace(r.FormValue("address")),
		Note: strings.TrimSpace(r.FormValue("note")), Errors: map[string]string{},
	}
	f.ZoneID, _ = strconv.ParseInt(r.FormValue("zone_id"), 10, 64)
	if l := utf8.RuneCountInString(f.Name); l < 1 || l > 100 {
		f.Errors["name"] = "Please enter your name."
	}
	if p, ok := normalizePhone(f.Phone); ok {
		f.Phone = p
	} else {
		f.Errors["phone"] = "Enter a valid Bangladeshi mobile number, e.g. 01712 345678."
	}
	if !validEmail(f.Email) {
		f.Errors["email"] = "Enter a valid email address."
	}
	if l := utf8.RuneCountInString(f.Address); l < 10 || l > 500 {
		f.Errors["address"] = "Please enter your full delivery address (at least 10 characters)."
	}
	if f.ZoneID == 0 {
		f.Errors["zone_id"] = "Choose a delivery area."
	}
	if utf8.RuneCountInString(f.Note) > 500 {
		f.Errors["note"] = "Note is too long (500 characters max)."
	}
	return f
}

func feeFor(zones []store.Zone, id int64) (int, bool) {
	for _, z := range zones {
		if z.ID == id {
			return z.Fee, true
		}
	}
	return 0, false
}

// totalsFor builds the totals view for a cart subtotal and the chosen zone. It
// is pure — no App, no context, no database — so the arithmetic can be unit
// tested without one.
func totalsFor(subtotal int, zones []store.Zone, zoneID int64) totalsView {
	fee, has := feeFor(zones, zoneID)
	return totalsView{Subtotal: subtotal, Fee: fee, Total: subtotal + fee, HasZone: has}
}

// zoneTotals loads the active zones and the totals for a cart subtotal and chosen zone.
func (a *App) zoneTotals(ctx context.Context, subtotal int, zoneID int64) ([]store.Zone, totalsView, error) {
	zones, err := a.st.ListZones(ctx, true)
	if err != nil {
		return nil, totalsView{}, err
	}
	return zones, totalsFor(subtotal, zones, zoneID), nil
}

// checkoutView renders the checkout page; redirects to /cart when the cart is empty.
func (a *App) checkoutView(w http.ResponseWriter, r *http.Request, f checkoutForm, status int, errMsg string) {
	v, kept, err := a.loadCart(w, r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(kept) == 0 {
		a.setFlash(w, "Your cart is empty.")
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	}
	zones, totals, err := a.zoneTotals(r.Context(), v.Subtotal, f.ZoneID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderStatus(w, r, status, "store/checkout.html", page{Title: "Checkout", NoIndex: true, Data: checkoutData{
		Totals: totals, Cart: v, Zones: zones, Form: f, Error: errMsg,
	}})
}

func (a *App) checkoutGet(w http.ResponseWriter, r *http.Request) {
	f := checkoutForm{Errors: map[string]string{}}
	if u := a.currentUser(r); u != nil {
		f.Name, f.Phone, f.Email, f.Address = u.Name, u.Phone, u.Email, u.Address
	}
	if zones, err := a.st.ListZones(r.Context(), true); err == nil && len(zones) == 1 {
		f.ZoneID = zones[0].ID
	}
	a.checkoutView(w, r, f, http.StatusOK, "")
}

// checkoutTotals renders the totals partial for the HTMX zone picker. Without
// the HX-Request header it redirects to the full checkout page, so the zone
// picker still works with JS disabled.
func (a *App) checkoutTotals(w http.ResponseWriter, r *http.Request) {
	if !isHTMX(r) {
		http.Redirect(w, r, "/checkout", http.StatusSeeOther)
		return
	}
	zoneID, _ := strconv.ParseInt(r.URL.Query().Get("zone_id"), 10, 64)
	v, _, err := a.buildCart(r.Context(), a.cartLines(r))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	_, totals, err := a.zoneTotals(r.Context(), v.Subtotal, zoneID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderPartial(w, "checkout_totals.html", totals)
}

func (a *App) checkoutPost(w http.ResponseWriter, r *http.Request) {
	f := parseCheckout(r)
	if !a.checkoutLimit.Allow(clientIP(r)) {
		a.checkoutView(w, r, f, http.StatusTooManyRequests, "Too many attempts. Please try again in an hour.")
		return
	}
	if len(f.Errors) > 0 {
		a.checkoutView(w, r, f, http.StatusUnprocessableEntity, "Please fix the highlighted fields.")
		return
	}
	v, kept, err := a.loadCart(w, r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(kept) == 0 {
		a.setFlash(w, "Your cart is empty.")
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	}
	// Confirm the chosen zone is still active before placing the order: a zone
	// deactivated between page render and submit must re-render the form (with
	// the customer's other fields intact) rather than silently drop to /cart.
	if _, totals, err := a.zoneTotals(r.Context(), v.Subtotal, f.ZoneID); err != nil {
		a.serverError(w, r, err)
		return
	} else if !totals.HasZone {
		f.Errors["zone_id"] = "This delivery area is no longer available. Please choose another."
		a.checkoutView(w, r, f, http.StatusUnprocessableEntity, "Please fix the highlighted fields.")
		return
	}
	in := store.NewOrder{Name: f.Name, Phone: f.Phone, Email: f.Email, Address: f.Address, Note: f.Note, ZoneID: f.ZoneID}
	for _, l := range kept {
		in.Lines = append(in.Lines, store.OrderLine{VariantID: l.VariantID, Qty: l.Qty})
	}
	u := a.currentUser(r)
	if u != nil {
		in.UserID = &u.ID
	}
	o, err := a.st.PlaceOrder(r.Context(), in)
	var oos store.ErrOutOfStock
	switch {
	case errors.As(err, &oos):
		a.checkoutView(w, r, f, http.StatusUnprocessableEntity, "Only "+strconv.Itoa(oos.Available)+" of "+oos.Name+" available. Please adjust your cart.")
		return
	case errors.Is(err, store.ErrNotFound):
		a.setFlash(w, "Something in your cart is no longer available.")
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	a.saveCart(w, nil)
	if u != nil {
		if err := a.st.UpdateProfile(r.Context(), u.ID, f.Name, f.Phone, f.Address); err != nil {
			a.log.Error("update profile after checkout", "user", u.ID, "err", err)
		}
	}
	a.sendOrderMails(o, "order_confirmation", "order_new_admin")
	a.setFlash(w, "Order placed! We'll call you to confirm.")
	http.Redirect(w, r, a.orderURL(o.Number), http.StatusSeeOther)
}

func (a *App) orderURL(number string) string {
	return "/orders/" + number + "?t=" + a.tok.OrderToken(number)
}

func (a *App) orderMailData(o store.OrderFull) map[string]any {
	return map[string]any{
		"Order": o.Order, "Items": o.Items,
		"TrackURL": a.cfg.BaseURL + a.orderURL(o.Number),
		"AdminURL": a.cfg.BaseURL + "/admin/orders/" + strconv.FormatInt(o.ID, 10),
	}
}

// sendOrderMails queues customer and/or admin emails; either template may be "".
func (a *App) sendOrderMails(o store.OrderFull, customerTmpl, adminTmpl string) {
	data := a.orderMailData(o)
	if customerTmpl != "" {
		a.mail.Send(o.Email, customerTmpl, data)
	}
	if adminTmpl != "" {
		a.mail.Send(a.cfg.OrderNotifyEmail, adminTmpl, data)
	}
}

// orderPage is visible with the tracking token, to the owning user, or to an admin.
func (a *App) orderPage(w http.ResponseWriter, r *http.Request) {
	o, err := a.st.GetOrderByNumber(r.Context(), r.PathValue("number"))
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	u := a.currentUser(r)
	allowed := a.tok.VerifyOrderToken(o.Number, r.URL.Query().Get("t")) ||
		(u != nil && (u.Role == "admin" || (o.UserID != nil && *o.UserID == u.ID)))
	if !allowed {
		a.notFound(w, r)
		return
	}
	a.render(w, r, "store/order.html", page{Title: "Order " + o.Number, NoIndex: true, Data: o})
}
