package app

import (
	"context"
	"net/http"
	"strconv"

	"avalonshop/internal/store"
	"avalonshop/internal/token"
)

type cartLineView struct {
	store.CartVariant
	Qty       int
	LineTotal int
	Short     bool // requested qty exceeds stock
}

type cartView struct {
	Lines    []cartLineView
	Subtotal int
	Count    int
}

// buildCart resolves cookie lines against the database. Lines whose variant is
// gone or whose product is inactive are dropped; the surviving lines are returned
// so the caller can rewrite the cookie.
func (a *App) buildCart(ctx context.Context, lines []token.CartLine) (cartView, []token.CartLine, error) {
	var v cartView
	if len(lines) == 0 {
		return v, nil, nil
	}
	ids := make([]int64, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.VariantID)
	}
	vars, err := a.st.VariantsForCart(ctx, ids)
	if err != nil {
		return v, nil, err
	}
	byID := map[int64]store.CartVariant{}
	for _, cv := range vars {
		if cv.Active {
			byID[cv.VariantID] = cv
		}
	}
	var kept []token.CartLine
	for _, l := range lines {
		cv, ok := byID[l.VariantID]
		if !ok {
			continue
		}
		kept = append(kept, l)
		v.Lines = append(v.Lines, cartLineView{CartVariant: cv, Qty: l.Qty, LineTotal: cv.Price * l.Qty, Short: cv.Stock < l.Qty})
		v.Subtotal += cv.Price * l.Qty
		v.Count += l.Qty
	}
	return v, kept, nil
}

// loadCart reads the cookie, builds the view, and rewrites the cookie if lines were dropped.
func (a *App) loadCart(w http.ResponseWriter, r *http.Request) (cartView, []token.CartLine, error) {
	lines := a.cartLines(r)
	v, kept, err := a.buildCart(r.Context(), lines)
	if err == nil && len(kept) != len(lines) {
		a.saveCart(w, kept)
	}
	return v, kept, err
}

func clampQty(s string, lo int) int {
	q, _ := strconv.Atoi(s)
	if q < lo {
		q = lo
	}
	if q > 99 {
		q = 99
	}
	return q
}

func (a *App) cartAdd(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.FormValue("variant_id"), 10, 64)
	qty := clampQty(r.FormValue("qty"), 1)
	vars, err := a.st.VariantsForCart(r.Context(), []int64{id})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(vars) == 0 || !vars[0].Active || vars[0].Stock == 0 {
		a.setFlash(w, "That item is not available right now.")
		a.redirect(w, r, "/products")
		return
	}
	lines := a.cartLines(r)
	found := false
	for i := range lines {
		if lines[i].VariantID == id {
			lines[i].Qty = min(lines[i].Qty+qty, 99)
			found = true
		}
	}
	if !found {
		if len(lines) >= token.MaxCartLines {
			a.setFlash(w, "Your cart is full.")
			a.redirect(w, r, "/cart")
			return
		}
		lines = append(lines, token.CartLine{VariantID: id, Qty: qty})
	}
	a.saveCart(w, lines)
	a.respondCart(w, r, lines, "Added to cart.")
}

func (a *App) cartUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	qty := clampQty(r.FormValue("qty"), 0)
	lines := a.cartLines(r)
	out := lines[:0]
	for _, l := range lines {
		if l.VariantID == id {
			if qty == 0 {
				continue
			}
			l.Qty = qty
		}
		out = append(out, l)
	}
	a.saveCart(w, out)
	a.respondCart(w, r, out, "")
}

// respondCart renders the drawer for HTMX, or flashes and redirects to /cart.
func (a *App) respondCart(w http.ResponseWriter, r *http.Request, lines []token.CartLine, flash string) {
	if isHTMX(r) {
		v, kept, err := a.buildCart(r.Context(), lines)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if len(kept) != len(lines) {
			a.saveCart(w, kept)
		}
		a.renderPartial(w, "cart_drawer.html", v)
		return
	}
	if flash != "" {
		a.setFlash(w, flash)
	}
	http.Redirect(w, r, "/cart", http.StatusSeeOther)
}

func (a *App) cartDrawer(w http.ResponseWriter, r *http.Request) {
	if !isHTMX(r) {
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	}
	v, _, err := a.loadCart(w, r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderPartial(w, "cart_drawer.html", v)
}

func (a *App) cartPage(w http.ResponseWriter, r *http.Request) {
	v, _, err := a.loadCart(w, r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "store/cart.html", page{Title: "Your cart", NoIndex: true, Data: v})
}
