package store

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Wild Forest Honey":    "wild-forest-honey",
		"  Himsagar   Mango! ": "himsagar-mango",
		"Café Crème":           "cafe-creme",
		"আম":                   "aam",
		"মধু":                  "modhu",
		"তেল":                  "tel",
		"খাঁটি সরিষার তেল": "khati-sorishar-tel",
		"আম (Mango) 5kg":   "aam-mango-5kg",
		"মধু Honey 500g":   "modhu-honey-500g",
		"ঘি":               "ghi",
		"৳500 Gift Box":    "500-gift-box",
		"---":              "item",
		"":                 "item",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
