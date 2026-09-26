package app

import (
	"encoding/json"
	"html/template"
	"net/url"
	"strings"
)

// Site is the business's public identity and contact details. It lives here
// once; templates read it as .Site and helpers below use it directly.
type Site struct {
	Name, Tagline, Bio         string
	Phone, PhoneE164, WhatsApp string
	Tel                        template.URL // html/template refuses tel: from a plain string
	Email, Hours, Facebook     string
	Street, City, PostalCode   string
}

func (s Site) Address() string {
	return s.Street + ", " + s.City + " " + s.PostalCode + ", Bangladesh"
}

var site = Site{
	Name:       "Avalon Foods",
	Tagline:    "Good Food. A Better Tomorrow.",
	Bio:        "Premium food products from Bangladesh. From local ingredients to better food.",
	Phone:      "+880 1933-309009",
	PhoneE164:  "+8801933309009",
	WhatsApp:   "8801933309009",
	Tel:        "tel:+8801933309009",
	Email:      "shop@avalonbd.com",
	Hours:      "Sat–Thu, 10am–8pm",
	Facebook:   "https://www.facebook.com/share/1Lzv9YkGFs/",
	Street:     "Shaheb Bazar, Ghoramara",
	City:       "Rajshahi",
	PostalCode: "6100",
}

// waLink is a wa.me chat link with msg prefilled. Spaces are %20, not +,
// because WhatsApp shows a literal + otherwise.
func waLink(msg string) string {
	return "https://wa.me/" + site.WhatsApp + "?text=" + strings.ReplaceAll(url.QueryEscape(msg), "+", "%20")
}

// was returns the regular price to show crossed out, or 0 when there is
// nothing to cross out (no regular price, or not above the price).
func was(price int, regular *int) int {
	if regular != nil && *regular > price {
		return *regular
	}
	return 0
}

// lines splits text into trimmed, non-empty lines (promo checklist).
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// businessJSONLD is the home page's LocalBusiness block, matching the
// Facebook page's name, address, hours and link.
func (a *App) businessJSONLD() template.JS {
	b, _ := json.Marshal(map[string]any{
		"@context": "https://schema.org", "@type": "LocalBusiness",
		"name": site.Name, "description": site.Bio,
		"url": a.cfg.BaseURL + "/", "logo": a.cfg.BaseURL + "/static/logo.svg", "image": a.cfg.BaseURL + "/static/img/hero-1600.webp",
		"telephone": site.PhoneE164, "email": site.Email,
		"address": map[string]any{
			"@type": "PostalAddress", "streetAddress": site.Street, "addressLocality": site.City,
			"postalCode": site.PostalCode, "addressCountry": "BD",
		},
		"openingHoursSpecification": []map[string]any{{
			"@type":     "OpeningHoursSpecification",
			"dayOfWeek": []string{"Saturday", "Sunday", "Monday", "Tuesday", "Wednesday", "Thursday"},
			"opens":     "10:00", "closes": "20:00",
		}},
		"sameAs": []string{site.Facebook},
	})
	return template.JS(b)
}
