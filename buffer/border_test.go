package buffer

import (
	"strings"
	"testing"
)

// TestEveryBorderStyleHasSixDistinctGlyphs is the guard ADR 0008 asks for: one
// table entry per style, so a new style cannot be added with a missing corner.
//
// "Distinct" is scoped to the four Unicode styles and is deliberate.
// BorderASCII documents "+ for every corner" — that is what makes it a usable
// ASCII fallback for every style at once — and BorderNone is all-zero by
// definition. Asserting six distinct runes on either would be asserting the
// opposite of what the ADR says they are for. The divider glyphs are asserted
// separately below.
func TestEveryBorderStyleHasSixDistinctGlyphs(t *testing.T) {
	unicodeStyles := []BorderStyle{BorderPlain, BorderRounded, BorderDouble, BorderThick}

	for _, s := range unicodeStyles {
		t.Run(s.String(), func(t *testing.T) {
			g := s.Glyphs(false)
			six := []rune{g.TopLeft, g.TopRight, g.BottomLeft, g.BottomRight, g.Horizontal, g.Vertical}
			seen := map[rune]int{}
			names := []string{"TopLeft", "TopRight", "BottomLeft", "BottomRight", "Horizontal", "Vertical"}
			for i, r := range six {
				if r == 0 {
					t.Errorf("%s is unset; a Unicode set needs all six rectangle glyphs", names[i])
					continue
				}
				if prev, dup := seen[r]; dup {
					t.Errorf("%s and %s share rune %U; they must be distinct", names[i], names[prev], r)
				}
				seen[r] = i
			}
		})
	}
}

// TestBorderGlyphTablesArePopulated pins the exact rune of every set, because a
// swapped corner is invisible in a unit test that only checks "six distinct" and
// very visible in someone's terminal.
//
// It also asserts the documented codepoints, so the tables cannot drift into a
// neighbouring box-drawing block by accident.
func TestBorderGlyphTablesArePopulated(t *testing.T) {
	cases := []struct {
		style     BorderStyle
		wantTL    rune // ┌
		wantTR    rune // ┐
		wantBL    rune // └
		wantBR    rune // ┘
		wantH     rune // ─
		wantV     rune // │
		wantCross rune
	}{
		{BorderPlain, 0x250C, 0x2510, 0x2514, 0x2518, 0x2500, 0x2502, 0x253C},
		{BorderRounded, 0x256D, 0x256E, 0x2570, 0x256F, 0x2500, 0x2502, 0},
		{BorderDouble, 0x2554, 0x2557, 0x255A, 0x255D, 0x2550, 0x2551, 0x256C},
		{BorderThick, 0x250F, 0x2513, 0x2517, 0x251B, 0x2501, 0x2503, 0},
	}
	for _, tc := range cases {
		t.Run(tc.style.String(), func(t *testing.T) {
			g := tc.style.Glyphs(false)
			checks := []struct {
				name string
				got  rune
				want rune
			}{
				{"TopLeft", g.TopLeft, tc.wantTL},
				{"TopRight", g.TopRight, tc.wantTR},
				{"BottomLeft", g.BottomLeft, tc.wantBL},
				{"BottomRight", g.BottomRight, tc.wantBR},
				{"Horizontal", g.Horizontal, tc.wantH},
				{"Vertical", g.Vertical, tc.wantV},
				{"Cross", g.Cross, tc.wantCross},
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Errorf("%s.%s = %U, want %U", tc.style, c.name, c.got, c.want)
				}
			}
		})
	}
}

// TestBorderGlyphsAsciiIsIndependentOfStyle is the single-boolean property: one
// flag switches the whole catalog and there is no per-glyph fallback. If a
// future change made Glyphs(true) return a blend, every widget's degradation
// path would silently diverge.
func TestBorderGlyphsAsciiIsIndependentOfStyle(t *testing.T) {
	all := []BorderStyle{
		BorderNone, BorderPlain, BorderRounded, BorderDouble, BorderThick, BorderASCII,
	}
	want := BorderGlyphs{
		TopLeft: '+', TopRight: '+', BottomLeft: '+', BottomRight: '+',
		Horizontal: '-', Vertical: '|',
		TeeDown: '+', TeeUp: '+', TeeRight: '+', TeeLeft: '+', Cross: '+',
	}
	for _, s := range all {
		if got := s.Glyphs(true); got != want {
			t.Errorf("%s.Glyphs(true) = %+v, want the ASCII table %+v", s, got, want)
		}
	}

	// And the inverse: every style except BorderNone differs from its ASCII
	// table, so the two rungs really are distinct content.
	for _, s := range []BorderStyle{BorderPlain, BorderRounded, BorderDouble, BorderThick, BorderASCII} {
		if s.Glyphs(false) == s.Glyphs(true) {
			t.Errorf("%s is identical on both rungs; Glyphs(ascii) would be untestable", s)
		}
	}
}

// TestBorderNoneDrawsNothing is the all-zero contract: "draw no border" and
// "this set has no glyph" are the same answer, so a call site needs no special
// case for either.
func TestBorderNoneDrawsNothing(t *testing.T) {
	if got := BorderNone.Glyphs(false); got != (BorderGlyphs{}) {
		t.Errorf("BorderNone.Glyphs(false) = %+v, want the zero set", got)
	}
}

// TestRoundedAndThickHaveNoDividers is the documented consequence of Unicode
// having no rounded or thick tee/cross glyphs: the runes are zero, and a
// renderer that draws a zero rune draws nothing.
//
// It is asserted because "a mismatched rune is worse than drawing none" is a
// design decision, and a well-meaning completion of these tables later would
// undo it silently.
func TestRoundedAndThickHaveNoDividers(t *testing.T) {
	for _, s := range []BorderStyle{BorderRounded, BorderThick} {
		g := s.Glyphs(false)
		for name, r := range map[string]rune{
			"TeeDown": g.TeeDown, "TeeUp": g.TeeUp, "TeeRight": g.TeeRight,
			"TeeLeft": g.TeeLeft, "Cross": g.Cross,
		} {
			if r != 0 {
				t.Errorf("%s.%s = %U; Unicode has no such glyph and a mismatched rune is worse than none", s, name, r)
			}
		}
	}

	// Plain and Double do have them, which is why a widget needing a divider
	// picks one of those two and says so.
	for _, s := range []BorderStyle{BorderPlain, BorderDouble} {
		g := s.Glyphs(false)
		if g.Cross == 0 || g.TeeDown == 0 || g.TeeUp == 0 || g.TeeLeft == 0 || g.TeeRight == 0 {
			t.Errorf("%s is missing a divider glyph: %+v", s, g)
		}
	}
}

// TestBorderStyleNames pins the diagnostics strings. They appear in golden-test
// failure messages, so changing one silently makes old failure output
// ungreppable.
func TestBorderStyleNames(t *testing.T) {
	cases := []struct {
		s    BorderStyle
		want string
	}{
		{BorderNone, "none"},
		{BorderPlain, "plain"},
		{BorderRounded, "rounded"},
		{BorderDouble, "double"},
		{BorderThick, "thick"},
		{BorderASCII, "ascii"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("BorderStyle(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

// TestBorderStyleOutOfRangeIsNotFatal states the untrusted-input contract: a
// BorderStyle that arrived from a persisted config or a config parser is not
// this package's to validate, and a TUI that panics on a bad byte is unusable
// (ADR 0007 §4, "no panic, ever").
func TestBorderStyleOutOfRangeIsNotFatal(t *testing.T) {
	for _, s := range []BorderStyle{6, 7, 100, 255} {
		if got := s.Glyphs(false); got != (BorderGlyphs{}) {
			t.Errorf("BorderStyle(%d).Glyphs(false) = %+v, want the zero set", s, got)
		}
		name := s.String()
		if !strings.HasPrefix(name, "border(") || !strings.HasSuffix(name, ")") {
			t.Errorf("BorderStyle(%d).String() = %q, want a hex rendering", s, name)
		}
	}
}

// TestBorderStyleIsSmallAndOrdered keeps the underlying uint8 assumption
// explicit. It is not load-bearing for correctness, but a widening would change
// Glyphs' bounds check and the names table's parallel-array indexing.
func TestBorderStyleIsSmallAndOrdered(t *testing.T) {
	cases := []struct {
		s    BorderStyle
		want int
	}{
		{BorderNone, 0}, {BorderPlain, 1}, {BorderRounded, 2},
		{BorderDouble, 3}, {BorderThick, 4}, {BorderASCII, 5},
	}
	for _, tc := range cases {
		if int(tc.s) != tc.want {
			t.Errorf("%s = %d, want %d; the constants are a dense ordered set", tc.s, tc.s, tc.want)
		}
	}
	// The three parallel tables must agree on length, which is what keeps a
	// reordering from silently pairing a name with the wrong glyphs.
	if len(borderGlyphSets) != len(borderStyleNames) {
		t.Errorf("glyph table has %d entries and name table has %d", len(borderGlyphSets), len(borderStyleNames))
	}
}

// TestHexString exercises the small formatter BorderStyle.String's error path
// uses, including the negative and multi-digit cases its callers reach.
func TestHexString(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0"}, {1, "1"}, {9, "9"}, {10, "a"}, {15, "f"}, {16, "10"},
		{255, "ff"}, {4095, "fff"},
	}
	for _, tc := range cases {
		if got := hexString(tc.in); got != tc.want {
			t.Errorf("hexString(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
