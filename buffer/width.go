package buffer

// RuneWidth returns the number of terminal cells a rune occupies: 0, 1, or 2.
//
// PROVISIONAL and UNVERIFIED. ADR 0002 lists wide characters and grapheme
// clusters as an open, unmeasured question, and STATUS.md defers
// internationalization. This is the narrow correct thing, not the right thing:
//   - width comes from a hard-coded East Asian Wide/Fullwidth range table, not
//     from Unicode EastAsianWidth data, so newly assigned wide blocks are
//     wrong until the table is updated;
//   - combining marks and format characters are zero-width;
//   - grapheme clusters (ZWJ emoji sequences, flags, Hangul jamo) are NOT
//     composed. A flag emoji renders as two cells' worth of junk rather than
//     one 2-cell glyph, and a ZWJ sequence renders as several glyphs.
//
// No benchmark exercised this until v0.1.0: the wide paths are now measured in
// buffer/wideglyph_test.go (the writers and Wrap) and internal/diff/wideglyph_test.go
// (a wide scene through the diff). Those measurements show the wide path is NOT
// slower — a wide rune costs one extra branch and a second cell write, and saves
// the loop iteration and RuneWidth call that two narrow runes would have needed.
// What they do not do is make the table correct; it is still hand-written and still
// needs a decision of its own if internationalization is scoped.
func RuneWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0 // control characters occupy no cell
	}
	if inRanges(r, zeroWidthRanges) {
		return 0
	}
	if inRanges(r, wideRanges) {
		return 2
	}
	return 1
}

// StringWidth returns the total cell width of s, summing RuneWidth.
func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += RuneWidth(r)
	}
	return w
}

type runeRange struct{ lo, hi rune }

// inRanges reports whether r falls in any of the sorted-or-unsorted ranges.
// Binary search would be premature: the tables are short and scanned linearly.
func inRanges(r rune, ranges []runeRange) bool {
	for _, rg := range ranges {
		if r >= rg.lo && r <= rg.hi {
			return true
		}
	}
	return false
}

// wideRanges are the East Asian Wide and Fullwidth blocks plus the emoji
// presentation blocks TermMosaic assumes are double-width.
//
// Sources are approximate and deliberately frozen; see RuneWidth's warning.
var wideRanges = []runeRange{
	{0x1100, 0x115F},   // Hangul Jamo initial consonants
	{0x2E80, 0x303E},   // CJK Radicals, Kangxi, CJK Symbols
	{0x3041, 0x33FF},   // Hiragana .. CJK Compatibility
	{0x3400, 0x4DBF},   // CJK Extension A
	{0x4E00, 0x9FFF},   // CJK Unified Ideographs
	{0xA000, 0xA4CF},   // Yi
	{0xA960, 0xA97F},   // Hangul Jamo Extended-A
	{0xAC00, 0xD7A3},   // Hangul Syllables
	{0xF900, 0xFAFF},   // CJK Compatibility Ideographs
	{0xFE10, 0xFE19},   // Vertical forms
	{0xFE30, 0xFE6F},   // CJK Compatibility Forms
	{0xFF00, 0xFF60},   // Fullwidth Forms
	{0xFFE0, 0xFFE6},   // Fullwidth signs
	{0x16FE0, 0x16FE4}, // Tangut/Nushu iteration marks
	{0x17000, 0x18AFF}, // Tangut
	{0x1B000, 0x1B2FF}, // Kana Supplement/Extended
	{0x1F004, 0x1F004}, // mahjong red dragon
	{0x1F0CF, 0x1F0CF}, // playing card black joker
	{0x1F18E, 0x1F18E}, // AB button
	{0x1F191, 0x1F19A}, // squared symbols
	{0x1F1E6, 0x1F1FF}, // regional indicators (flags)
	{0x1F200, 0x1F251}, // enclosed ideographic supplement
	{0x1F300, 0x1F64F}, // misc symbols & emoticons
	{0x1F680, 0x1F6FF}, // transport & map
	{0x1F7E0, 0x1F7EB}, // geometric shapes extended
	{0x1F900, 0x1F9FF}, // supplemental symbols & pictographs
	{0x1FA70, 0x1FAFF}, // symbols & pictographs extended-A
	{0x20000, 0x3FFFD}, // CJK Extension B and beyond
}

// zeroWidthRanges are combining marks, format characters and variation
// selectors, which occupy no cell of their own.
var zeroWidthRanges = []runeRange{
	{0x0300, 0x036F},   // combining diacritical marks
	{0x0483, 0x0489},   // Cyrillic combining
	{0x0591, 0x05BD},   // Hebrew points
	{0x0610, 0x061A},   // Arabic marks
	{0x064B, 0x065F},   // Arabic marks
	{0x0670, 0x0670},   // Arabic superscript alef
	{0x06D6, 0x06DC},   // Arabic marks
	{0x0730, 0x074A},   // Syriac marks
	{0x07A6, 0x07B0},   // NKo marks
	{0x0900, 0x0903},   // Devanagari signs
	{0x093A, 0x094F},   // Devanagari vowels and marks
	{0x0951, 0x0957},   // Devanagari stress
	{0x0E31, 0x0E31},   // Thai vowel
	{0x0E34, 0x0E3A},   // Thai vowels
	{0x0E47, 0x0E4E},   // Thai tone marks
	{0x1AB0, 0x1AFF},   // combining diacritical marks extended
	{0x1DC0, 0x1DFF},   // combining diacritical marks supplement
	{0x200B, 0x200F},   // zero width space .. RTL mark
	{0x202A, 0x202E},   // bidi embedding
	{0x2060, 0x2064},   // word joiner and invisible operators
	{0x20D0, 0x20F0},   // combining marks for symbols
	{0xFE00, 0xFE0F},   // variation selectors
	{0xFE20, 0xFE2F},   // combining half marks
	{0xFEFF, 0xFEFF},   // zero width no-break space
	{0xE0100, 0xE01EF}, // variation selectors supplement
}
