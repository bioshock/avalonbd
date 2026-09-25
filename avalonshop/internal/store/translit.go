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
//     consonant joins directly with what follows (a conjunct); or
//   - the consonant ends a word, which also drops the inherent vowel.
//
// Runes this function does not recognise (Latin letters, digits, punctuation,
// whitespace, the taka sign U+09F3 which sits in the Bengali block but is not a
// letter, and any other unmapped rune) are passed through unchanged, so
// Slugify's existing NFD/filter pass drops them exactly as it does today.
func Transliterate(s string) string {
	runes := []rune(s)
	n := len(runes)
	var b strings.Builder

	for i := 0; i < n; {
		r := runes[i]

		if sound, ok := banglaConsonants[r]; ok {
			var next rune
			hasNext := i+1 < n
			if hasNext {
				next = runes[i+1]
			}
			matra, isMatra := banglaMatras[next]

			switch {
			case hasNext && next == hasanta:
				// Hasanta suppresses the inherent vowel; the next consonant
				// (a conjunct) is processed on its own in the next iteration.
				b.WriteString(sound)
				i += 2
			case hasNext && isMatra:
				// The matra replaces the inherent vowel.
				b.WriteString(sound)
				b.WriteString(matra)
				i += 2
			default:
				// Bare consonant: keep the inherent vowel unless this is the
				// last letter of the word (end of string, or followed by
				// something outside the Bangla letter chain).
				b.WriteString(sound)
				if hasNext && isBanglaJoiner(next) {
					b.WriteString("o")
				}
				i++
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
			// BENGALI SIGN NUKTA with no preceding base consonant we recognise, or a
			// stray hasanta with no preceding consonant: nothing to attach to,
			// drop it like any other unmapped rune.
			i++
			continue
		}

		if digit, ok := banglaDigits[r]; ok {
			b.WriteRune(digit)
			i++
			continue
		}

		if sound, ok := banglaVowels[r]; ok {
			if r == 'আ' { // BENGALI LETTER AA
				// Word-initial আ reads as a long "a": aam, not am.
				if i == 0 || !isBanglaJoiner(runes[i-1]) {
					b.WriteString("aa")
					i++
					continue
				}
			}
			b.WriteString(sound)
			i++
			continue
		}

		// Not a Bangla rune we transliterate: pass it through unchanged.
		// Slugify's NFD/filter pass keeps [a-z0-9], lowercases Latin letters,
		// and turns everything else (including the taka sign) into a
		// separator, exactly as it does today for runes with no mapping.
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
// including the inherent vowel (the caller adds "o" when appropriate).
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
