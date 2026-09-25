package store

import "testing"

// TestTransliterate isolates each of the four abugida rules from the brief,
// plus the digit/chandrabindu/anusvara/visarga mappings and the taka sign,
// so a regression in any one of them fails on its own rather than only
// through a compound Slugify case.
func TestTransliterate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "inherent vowel on a bare non-final consonant",
			// মন (mon, "mind"): ম has no matra/hasanta and is not
			// word-final, so it keeps the inherent vowel "o".
			in:   "মন",
			want: "mon",
		},
		{
			name: "matra replaces the inherent vowel",
			// দিন (din, "day"): the ি matra on দ replaces "o" with "i".
			in:   "দিন",
			want: "din",
		},
		{
			name: "hasanta suppresses the inherent vowel",
			// রাত্রি (ratri, "night"): ত is followed by hasanta, so it
			// contributes no vowel at all and joins directly with the
			// following র.
			in:   "রাত্রি",
			want: "ratri",
		},
		{
			name: "word-final consonant drops the inherent vowel",
			// তেল (tel, "oil"): ল ends the word, so it drops the "o" it
			// would otherwise carry (it would read "telo" without this
			// rule, per the brief).
			in:   "তেল",
			want: "tel",
		},
		{
			name: "chandrabindu is dropped",
			// চাঁদ (chad, "moon"): ঁ contributes nothing.
			in:   "চাঁদ",
			want: "chad",
		},
		{
			name: "anusvara maps to ng",
			// রং (rong, "color").
			in:   "রং",
			want: "rong",
		},
		{
			name: "visarga maps to h",
			// দুঃখ ("sorrow"): mapping ঃ to "h" literally, as the brief
			// requires, rather than to the colloquial "dukkho" reading.
			in:   "দুঃখ",
			want: "duhkh",
		},
		{
			name: "bengali digits map to ascii digits",
			in:   "০১২৩৪৫৬৭৮৯",
			want: "0123456789",
		},
		{
			name: "taka sign is not transliterated",
			// U+09F3 sits in the Bengali Unicode block but is not a
			// letter; it must pass through untouched, not turn into a
			// word like "taka".
			in:   "৳100",
			want: "৳100",
		},
		{
			name: "word-initial আ is aa",
			in:   "আম",
			want: "aam",
		},
		{
			name: "word-initial আ after a word break is still aa",
			in:   "আম আম",
			want: "aam aam",
		},
		{
			name: "mid-word আ is a, not aa",
			// Not a real Bangla word: an independent vowel directly after
			// a bare consonant is unusual in real orthography (a matra is
			// normally used instead). This exists only to pin down that
			// the "aa" special case is specifically about word-initial
			// position, not about the letter আ in general.
			in:   "কআ",
			want: "koa",
		},
		{
			name: "an unmapped Bangla letter passes through unchanged",
			// KHANDA TA (U+09CE) has no entry in banglaConsonants; it is
			// dropped later by Slugify's filter, exactly like today's
			// behaviour for any other unrecognised rune.
			in:   "ৎ",
			want: "ৎ",
		},
		{
			name: "latin text is untouched",
			in:   "Hello, World! 123",
			want: "Hello, World! 123",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Transliterate(c.in); got != c.want {
				t.Errorf("Transliterate(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
