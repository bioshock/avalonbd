package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"avalonshop/internal/store"
)

// validateProduct normalizes p (trimmed name, slug derived from the name when
// empty, then slugified) and checks the rules the admin form and the JSON API
// share, so the two paths cannot drift. Keys: "name", "tagline",
// "meta_description", "variants", and "variants[i].<field>".
// regular_price may be any value ≥ 0: it is only shown when above price.
func validateProduct(p *store.Product, vs []store.Variant) map[string]string {
	errs := map[string]string{}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		errs["name"] = "Name is required."
	}
	if utf8.RuneCountInString(p.Tagline) > 160 {
		errs["tagline"] = "Keep the tagline under 160 characters."
	}
	if utf8.RuneCountInString(p.MetaDescription) > 160 {
		errs["meta_description"] = "Keep the meta description under 160 characters; search engines cut it there."
	}
	slug := strings.TrimSpace(p.Slug)
	if slug == "" {
		slug = p.Name
	}
	p.Slug = store.Slugify(slug)
	if len(vs) == 0 {
		errs["variants"] = "Add at least one variant (even a single default size)."
	}
	for i, v := range vs {
		k := fmt.Sprintf("variants[%d].", i)
		if strings.TrimSpace(v.Name) == "" {
			errs[k+"name"] = "is required"
		}
		if v.Price < 0 {
			errs[k+"price"] = "must be 0 or more"
		}
		if v.Stock < 0 {
			errs[k+"stock"] = "must be 0 or more"
		}
		if v.RegularPrice != nil && *v.RegularPrice < 0 {
			errs[k+"regular_price"] = "must be 0 or more"
		}
	}
	return errs
}
