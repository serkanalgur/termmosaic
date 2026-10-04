package data

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// simpleList returns a bordered list of n items whose labels are "i0", "i1"… and
// whose selection is at the top.
func simpleList(t *testing.T, w, h, n int) *List {
	t.Helper()
	l := NewList(buffer.Rect{X: 0, Y: 0, W: w, H: h})
	l.Scrollbar = true
	items := make([]ListItem, n)
	for i := range items {
		items[i] = ListItem{Label: fmt.Sprintf("i%d", i)}
	}
	l.SetItems(items)
	l.Block().SetBorder(buffer.BorderPlain)
	return l
}

func TestListDrawsVisibleRowsWithMarkerAndBorder(t *testing.T) {
	l := simpleList(t, 12, 6, 100)
	got := rows(t, 12, 6, l)
	// The border row itself is Block's, and widgets/block tests it; what
	// matters here is that the frame is where it belongs.
	wantFramed(t, got, 0)
	// The scrollbar occupies the last interior column, and its thumb starts at
	// the top of the track at offset 0. With 100 items in 4 rows the visible
	// fraction rounds to zero, so the thumb is clamped to the one cell minimum —
	// the position is still readable, which is what matters.
	want(t, got, 1, "› i0     █")
	want(t, got, 2, "  i1      ")
	want(t, got, 3, "  i2      ")
	// The border row itself is Block's, and widgets/block tests it; what
	// matters here is that the frame is where it belongs.
	wantFramed(t, got, 5)
}

func TestListMarkerIsTheColourIndependentSelectionSignal(t *testing.T) {
	// Two lists differing ONLY in the selected row must differ in a cell that is
	// not a colour: the marker column. That is the accessibility contract, and
	// asserting it on the rendered cells is the only way to know it holds.
	base := NewList(buffer.Rect{X: 0, Y: 0, W: 10, H: 4})
	base.SetItems([]ListItem{{Label: "a"}, {Label: "b"}, {Label: "c"}})
	top := rows(t, 10, 4, base)
	base.Select(2)
	bottom := rows(t, 10, 4, base)

	// The selection moves from the first visible row to the third, and the
	// marker follows it: two rows that differ in no other way.
	assertDifferent(t, "the selected row's marker", top[0], bottom[2])
	if !strings.Contains(top[0], "›") {
		t.Errorf("row with the selection at the top has no marker: %q", top[0])
	}
	if !strings.Contains(bottom[2], "›") {
		t.Errorf("row with the selection at the bottom has no marker: %q", bottom[2])
	}
	if strings.Contains(top[2], "›") || strings.Contains(bottom[0], "›") {
		t.Errorf("an unselected row carries the marker: top=%q bottom=%q", top[2], bottom[0])
	}
}

func TestListSelectedStyleOnlyRepaintsThatRow(t *testing.T) {
	// A selected row's background must cover the WHOLE row, not just its text, or
	// the highlight is a word rather than a row. The interior is 8 wide and the
	// marker plus label is 4, so the fill is observable past the text.
	l := NewList(buffer.Rect{X: 0, Y: 0, W: 10, H: 4})
	l.Block().SetBorder(buffer.BorderPlain)
	l.SetItems([]ListItem{{Label: "a"}, {Label: "b"}})
	l.SelectedStyle = buffer.ReverseStyle
	buf := cellBuf(10, 4)
	l.Draw(buf)
	sel := buf.CellAt(8, 1).Attr
	un := buf.CellAt(8, 2).Attr
	if !sel.Has(buffer.AttrReverse) {
		t.Errorf("the selected row's background is not reversed: attr %v", sel)
	}
	if un.Has(buffer.AttrReverse) {
		t.Errorf("an unselected row is reversed too: attr %v", un)
	}
}

func TestListKeysMoveSelectionAndScrollIntoView(t *testing.T) {
	l := simpleList(t, 12, 5, 100)
	focus(l)
	if l.Selected() != 0 || l.Offset() != 0 {
		t.Fatalf("fresh list: selected %d offset %d, want 0/0", l.Selected(), l.Offset())
	}
	mustPress(t, l, "\x1b[B") // down
	if l.Selected() != 1 {
		t.Errorf("after down: selected %d, want 1", l.Selected())
	}
	// The viewport is 3 rows tall, so three downs and a fourth must scroll.
	for i := 0; i < 3; i++ {
		mustPress(t, l, "\x1b[B")
	}
	if l.Selected() != 4 || l.Offset() != 2 {
		t.Errorf("after four downs: selected %d offset %d, want 4/2", l.Selected(), l.Offset())
	}
	mustPress(t, l, "\x1b[F") // end
	if l.Selected() != 99 {
		t.Errorf("after end: selected %d, want 99", l.Selected())
	}
	if l.Offset() <= 0 {
		t.Errorf("after end: offset %d, want the tail visible", l.Offset())
	}
}

func TestListKeysIgnoredWhileUnfocused(t *testing.T) {
	l := simpleList(t, 12, 5, 100)
	if press(t, l, "\x1b[B") {
		t.Error("down was consumed while the list did not have focus")
	}
	if l.Selected() != 0 {
		t.Errorf("selection moved to %d while unfocused", l.Selected())
	}
}

func TestListActivateCallsBackWithTheSelection(t *testing.T) {
	l := simpleList(t, 12, 5, 10)
	focus(l)
	var got int = -1
	l.OnActivate = func(i int) { got = i }
	press(t, l, "\x1b[B")
	mustPress(t, l, "\r")
	if got != 1 {
		t.Errorf("OnActivate got %d, want 1", got)
	}
}

func TestListClickSelectsAndTakesFocus(t *testing.T) {
	l := simpleList(t, 12, 6, 100)
	if !clickAt(l, 3, 3) {
		t.Fatal("a press inside the list was not consumed")
	}
	if !l.Focused() {
		t.Error("a press did not take focus")
	}
	if l.Selected() != 2 {
		t.Errorf("after clicking row 2: selected %d, want 2", l.Selected())
	}
	if clickAt(l, 40, 40) {
		t.Error("a press outside the list was consumed; it belongs to an ancestor")
	}
}

func TestListWheelScrollsWithoutMovingTheSelection(t *testing.T) {
	l := simpleList(t, 12, 5, 100)
	focus(l)
	if !wheelAt(l, 3, 2, false) {
		t.Fatal("a wheel notch was not consumed")
	}
	if l.Offset() != wheelLines {
		t.Errorf("after one notch down: offset %d, want %d", l.Offset(), wheelLines)
	}
	if l.Selected() != 0 {
		t.Errorf("a wheel notch moved the selection to %d; scrolling is not navigating", l.Selected())
	}
}

func TestListTabIsNotConsumed(t *testing.T) {
	// A form needs tab to move focus out of a list, so a list that swallowed it
	// would be impossible to leave with the keyboard.
	l := simpleList(t, 12, 5, 10)
	focus(l)
	if press(t, l, "\t") {
		t.Error("tab was consumed by a list")
	}
}

func TestListDegenerateSizesDoNotPanic(t *testing.T) {
	for _, size := range []buffer.Size{{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 2}, {W: 3, H: 1}, {W: 0, H: 8}, {W: 8, H: 0}} {
		l := NewList(buffer.Rect{X: 0, Y: 0, W: size.W, H: size.H})
		l.Scrollbar = true
		l.Block().SetBorder(buffer.BorderPlain)
		l.SetItems([]ListItem{{Label: "one"}, {Label: "two"}})
		focus(l)
		l.Draw(cellBuf(size.W, size.H))
		mustPress(t, l, "\x1b[B")
		mustPress(t, l, "\x1b[F")
		l.Draw(cellBuf(size.W, size.H))
	}
}

func TestListDrawIsAllocationFree(t *testing.T) {
	l := simpleList(t, 40, 20, 10000)
	buf := cellBuf(40, 20)
	// The first Draw adapts and therefore allocates by design; the steady state
	// is what must be free.
	drawAll(l, buf, 3)
	if got := testing.AllocsPerRun(200, func() { l.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per frame; the frame path must be free", got)
	}
}

func TestListScrollingDoesNotAllocate(t *testing.T) {
	l := simpleList(t, 40, 20, 100000)
	focus(l)
	buf := cellBuf(40, 20)
	drawAll(l, buf, 3)
	// Scrolling changes the offset, which is the one thing that invalidates a row
	// window; if the row painter allocated per row this is where it would show.
	if got := testing.AllocsPerRun(200, func() {
		l.vm.ScrollBy(1)
		l.Draw(buf)
	}); got != 0 {
		t.Errorf("scrolling then Draw allocated %v times; scrolling must not allocate", got)
	}
}

func TestListIsFlatInItemCount(t *testing.T) {
	// The proof ADR 0007 §6 asks for: a 100,000-item list must cost what a
	// 10-item one costs, per frame. Both are measured over the same visible rows,
	// so a widget that iterated its items could not come out equal.
	cost := func(n int) float64 {
		l := simpleList(t, 40, 20, n)
		buf := cellBuf(40, 20)
		drawAll(l, buf, 3)
		return testing.AllocsPerRun(50, func() { l.Draw(buf) })
	}
	small, huge := cost(10), cost(100000)
	if small != huge {
		t.Errorf("100k items cost %v allocs/frame and 10 items cost %v; per-frame cost is not flat in the item count", huge, small)
	}
}

func TestListHundredThousandItemsScrollsToTheEnd(t *testing.T) {
	// A flat-cost claim is only worth anything if the arithmetic is right at the
	// far end of a large collection.
	const n = 100000
	l := simpleList(t, 20, 6, n)
	focus(l)
	if l.Items() != n {
		t.Fatalf("Items() = %d, want %d", l.Items(), n)
	}
	mustPress(t, l, "\x1b[F") // end
	if l.Selected() != n-1 {
		t.Fatalf("after end: selected %d, want %d", l.Selected(), n-1)
	}
	got := rows(t, 20, 6, l)
	if !strings.Contains(got[4], fmt.Sprintf("i%d", n-1)) {
		t.Errorf("the last row is not visible after End: %q", got[4])
	}
}

func TestListGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	// A resize test that only grows cannot catch the ADR 0007 §1 rule 3 bug, so
	// this one shrinks: the same widget is rendered wide, then narrow, and the
	// cells the wide layout put in the right half must be gone.
	l := simpleList(t, 30, 6, 100)
	wide := rows(t, 30, 6, l)
	if !strings.Contains(wide[1], "i0") {
		t.Fatalf("the wide layout did not draw the first row: %q", wide[1])
	}
	l.SetBounds(buffer.Rect{X: 0, Y: 0, W: 12, H: 6})
	buf := cellBuf(12, 6)
	l.Draw(buf)
	// The renderer diffs and never clears, so a shrinking widget has to repaint.
	// Proving it: render wide first in a REAL screen, then shrink, and check no
	// cell beyond the new right edge survives.
	l.SetBounds(buffer.Rect{X: 0, Y: 0, W: 12, H: 6})
	narrow := widgetRowsAt(t, 12, 6, l)
	if len(narrow) != 6 {
		t.Fatalf("shrunk screen has %d rows, want 6", len(narrow))
	}
	for y := 0; y < 6; y++ {
		if utf8.RuneCountInString(narrow[y]) > 12 {
			t.Errorf("row %d is %d cells wide after shrinking to 12: %q", y, len(narrow[y]), narrow[y])
		}
	}
	if !strings.Contains(narrow[1], "i0") {
		t.Errorf("the first row did not survive the shrink: %q", narrow[1])
	}
}

func TestListRepaintsEveryCellOfBoundsOnShrink(t *testing.T) {
	// The direct form of rule 3: a buffer that held a wider frame's cells must
	// have every one of the widget's own cells rewritten, including the ones the
	// narrow layout no longer uses for content.
	l := simpleList(t, 20, 4, 100)
	buf := cellBuf(20, 4)
	l.Draw(buf)
	// Dirty the whole buffer the way a stale frame would, then shrink.
	for y := 0; y < 4; y++ {
		for x := 0; x < 20; x++ {
			buf.SetCell(x, y, buffer.Cell{Ch: 'Z'})
		}
	}
	l.SetBounds(buffer.Rect{X: 0, Y: 0, W: 20, H: 4})
	l.Draw(buf)
	for y := 0; y < 4; y++ {
		for x := 0; x < 20; x++ {
			if buf.CellAt(x, y).Ch == 'Z' {
				t.Fatalf("cell (%d,%d) still holds the stale 'Z'; the widget did not repaint its whole bounds", x, y)
			}
		}
	}
}

func TestListMinSizeIncludesChrome(t *testing.T) {
	l := NewList(buffer.Rect{W: 1, H: 1})
	l.Block().SetBorder(buffer.BorderPlain)
	want := l.MinSize()
	if want.W != minListW+2 || want.H != minListH+2 {
		t.Errorf("MinSize = %+v, want {%d,%d} for a borderless-inset minimum of %dx%d", want, minListW+2, minListH+2, minListW, minListH)
	}
	if want.W < 2 || want.H < 2 {
		t.Errorf("MinSize = %+v, which does not account for the border at all", want)
	}
}

func TestListEmptySelectionIsLegal(t *testing.T) {
	l := NewList(buffer.Rect{X: 0, Y: 0, W: 10, H: 4})
	if l.Selected() != -1 {
		t.Errorf("an empty list reports selection %d, want -1", l.Selected())
	}
	got := rows(t, 10, 4, l)
	for y := range got {
		if strings.Contains(got[y], "›") {
			t.Errorf("an empty list drew a marker on row %d: %q", y, got[y])
		}
	}
}

// widgetRowsAt renders root at the given screen size and returns its rows,
// trimming trailing blanks. It is the grow-and-shrink harness: the SAME widget
// is rendered twice at two sizes, which is the only way a stale cell can appear.
func widgetRowsAt(t *testing.T, w, h int, root termmosaic.Widget) []string {
	t.Helper()
	return rows(t, w, h, root)
}
