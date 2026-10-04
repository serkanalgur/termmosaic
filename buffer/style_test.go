package buffer

import (
	"testing"
	"unsafe"
)

// TestStyleIsTwelveBytesAndComparable pins the two properties ADR 0008's
// whole "Style is free" argument rests on: the struct is small enough to live
// in registers, and it is comparable with == so a widget can store one in a
// field and compare it.
//
// The 12 is 10 bytes of data plus 2 of tail padding. It is not Cell's 16 and it
// does not have to be padding-free — Style is never byte-compared.
func TestStyleIsTwelveBytesAndComparable(t *testing.T) {
	if got := unsafe.Sizeof(Style{}); got != 12 {
		t.Fatalf("sizeof(Style) = %d, want 12; the ABI argument in ADR 0008 assumes 3 fields in registers", got)
	}

	a := NewStyle(NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold)
	b := NewStyle(NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold)
	if a != b {
		t.Errorf("two identical styles compare unequal: %+v vs %+v", a, b)
	}
	if a == DefaultStyle {
		t.Error("a coloured style must not equal DefaultStyle")
	}

	// Comparability must not extend to the padded bytes: two styles that agree
	// on every field agree, and that is the only thing == may look at.
	c := NewStyle(NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold|AttrItalic)
	if a == c {
		t.Error("styles differing only in Attr must compare unequal")
	}
}

// TestCellRemainsSixteenBytesWithStyle is the guard ADR 0008 requires against
// someone "simplifying" Cell by embedding Style in it.
//
// Style is the 10-byte flat projection of Cell's three styling fields.
// Embedding it would make the cell 28+ bytes with interior padding, and ADR
// 0002's TestCellHasNoPadding would correctly fail: bytes.Equal over a row
// would compare undefined padding and the tier-1 row skip would report phantom
// changes, i.e. permanent flicker.
func TestCellRemainsSixteenBytesWithStyle(t *testing.T) {
	if got := unsafe.Sizeof(Cell{}); got != 16 {
		t.Fatalf("sizeof(Cell) = %d, want 16, even now that Style exists", got)
	}

	// The projection must be a field copy in both directions, with nothing left
	// over: an embedded Style would make Cell.Style() lossy, because the
	// continuation flag lives outside the style.
	st := NewStyle(NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold)
	if got := st.Cell('x').Style(); got != st {
		t.Errorf("Cell(r).Style() = %+v, want %+v", got, st)
	}
	if got := st.Blank(); got.Ch != ' ' || got.Style() != st {
		t.Errorf("Blank() = %+v, want a space in %+v", got, st)
	}
	c := NewCell('x', st)
	if c.Ch != 'x' || c.FG != st.FG || c.BG != st.BG || c.Attr != st.Attr {
		t.Errorf("NewCell wrote %+v, want %+v", c, st)
	}
	if got := ContinuationCell(st); !got.IsContinuation() || got.Style() != st || got.Ch != 0 {
		t.Errorf("ContinuationCell = %+v, want a continuation in %+v with no rune", got, st)
	}
}

// TestStyleZeroIsUnsetAndResolvesToDefaultStyle is the test for the sharpest
// edge in ADR 0008: Colour(0) is opaque black, so Style{} reads as black on
// black rather than as obviously empty.
//
// The assertions are deliberately three-in-one: the sentinel must be
// identifiable (IsUnset), it must resolve to the terminal's own colours rather
// than to a guess, and Resolved must be idempotent so a widget that resolves
// twice — through its own field and again inside SetSpans — changes nothing.
func TestStyleZeroIsUnsetAndResolvesToDefaultStyle(t *testing.T) {
	var zero Style
	if !zero.IsUnset() {
		t.Error("the zero Style must report IsUnset")
	}
	if zero.FG != NewColour(0, 0, 0) {
		t.Errorf("Style{}.FG = %v, want opaque black — the sentinel is defined in terms of that", zero.FG)
	}

	got := zero.Resolved()
	if got != DefaultStyle {
		t.Errorf("Style{}.Resolved() = %+v, want DefaultStyle %+v", got, DefaultStyle)
	}
	if got.FG != DefaultColour || got.BG != DefaultColour || got.Attr != 0 {
		t.Errorf("Resolved did not yield the terminal defaults: %+v", got)
	}
	if again := got.Resolved(); again != got {
		t.Errorf("Resolved is not idempotent: %+v then %+v", got, again)
	}

	// PlainStyle is documented as the same thing resolved, so it must resolve to
	// DefaultStyle rather than to black on black.
	if PlainStyle.Resolved() != DefaultStyle {
		t.Errorf("PlainStyle.Resolved() = %+v, want DefaultStyle", PlainStyle.Resolved())
	}
}

// TestResolvedIsIdempotentOverEveryNamedStyle keeps the idempotence claim
// honest for the values widgets actually hold, not just the zero one. A style
// built by NewStyle or a With* method must be a fixed point of Resolved.
func TestResolvedIsIdempotentOverEveryNamedStyle(t *testing.T) {
	for _, st := range []Style{
		PlainStyle, BoldStyle, FaintStyle, ItalicStyle, UnderlineStyle,
		ReverseStyle, StrikeStyle, EmphasisStyle, HeadingStyle, MutedStyle,
		DefaultStyle,
		NewStyle(NewColour(1, 2, 3), NewColour(4, 5, 6), AttrBold),
		NewStyle(NewColour(0, 0, 0), NewColour(0, 0, 0), 0),
		Style{FG: UnsetColour, BG: UnsetColour, Attr: AttrItalic},
	} {
		once := st.Resolved()
		if twice := once.Resolved(); twice != once {
			t.Errorf("%+v: Resolved is not idempotent: %+v then %+v", st, once, twice)
		}
		if once.FG.IsUnset() || once.BG.IsUnset() {
			t.Errorf("%+v: Resolved left the inherit marker in place: %+v", st, once)
		}
	}
}

// TestResolvedPreservesOpaqueBlack is the other half of the sentinel contract:
// resolving must not turn an author's deliberate black into the terminal
// default. Only UnsetColour and the zero Style mean "unspecified".
//
// The last sub-case records the one collision the design cannot avoid, so that
// it is on file rather than discovered by an application: NewStyle(black, black,
// 0) IS the zero Style, so it resolves to DefaultStyle. An author who genuinely
// wants black on black cannot get it through the sentinel-resolving path. That
// is ADR 0008's sharpest edge, named there as risk 1, and the mitigation is the
// authoring rule plus this test rather than a type change — but the trap is real
// and is stated here rather than papered over.
func TestResolvedPreservesOpaqueBlack(t *testing.T) {
	black := NewColour(0, 0, 0)
	st := Style{FG: black, BG: black, Attr: AttrBold}
	got := st.Resolved()
	if got.FG != black || got.BG != black {
		t.Errorf("Resolved turned authored black into %v/%v", got.FG, got.BG)
	}
	if got.FG == DefaultColour {
		t.Error("opaque black resolved to the terminal default")
	}

	// With a non-zero attribute the style is unambiguously not the sentinel, so
	// NewStyle's black survives intact.
	if got := NewStyle(black, black, AttrBold).Resolved(); got.FG != black {
		t.Errorf("NewStyle(black, black, bold).Resolved().FG = %v, want black", got.FG)
	}

	// The documented collision: with no attributes there is nothing to
	// distinguish it from "the author chose nothing".
	attrless := NewStyle(black, black, 0)
	if !attrless.IsUnset() {
		t.Error("NewStyle(black, black, 0) is expected to collide with the sentinel")
	}
	if attrless.Resolved() != DefaultStyle {
		t.Errorf("NewStyle(black, black, 0).Resolved() = %+v, want DefaultStyle; "+
			"opaque black is unreachable through the no-attribute path", attrless.Resolved())
	}
}

// TestStylePatchUsesUnsetColourOnly is the test that pins the sentinel's reason
// for existing.
//
// The distinction Patch draws is "specified" versus "not specified", and the
// ONLY way a channel says "not specified" is UnsetColour. Opaque black is a
// perfectly good colour and must override. If UnsetColour were removed, or if
// Patch treated a zero channel as unspecified, the framework would lose the
// ability to express "inherit this channel" — which is what makes "bold and
// this foreground, inherited background" expressible at all.
func TestStylePatchUsesUnsetColourOnly(t *testing.T) {
	accent := NewColour(0x30, 0xc0, 0x80)
	base := NewStyle(NewColour(1, 1, 1), NewColour(2, 2, 2), 0)

	t.Run("unset channel inherits", func(t *testing.T) {
		// The override is a raw Style, not a NewStyle result: NewStyle resolves
		// UnsetColour to DefaultColour, which is correct for a concrete style
		// but would turn an "inherit this channel" override into a "reset to the
		// terminal default" override. Patch is the one place that reads the
		// marker, and this is how the deferred theme type will feed it.
		got := base.Patch(Style{FG: accent, BG: UnsetColour, Attr: AttrBold})
		if got.FG != accent {
			t.Errorf("FG = %v, want the patch's %v", got.FG, accent)
		}
		if got.BG != NewColour(2, 2, 2) {
			t.Errorf("BG = %v, want the base's, because UnsetColour means inherit", got.BG)
		}
		if !got.Attr.Has(AttrBold) {
			t.Errorf("Attr = %v, want bold ORed in", got.Attr)
		}
		// And the inheriting style still resolves correctly afterwards.
		if r := got.Resolved(); r.BG != NewColour(2, 2, 2) {
			t.Errorf("resolved BG = %v, want the inherited %v", r.BG, NewColour(2, 2, 2))
		}
	})

	t.Run("NewStyle resolves the marker, so it is not an inherit override", func(t *testing.T) {
		got := base.Patch(NewStyle(accent, UnsetColour, 0))
		if got.BG != DefaultColour {
			t.Errorf("BG = %v, want the terminal default NewStyle resolved it to", got.BG)
		}
	})

	t.Run("opaque black overrides", func(t *testing.T) {
		black := NewColour(0, 0, 0)
		got := base.Patch(NewStyle(black, black, 0))
		if got.FG != black || got.BG != black {
			t.Errorf("an opaque black patch must override, got %+v", got)
		}
		if got.FG == DefaultColour || got.BG == DefaultColour {
			t.Error("Patch conflated opaque black with the terminal default")
		}
	})

	t.Run("attributes accumulate and cannot be cleared", func(t *testing.T) {
		b := base.Patch(Style{Attr: AttrBold}).Patch(Style{Attr: AttrItalic})
		if b.Attr != AttrBold|AttrItalic {
			t.Errorf("Attr = %v, want bold|italic", b.Attr)
		}
		if cleared := b.Patch(Style{}); cleared.Attr != AttrBold|AttrItalic {
			t.Errorf("Patch cleared attributes (%v); it must only accumulate", cleared.Attr)
		}
	})

	t.Run("patching a zero base inherits through it", func(t *testing.T) {
		// The base may legitimately be the unset sentinel: a widget whose field
		// the author never touched. Patch must not resolve it away first.
		var zero Style
		got := zero.Patch(NewStyle(accent, accent, AttrBold)).Resolved()
		if got.FG != accent || got.BG != accent || got.Attr != AttrBold {
			t.Errorf("patching the zero Style = %+v, want the override", got)
		}
	})

	t.Run("default colour still overrides", func(t *testing.T) {
		got := base.Patch(NewStyle(DefaultColour, DefaultColour, 0))
		if got.FG != DefaultColour || got.BG != DefaultColour {
			t.Errorf("an explicit terminal-default patch must override, got %+v", got)
		}
	})
}

// TestWithMethodsAccumulate pins the one sanctioned derivation chain and its
// difference from Patch: WithAttr ORs, and With* on UnsetColour is a no-op so a
// chain cannot accidentally introduce an inherit marker mid-build.
func TestWithMethodsAccumulate(t *testing.T) {
	accent := NewColour(0x30, 0xc0, 0x80)
	surface := NewColour(0x10, 0x14, 0x1c)

	got := DefaultStyle.WithBG(surface).WithAttr(AttrBold).WithFG(accent)
	if got.FG != accent || got.BG != surface || got.Attr != AttrBold {
		t.Errorf("chain = %+v, want FG=%v BG=%v bold", got, accent, surface)
	}
	if got.WithAttr(AttrItalic).Attr != AttrBold|AttrItalic {
		t.Error("WithAttr must OR, so intent accumulates")
	}
	if got.WithAttr(AttrItalic).Attr == got.Attr {
		t.Error("WithAttr returned the receiver unchanged")
	}

	// The receiver is a value: a With* call must not mutate its argument.
	original := DefaultStyle
	_ = original.WithFG(accent)
	if original != DefaultStyle {
		t.Error("WithFG mutated the receiver")
	}

	if got.WithFG(UnsetColour) != got || got.WithBG(UnsetColour) != got {
		t.Error("With* must ignore UnsetColour rather than installing an inherit marker")
	}
}

// TestNamedStylesUseOnlyAttributes states the framework's no-default-colours
// decision as an assertion rather than a comment, so a later "helpful" accent
// added to HeadingStyle fails here.
//
// The colours being Colour(0) rather than DefaultColour is deliberate and is
// documented on the var block: these are patch bases, so an unset channel in an
// override inherits the attribute instead of an authored black.
func TestNamedStylesUseOnlyAttributes(t *testing.T) {
	cases := []struct {
		name string
		st   Style
		want Attr
	}{
		{"PlainStyle", PlainStyle, 0},
		{"BoldStyle", BoldStyle, AttrBold},
		{"FaintStyle", FaintStyle, AttrFaint},
		{"ItalicStyle", ItalicStyle, AttrItalic},
		{"UnderlineStyle", UnderlineStyle, AttrUnderline},
		{"ReverseStyle", ReverseStyle, AttrReverse},
		{"StrikeStyle", StrikeStyle, AttrStrike},
		{"EmphasisStyle", EmphasisStyle, AttrBold},
		{"HeadingStyle", HeadingStyle, AttrBold | AttrUnderline},
		{"MutedStyle", MutedStyle, AttrFaint},
	}
	for _, tc := range cases {
		if tc.st.Attr != tc.want {
			t.Errorf("%s.Attr = %v, want %v", tc.name, tc.st.Attr, tc.want)
		}
		// The channels must be UnsetColour rather than Colour(0), or Resolved
		// would hand back opaque black and every framework default would be
		// invisible-ish on a dark terminal.
		if tc.name != "PlainStyle" && (!tc.st.FG.IsUnset() || !tc.st.BG.IsUnset()) {
			t.Errorf("%s carries a real colour (%v/%v); the framework ships no default colours",
				tc.name, tc.st.FG, tc.st.BG)
		}
		// Every named style must resolve to the terminal's own colours plus its
		// attributes. This is the behaviour ADR 0008 requires of the defaults.
		got := tc.st.Resolved()
		if got.FG != DefaultColour || got.BG != DefaultColour {
			t.Errorf("%s.Resolved() = %+v, want the terminal's own colours", tc.name, got)
		}
		if got.Attr != tc.want {
			t.Errorf("%s.Resolved().Attr = %v, want %v", tc.name, got.Attr, tc.want)
		}
	}

	// And they compose: a named default plus an application colour, which is the
	// whole reason the channels are unset rather than black. The override has to
	// spell BG as UnsetColour — a bare Style{FG: accent} means "black
	// background", which is the same rule as TestStylePatchUsesUnsetColourOnly
	// seen from the other side.
	accent := NewColour(0x30, 0xc0, 0x80)
	got := HeadingStyle.Patch(Style{FG: accent, BG: UnsetColour}).Resolved()
	if got.FG != accent || got.BG != DefaultColour || got.Attr != AttrBold|AttrUnderline {
		t.Errorf("HeadingStyle patched with a foreground = %+v, want the accent over the terminal background", got)
	}

	// The inverse hazard, pinned explicitly: an override that does not mention
	// the background at all overrides it with opaque black. This is deliberate —
	// UnsetColour exists precisely so "did not mention it" is expressible — but
	// it is the second sharp edge in this file and the deferred theme type will
	// have to live with it.
	if got := HeadingStyle.Patch(Style{FG: accent}).Resolved(); got.BG != NewColour(0, 0, 0) {
		t.Errorf("a bare Style{FG: accent} override gave BG = %v; Patch documents black as an override", got.BG)
	}
}

// TestUnsetColourSentinelsAreDistinctFromEachOther is a one-line guard that the
// two sentinels stayed separate values. If UnsetColour were ever given
// DefaultColour's value, Patch could not tell "inherit" from "the terminal's
// own colour" and every themed override would silently break.
func TestUnsetColourSentinelsAreDistinctFromEachOther(t *testing.T) {
	if UnsetColour == DefaultColour {
		t.Fatal("UnsetColour and DefaultColour must be different sentinels")
	}
	if UnsetColour != 0xFFFFFFFE {
		t.Errorf("UnsetColour = %#x, want 0xFFFFFFFE", uint32(UnsetColour))
	}
	// Both sit outside 0x00RRGGBB, so neither can ever be a real colour.
	for _, c := range []Colour{DefaultColour, UnsetColour} {
		if _, g, b := c.RGB(); uint32(c)>>8 == 0 && c <= 0x00FFFFFF {
			t.Errorf("%v is inside the RGB space", c)
		} else if g == 0 && b == 0 && uint32(c)&0xFFFFFF == 0 && c == 0 {
			t.Errorf("%v is opaque black", c)
		}
	}
}

// TestStyleDoesNotResolveViaCell is the boundary the encoder depends on: a Style
// is written into a cell verbatim, so an author who forgets Resolved gets the
// documented black-on-black rather than a silent correction — and the author
// rule ("never a partial composite literal") is what prevents it.
func TestStyleDoesNotResolveViaCell(t *testing.T) {
	// This is the footgun, stated as an assertion so the trade is on record:
	// Style.FG alone compiles and is opaque black.
	partial := Style{FG: NewColour(0xff, 0, 0)}
	c := partial.Cell('x')
	if c.BG != NewColour(0, 0, 0) {
		t.Errorf("a partial literal must leave BG opaque black, got %v", c.BG)
	}
	if c.FG == DefaultColour {
		t.Error("Cell must not resolve the style behind the author's back")
	}

	// The safe path gives the same foreground and a sane background.
	safe := partial.WithBG(DefaultColour).Resolved()
	if safe.FG != NewColour(0xff, 0, 0) || safe.BG != DefaultColour {
		t.Errorf("safe path = %+v, want the author's fg over the terminal background", safe)
	}
}
