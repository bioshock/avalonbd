package store

import (
	"testing"
	"unicode"
)

// TestTransliterate isolates each of the four abugida rules from the brief,
// the nukta-letter rules added in fix round 1 (ড়/ঢ়/য় and the "-ওয়া" glide),
// plus the digit/chandrabindu/anusvara/visarga mappings and the taka sign, so
// a regression in any one of them fails on its own rather than only through
// a compound Slugify case.
func TestTransliterate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "khanda ta is a bare /t/ and never takes the inherent vowel",
			// উৎসব (utsob, "festival"): ৎ is ত stripped of its inherent
			// vowel, which is the only difference between the two letters, so
			// it contributes "t" and never "to" -- even mid-word, where an
			// ordinary consonant before another consonant would take one.
			// Dropping it instead lost the consonant outright ("usob").
			in:   "উৎসব",
			want: "utsob",
		},
		{
			name: "word-final khanda ta contributes a bare t",
			// বিদ্যুৎ (bidyut, "electricity"). The "dj" is the known
			// ya-phala imprecision -- ্য after a consonant geminates in speech
			// ("bidyut"), which needs phonological context this table
			// deliberately does not model, the same accepted trade-off as
			// medial schwa deletion. What this case pins is the trailing
			// "t": before khanda ta was mapped, the word ended in "u".
			in:   "বিদ্যুৎ",
			want: "bidjut",
		},
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
			name: "taka sign is dropped, not transliterated",
			// U+09F3 sits in the Bengali Unicode block but is not a
			// letter. Fix round 1 (C2): Transliterate must never return a
			// Bengali-block rune, so this is dropped here rather than
			// passed through for Slugify's filter to drop later.
			in:   "৳100",
			want: "100",
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
			name: "a nukta letter (decomposed) still takes its own matra",
			// বড়ি (bori, "lentil dumpling"): ড়  is spelled as ড + nukta
			// (U+09A1 U+09BC), the normal decomposed form. Fix round 1
			// (C1): the matra ি that follows the nukta must still attach
			// to ড়, not leak through as a raw rune because the lookahead
			// only checked the rune immediately after the base consonant.
			in:   "বড়ি",
			want: "bori",
		},
		{
			name: "a bare nukta letter never takes the inherent vowel",
			// ময়দা (moyda, "flour"): য় (nukta form) sits between ম and
			// দ with no matra of its own. Fix round 1 (C1): unlike an
			// ordinary consonant, ড়/ঢ়/য় never take the "o" filler, so
			// this reads "moyda", not "moyoda".
			in:   "ময়দা",
			want: "moyda",
		},
		{
			name: `ও followed by a bare nukta য় contracts to a "wa" glide`,
			// খাওয়া (khawa, "to eat"): the common "-ওয়া" ending. ও
			// contributes nothing on its own here; য় (nukta form)
			// becomes "w" and takes its own matra normally.
			in:   "খাওয়া",
			want: "khawa",
		},
		{
			name: "a precomposed nukta letter is handled the same as decomposed",
			// The same word as above ("bori"), but with the precomposed
			// single-codepoint ড় (U+09DC) instead of ড + nukta. Real text
			// is not expected to use this spelling (see Transliterate's
			// doc comment), but the defensive fallback table entry must
			// still work and must not take the inherent vowel either.
			in:   "ব" + "ড়" + "ি",
			want: "bori",
		},
		{
			name: "an unmapped Bangla letter is dropped, not passed through",
			// VOCALIC L (U+098C) has no entry in banglaConsonants and is
			// listed in banglaDeliberatelyDropped. Fix round 1 (C2):
			// Transliterate itself drops it (rather than passing it
			// through for Slugify's filter to drop later), so it never
			// appears in Transliterate's return value. This case used
			// KHANDA TA until it was mapped to "t"; the fixture has to be a
			// letter that is genuinely still dropped or it asserts nothing.
			in:   "ঌ",
			want: "",
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

// TestTransliterateNeverReturnsBengaliRunes is fix round 1's C2 invariant:
// Transliterate must never return a rune in the Bengali Unicode block
// (U+0980-U+09FF), whether that rune is fully unmapped (like the taka sign)
// or only partially consumed (like a matra stranded by a nukta-letter bug).
// Slugify's later NFD/filter pass happens to strip stray Bengali runes too,
// which is exactly how the missing-nukta-matra bug (fix round 1, C1) went
// undetected: the corpus below is every case from TestTransliterate plus the
// five real product names that first exposed the bug, and every required
// case from TestSlugify.
func TestTransliterateNeverReturnsBengaliRunes(t *testing.T) {
	corpus := []string{
		"মন", "দিন", "রাত্রি", "তেল", "চাঁদ", "রং", "দুঃখ",
		"০১২৩৪৫৬৭৮৯", "৳100", "আম", "আম আম", "কআ",
		"বড়ি", "ময়দা", "খাওয়া", "ব" + "ড়" + "ি", "ৎ",
		"Hello, World! 123", "",
		"হলুদ গুঁড়া", "আটা ময়দা সুজি", "গাওয়া ঘি", "চিনিগুঁড়া চাল", "প্রিমিয়াম মধু",
		"আম", "মধু", "তেল", "খাঁটি সরিষার তেল", "আম (Mango) 5kg", "মধু Honey 500g", "ঘি",
		"৳500 Gift Box", "৳৫০০ উপহার বক্স",
	}

	for _, in := range corpus {
		out := Transliterate(in)
		for _, r := range out {
			if r >= 0x0980 && r <= 0x09FF {
				t.Errorf("Transliterate(%q) = %q contains Bengali-block rune %q (U+%04X)", in, out, r, r)
			}
		}
	}
}

// TestBanglaTableIsComplete is fix round 1's M1: it walks every Bengali
// letter, matra, and digit code point Unicode has assigned, and fails if any
// is neither mapped (banglaConsonants / banglaVowels / banglaMatras /
// banglaDigits) nor in banglaDeliberatelyDropped. Without this test, a
// letter like ড়/ঢ়/য় can be silently missing from the table forever: nothing
// else notices, because Transliterate already has a generic fallback for any
// rune it does not recognise.
func TestBanglaTableIsComplete(t *testing.T) {
	// Walk the WHOLE Bengali block and classify by Unicode general category,
	// rather than by hand-picked sub-ranges. An earlier version of this test
	// listed six ranges taken from the task instructions, and five assigned
	// letters and matras (U+0980, U+09BD, U+09E0-U+09E3, U+09FC, U+09FE) fell
	// outside every one of them -- so the test could not tell "we decided to
	// drop this" from "we never knew this existed", which is the entire
	// guarantee it exists to provide. A category-driven loop cannot develop
	// that kind of blind spot as Unicode assigns new code points.
	//
	// Only the categories that carry a sound are in scope: Lo (letters),
	// Mn/Mc (combining vowel signs and other marks) and Nd (digits).
	// Currency signs (Sc, including the taka sign), the Bengali currency
	// numerators (No), symbols (So) and punctuation (Po) are correctly not
	// transliterated -- Transliterate drops them through its generic
	// Bengali-block branch, which TestTransliterateNeverReturnsBengaliRunes
	// covers.
	inScope := []*unicode.RangeTable{unicode.Lo, unicode.Mn, unicode.Mc, unicode.Nd}

	// Signs handled by name in Transliterate's switch rather than by table.
	handledAsSign := map[rune]bool{
		chandrabindu: true, anusvara: true, visarga: true, nukta: true, hasanta: true,
	}

	for cp := rune(0x0980); cp <= 0x09FF; cp++ {
		if !unicode.Is(unicode.Bengali, cp) {
			continue // unassigned
		}
		relevant := false
		for _, tab := range inScope {
			if unicode.Is(tab, cp) {
				relevant = true
				break
			}
		}
		if !relevant || handledAsSign[cp] {
			continue
		}
		_, consonant := banglaConsonants[cp]
		_, vowel := banglaVowels[cp]
		_, matra := banglaMatras[cp]
		_, digit := banglaDigits[cp]
		_, nuktaForm := banglaNuktaConsonants[cp]
		reason, dropped := banglaDeliberatelyDropped[cp]
		switch {
		case consonant, vowel, matra, digit, nuktaForm:
			// mapped
		case dropped && reason == "":
			t.Errorf("U+%04X (%q) is in banglaDeliberatelyDropped with no reason", cp, cp)
		case dropped:
			// documented, deliberate
		default:
			t.Errorf("U+%04X (%q) is an assigned Bangla letter, mark or digit with no "+
				"entry in any mapping table and none in banglaDeliberatelyDropped -- "+
				"map it or record why it is dropped", cp, cp)
		}
	}
}
