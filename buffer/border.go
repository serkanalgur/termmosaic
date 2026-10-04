package buffer

// BorderStyle names one of the shared box-drawing glyph sets.
//
// This file is the ONLY place in TermMosaic that contains a box-drawing rune
// literal. A widget holds a BorderStyle and asks for glyphs; a widget NEVER
// spells a border rune itself. Three authors each with their own glyph table is
// the collision this type exists to prevent, and it is also the collision that
// makes "BorderThick" mean different runes in different widgets.
//
// The tables live here rather than with a widget because this is pure data
// about a rendering convention with no state, exactly like RuneWidth. A widget
// package holding them would force a widget that merely draws a divider to
// import the border widget's package.
type BorderStyle uint8

const (
	// BorderNone draws no border. Its glyph set is all-zero, which the renderer
	// reads as "draw nothing here".
	BorderNone BorderStyle = iota
	// BorderPlain is the single-line set: ─ │ ┌ ┐ └ ┘
	// (U+2500, U+2502, U+250C, U+2510, U+2514, U+2518).
	BorderPlain
	// BorderRounded is ─ │ with ╭ ╮ ╰ ╯
	// (U+2500, U+2502, U+256D, U+256E, U+2570, U+256F).
	BorderRounded
	// BorderDouble is ═ ║ ╔ ╗ ╚ ╝
	// (U+2550, U+2551, U+2554, U+2557, U+255A, U+255D).
	BorderDouble
	// BorderThick is ━ ┃ ┏ ┓ ┗ ┛
	// (U+2501, U+2503, U+250F, U+2513, U+2517, U+251B).
	BorderThick
	// BorderASCII is - | and + for every corner, tee and cross. It is the
	// degradation target for every other style rather than a style in its own
	// right: a widget asks for BorderPlain and gets ASCII when the terminal
	// cannot do Unicode. It is nonetheless a named constant so tests can assert
	// it and so Glyphs(true) has one table to return.
	BorderASCII
)

// BorderGlyphs is the six-glyph rectangle plus the five divider glyphs a Tree
// or a Table needs.
//
// A zero rune means "this set has no glyph for this position" and the renderer
// draws NOTHING there. That is deliberate: BorderRounded and BorderThick have
// no tee or cross glyphs in Unicode, and drawing a mismatched rune is worse
// than drawing none. A widget that needs a divider therefore picks a set that
// has one (BorderPlain or BorderDouble) and documents that choice.
//
// BorderNone returns the all-zero set, which is how "draw no border" and "this
// style has no glyph" are the same answer rather than two special cases at
// every call site.
type BorderGlyphs struct {
	// TopLeft, TopRight, BottomLeft and BottomRight are the four corner runes.
	TopLeft, TopRight, BottomLeft, BottomRight rune
	// Horizontal and Vertical are the edge runes, repeated along the top,
	// bottom, left and right edges respectively.
	Horizontal, Vertical rune
	// TeeDown, TeeUp, TeeRight and TeeLeft join an edge to a divider, and
	// Cross is a four-way junction. Zero means the set has no such glyph.
	TeeDown, TeeUp, TeeRight, TeeLeft, Cross rune
}

// Glyphs returns the glyph set for s.
//
// ascii == true returns the BorderASCII table for EVERY style — a single
// boolean switches the whole catalog and there is no per-glyph fallback. The
// one boolean the widget path passes is `caps.Unicode == false`, and a widget
// must never branch on caps.Unicode itself.
//
// An unknown style value yields the all-zero set rather than panicking: a
// BorderStyle arriving from a persisted config is untrusted input, and a TUI
// that panics on a bad byte is unusable (ADR 0007 §4).
//
// This is the ONLY box-drawing lookup in TermMosaic.
func (s BorderStyle) Glyphs(ascii bool) BorderGlyphs {
	if ascii {
		return asciiGlyphs
	}
	if s >= BorderStyle(len(borderGlyphSets)) {
		return BorderGlyphs{}
	}
	return borderGlyphSets[s]
}

// String returns the style's name for diagnostics and golden-test failure
// messages: "none", "plain", "rounded", "double", "thick", "ascii". An unknown
// value renders in hex rather than panicking, matching Glyphs.
func (s BorderStyle) String() string {
	if s < BorderStyle(len(borderStyleNames)) {
		return borderStyleNames[s]
	}
	return "border(" + hexString(uint64(s)) + ")"
}

// borderGlyphSets is the one glyph table in TermMosaic, indexed by BorderStyle
// so that Glyphs is an array read rather than a switch.
//
// The ordering must stay in step with the BorderStyle constants and with
// borderStyleNames; TestEveryBorderStyleHasSixDistinctGlyphs reads all three
// together so a reordering cannot pass unnoticed.
var borderGlyphSets = [...]BorderGlyphs{
	BorderNone: {},
	BorderPlain: {
		TopLeft: '┌', TopRight: '┐', BottomLeft: '└', BottomRight: '┘',
		Horizontal: '─', Vertical: '│',
		TeeDown: '┬', TeeUp: '┴', TeeRight: '┤', TeeLeft: '├', Cross: '┼',
	},
	BorderRounded: {
		TopLeft: '╭', TopRight: '╮', BottomLeft: '╰', BottomRight: '╯',
		Horizontal: '─', Vertical: '│',
		// Rounded corners have no tee or cross glyph in Unicode; zeros here are
		// the documented "draw nothing", not an oversight.
	},
	BorderDouble: {
		TopLeft: '╔', TopRight: '╗', BottomLeft: '╚', BottomRight: '╝',
		Horizontal: '═', Vertical: '║',
		TeeDown: '╦', TeeUp: '╩', TeeRight: '╣', TeeLeft: '╠', Cross: '╬',
	},
	BorderThick: {
		TopLeft: '┏', TopRight: '┓', BottomLeft: '┗', BottomRight: '┛',
		Horizontal: '━', Vertical: '┃',
		// As with rounded: no tee or cross exists for a thick line.
	},
	BorderASCII: {},
}

// borderStyleNames parallels borderGlyphSets. ASCII has its own name even
// though Glyphs(true) returns asciiGlyphs for every style, because the name
// identifies the value rather than the table.
var borderStyleNames = [...]string{"none", "plain", "rounded", "double", "thick", "ascii"}

// asciiGlyphs is the degradation rung every Unicode style falls back to. It is
// a separate variable rather than borderGlyphSets[BorderASCII] because
// Glyphs(true) must return it regardless of the requested style.
var asciiGlyphs = BorderGlyphs{
	TopLeft: '+', TopRight: '+', BottomLeft: '+', BottomRight: '+',
	Horizontal: '-', Vertical: '|',
	TeeDown: '+', TeeUp: '+', TeeRight: '+', TeeLeft: '+', Cross: '+',
}

// hexString renders v in lowercase hex with no leading zeros beyond the digits
// themselves. BorderStyle.String uses it for an out-of-range value; it is here
// rather than in colour.go because it is a formatting concern of this file.
func hexString(v uint64) string {
	const digits = "0123456789abcdef"
	if v == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v&0xF]
		v >>= 4
	}
	return string(buf[i:])
}
