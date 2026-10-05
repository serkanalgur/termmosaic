package form

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// screenRows renders root on a w-by-h screen and returns the rows with trailing
// spaces trimmed, so an expected string need not count the background blanks a
// widget paints.
func screenRows(t *testing.T, w, h int, root termmosaic.Widget) []string {
	t.Helper()
	sink := widgettest.Render(t, w, h, 1, root)
	out := make([]string, h)
	for y := 0; y < h; y++ {
		out[y] = widgettest.Row(sink, y)
	}
	return out
}

// wantRow asserts that row y of a rendered screen equals want. Both sides are
// compared with trailing spaces trimmed, so a caller passing a raw buffer row
// need not count the background blanks a widget paints.
func wantRow(t *testing.T, got []string, y int, want string) {
	t.Helper()
	if y >= len(got) {
		t.Fatalf("screen has %d rows, wanted row %d", len(got), y)
	}
	g, w := strings.TrimRight(got[y], " "), strings.TrimRight(want, " ")
	if g != w {
		t.Errorf("row %d\n got %q\nwant %q", y, g, w)
	}
}

// rowOf renders one row of w cells from buf, without trimming, for assertions
// about exact cell contents.
func rowOf(buf *buffer.Buffer, y, w int) string {
	var b strings.Builder
	row := buf.Row(y)
	for x := 0; x < w && x < len(row); x++ {
		b.WriteRune(row[x].Ch)
	}
	return b.String()
}

// cells returns w cells of row y starting at column x0, indexed by RUNE rather
// than by byte: slicing a rendered row by byte lands inside a three-byte
// truncation marker and makes an assertion read as a control character.
func cells(buf *buffer.Buffer, y, x0, w int) string {
	row := buf.Row(y)
	var b strings.Builder
	for x := x0; x < x0+w && x < len(row); x++ {
		if x < 0 {
			continue
		}
		b.WriteRune(row[x].Ch)
	}
	return b.String()
}

// decodeKey runs seq through input.Decode and returns the single event it
// produced. Every keyboard assertion in this package goes through the real
// decoder rather than a hand-built Event, so a test cannot pass on an event the
// decoder would never emit.
func decodeKey(t *testing.T, seq string) termmosaic.Event {
	t.Helper()
	ev, n, status := input.Decode([]byte(seq), input.DefaultConfig())
	if status != input.StatusOK {
		t.Fatalf("decoding %q: status %v, want ok", seq, status)
	}
	if n != len(seq) {
		t.Errorf("decoding %q: consumed %d bytes of %d", seq, n, len(seq))
	}
	if ev.Kind == termmosaic.EventNone {
		t.Fatalf("decoding %q produced no event", seq)
	}
	return ev
}

// press runs seq through the decoder and offers the event to w, requiring that w
// consumed it. A key in a documented contract that a widget does not consume is a
// test failure, not a silent pass.
func press(t *testing.T, w termmosaic.Widget, seq string) {
	t.Helper()
	if !w.Handle(decodeKey(t, seq)) {
		t.Errorf("%q was not consumed by %T", seq, w)
	}
}

// types offers each printable character of s to w as its own key event.
func types(t *testing.T, w termmosaic.Widget, s string) {
	t.Helper()
	for _, r := range s {
		if !w.Handle(termmosaic.KeyEvent(r, 0)) {
			t.Errorf("rune %q was not consumed by %T", r, w)
		}
	}
}

// clickAt offers a left-button press at (x, y) to w and reports whether it was
// consumed.
func clickAt(w termmosaic.Widget, x, y int) bool {
	return w.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress},
	})
}

// dragAt offers a left-button drag at (x, y) to w.
func dragAt(w termmosaic.Widget, x, y int) bool {
	return w.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: termmosaic.MouseLeft, Action: termmosaic.MouseDrag},
	})
}

// wheelAt offers a wheel notch to w and reports whether it was consumed.
func wheelAt(w termmosaic.Widget, x, y int, up bool) bool {
	button := termmosaic.MouseWheelDown
	if up {
		button = termmosaic.MouseWheelUp
	}
	return w.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: button, Action: termmosaic.MousePress},
	})
}

// paste offers a bracketed-paste payload to w through the real decoder, so the
// one-event-per-paste rule of ADR 0005 §4 is exercised end to end rather than
// simulated.
func paste(t *testing.T, w termmosaic.Widget, payload string) {
	t.Helper()
	ev, n, status := input.Decode([]byte("\x1b[200~"+payload+"\x1b[201~"), input.DefaultConfig())
	if status != input.StatusOK || n != len("\x1b[200~"+payload+"\x1b[201~") {
		t.Fatalf("decoding a bracketed paste of %d bytes: status %v, consumed %d", len(payload), status, n)
	}
	if ev.Kind != termmosaic.EventPaste {
		t.Fatalf("decoding a bracketed paste produced %v, want a paste event", ev.Kind)
	}
	if !w.Handle(ev) {
		t.Errorf("a paste of %d bytes was not consumed by %T", len(payload), w)
	}
}

// assertDifferent fails the test if a and b are equal.
//
// Every assertion in this package that compares a rendered value against an
// expectation goes through this when it is comparing two widget-produced
// values, because a test that compares a widget's output with itself proves
// nothing. It is the direct answer to a test that can never fail.
func assertDifferent(t *testing.T, what, a, b string) {
	t.Helper()
	if a == b {
		t.Fatalf("%s: both values are %q, so the assertion cannot fail; the test is vacuous", what, a)
	}
}
