package dialog

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// TestDrawIsZeroAllocation is the hot-path claim ADR 0008 §4 makes for every widget
// in the catalog: nothing derived from the size or the content is built inside Draw.
//
// It is measured over all three variants and at four sizes, because the failure is
// not uniform — a choice dialog allocates where an info dialog does not, and a wide
// one allocates where a narrow one does not. Testing only the easy case would be
// testing the case that was never at risk.
//
// AllocsPerRun does its own warm-up, so the first frame's adaptation is not counted;
// drawAll warms the cache explicitly anyway so a reader can see why the first frame
// is excluded rather than trusting the harness.
func TestDrawIsZeroAllocation(t *testing.T) {
	for _, size := range []struct{ w, h int }{
		{40, 9}, {80, 24}, {13, 6}, {200, 60},
	} {
		for _, v := range []Variant{VariantInfo, VariantConfirm, VariantChoice} {
			t.Run(v.String(), func(t *testing.T) {
				d := New(rect(size.w, size.h), v)
				d.SetTitle("Alloc", buffer.PlainStyle)
				d.SetBodyString("a body long enough to wrap over several rows in a narrow dialog")
				d.SetChoices([]string{"one", "two", "three", "four", "five"})
				buf := cellBuf(size.w, size.h)
				drawAll(d, buf, 3) // the first frame adapts, by design

				if got := testing.AllocsPerRun(200, func() { d.Draw(buf) }); got != 0 {
					t.Errorf("Draw allocated %v times per frame at %dx%d", got, size.w, size.h)
				}
			})
		}
	}
}

// TestDrawIsZeroAllocationAfterAFocusMove is the harder half of the same claim.
//
// The layout cache is keyed on the rect and the resolved STYLES, so moving the focus
// must not invalidate it — and if a focus change did force a rebuild, every Tab in
// an application would allocate a wrapped body. This measures the frame AFTER the
// move, which is the one that would carry the cost.
func TestDrawIsZeroAllocationAfterAFocusMove(t *testing.T) {
	d := choiceDialog(40, 12, 8)
	focus(d)
	buf := cellBuf(40, 12)
	drawAll(d, buf, 3)

	d.SetFocus(4)
	d.Draw(buf) // absorb the focus change itself

	if got := testing.AllocsPerRun(200, func() { d.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per frame after a focus move", got)
	}

	// And a Tab — the interaction a user performs most — must not allocate either.
	if got := testing.AllocsPerRun(200, func() { d.Handle(keyFromSeq(t, keyTab)) }); got != 0 {
		t.Errorf("a Tab allocated %v times: the key path rebuilds the layout", got)
	}
}

// TestDrawIsZeroAllocationWhileTheChoiceListScrolls covers the virtualized path,
// because scrolling is the one thing a dialog does that touches the engine: a
// scroll changes which labels are visible, so a widget that truncated them inside
// Draw would allocate on every wheel notch.
func TestDrawIsZeroAllocationWhileTheChoiceListScrolls(t *testing.T) {
	d := choiceDialog(40, 12, 500)
	focus(d)
	buf := cellBuf(40, 12)
	drawAll(d, buf, 3)

	for _, wheel := range []termmosaic.MouseButton{termmosaic.MouseWheelDown, termmosaic.MouseWheelUp} {
		wheelAt(d, wheel, 2, 2)
		d.Draw(buf)
		if got := testing.AllocsPerRun(200, func() {
			wheelAt(d, wheel, 2, 2)
			d.Draw(buf)
		}); got != 0 {
			t.Errorf("scrolling allocated %v times per notch", got)
		}
	}
}

// TestDrawIsZeroAllocationAtDegenerateSizes closes the loop on the degenerate-size
// contract: a size where the layout CLIPS is exactly where a fallback path tends to
// grow one, and an allocation there would be invisible at any comfortable size.
func TestDrawIsZeroAllocationAtDegenerateSizes(t *testing.T) {
	for _, size := range []struct{ w, h int }{
		{1, 1}, {2, 2}, {3, 3}, {5, 4}, {7, 5}, {11, 4},
	} {
		d := choiceDialog(size.w, size.h, 30)
		buf := cellBuf(max(size.w, 1), max(size.h, 1))
		drawAll(d, buf, 3)
		if got := testing.AllocsPerRun(200, func() { d.Draw(buf) }); got != 0 {
			t.Errorf("Draw allocated %v times per frame at %dx%d", got, size.w, size.h)
		}
	}
}

// TestAResizeCostsOneAdaptationAndThenNothing pins the ADR 0007 §3 rule that makes
// the frame path free: a widget adapts ONCE per distinct rect, not once per frame
// and not once per row.
//
// It is asserted through AllocsPerRun's own semantics — the second draw at a new
// size must be free — because "caches the result" is a claim about a count, and the
// count is what the allocator can see.
func TestAResizeCostsOneAdaptationAndThenNothing(t *testing.T) {
	d := confirmDialog(40, 9)
	buf := cellBuf(200, 60)
	drawAll(d, buf, 3)

	// One draw at a new size adapts. Every draw after it is free.
	atSize(d, 60, 20)
	d.Draw(buf)
	if got := testing.AllocsPerRun(100, func() { d.Draw(buf) }); got != 0 {
		t.Errorf("after a resize, Draw still allocated %v times per frame", got)
	}

	// Growing and shrinking repeatedly re-adapts each time, which is correct: the
	// rect changed. What must not happen is an allocation on a STEADY frame, so the
	// sweep ends on a frame at a size it has already drawn.
	for _, size := range []struct{ w, h int }{{80, 30}, {40, 12}, {20, 8}, {100, 40}, {40, 12}} {
		atSize(d, size.w, size.h)
		d.Draw(buf)
	}
	if got := testing.AllocsPerRun(100, func() { d.Draw(buf) }); got != 0 {
		t.Errorf("after a resize sweep, Draw allocated %v times per frame", got)
	}
}

// TestHandleIsZeroAllocation covers the input path, which is the other hot loop: a
// key arrives per keystroke, and a widget that rebuilt its truncated labels on each
// one would allocate at typing speed.
//
// It is measured on a dialog whose content would have to be re-truncated, so the
// measurement is of the path that would be expensive rather than of the cheapest
// one available.
func TestHandleIsZeroAllocation(t *testing.T) {
	d := New(rect(40, 12), VariantChoice)
	d.SetBodyString("a body long enough that re-wrapping it is not free")
	d.SetChoices([]string{"one", "two", "three", "four", "five", "six"})
	focus(d)
	d.Draw(cellBuf(40, 12))

	for _, seq := range []string{keyTab, keyDown, keyUp, keyEnter, keyHome, keyEnd} {
		ev := keyFromSeq(t, seq)
		press(t, d, seq) // warm any one-off cost this key path has
		if got := testing.AllocsPerRun(200, func() { d.Handle(ev) }); got != 0 {
			t.Errorf("Handle(%q) allocated %v times per key", seq, got)
		}
	}
}

// BenchmarkDialogDraw is the measurement behind the allocation claim, so a
// regression shows up as a benchmark rather than only as a failing test.
func BenchmarkDialogDraw(b *testing.B) {
	for _, v := range []Variant{VariantInfo, VariantConfirm, VariantChoice} {
		b.Run(v.String(), func(b *testing.B) {
			d := New(rect(80, 24), v)
			d.SetTitle("Bench", buffer.PlainStyle)
			d.SetBodyString("a body long enough to wrap over several rows in a narrow dialog")
			d.SetChoices([]string{"one", "two", "three", "four", "five"})
			buf := cellBuf(80, 24)
			drawAll(d, buf, 3)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				d.Draw(buf)
			}
		})
	}
}

// BenchmarkDialogDrawHundredChoices is the virtualization claim as a measurement: a
// hundred choices must cost what five do, because nothing in Draw is O(count).
func BenchmarkDialogDrawHundredChoices(b *testing.B) {
	d := choiceDialog(80, 24, 100)
	buf := cellBuf(80, 24)
	drawAll(d, buf, 3)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Draw(buf)
	}
}
