package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"avalonshop/internal/store"
)

var orderStatuses = []string{"new", "confirmed", "shipped", "delivered", "cancelled"}

func (a *App) adminOrders(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "new"
	}
	filter := status
	if status == "all" {
		filter = ""
	}
	orders, err := a.st.ListOrders(r.Context(), filter, 200)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/orders.html", page{Title: "Orders", NoIndex: true, Data: map[string]any{"Orders": orders, "Status": status, "Statuses": orderStatuses}})
}

func (a *App) adminOrder(w http.ResponseWriter, r *http.Request) {
	o, err := a.st.GetOrder(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/order.html", page{Title: "Order " + o.Number, NoIndex: true, Data: map[string]any{
		"Order": o, "Next": store.NextStatuses(o.Status), "TrackURL": a.orderURL(o.Number),
	}})
}

func (a *App) adminOrderStatus(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	back := "/admin/orders/" + strconv.FormatInt(id, 10)
	to := r.FormValue("status")
	o, err := a.st.UpdateOrderStatus(r.Context(), id, to)
	switch {
	case errors.Is(err, store.ErrTransition):
		a.flashBack(w, r, "That change isn't allowed from the order's current status.", back)
		return
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	switch to {
	case "shipped":
		a.sendOrderMails(o, "order_shipped", "")
	case "delivered":
		a.sendOrderMails(o, "order_delivered", "")
	}
	a.flashBack(w, r, "Order marked "+to+".", back)
}

func (a *App) adminOrderNote(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.SetAdminNote(r.Context(), id, strings.TrimSpace(r.FormValue("note"))); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.notFound(w, r)
			return
		}
		a.serverError(w, r, err)
		return
	}
	a.flashBack(w, r, "Note saved.", "/admin/orders/"+strconv.FormatInt(id, 10))
}
