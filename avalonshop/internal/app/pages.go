package app

import "net/http"

// contentPages are the static information pages in templates/pages/.
// Adding one = a row here + a template; routes and the sitemap follow.
var contentPages = []struct{ Slug, Title, Description string }{
	{"about", "About us", "Avalon Foods is a small food business in Rajshahi making everyday spice powders in small batches from carefully selected ingredients."},
	{"journey", "How it's made", "How Avalon Foods spice powders are made: sourcing, cleaning and drying, grinding, packing and delivery."},
	{"contact", "Contact", "Call, WhatsApp or email Avalon Foods in Shaheb Bazar, Rajshahi. Open Saturday to Thursday, 10am to 8pm."},
	{"delivery-returns", "Delivery & Returns", "Cash on delivery across Bangladesh. Delivery times, charges by area, and how returns work at Avalon Foods."},
	{"privacy", "Privacy policy", "What personal data Avalon Foods collects, why, who sees it, and how to ask for it to be deleted."},
	{"terms", "Terms & Conditions", "The terms for ordering from Avalon Foods: confirmation, prices, cash on delivery, cancellation and liability."},
}

func (a *App) contentPage(slug, title, desc string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var data any
		if slug == "delivery-returns" {
			zones, err := a.st.ListZones(r.Context(), true)
			if err != nil {
				a.serverError(w, r, err)
				return
			}
			data = zones
		}
		a.render(w, r, "pages/"+slug+".html", page{Title: title, Description: desc, Data: data})
	}
}
