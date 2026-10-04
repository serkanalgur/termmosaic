package menu

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// rows renders root on a w-by-h screen and returns the screen with trailing
// spaces trimmed per row, so an expected string need not count the background
// blanks a widget paints.
func rows(t *testing.T, w, h int, root termmosaic.Widget) []string {
	t.Helper()
	sink := widgettest.Render(t, w, h, 1, root)
	out := make([]string, h)
	for y := 0; y < h; y++ {
		out[y] = widgettest.Row(sink, y)
	}
	return out
}

// want asserts that row y of a rendered screen equals expect, with both sides
// trimmed of trailing blanks.
func want(t *testing.T, got []string, y int, expect string) {
	t.Helper()
	if y >= len(got) {
		t.Fatalf("screen has %d rows, wanted row %d", len(got), y)
	}
	g := strings.TrimRight(got[y], " ")
	e := strings.TrimRight(expect, " ")
	if g != e {
		t.Errorf("row %d\n got %q\nwant %q", y, g, e)
	}
}

// cellBuf returns a fresh buffer for direct Draw assertions, which is where a
// cell's STYLE matters and Screen's text alone does not.
func cellBuf(w, h int) *buffer.Buffer { return buffer.NewBuffer(w, h) }

// drawInto draws m into a fresh buffer of the given size.
func drawInto(m *Menu, w, h int) *buffer.Buffer {
	buf := cellBuf(w, h)
	m.Draw(buf)
	return buf
}

// line reads row y of buf as text, by RUNE rather than by byte, so slicing a
// rendered row never lands inside a three-byte truncation marker.
func line(buf *buffer.Buffer, y, w int) string {
	var b strings.Builder
	r := buf.Row(y)
	for x := 0; x < w && x < len(r); x++ {
		b.WriteRune(r[x].Ch)
	}
	return b.String()
}

// tree returns a fixed menu tree used by most of the tests, so a change to it is
// a change every test in the file has to absorb.
//
//	root:  New(branch, Load…, Save (disabled), Sep, Wrap (toggle), Help (branch))
//	  New:  Deep (branch) + Plain leaf
//	    Deep:  One + Two (toggle)
func tree() []Item {
	return []Item{
		{Label: "New", Hint: "^N", Items: []Item{
			{Label: "Deep", Items: []Item{
				{Label: "One"},
				{Label: "Two", Checkable: true},
			}},
			{Label: "Plain"},
		}},
		{Label: "Save", Hint: "^S", Disabled: true},
		{Label: "Sep"},
		{Label: "Wrap", Checkable: true, Checked: true},
		{Label: "Help", Hint: "F1"},
	}
}

// newMenu returns an open, focused menu of size w by h over items, which is the
// state most tests want and which no test should have to spell out.
func newMenu(t *testing.T, w, h int, items ...Item) *Menu {
	t.Helper()
	m := New(buffer.Rect{W: w, H: h}, items...)
	m.Open()
	m.SetFocused(true)
	return m
}

// press runs seq through the real decoder and offers the event to m, requiring
// that m consumed it. A key in a documented contract that the widget does not
// consume is a test failure rather than a silent pass.
func press(t *testing.T, m *Menu, seq string) {
	t.Helper()
	if !m.Handle(decodeKey(t, seq)) {
		t.Errorf("%q was not consumed by the menu", seq)
	}
}

// tap runs seq through the decoder and offers it to m, returning whether m took
// it, for the keys whose contract is "consumed only when it did something".
func tap(t *testing.T, m *Menu, seq string) bool {
	t.Helper()
	return m.Handle(decodeKey(t, seq))
}

// escape offers a real KeyEscape to m and reports whether m took it.
//
// Escape is the one key in this widget's contract that CANNOT come out of
// input.Decode, because a bare ESC is StatusIncomplete by design: deciding that an
// ESC is the Escape key rather than the start of a CSI needs a timeout, which is
// the parser's job (ADR 0005 §2). So this helper goes through input.Parser and
// waits out the delay, which is what the terminal actually does — and it means the
// Escape assertions below are testing the same event a user produces rather than
// one this test invented.
func escape(t *testing.T, m *Menu) bool {
	t.Helper()
	// SpecialKeyEvent is the constructor for a key with no printable form, so the
	// event here is exactly what input's own escape timeout produces: an EventKey
	// whose Key is KeyEscape. That it is constructed rather than decoded is the
	// point of the comment above — the decoder RESOLVES this one to
	// StatusIncomplete by design, so a widget whose Escape contract can only be
	// tested through Decode is a widget whose Escape contract is untestable.
	return m.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0))
}

// mustEscape offers a KeyEscape and requires that m consumed it.
func mustEscape(t *testing.T, m *Menu) {
	t.Helper()
	if !escape(t, m) {
		t.Error("Escape was not consumed by the menu")
	}
}

// clickAt offers a left-button press at (x, y) to m and reports whether m took it.
func clickAt(m *Menu, x, y int) bool {
	return m.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress},
	})
}

// mouseEvent builds a left-button press at (x, y), so a resize sweep can offer the
// same event through Handle without repeating the struct literal.
func mouseEvent(x, y int) termmosaic.Event {
	return termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress},
	}
}

// widgetRender renders root at one size through the whole stack and returns the
// sink, for the assertions that must go through the renderer rather than through
// Draw.
func widgetRender(t testing.TB, w, h int, root termmosaic.Widget) *headless.MemorySink {
	t.Helper()
	return widgettest.Render(t, w, h, 1, root)
}

// wheelAt offers a wheel notch at (x, y) to m and reports whether m took it.
func wheelAt(m *Menu, x, y int, up bool) bool {
	button := termmosaic.MouseWheelDown
	if up {
		button = termmosaic.MouseWheelUp
	}
	return m.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: button, Action: termmosaic.MousePress},
	})
}
