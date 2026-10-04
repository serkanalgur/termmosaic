package data

// This file settles STATUS.md's open question about buffer.SetSpansWindowIn's
// `skip` argument: is the partially-visible-column case reachable from Table?
//
// STATUS.md recorded it as UNREACHABLE, on the reasoning that "scrollCols always
// positions on a column start, so the partially-visible-column case cannot arise".
// That reasoning is wrong, and this file is the proof.
//
// # Where the reasoning goes wrong
//
// Table keeps TWO horizontal offsets (table.go): hOffset, a column index, and
// hCells, a CELL position. Scroll keys and SetColOffset set hCells to a column
// start. But clampColOffset does not: it clamps hCells into
//
//	maxCells = totalW - contentW
//
// and that ceiling is the right-hand edge of the content, which is generally NOT a
// column start. Two consequences follow, and both are pinned below:
//
//  1. Scrolling to the END of the table clamps hCells to that ceiling, and the
//     column immediately left of the clamp point is then PARTIALLY VISIBLE — its
//     head is off screen and its tail is on it.
//  2. Any shrink that changes contentW or totalW re-clamps to the same ceiling, so
//     the partially-visible column appears without the user scrolling at all.
//
// So `skip` is on the production path, not dead code. The correct action is
// therefore neither of the two STATUS.md offered — Table does not need to be taught
// to scroll by cell, and the primitive does not need documenting as unused. It is
// to pin the behaviour with a test, which is what the rest of this file does.

import (
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// newPartialColumnTable returns a table whose horizontal clamp lands mid-column.
//
// Three columns five cells wide, so colX is [0, 6, 12] and totalW is 18. A 16-wide
// table spends two cells on the border and one on the scrollbar, leaving a
// contentW of 11 — which is neither the start nor the end of a column.
func newPartialColumnTable(t *testing.T, w, h int) *Table {
	t.Helper()
	tb := NewTable(buffer.Rect{X: 0, Y: 0, W: w, H: h},
		named("AAA", 5), named("BBB", 5), named("CCC", 5))
	tb.Header = true
	tb.SetRows([]Row{{Cells: []Cell{
		{Text: "aaaaa"},
		{Text: "bbbbb"},
		{Text: "ccccc"},
	}}})
	tb.Block().SetBorder(buffer.BorderPlain)
	return tb
}

// TestTablePartialColumnSkipIsReachable is the direct proof: after scrolling to the
// end, a column is partially visible and its TAIL — not its head — is what gets
// drawn.
//
// The assertion is on the rendered cells rather than on the internal offsets,
// because the internal offsets are the thing in question. "bbb" at the left edge,
// not "aaaaa", is what a reader sees, and "aaa" appearing there is the bug the
// `skip` argument exists to prevent.
func TestTablePartialColumnSkipIsReachable(t *testing.T) {
	tb := newPartialColumnTable(t, 16, 5)
	rows(t, 16, 5, tb) // the first Draw establishes the column positions

	// Scroll to the end. This is the documented way to ask for it: shift+End, and
	// the same call Handle's KeyEnd makes.
	tb.SetColOffset(tb.maxColOffsetForTest())
	_ = rows(t, 16, 5, tb)

	// The clamp must NOT be on a column start, or this test is proving nothing.
	if tb.hCells == tb.colX[tb.ColOffset()] {
		t.Fatalf("hCells (%d) equals the start of column %d: the clamp landed on a "+
			"column boundary and no column is partially visible, so the skip path is "+
			"not exercised by this case", tb.hCells, tb.ColOffset())
	}
	if tb.hCells != tb.maxCells {
		t.Errorf("hCells = %d, want the clamp ceiling maxCells = %d", tb.hCells, tb.maxCells)
	}

	// A partially visible column must be reported visible.
	var partial []int
	for _, c := range tb.visCols {
		origin := tb.body.X + tb.colX[c] - tb.hCells
		if origin < tb.body.X {
			partial = append(partial, c)
		}
	}
	if len(partial) == 0 {
		t.Fatalf("no visible column starts off screen to the left: visCols = %v, "+
			"hCells = %d, colX = %v", tb.visCols, tb.hCells, tb.colX)
	}
	for _, c := range partial {
		skip := tb.body.X - (tb.body.X + tb.colX[c] - tb.hCells)
		if skip <= 0 {
			t.Errorf("column %d is reported left-clipped with skip = %d", c, skip)
		}
	}

	// And the pixels. The assertion has to be about POSITION and COUNT, not merely
	// about the presence of the tail's characters: a drawCell that ignored hCells
	// would still draw "bbb" — at the column's own x instead of at the body's left
	// edge, and with all five characters instead of the four the window can show.
	// Both of those are the bug, and a `strings.Contains` check misses both.
	sink := widgettest.Render(t, 16, 5, 1, tb)
	const bodyRow = 2
	colB := 1
	skip := tb.body.X - (tb.body.X + tb.colX[colB] - tb.hCells)
	if skip <= 0 {
		t.Fatalf("column %d is not left-clipped at this offset: skip = %d", colB, skip)
	}
	wantVisible := 5 - skip
	seen := 0
	for x := 0; x < 16; x++ {
		got := sink.CellAt(x, bodyRow).Rune()
		switch got {
		case 'b':
			if x < tb.body.X {
				t.Errorf("row %d: column B's content at column %d, which is LEFT of the body's "+
					"own edge %d: a scrolled column painted over the gutter", bodyRow, x, tb.body.X)
			}
			seen++
		case 'a':
			t.Errorf("row %d: column A's content at column %d although it is entirely off "+
				"screen to the left", bodyRow, x)
		}
	}
	if seen != wantVisible {
		t.Errorf("row %d shows %d of column B's 5 cells, want %d: the window must drop the %d "+
			"cell(s) hidden off the left edge and show the tail in their place",
			bodyRow, seen, wantVisible, skip)
	}
	if got := sink.CellAt(tb.body.X, bodyRow).Rune(); got != 'b' {
		t.Errorf("row %d starts at column %d, want the tail of the clipped column there, got %q",
			bodyRow, tb.body.X, got)
	}
	// The frame survives: a skip that ran past the column's own end would paint the
	// border beside it.
	wantFramed(t, rows(t, 16, 5, tb), bodyRow)
}

// TestTablePartialColumnSurvivesAShrink is the second way the case arises, and the
// one that makes it not a corner case of scrolling to the end: a resize re-clamps
// hCells to the same content-width-derived ceiling, so a column becomes partially
// visible with no scrolling key pressed at all.
//
// The sequence is scroll-to-end THEN widen, which is the only order that works:
// clamping only ever moves an offset DOWN, so a table that was never scrolled has
// nothing to clamp. Widening reduces contentW, so the ceiling falls below the
// current offset, and the offset lands wherever the new ceiling is — which for a
// non-column-start ceiling is mid-column.
//
// Without this test the reachability argument rests entirely on one keypress.
func TestTablePartialColumnSurvivesAShrink(t *testing.T) {
	const narrowW, narrowH = 16, 5
	// wideW is chosen so contentW lands at 16: with totalW 18 that puts the clamp
	// ceiling at 2, which is inside column 0 rather than on a boundary or at zero.
	const wideW = 21
	tb := newPartialColumnTable(t, narrowW, narrowH)
	rows(t, narrowW, narrowH, tb)

	// Scroll to the end at the narrow size, where the ceiling is well clear of zero.
	tb.SetColOffset(tb.maxColOffsetForTest())
	rows(t, narrowW, narrowH, tb)
	scrolledTo := tb.hCells
	if scrolledTo != tb.maxCells || scrolledTo == 0 {
		t.Fatalf("hCells = %d, maxCells = %d at %dx%d: the fixture no longer produces a "+
			"mid-column clamp and the rest of this test would prove nothing",
			scrolledTo, tb.maxCells, narrowW, narrowH)
	}

	// Widen. contentW rises, so totalW-contentW falls, and the offset is clamped
	// DOWN onto the new ceiling.
	tb.SetBounds(buffer.Rect{X: 0, Y: 0, W: wideW, H: narrowH})
	rows(t, wideW, narrowH, tb)

	if tb.hCells != tb.maxCells {
		t.Errorf("hCells = %d, want the clamp ceiling %d after the resize", tb.hCells, tb.maxCells)
	}
	if tb.hCells >= scrolledTo {
		t.Fatalf("hCells = %d did not move on the widening resize (was %d): no clamping "+
			"happened, so nothing below is exercised", tb.hCells, scrolledTo)
	}

	// And the offset is mid-column: at least one visible column starts off screen.
	partial := 0
	for _, c := range tb.visCols {
		if tb.body.X+tb.colX[c]-tb.hCells < tb.body.X {
			partial++
		}
	}
	if partial == 0 {
		t.Errorf("after a widening resize no column is left-clipped: visCols = %v, hCells = %d, "+
			"colX = %v, contentW = %d", tb.visCols, tb.hCells, tb.colX, tb.contentW)
	}
}

// TestTableClipNeverPaintsOverTheGutter is the property the skip path protects, at
// its strongest: whatever the horizontal offset, the border and the selection gutter
// keep their own cells.
//
// A drawCell that computed origin without subtracting hCells would paint the
// clipped column's head across the gutter, which is the specific failure
// SetSpansWindowIn's `skip` was written to prevent. This pins it across every legal
// offset, so it does not depend on which offset happens to be the partial one.
//
// It asserts on CELLS rather than on a row string, because the column indices being
// checked are byte offsets into a multi-byte border: a rune-indexed assertion here
// would be reading the wrong thing.
func TestTableClipNeverPaintsOverTheGutter(t *testing.T) {
	const w, h = 20, 5
	tb := newPartialColumnTable(t, w, h)
	rows(t, w, h, tb)

	g := edge()
	// The characters a border or a gutter cell is allowed to hold.
	allowed := map[rune]bool{
		g.Vertical: true, ' ': true,
		[]rune(tb.Marker)[0]: true,
	}
	for i := 0; i <= tb.maxColOffsetForTest(); i++ {
		tb.SetColOffset(i)
		sink := widgettest.Render(t, w, h, 1, tb)
		for y := 1; y < h-1; y++ {
			for x := 0; x < 1+tb.markerW; x++ {
				if got := sink.CellAt(x, y).Rune(); !allowed[got] {
					t.Errorf("offset %d: cell (%d,%d) is %q; a scrolled column painted over "+
						"the border or the selection gutter", i, x, y, got)
				}
			}
			wantFramed(t, rows(t, w, h, tb), y)
		}
	}
}
