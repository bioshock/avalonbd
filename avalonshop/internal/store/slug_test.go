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
		// Fix round 1: real product names probed against the committed
		// code (not written by the brief's author), which exposed the
		// missing ড়/ঢ়/য় (nukta letter) handling.
		"হলুদ গুঁড়া":    "holud-gura",
		"আটা ময়দা সুজি": "aata-moyda-suji",
		"গাওয়া ঘি":      "gawa-ghi",
		"চিনিগুঁড়া চাল": "chinigura-chal",
		"প্রিমিয়াম মধু": "primiyam-modhu",
		"---": "item",
		"":    "item",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
