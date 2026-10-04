package data

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/input"
)

// keyFromSeq runs a key sequence through the real input decoder and returns the
// single event it produced.
//
// Every keyboard assertion in this package goes through the decoder rather than a
// hand-built Event, so a test cannot pass on an event no terminal would ever
// deliver.
func keyFromSeq(t *testing.T, seq string) termmosaic.Event {
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

// clickAt offers a left-button press at (x, y) to w and reports whether it was
// consumed.
func clickAt(w termmosaic.Widget, x, y int) bool {
	return w.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress},
	})
}

// wheelAt offers one wheel notch at (x, y) to w and reports whether it was
// consumed.
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

// pressKey is the rune form of press, for the keys with no escape sequence of
// their own.
func pressRune(w termmosaic.Widget, r rune) bool {
	return w.Handle(termmosaic.KeyEvent(r, 0))
}

// assertDifferent fails the test when a and b are equal.
//
// Every assertion here that compares a rendered value against a WANTED value
// derived from the same widget passes through this when it compares two
// widget-produced values, because a test that compares a widget's output with
// itself proves nothing. It is the direct answer to a test that cannot fail.
func assertDifferent(t *testing.T, what, a, b string) {
	t.Helper()
	if a == b {
		t.Fatalf("%s: both values are %q, so the assertion cannot fail; the test is vacuous", what, a)
	}
}

// termmosaicModShift is ModShift, spelled out here so a test that needs a
// modified mouse event does not have to import the root package under a second
// name.
const termmosaicModShift = termmosaic.ModShift

// termmosaicMouse builds a wheel-down event carrying a modifier.
func termmosaicMouse(x, y int, mod termmosaic.KeyMod) termmosaic.Event {
	return termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: termmosaic.MouseWheelDown, Action: termmosaic.MousePress, Mod: mod},
	}
}
