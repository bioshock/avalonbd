package store

import "strings"

// Transliterate converts Bangla (Bengali script) text to a readable Latin
// approximation so Slugify can build a meaningful slug from a Bangla product
// name instead of falling back to "item". It targets readable URLs, not a
// scholarly transliteration scheme (ISO 15919 / IAST): "aam" beats "ama" with
// diacritics, and Slugify's NFD pass would strip those diacritics anyway.
//
// Bangla is an abugida: a bare consonant carries an inherent vowel
// (transliterated "o", the Bengali pronunciation), unless:
//
//   - a matra (dependent vowel sign) follows it, replacing the inherent vowel;
//   - a hasanta / virama follows it, suppressing the inherent vowel so the
//     consonant joins directly with what follows (a conjunct);
//   - the consonant ends a word, which also drops the inherent vowel; or
//   - the consonant is ড়, ঢ়, or য় (RRA/RHA/YYA): these three letters are
//     modified consonants (traditionally a rhotic tap and a semivowel, not
//     part of the abugida's ordinary vowel system) and never take the
//     inherent vowel at all, e.g. ময়দা reads
//     "moyda", not "moyoda".
//
// A special case: ও (independent vowel O) immediately followed by a bare
// য়/য+nukta is the common "-ওয়া" verb/word ending (গাওয়া, খাওয়া, যাওয়া, ...),
// read as one "wa" glide rather than "o" + "ya"; ও contributes nothing on its
// own there and য়/য+nukta becomes "w" instead of "y".
//
// ড়, ঢ়, and য় are Unicode composition exclusions: text almost always spells
// them as base consonant (ড/ঢ/য) + U+09BC NUKTA, because NFC never recomposes
// that pair back into the single precomposed codepoint. Transliterate treats
// base+nukta as one letter for the purposes of every rule above; the
// precomposed codepoints are also mapped directly, as a defensive fallback,
// but are not expected to appear in normalised text.
//
// Runes this function does not recognise, plus any other rune in the Bengali
// Unicode block (U+0980-U+09FF) that has no mapping — including matras or
// letters this table deliberately does not cover (see
// banglaDeliberatelyDropped) and the taka sign U+09F3, which sits in the
// block but is not a letter — are dropped. Transliterate never returns a
// rune in that block; only true non-Bangla input (Latin letters, digits,
// punctuation, whitespace, other scripts) is passed through unchanged, for
// Slugify's existing NFD/filter pass to handle as it does today.
func Transliterate(s string) string {
	runes := []rune(s)
	n := len(runes)
	var b strings.Builder

	for i := 0; i < n; {
		r := runes[i]

		if sound, ok := banglaConsonants[r]; ok {
			letterEnd := i + 1
			noInherentVowel := false

			switch r {
			case 'ড়', 'ঢ়', 'য়':
				// Already a single nukta-letter codepoint (see the doc comment
				// above): no separate nukta rune follows to merge with.
				noInherentVowel = true
			default:
				if letterEnd < n && runes[letterEnd] == nukta {
					if nuktaSound, ok2 := banglaNuktaConsonants[r]; ok2 {
						// Base consonant + nukta is one letter (ড়, ঢ়, or য়), not
						// two: consuming both here, before looking for a matra or
						// hasanta, is what lets a following matra (as in গুঁড়া,
						// "powder") still attach correctly.
						sound = nuktaSound
						letterEnd++
						noInherentVowel = true
					}
				}
			}

			if noInherentVowel && (r == 'য' || r == 'য়') &&
				i > 0 && runes[i-1] == independentO {
				// Completes the ও+য় "wa" glide: ও (below) wrote nothing for
				// itself, so this becomes "w" instead of "y".
				sound = "w"
			}

			var next rune
			hasNext := letterEnd < n
			if hasNext {
				next = runes[letterEnd]
			}
			matra, isMatra := banglaMatras[next]

			switch {
			case hasNext && next == hasanta:
				// Hasanta suppresses the inherent vowel; the next consonant
				// (a conjunct) is processed on its own in the next iteration.
				b.WriteString(sound)
				i = letterEnd + 1
			case hasNext && isMatra:
				// The matra replaces the inherent vowel.
				b.WriteString(sound)
				b.WriteString(matra)
				i = letterEnd + 1
			default:
				// Bare letter: keep the inherent vowel unless this is the last
				// letter of the word, or this letter never takes one at all
				// (noInherentVowel; see the doc comment above).
				b.WriteString(sound)
				if !noInherentVowel && hasNext && isBanglaJoiner(next) {
					b.WriteString("o")
				}
				i = letterEnd
			}
			continue
		}

		switch r {
		case 'ঁ': // BENGALI SIGN CANDRABINDU
			// Nasalization has no useful Latin spelling for a slug: drop it.
			i++
			continue
		case 'ং': // BENGALI SIGN ANUSVARA
			b.WriteString("ng")
			i++
			continue
		case 'ঃ': // BENGALI SIGN VISARGA
			b.WriteString("h")
			i++
			continue
		case '়', '্':
			// A nukta with no consonant it combines with (see
			// banglaNuktaConsonants above), or a stray hasanta with no
			// preceding consonant: nothing to attach to.
			i++
			continue
		}

		if digit, ok := banglaDigits[r]; ok {
			b.WriteRune(digit)
			i++
			continue
		}

		if sound, ok := banglaVowels[r]; ok {
			if r == aaLetter { // BENGALI LETTER AA
				// Word-initial আ reads as a long "a": aam, not am.
				if i == 0 || !isBanglaJoiner(runes[i-1]) {
					b.WriteString("aa")
					i++
					continue
				}
			}
			if r == independentO && i+2 < n && runes[i+1] == 'য' && runes[i+2] == nukta {
				// ও immediately followed by bare য়/য+nukta: the "-ওয়া" glide
				// (see the doc comment above). Write nothing for ও itself; the
				// following য়/য+nukta becomes "w" and picks up its own matra
				// normally.
				i++
				continue
			}
			b.WriteString(sound)
			i++
			continue
		}

		if r >= bengaliBlockLo && r <= bengaliBlockHi {
			// Any other rune in the Bengali Unicode block: the taka sign (৳,
			// U+09F3, which is not a letter), a deliberately-dropped letter or
			// matra (see banglaDeliberatelyDropped), or anything else this
			// table does not recognise. Transliterate must never return a
			// Bengali-block rune (see TestTransliterateNeverReturnsBengali).
			i++
			continue
		}

		// Not a Bangla rune at all: pass it through unchanged. Slugify's
		// NFD/filter pass keeps [a-z0-9] and lowercases Latin letters.
		b.WriteRune(r)
		i++
	}

	return b.String()
}

// isBanglaJoiner reports whether r is part of a Bangla "letter chain": a
// consonant, an independent vowel, a matra, or one of the combining signs
// (hasanta, chandrabindu, anusvara, visarga, nukta). It is used to decide
// whether a bare consonant is word-final, and whether an independent vowel
// starts a new word.
func isBanglaJoiner(r rune) bool {
	if _, ok := banglaConsonants[r]; ok {
		return true
	}
	if _, ok := banglaVowels[r]; ok {
		return true
	}
	if _, ok := banglaMatras[r]; ok {
		return true
	}
	switch r {
	case '্', 'ঁ', 'ং', 'ঃ', '়':
		return true
	}
	return false
}

const (
	hasanta = '্' // BENGALI SIGN VIRAMA: suppresses a consonant's inherent vowel
)

// banglaConsonants maps a Bangla consonant letter to its Latin sound, not
// including the inherent vowel (the caller adds "o" when appropriate). The
// last three entries are the precomposed spellings of ড়/ঢ়/য়, kept as a
// defensive fallback: see Transliterate's doc comment for why real text is
// expected to spell them as base consonant + nukta instead (banglaNuktaConsonants).
var banglaConsonants = map[rune]string{
	'ক': "k",   // BENGALI LETTER KA
	'খ': "kh",  // BENGALI LETTER KHA
	'গ': "g",   // BENGALI LETTER GA
	'ঘ': "gh",  // BENGALI LETTER GHA
	'ঙ': "ng",  // BENGALI LETTER NGA
	'চ': "ch",  // BENGALI LETTER CA
	'ছ': "chh", // BENGALI LETTER CHA
	'জ': "j",   // BENGALI LETTER JA
	'ঝ': "jh",  // BENGALI LETTER JHA
	'ঞ': "n",   // BENGALI LETTER NYA
	'ট': "t",   // BENGALI LETTER TTA
	'ঠ': "th",  // BENGALI LETTER TTHA
	'ড': "d",   // BENGALI LETTER DDA
	'ঢ': "dh",  // BENGALI LETTER DDHA
	'ণ': "n",   // BENGALI LETTER NNA
	'ত': "t",   // BENGALI LETTER TA
	'থ': "th",  // BENGALI LETTER THA
	'দ': "d",   // BENGALI LETTER DA
	'ধ': "dh",  // BENGALI LETTER DHA
	'ন': "n",   // BENGALI LETTER NA
	'প': "p",   // BENGALI LETTER PA
	'ফ': "ph",  // BENGALI LETTER PHA
	'ব': "b",   // BENGALI LETTER BA
	'ভ': "bh",  // BENGALI LETTER BHA
	'ম': "m",   // BENGALI LETTER MA
	'য': "j",   // BENGALI LETTER YA
	'র': "r",   // BENGALI LETTER RA
	'ল': "l",   // BENGALI LETTER LA
	'শ': "sh",  // BENGALI LETTER SHA
	'ষ': "sh",  // BENGALI LETTER SSA
	'স': "s",   // BENGALI LETTER SA
	'হ': "h",   // BENGALI LETTER HA
	'ড়': "r",   // BENGALI LETTER RRA
	'ঢ়': "rh",  // BENGALI LETTER RHA
	'য়': "y",   // BENGALI LETTER YYA
}

// banglaNuktaConsonants maps the base consonant of a nukta letter (ড, ঢ, য)
// to that letter's sound (ড়, ঢ়, য়) when it is immediately followed by
// U+09BC NUKTA — the normal, decomposed spelling produced by standard Bangla
// input methods and never recomposed by NFC (see Transliterate's doc
// comment).
var banglaNuktaConsonants = map[rune]string{
	'ড': "r",  // BENGALI LETTER DDA + nukta
	'ঢ': "rh", // BENGALI LETTER DDHA + nukta
	'য': "y",  // BENGALI LETTER YA + nukta
}

// banglaVowels maps a Bangla independent vowel letter to its Latin sound.
var banglaVowels = map[rune]string{
	'অ': "o",  // BENGALI LETTER A
	'আ': "a",  // BENGALI LETTER AA
	'ই': "i",  // BENGALI LETTER I
	'ঈ': "i",  // BENGALI LETTER II
	'উ': "u",  // BENGALI LETTER U
	'ঊ': "u",  // BENGALI LETTER UU
	'ঋ': "ri", // BENGALI LETTER VOCALIC R
	'এ': "e",  // BENGALI LETTER E
	'ঐ': "oi", // BENGALI LETTER AI
	'ও': "o",  // BENGALI LETTER O
	'ঔ': "ou", // BENGALI LETTER AU
}

// banglaMatras maps a Bangla dependent vowel sign (matra) to its Latin sound.
var banglaMatras = map[rune]string{
	'া': "a",  // BENGALI VOWEL SIGN AA
	'ি': "i",  // BENGALI VOWEL SIGN I
	'ী': "i",  // BENGALI VOWEL SIGN II
	'ু': "u",  // BENGALI VOWEL SIGN U
	'ূ': "u",  // BENGALI VOWEL SIGN UU
	'ৃ': "ri", // BENGALI VOWEL SIGN VOCALIC R
	'ে': "e",  // BENGALI VOWEL SIGN E
	'ৈ': "oi", // BENGALI VOWEL SIGN AI
	'ো': "o",  // BENGALI VOWEL SIGN O
	'ৌ': "ou", // BENGALI VOWEL SIGN AU
}

// banglaDigits maps a Bangla digit to its ASCII digit.
var banglaDigits = map[rune]rune{
	'০': '0', // BENGALI DIGIT ZERO
	'১': '1', // BENGALI DIGIT ONE
	'২': '2', // BENGALI DIGIT TWO
	'৩': '3', // BENGALI DIGIT THREE
	'৪': '4', // BENGALI DIGIT FOUR
	'৫': '5', // BENGALI DIGIT FIVE
	'৬': '6', // BENGALI DIGIT SIX
	'৭': '7', // BENGALI DIGIT SEVEN
	'৮': '8', // BENGALI DIGIT EIGHT
	'৯': '9', // BENGALI DIGIT NINE
}

// banglaDeliberatelyDropped lists assigned Bengali letters and matras that
// Transliterate does not map, together with the reason. Every rune here is
// still handled correctly at runtime — it falls through to Transliterate's
// generic "drop any unrecognised Bengali-block rune" branch, exactly like
// the taka sign or a stray nukta. This map exists so
// TestBanglaTableIsComplete can tell "we decided to drop this" apart from
// "we forgot this exists", which is the class of bug (missing ড়/ঢ়/য়
// handling) this map and its test were added to catch.
var banglaDeliberatelyDropped = map[rune]string{
	'ঌ': "VOCALIC L: a Sanskrit-derived letter, not used in modern Bangla spelling",                                                                                                                                                                                                                       // BENGALI LETTER VOCALIC L (letter)
	'ৎ': "KHANDA TA: a distinct bare-consonant letter (e.g. উৎসব, utsob/festival); left unmapped to keep this table to the brief's four abugida rules rather than adding a case for one rare letter",                                                                                                      // BENGALI LETTER KHANDA TA (letter)
	'ৰ': "RA WITH MIDDLE DIAGONAL: an Assamese letter, not standard Bangla",                                                                                                                                                                                                                               // BENGALI LETTER RA WITH MIDDLE DIAGONAL (letter)
	'ৱ': "RA WITH LOWER DIAGONAL: an Assamese letter, not standard Bangla",                                                                                                                                                                                                                                // BENGALI LETTER RA WITH LOWER DIAGONAL (letter)
	'ৄ': "VOWEL SIGN VOCALIC RR: vanishingly rare even in Sanskrit loanwords",                                                                                                                                                                                                                             // BENGALI VOWEL SIGN VOCALIC RR (matra)
	'ৗ': "AU LENGTH MARK: the decomposed spelling of ৌ is E-matra + this mark, but unlike the nukta letters this pair is not a composition exclusion (NFC recomposes it back to precomposed ৌ, which is what standard Bangla keyboards emit directly), so this mark is not expected to appear on its own", // BENGALI AU LENGTH MARK (matra)
}

const (
	chandrabindu   = 'ঁ' // BENGALI SIGN CANDRABINDU: dropped, no useful Latin spelling
	anusvara       = 'ং' // BENGALI SIGN ANUSVARA: mapped to "ng"
	visarga        = 'ঃ' // BENGALI SIGN VISARGA: mapped to "h"
	nukta          = '়' // BENGALI SIGN NUKTA: combines with ড/ঢ/য, see banglaNuktaConsonants
	aaLetter       = 'আ' // BENGALI LETTER AA: "aa" when word-initial, else "a"
	independentO   = 'ও' // BENGALI LETTER O: see the "-ওয়া" wa-glide comment on Transliterate
	bengaliBlockLo = 'ঀ' // first Bengali Unicode block code point
	bengaliBlockHi = '৿' // last Bengali Unicode block code point
)
