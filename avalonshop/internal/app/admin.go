package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"avalonshop/internal/store"
)

// requireAdmin hides admin routes from everyone else with a 404.
func (a *App) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil || u.Role != "admin" {
			a.notFound(w, r)
			return
		}
		h(w, r)
	}
}

func (a *App) adminDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := a.st.Dashboard(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/dashboard.html", page{Title: "Dashboard", NoIndex: true, Data: d})
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func formInt(r *http.Request, name string) (int, bool) {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// flashBack sets a flash and redirects; the one-liner every admin POST ends
// with. It uses a.redirect (Task 10, render.go) rather than a bare
// http.Redirect: the category and zone delete buttons post with HTMX
// (hx-post + hx-confirm), and htmx transparently follows a 303 and swaps the
// response into the target — which for these forms means the entire admin
// page gets injected into an empty <form> element. a.redirect sets
// HX-Redirect and returns 204 for HTMX requests instead, and falls back to a
// plain 303 otherwise; the flash cookie is set before either, so the message
// still shows after the browser reloads.
func (a *App) flashBack(w http.ResponseWriter, r *http.Request, msg, to string) {
	a.setFlash(w, msg)
	a.redirect(w, r, to)
}

// ---- categories ----

func (a *App) adminCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := a.st.ListCategories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/categories.html", page{Title: "Categories", NoIndex: true, Data: cats})
}

func (a *App) categoryFromForm(r *http.Request, excludeID int64) (store.Category, string) {
	c := store.Category{ID: excludeID, Name: strings.TrimSpace(r.FormValue("name"))}
	if c.Name == "" {
		return c, "Name is required."
	}
	slug := strings.TrimSpace(r.FormValue("slug"))
	if slug == "" {
		slug = c.Name
	}
	var err error
	if c.Slug, err = a.st.UniqueSlug(r.Context(), "categories", store.Slugify(slug), excludeID); err != nil {
		return c, "Could not generate a slug."
	}
	sort, ok := formInt(r, "sort")
	if !ok {
		return c, "Sort must be a whole number."
	}
	c.Sort = sort
	return c, ""
}

func (a *App) adminCategoryCreate(w http.ResponseWriter, r *http.Request) {
	c, msg := a.categoryFromForm(r, 0)
	if msg == "" {
		if _, err := a.st.CreateCategory(r.Context(), c); err != nil {
			msg = "Could not save: " + err.Error()
		} else {
			msg = "Category added."
		}
	}
	a.flashBack(w, r, msg, "/admin/categories")
}

func (a *App) adminCategoryUpdate(w http.ResponseWriter, r *http.Request) {
	c, msg := a.categoryFromForm(r, pathID(r))
	if msg == "" {
		if err := a.st.UpdateCategory(r.Context(), c); err != nil {
			msg = "Could not save: " + err.Error()
		} else {
			msg = "Saved."
		}
	}
	a.flashBack(w, r, msg, "/admin/categories")
}

func (a *App) adminCategoryDelete(w http.ResponseWriter, r *http.Request) {
	err := a.st.DeleteCategory(r.Context(), pathID(r))
	switch {
	case errors.Is(err, store.ErrInUse):
		a.flashBack(w, r, "That category still has products. Move them first.", "/admin/categories")
	case err != nil:
		a.flashBack(w, r, "Could not delete: "+err.Error(), "/admin/categories")
	default:
		a.flashBack(w, r, "Category deleted.", "/admin/categories")
	}
}

// ---- zones ----

func (a *App) adminZones(w http.ResponseWriter, r *http.Request) {
	zones, err := a.st.ListZones(r.Context(), false)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "admin/zones.html", page{Title: "Delivery zones", NoIndex: true, Data: zones})
}

func zoneFromForm(r *http.Request, id int64) (store.Zone, string) {
	z := store.Zone{ID: id, Name: strings.TrimSpace(r.FormValue("name")), Active: r.FormValue("active") == "on"}
	if z.Name == "" {
		return z, "Name is required."
	}
	if strings.TrimSpace(r.FormValue("fee")) == "" {
		return z, "Delivery fee is required."
	}
	fee, ok := formInt(r, "fee")
	if !ok || fee < 0 {
		return z, "Delivery fee must be a whole number of taka, 0 or more."
	}
	sort, ok := formInt(r, "sort")
	if !ok {
		return z, "Sort must be a whole number."
	}
	z.Fee, z.Sort = fee, sort
	return z, ""
}

func (a *App) adminZoneCreate(w http.ResponseWriter, r *http.Request) {
	z, msg := zoneFromForm(r, 0)
	if msg == "" {
		if _, err := a.st.CreateZone(r.Context(), z); err != nil {
			msg = "Could not save: " + err.Error()
		} else {
			msg = "Zone added."
		}
	}
	a.flashBack(w, r, msg, "/admin/zones")
}

func (a *App) adminZoneUpdate(w http.ResponseWriter, r *http.Request) {
	z, msg := zoneFromForm(r, pathID(r))
	if msg == "" {
		if err := a.st.UpdateZone(r.Context(), z); err != nil {
			msg = "Could not save: " + err.Error()
		} else {
			msg = "Saved."
		}
	}
	a.flashBack(w, r, msg, "/admin/zones")
}

func (a *App) adminZoneDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.st.DeleteZone(r.Context(), pathID(r)); err != nil {
		a.flashBack(w, r, "Could not delete: "+err.Error(), "/admin/zones")
		return
	}
	a.flashBack(w, r, "Zone deleted.", "/admin/zones")
}
