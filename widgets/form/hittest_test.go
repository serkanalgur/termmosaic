package form

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// The tests in this file are ADR 0010's: a widget handles a pointer event only
// if the pointer is inside its Bounds. The click half of that rule was already
// true for every widget here; the wheel half was not, and the three widgets that
// route a wheel through the shared optionList.wheelDelta helper were the ones
// that broke it — because Handle tested the wheel BEFORE the switch, so the
// Contains check the click path relies on was never reached.

// scrollable is one widget that scrolls on a wheel notch, with the knobs a test
// needs to observe the scroll and to place a pointer inside or outside it.
type scrollable struct {
	name   string
	bounds buffer.Rect
	labels []string
	make   func() termmosaic.Widget
}

// scrollables are the three widgets that consume a wheel notch. Each is built
// WIDER than its viewport so the wheel has somewhere to scroll to, which is what
// makes "declined" observable as "the offset did not move" rather than only as
// the boolean Handle returns.
func scrollables() []scrollable {
	labels := []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg", "hh"}
	bounds := buffer.Rect{X: 4, Y: 2, W: 8, H: 2}
	return []scrollable{
		{
			name: "Tabs", bounds: bounds, labels: labels,
			make: func() termmosaic.Widget { return NewTabs(bounds, labels) },
		},
		{
			name: "Select", bounds: bounds, labels: labels,
			make: func() termmosaic.Widget { return NewSelect(bounds, labels) },
		},
		{
			name: "Radio", bounds: bounds, labels: labels,
			make: func() termmosaic.Widget { return NewRadio(bounds, labels) },
		},
	}
}

// offsetOf reads the scroll offset through each widget's own accessor, so the
// test asserts on the widget's public state rather than on an internal field.
func offsetOf(t *testing.T, w termmosaic.Widget) int {
	t.Helper()
	switch v := w.(type) {
	case *Tabs:
		return v.Offset()
	case *Select:
		return v.Offset()
	case *Radio:
		return v.Offset()
	default:
		t.Fatalf("no accessor for %T", w)
		return 0
	}
}

// TestAWheelInsideBoundsScrolls is the positive half of ADR 0010 for all three
// scrollable widgets, and it is deliberately a test of BOTH the return value and
// the scroll: a widget that returns true without scrolling would pass a
// boolean-only assertion.
func TestAWheelInsideBoundsScrolls(t *testing.T) {
	for _, tc := range scrollables() {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.bounds
			w := tc.make()
			// The first cell of the widget's own rectangle, so this is as
			// unambiguously inside as a pointer can be.
			inside := [2]int{b.X, b.Y}

			if !wheelAt(w, inside[0], inside[1], false) {
				t.Fatalf("a wheel notch at %v, inside Bounds %+v, was not consumed", inside, b)
			}
			if got := offsetOf(t, w); got == 0 {
				t.Errorf("the wheel notch inside Bounds was consumed but the scroll offset is still 0")
			}

			scrolled := offsetOf(t, w)
			if !wheelAt(w, inside[0], inside[1], true) {
				t.Fatalf("an upward wheel notch at %v was not consumed", inside)
			}
			if got := offsetOf(t, w); got >= scrolled {
				t.Errorf("the upward notch did not scroll back: offset %d then %d", scrolled, got)
			}
		})
	}
}

// TestAWheelOutsideBoundsIsDeclined is the defect, pinned. Before ADR 0010 every
// one of these three widgets returned true for a notch at (900, 900) and
// scrolled, which is how a tab row swallowed every wheel notch in the markets
// example and the table under the pointer never saw one.
func TestAWheelOutsideBoundsIsDeclined(t *testing.T) {
	// A rect at a known screen position, so "outside" is unambiguous and cannot
	// be satisfied by a widget whose bounds happen to contain the origin.
	b := buffer.Rect{X: 4, Y: 2, W: 8, H: 2}
	outside := [][2]int{
		{900, 900},       // far away
		{b.X - 1, b.Y},   // one cell left
		{b.X, b.Y - 1},   // one cell above
		{b.X + b.W, b.Y}, // one cell right, exclusive edge
		{b.X, b.Y + b.H}, // one cell below, exclusive edge
		{-1, -1},         // negative
	}
	for _, tc := range scrollables() {
		t.Run(tc.name, func(t *testing.T) {
			for _, at := range outside {
				for _, up := range []bool{false, true} {
					w := tc.make()
					before := offsetOf(t, w)
					if wheelAt(w, at[0], at[1], up) {
						t.Errorf("a wheel notch (up=%v) at %v was consumed, want it declined: "+
							"outside Bounds %+v", up, at, b)
						continue
					}
					if got := offsetOf(t, w); got != before {
						t.Errorf("a wheel notch (up=%v) at %v moved the offset %d -> %d "+
							"while being declined", up, at, before, got)
					}
				}
			}
		})
	}
}

// TestADeclinedWheelReachesTheWidgetUnderneath is what the decline is FOR, stated
// as a test rather than as a comment. Two overlapping widgets is the shape
// ADR 0009 §6 describes — a command reachable by mouse is reached through the
// widget under the pointer — and a container that declined the notch is the only
// way the inner one gets it.
func TestADeclinedWheelReachesTheWidgetUnderneath(t *testing.T) {
	labels := []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg", "hh"}
	// The Select sits inside the Tabs' rectangle, so a notch over the Tabs' row
	// is over both.
	over := buffer.Rect{X: 4, Y: 2, W: 8, H: 2}
	inner := buffer.Rect{X: 4, Y: 2, W: 8, H: 2}
	tabs := NewTabs(over, labels)
	sel := NewSelect(inner, labels)

	// A notch BELOW both, which neither owns.
	if wheelAt(tabs, 40, 40, false) {
		t.Errorf("the outer Tabs consumed a notch at (40,40), outside both rects")
	}
	// And now one inside, where the inner widget is the one that scrolls. This
	// is the assertion that would fail under the old lenient wheel: the outer
	// Tabs answered first and the inner Select never saw the notch.
	if !wheelAt(sel, inner.X, inner.Y, false) {
		t.Fatalf("the inner Select did not consume a notch inside its own rect")
	}
	if got := sel.Offset(); got == 0 {
		t.Errorf("the inner Select consumed the notch but did not scroll")
	}
}

// TestAWheelNeverTakesFocus is the other half of the contract, and it is the
// half a bounds check alone would not give: a scroll gesture is a read, not a
// commitment. Markets' handleMouse already declines to move focus on the wheel,
// and it can only do that because the widgets themselves decline a notch they
// do not own.
func TestAWheelNeverTakesFocus(t *testing.T) {
	for _, tc := range scrollables() {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.bounds
			w := tc.make()
			wheelAt(w, b.X, b.Y, false)
			if f, ok := w.(termmosaic.Focusable); ok && f.Focused() {
				t.Errorf("a wheel notch took focus; scrolling is reading, and " +
					"moving the focus under the pointer rewrites the key hint mid-read")
			}
		})
	}
}

// TestTextWidgetsDeclineTheWheel is the per-widget judgement, pinned so it
// cannot change by accident.
//
// TextInput and TextArea handle the wheel by DECLINING it, inside Bounds and
// outside it alike, and that is a decision rather than an omission. Both are
// caret-position widgets whose vertical extent is not a viewport of a larger
// document the way a Select's is: TextInput is a single line with a horizontal
// offset, and a wheel notch has no axis that means anything on it. TextArea does
// scroll vertically, and wheel-to-scroll there is a plausible future feature —
// but it is a feature, and adding it under an ADR about who gets an event would
// be answering a different question than the one the ADR asks. The rule is
// "inside Bounds or nothing", and declining a wheel that is inside Bounds is
// still declining: there is no widget here whose business a notch is.
func TestTextWidgetsDeclineTheWheel(t *testing.T) {
	in := NewTextInput(buffer.Rect{X: 4, Y: 2, W: 8, H: 1})
	ta := NewTextArea(buffer.Rect{X: 4, Y: 2, W: 8, H: 3})
	for _, w := range []termmosaic.Widget{in, ta} {
		for _, at := range [][2]int{{4, 2}, {900, 900}} { // inside, then outside
			for _, up := range []bool{false, true} {
				if wheelAt(w, at[0], at[1], up) {
					t.Errorf("%T consumed a wheel notch (up=%v) at %v, want it declined: "+
						"a caret widget has no scroll axis the wheel means", w, up, at)
				}
			}
		}
	}
}

// TestEveryFormWidgetDeclinesAMouseEventOutsideItsBounds is the whole package in
// one table, and it covers every mouse ACTION, not just the click and the wheel.
// A widget that declines a press but claims a drag is the same defect wearing a
// different hat, and Tabs' wheel bug was exactly that: the Contains check was
// there, on a path the wheel never reached.
func TestEveryFormWidgetDeclinesAMouseEventOutsideItsBounds(t *testing.T) {
	labels := []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg", "hh"}
	b := buffer.Rect{X: 4, Y: 2, W: 10, H: 2}
	buttons := []termmosaic.MouseButton{
		termmosaic.MouseLeft, termmosaic.MouseRight, termmosaic.MouseMiddle,
		termmosaic.MouseWheelUp, termmosaic.MouseWheelDown,
	}
	actions := []termmosaic.MouseAction{
		termmosaic.MousePress, termmosaic.MouseRelease, termmosaic.MouseDrag, termmosaic.MouseMove,
	}
	outside := [][2]int{{900, 900}, {0, 0}, {b.X - 1, b.Y}, {b.X, b.Y + b.H}, {-5, -5}}

	for _, w := range []termmosaic.Widget{
		NewTabs(b, labels),
		NewSelect(b, labels),
		NewRadio(b, labels),
		NewCheckbox(b, "on"),
		NewToggle(b, "on"),
		NewButton(b, "go"),
		NewTextInput(b),
		NewTextArea(b),
	} {
		for _, btn := range buttons {
			for _, act := range actions {
				for _, at := range outside {
					// (0,0) is outside every rect here, all of which start at
					// X:4,Y:2, so no case in this loop is accidentally inside.
					ev := termmosaic.Event{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{
						X: at[0], Y: at[1], Button: btn, Action: act,
					}}
					if w.Handle(ev) {
						t.Errorf("%T consumed a %v/%v at %v, want it declined: outside Bounds %+v",
							w, btn, act, at, b)
					}
				}
			}
		}
	}
}
