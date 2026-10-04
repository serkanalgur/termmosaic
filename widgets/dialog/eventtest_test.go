package dialog

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// newRenderer builds a renderer over sink with colour suppressed when noColor is
// set, so the NO_COLOR tests go through the SAME path a terminal does rather than
// asserting on a buffer the renderer never saw.
func newRenderer(t *testing.T, sink *headless.MemorySink, w, h int, noColor bool) *render.Renderer {
	t.Helper()
	return render.New(sink, render.Config{
		Width: w, Height: h, Caps: termmosaic.DefaultCaps(), NoColor: noColor,
	})
}

// keyFromSeq decodes an escape sequence through the real input decoder, so no test
// can pass on an event the decoder would never emit.
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

// press offers the decoded sequence seq to w and reports whether it was consumed.
func press(t *testing.T, w termmosaic.Widget, seq string) bool {
	t.Helper()
	return w.Handle(keyFromSeq(t, seq))
}

// mustPress is press for a key the documented contract says the widget consumes.
// A key that is in the contract but not consumed is a failure, never a silent
// pass.
func mustPress(t *testing.T, w termmosaic.Widget, seq string) {
	t.Helper()
	if !press(t, w, seq) {
		t.Errorf("%q was not consumed by %T", seq, w)
	}
}

// clickAt offers a left-button press at (x, y) to w.
func clickAt(w termmosaic.Widget, x, y int) bool {
	return w.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress},
	})
}

// wheelAt offers one wheel notch of button at (x, y) to w.
func wheelAt(w termmosaic.Widget, button termmosaic.MouseButton, x, y int) bool {
	return w.Handle(termmosaic.Event{
		Kind:  termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: x, Y: y, Button: button, Action: termmosaic.MouseMove},
	})
}

// focus gives w focus, for the tests whose keys are consumed only while focused.
func focus(w termmosaic.Widget) {
	if f, ok := w.(termmosaic.Focusable); ok {
		f.SetFocused(true)
	}
}

// rect is the shorthand for a dialog's bounds at the origin.
func rect(w, h int) buffer.Rect { return buffer.Rect{W: w, H: h} }

// infoDialog returns a titled info dialog of size w by h with one body line.
func infoDialog(w, h int) *Dialog {
	d := New(rect(w, h), VariantInfo)
	d.SetTitle("Saved", buffer.PlainStyle)
	d.SetBodyString("Your changes were written.")
	return d
}

// confirmDialog returns a titled confirm dialog of size w by h with a body line
// long enough to wrap at narrow widths.
func confirmDialog(w, h int) *Dialog {
	d := New(rect(w, h), VariantConfirm)
	d.SetTitle("Overwrite file", buffer.PlainStyle)
	d.SetBodyString("The existing copy will be replaced.")
	return d
}

// choiceDialog returns a titled choice dialog of size w by h over n labels.
func choiceDialog(w, h, n int) *Dialog {
	d := New(rect(w, h), VariantChoice)
	d.SetTitle("Theme", buffer.PlainStyle)
	d.SetBodyString("Pick one.")
	labels := make([]string, n)
	for i := range labels {
		labels[i] = string(rune('a'+i%26)) + "-option"
	}
	d.SetChoices(labels)
	return d
}

// plain is a body span set with no attributes, for the tests that want the body
// drawn in the terminal's own colours.
func plainSpans(s string) []buffer.Span { return []buffer.Span{buffer.NewSpan(s, buffer.PlainStyle)} }

// blockOf is used by the composition test to prove a caller can configure the
// chrome through Block without Dialog re-exporting anything.
func blockOf(d *Dialog) *block.Block { return d.Block() }
