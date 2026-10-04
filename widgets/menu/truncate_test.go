package menu

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// TestMenuTruncatedLabelKeepsItsMarker is a small rule with a large consequence:
// a label cut to the region's width must still show the truncation marker, because
// the marker is the only thing telling the user there was more text.
//
// It is asserted on the drawn cells, not on a cached string, because the failure
// mode is precisely that the two disagree — a widget can hold the right string and
// write only its first run.
func TestMenuTruncatedLabelKeepsItsMarker(t *testing.T) {
	// At six cells the label region is three wide, so "Save" cannot fit whole.
	m := newMenu(t, 6, 4, Item{Label: "Save"}, Item{Label: "New"})
	buf := drawInto(m, 6, 4)
	c := &m.lay.cols[0]
	if c.labelW >= buffer.StringWidth("Save") {
		t.Skipf("the label region is %d cells at 6 wide, which fits 'Save'; the threshold has moved", c.labelW)
	}
	got := line(buf, c.itemRect.Y, 6)
	if !strings.Contains(got, string(buffer.TruncSuffix)) && !strings.Contains(got, string(buffer.AscTruncSuffix)) {
		t.Errorf("row %d = %q: the truncated label shows no marker, so the user cannot tell it was cut", c.itemRect.Y, got)
	}
	if strings.Contains(got, "Save") {
		t.Errorf("row %d = %q: the label was not truncated at all despite a %d-cell region", c.itemRect.Y, got, c.labelW)
	}
}

// TestMenuAsciiTruncationMarkerIsUsedOnTheASCIIRung is the degradation half: the
// marker must be ASCII when the menu is, or the label carries a glyph the terminal
// cannot render in the one place where the user is being told something.
func TestMenuAsciiTruncationMarkerIsUsedOnTheASCIIRung(t *testing.T) {
	// Four cells wide leaves a three-cell label region, which "Save" cannot fill.
	uni := newMenu(t, 4, 4, Item{Label: "Save"}, Item{Label: "New"})
	uniBuf := drawInto(uni, 4, 4)
	m := newMenu(t, 4, 4, Item{Label: "Save"}, Item{Label: "New"})
	m.Ascii = true
	buf := drawInto(m, 4, 4)

	c, uc := &m.lay.cols[0], &uni.lay.cols[0]
	got := line(buf, c.itemRect.Y, 4)
	if !strings.Contains(got, buffer.AscTruncSuffix) {
		t.Errorf("row %d = %q: the ASCII rung did not use %q", c.itemRect.Y, got, buffer.AscTruncSuffix)
	}
	if strings.Contains(got, buffer.TruncSuffix) {
		t.Errorf("row %d = %q: a Unicode truncation marker survived on the ASCII rung", c.itemRect.Y, got)
	}
	// The Unicode rung must show the other marker, or the test above would pass
	// against a widget that used the same one on both.
	if u := line(uniBuf, uc.itemRect.Y, 4); !strings.Contains(u, buffer.TruncSuffix) {
		t.Errorf("row %d = %q: the Unicode rung did not use %q", uc.itemRect.Y, u, buffer.TruncSuffix)
	}
	// And the label region is the same width on both rungs, which is the property
	// the one-cell marker exists to preserve.
	if uc.labelW != c.labelW {
		t.Errorf("the label region is %d cells on ASCII and %d on Unicode: the markers must be one cell on both",
			c.labelW, uc.labelW)
	}
}

// TestMenuHeaderIsTruncatedRatherThanOverwritingItsBracket covers the header's
// capping. The header is written with SetSpansCappedIn so a long parent label gives
// up its last cell to a marker instead of running over the closing bracket — and
// losing the bracket would lose the active-level signal, which is the one thing in
// a header that colour must never be the only carrier of.
func TestMenuHeaderIsTruncatedRatherThanOverwritingItsBracket(t *testing.T) {
	m := newMenu(t, 20, 5, tree()...)
	press(t, m, "\x1b[C")
	buf := drawInto(m, 20, 5)
	c := &m.lay.cols[len(m.lay.cols)-1]
	if !c.active {
		t.Fatalf("setup: the last column is not the active one: %+v", c)
	}
	// The header is read as the column's OWN cells, not as the whole row: two
	// columns share a row, and a prefix check against the row would be asserting
	// about whichever column happened to be leftmost.
	head := cellsOf(buf, c.headRect)
	if !strings.HasPrefix(head, "[") {
		t.Errorf("the active header is %q: the opening bracket is missing", head)
	}
	if !strings.HasSuffix(head, "]") {
		t.Errorf("the active header is %q: the closing bracket is missing or buried", head)
	}
	// The text is capped INSIDE the brackets, so it never reaches the closing one.
	// This is the assertion that would fail if the header were written with
	// SetString over the whole interior.
	inner := strings.TrimSuffix(strings.TrimPrefix(head, "["), "]")
	if got := buffer.StringWidth(inner); got > c.headRect.W-2 {
		t.Errorf("the header text occupies %d cells of a %d-cell interior: it overruns the bracket", got, c.headRect.W)
	}
}

// cellsOf returns r's cells as a string, by RUNE rather than by byte so a
// multi-byte marker never splits the result.
func cellsOf(buf *buffer.Buffer, r buffer.Rect) string {
	var b strings.Builder
	for y := r.Y; y < r.Bottom(); y++ {
		for x := r.X; x < r.Right(); x++ {
			b.WriteRune(buf.CellAt(x, y).Ch)
		}
	}
	return b.String()
}

// TestMenuEmptyLabelAndEmptyTreeAreDrawnNotPanicked is totality over CONTENT rather
// than over size, and both shapes are ordinary: an item with no label is a divider
// row a caller wants, and an empty menu is what a caller has before it loads
// anything.
func TestMenuEmptyLabelAndEmptyTreeAreDrawnNotPanicked(t *testing.T) {
	t.Run("an empty tree", func(t *testing.T) {
		m := newMenu(t, 30, 6)
		if got := m.Selected(); got != -1 {
			t.Errorf("an empty menu selected %d, want -1", got)
		}
		// The navigation keys are still consumed: they were menu keys, and refusing
		// them would let them reach a parent with different bindings.
		for _, seq := range []string{"\x1b[A", "\x1b[B", "\r", "\x1b[H", "\x1b[F"} {
			if !tap(t, m, seq) {
				t.Errorf("%q was not consumed on an empty menu", seq)
			}
		}
		// Right is the documented exception: it is consumed only when it OPENED
		// something, so on a menu with nothing to open it is declined and the
		// application keeps the binding.
		if tap(t, m, "\x1b[C") {
			t.Error("right was consumed on an empty menu; nothing was opened")
		}
		// And the header still renders, so the column is not blank.
		if m.lay.cols[0].headRect.Empty() {
			t.Error("an empty menu drew no header row")
		}
	})

	t.Run("an empty label", func(t *testing.T) {
		m := newMenu(t, 30, 6, Item{Label: ""}, Item{Label: "real"})
		buf := drawInto(m, 30, 6)
		c := &m.lay.cols[0]
		// The empty row's marker still moves to it, so an empty row is still
		// selectable and still shows where the cursor is.
		m.selectIndex(0, 0)
		m.Draw(buf)
		if got := buf.CellAt(c.markerX, c.itemRect.Y).Ch; got != firstRuneOf(DefaultMarker) {
			t.Errorf("the empty row's marker cell is %q, want the marker: an empty label is still a row", got)
		}
	})
}

// TestMenuSetItemsResetsThePath pins the reload contract: indices from the old tree
// mean nothing in the new one, so the path is reset to the root rather than left
// pointing into a tree that no longer exists.
func TestMenuSetItemsResetsThePath(t *testing.T) {
	m := newMenu(t, 60, 12, tree()...)
	press(t, m, "\x1b[C")
	press(t, m, "\x1b[C")
	if d := m.Depth(); d != 3 {
		t.Fatalf("setup: depth %d, want 3", d)
	}
	m.SetItems([]Item{{Label: "fresh"}, {Label: "items"}})
	if d := m.Depth(); d != 1 {
		t.Errorf("after SetItems: depth %d, want 1 — a path into the old tree is meaningless", d)
	}
	if got := m.Selected(); got != 0 {
		t.Errorf("after SetItems: selection %d, want 0", got)
	}
	drawInto(m, 60, 12)
}

// TestMenuSetItemsCopiesTheRootSlice is the aliasing contract, stated because its
// absence is invisible until an application mutates the slice it passed:
//
//	m := menu.New(rect, items...)
//	items[0].Label = "changed"   // must NOT change the menu
//
// It is the same rule List follows, and the reason is that a drawn frame has to mean
// one thing: a frame drawn before the mutation must not silently change meaning
// after it.
func TestMenuSetItemsCopiesTheRootSlice(t *testing.T) {
	items := []Item{{Label: "before"}}
	m := newMenu(t, 30, 5, items...)
	items[0].Label = "after"
	if got := m.Items()[0].Label; got != "before" {
		t.Errorf("the menu's root item is %q after the caller mutated its slice: SetItems must copy", got)
	}
}

// TestMenuSetItemsKeepsToggleStateInsideTheNewTree is the other half of that rule:
// the root SLICE is copied, the tree inside it is the caller's. So a caller
// rebuilding a menu from its own state model keeps the states, which is what makes
// SetItems usable as "reload" rather than only as "replace".
func TestMenuSetItemsKeepsToggleStateInsideTheNewTree(t *testing.T) {
	items := []Item{{Label: "on", Checkable: true, Checked: true}}
	m := newMenu(t, 30, 5, items...)
	m.SetItems(items)
	if !m.Checked(0, 0) {
		t.Error("SetItems lost a toggle state that the caller's tree carried")
	}
}

// TestMenuZeroValueIsSafeToQuery is the "safe before the first Draw" clause of the
// Widget contract, applied to every accessor an application might reach for while
// building its layout.
func TestMenuZeroValueIsSafeToQuery(t *testing.T) {
	var m Menu
	// Bounds and MinSize are the two a LAYOUT calls, so they are the two that must
	// not depend on anything having been drawn or set.
	if got := m.Bounds(); got.W != 0 || got.H != 0 {
		t.Errorf("the zero Menu's Bounds is %v, want the empty rect", got)
	}
	if got := m.MinSize(); got.W < minMenuW || got.H < minMenuH {
		t.Errorf("the zero Menu's MinSize is %+v, want at least %dx%d", got, minMenuW, minMenuH)
	}
	// And the rest must answer rather than panic, because a caller wiring up a
	// layout reads state before it has configured anything.
	if got := m.Depth(); got != 1 {
		t.Errorf("the zero Menu's Depth is %d, want 1", got)
	}
	if got := m.Selected(); got != -1 {
		t.Errorf("the zero Menu's selection is %d, want -1", got)
	}
	if _, ok := m.SelectedItem(); ok {
		t.Error("the zero Menu reported a selected item")
	}
	if m.IsOpen() {
		t.Error("the zero Menu reports itself open")
	}
	m.SetItems([]Item{{Label: "x"}})
	m.Open()
	buf := cellBuf(20, 5)
	m.Draw(buf) // must not panic on a Menu whose block was never given a rect
}
