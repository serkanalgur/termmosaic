package ansi

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

var (
	red   = buffer.NewColour(0xff, 0x00, 0x00)
	green = buffer.NewColour(0x30, 0xc0, 0x80)
	blue  = buffer.NewColour(0x00, 0x00, 0xff)
)

func TestAppendCursorPositionIsOneBased(t *testing.T) {
	// Terminals are one-based; the diff converts from zero-based cell
	// coordinates. An off-by-one here shifts every frame by a cell.
	if got := string(AppendCursorPosition(nil, 1, 1)); got != "\x1b[1;1H" {
		t.Errorf("got %q, want %q", got, "\x1b[1;1H")
	}
	if got := string(AppendCursorPosition(nil, 21, 45)); got != "\x1b[21;45H" {
		t.Errorf("got %q, want %q", got, "\x1b[21;45H")
	}
}

func TestAppendRune(t *testing.T) {
	cases := []struct {
		r    rune
		want string
	}{
		{'a', "a"},
		{'~', "~"},
		{0xe9, "é"},
		{0x2588, "█"}, // three bytes
		{0x1F600, "\U0001F600"},
		{-1, "�"},
		{0x110000, "�"},
		{0xD800, "�"}, // lone surrogate
	}
	for _, tc := range cases {
		if got := string(AppendRune(nil, tc.r)); got != tc.want {
			t.Errorf("AppendRune(%U) = %q, want %q", tc.r, got, tc.want)
		}
	}
}

func TestAppendRuneInvalidDoesNotShiftTheLine(t *testing.T) {
	// A dropped bad rune would misalign every cell after it on the line, so an
	// invalid rune becomes U+FFFD rather than vanishing.
	out := AppendRune(nil, -1)
	if len(out) == 0 {
		t.Fatal("an invalid rune must still emit bytes")
	}
}

func TestSGRTrueColor(t *testing.T) {
	e := Encoder{Depth: DepthTrueColor}
	got := e.StyleString(Style{FG: red, BG: blue})
	want := "\x1b[38;2;255;0;0;48;2;0;0;255m"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSGRDefaultColours(t *testing.T) {
	e := Encoder{Depth: DepthTrueColor}
	got := e.StyleString(Style{FG: buffer.DefaultColour, BG: buffer.DefaultColour})
	if got != "\x1b[39;49m" {
		t.Errorf("got %q, want %q", got, "\x1b[39;49m")
	}
}

func TestSGRAttributesAlwaysResetFirst(t *testing.T) {
	// There is no portable "SGR 22" that reliably clears bold without also
	// clearing faint, so an attribute change must be preceded by a full reset.
	e := Encoder{Depth: DepthTrueColor}
	got := e.StyleString(Style{FG: red, BG: buffer.DefaultColour, Attr: buffer.AttrBold})
	if !strings.HasPrefix(got, "\x1b[0;1;") {
		t.Errorf("got %q, want a reset before the attribute codes", got)
	}
	all := e.StyleString(Style{
		FG: red, BG: buffer.DefaultColour,
		Attr: buffer.AttrBold | buffer.AttrFaint | buffer.AttrItalic |
			buffer.AttrUnderline | buffer.AttrReverse | buffer.AttrStrike,
	})
	want := "\x1b[0;1;2;3;4;7;9;38;2;255;0;0;49m"
	if all != want {
		t.Errorf("got %q, want %q", all, want)
	}
}

func TestSGRPlainStyleEmitsReset(t *testing.T) {
	// An empty style still needs a sequence: the previous cell may have set
	// attributes, and nothing turns them off except a reset.
	e := Encoder{Depth: DepthTrueColor}
	if got := e.StyleString(Style{FG: buffer.DefaultColour, BG: buffer.DefaultColour}); got != "\x1b[39;49m" {
		t.Errorf("got %q", got)
	}
	e2 := Encoder{Depth: DepthNone}
	if got := e2.StyleString(Style{}); got != "\x1b[0m" {
		t.Errorf("at DepthNone got %q, want a bare reset", got)
	}
}

func TestSGRColourDepthLadder(t *testing.T) {
	style := Style{FG: red, BG: buffer.DefaultColour}
	cases := []struct {
		depth Depth
		want  string
	}{
		{DepthTrueColor, "\x1b[38;2;255;0;0;49m"},
		{Depth256, "\x1b[38;5;9;49m"},
		{Depth16, "\x1b[91;49m"},
	}
	for _, tc := range cases {
		e := Encoder{Depth: tc.depth}
		if got := e.StyleString(style); got != tc.want {
			t.Errorf("depth %s: got %q, want %q", tc.depth, got, tc.want)
		}
	}
}

func TestSGRBrightNamedColoursUseThe90sAnd100s(t *testing.T) {
	// Indices 8-15 have no SGR codes in the 30-47 range; a terminal reads 40 as
	// background black, not bright black. Getting this wrong makes bold text on a
	// dark background render as black-on-black.
	e := Encoder{Depth: Depth16}
	if got := e.StyleString(Style{FG: red, BG: buffer.DefaultColour}); !strings.HasPrefix(got, "\x1b[91;") {
		t.Errorf("bright red foreground should be 91, got %q", got)
	}
	bright := buffer.NewColour(0xff, 0xff, 0xff)
	if got := e.StyleString(Style{FG: buffer.DefaultColour, BG: bright}); got != "\x1b[39;107m" {
		t.Errorf("bright white background should be 107, got %q", got)
	}
	dark := buffer.NewColour(0x00, 0x00, 0x00)
	if got := e.StyleString(Style{FG: dark, BG: buffer.DefaultColour}); got != "\x1b[30;49m" {
		t.Errorf("black foreground should be 30, got %q", got)
	}
}

func TestNoColorSuppressesColourKeepsAttributes(t *testing.T) {
	e := Encoder{Depth: DepthTrueColor, NoColor: true}
	got := e.StyleString(Style{FG: red, BG: blue, Attr: buffer.AttrBold})
	if strings.Contains(got, "38;2") || strings.Contains(got, "48;2") {
		t.Errorf("NO_COLOR leaked a colour sequence: %q", got)
	}
	if got != "\x1b[0;1m" {
		t.Errorf("got %q, want %q", got, "\x1b[0;1m")
	}
	if e.ColoursEnabled() {
		t.Error("ColoursEnabled must be false under NO_COLOR")
	}
}

func TestDepthNoneEmitsNoColour(t *testing.T) {
	e := Encoder{Depth: DepthNone}
	if e.ColoursEnabled() {
		t.Error("ColoursEnabled must be false at DepthNone")
	}
	got := e.StyleString(Style{FG: red, BG: blue})
	if strings.Contains(got, "38") || strings.Contains(got, "48") {
		t.Errorf("got %q, want no colour codes", got)
	}
}

func TestQuantiserHookIsUsed(t *testing.T) {
	// A caller-supplied quantiser must be consulted, not bypassed. This is the
	// hook the still-OPEN colour decision refers to.
	c := &stubQuantiser{n16: 3, n256: 200}
	e := Encoder{Depth: Depth16, Quantiser: c}
	if got := e.StyleString(Style{FG: red, BG: buffer.DefaultColour}); got != "\x1b[33;49m" {
		t.Errorf("got %q, want the stub's 33", got)
	}
	e2 := Encoder{Depth: Depth256, Quantiser: c}
	if got := e2.StyleString(Style{FG: red, BG: buffer.DefaultColour}); got != "\x1b[38;5;200;49m" {
		t.Errorf("got %q, want the stub's 38;5;200", got)
	}
	if c.calls != 2 {
		t.Errorf("quantiser was called %d times, want 2", c.calls)
	}
}

type stubQuantiser struct {
	n16, n256 uint8
	calls     int
}

func (s *stubQuantiser) Nearest16(buffer.Colour) uint8  { s.calls++; return s.n16 }
func (s *stubQuantiser) Nearest256(buffer.Colour) uint8 { s.calls++; return s.n256 }

func TestAppendSGRDelta(t *testing.T) {
	e := Encoder{Depth: DepthTrueColor}
	base := Style{FG: buffer.DefaultColour, BG: buffer.DefaultColour}

	if got := e.AppendSGRDelta(nil, base, base); len(got) != 0 {
		t.Errorf("no change must emit nothing, got %q", got)
	}

	// Only the foreground changed: only the foreground is restated. Emitting the
	// background again would cost 16 wasted bytes per styling change.
	onlyFG := Style{FG: red, BG: buffer.DefaultColour}
	if got := string(e.AppendSGRDelta(nil, base, onlyFG)); got != "\x1b[38;2;255;0;0m" {
		t.Errorf("got %q, want a foreground-only sequence", got)
	}

	// Only the background changed.
	onlyBG := Style{FG: buffer.DefaultColour, BG: blue}
	if got := string(e.AppendSGRDelta(nil, base, onlyBG)); got != "\x1b[48;2;0;0;255m" {
		t.Errorf("got %q, want a background-only sequence", got)
	}

	// Both changed: both are restated.
	both := Style{FG: red, BG: blue}
	if got := string(e.AppendSGRDelta(nil, base, both)); got != "\x1b[38;2;255;0;0;48;2;0;0;255m" {
		t.Errorf("got %q, want both components", got)
	}
}

func TestAppendSGRDeltaAttributeChangeIsAbsolute(t *testing.T) {
	e := Encoder{Depth: DepthTrueColor}
	prev := Style{FG: red, BG: buffer.DefaultColour}
	next := Style{FG: red, BG: buffer.DefaultColour, Attr: buffer.AttrBold}
	// An attribute change resets the terminal, which also clears both colours, so
	// a delta is not merely an optimisation away — it would be wrong.
	got := string(e.AppendSGRDelta(nil, prev, next))
	if !strings.HasPrefix(got, "\x1b[0;1;") {
		t.Errorf("got %q, want an absolute reset-then-set sequence", got)
	}
	if !strings.Contains(got, "38;2") {
		t.Errorf("got %q, want the foreground restated after the reset", got)
	}
}

func TestAppendSGRDeltaRespectsNoColor(t *testing.T) {
	e := Encoder{Depth: DepthTrueColor, NoColor: true}
	base := Style{FG: buffer.DefaultColour, BG: buffer.DefaultColour}
	next := Style{FG: red, BG: red}
	if got := e.AppendSGRDelta(nil, base, next); len(got) != 0 {
		t.Errorf("under NO_COLOR a colour-only change emits nothing, got %q", got)
	}
}

func TestDepthString(t *testing.T) {
	for d, want := range map[Depth]string{
		DepthTrueColor: "truecolor",
		Depth256:       "256",
		Depth16:        "16",
		DepthNone:      "none",
		Depth(99):      "unknown",
	} {
		if got := d.String(); got != want {
			t.Errorf("Depth(%d).String() = %q, want %q", int(d), got, want)
		}
	}
}

// TestStyleIsAnAliasOfBufferStyle pins ADR 0008's collapse of the two
// same-named, same-fielded Style types into one.
//
// Assignability in both directions WITHOUT a conversion is the assertion: if
// someone turns the alias back into a defined type, the two blocks below stop
// compiling, which is the failure the ADR is about — a widget style that is
// ==-comparable but not assignable to what the diff tracks.
func TestStyleIsAnAliasOfBufferStyle(t *testing.T) {
	var s Style = buffer.NewStyle(red, blue, buffer.AttrBold)
	var b buffer.Style = s
	if b != buffer.NewStyle(red, blue, buffer.AttrBold) {
		t.Fatalf("alias round trip lost the value: %+v", b)
	}

	// And the projection the diff actually uses is the cell's own Style method.
	c := buffer.NewCell('a', buffer.NewStyle(red, blue, buffer.AttrBold))
	if c.Style() != s {
		t.Errorf("Cell.Style() = %+v, want %+v", c.Style(), s)
	}

	// The encoder must accept the alias directly, with no conversion at the
	// call site: that is the property that makes the diff's tracked style and
	// the widget-facing style the same type.
	enc := Encoder{Depth: DepthTrueColor}
	if got, want := enc.StyleString(s), enc.StyleString(b); got != want {
		t.Errorf("encoding through the alias differs: %q vs %q", got, want)
	}
}

// TestUnsetColourIsNotARealColour pins the sentinel's whole reason for
// existing: it must be distinguishable from every RGB colour AND from the
// terminal-default sentinel, or Patch could not tell "inherit" from "default".
func TestUnsetColourIsNotARealColour(t *testing.T) {
	if buffer.UnsetColour.IsDefault() {
		t.Error("UnsetColour must not equal DefaultColour")
	}
	if buffer.DefaultColour.IsUnset() {
		t.Error("DefaultColour must not be the inherit marker")
	}
	for _, c := range []buffer.Colour{
		buffer.NewColour(0, 0, 0),
		buffer.NewColour(255, 255, 254),
		buffer.DefaultColour,
	} {
		if c.IsUnset() {
			t.Errorf("%v reports as the inherit marker", c)
		}
	}
}

// TestEncoderTreatsUnresolvedColourAsDefault is the backstop for a style written
// into a cell without being resolved first. Resolved() maps the marker to
// DefaultColour, so this is unreachable through the normal path — but
// quantising 0xFFFFFFFE as near-white would produce a confusing frame rather
// than a visible no-op, so the encoder refuses to.
func TestEncoderTreatsUnresolvedColourAsDefault(t *testing.T) {
	enc := Encoder{Depth: DepthTrueColor}
	leaked := Style{FG: buffer.UnsetColour, BG: buffer.DefaultColour}
	want := enc.StyleString(Style{FG: buffer.DefaultColour, BG: buffer.DefaultColour})
	if got := enc.StyleString(leaked); got != want {
		t.Errorf("an unresolved colour encoded as %q, want the terminal default %q", got, want)
	}
}

func BenchmarkAppendSGR(b *testing.B) {
	e := Encoder{Depth: DepthTrueColor}
	s := Style{FG: green, BG: buffer.NewColour(0x18, 0x1c, 0x24), Attr: buffer.AttrBold}
	dst := make([]byte, 0, 64)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dst = e.AppendSGR(dst[:0], s)
	}
	_ = dst
}

func BenchmarkAppendSGRDelta(b *testing.B) {
	e := Encoder{Depth: DepthTrueColor}
	prev := Style{FG: green, BG: buffer.NewColour(0x18, 0x1c, 0x24)}
	next := Style{FG: red, BG: prev.BG}
	dst := make([]byte, 0, 64)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dst = e.AppendSGRDelta(dst[:0], prev, next)
	}
	_ = dst
}
