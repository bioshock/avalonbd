package store

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Wild Forest Honey":   "wild-forest-honey",
		"  Himsagar   Mango! ": "himsagar-mango",
		"Café Crème":          "cafe-creme",
		"আম (Mango) 5kg":      "mango-5kg",
		"---":                 "item",
		"":                    "item",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
