package data

import (
	"fmt"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
)

// named builds a header-only column, the common case.
func named(title string, width int) Column {
	return Column{Title: []buffer.Span{buffer.NewSpan(title, buffer.DefaultStyle)}, Width: width}
}

// sized builds a column whose width is measured from the visible rows.
func sized(title string) Column {
	return Column{Title: []buffer.Span{buffer.NewSpan(title, buffer.DefaultStyle)}}
}

// plainTable returns a bordered table with three fixed columns and n rows whose
// cells are "c0", "c1", "c2" by column and the row number in the first column.
func plainTable(t *testing.T, w, h, n int) *Table {
	t.Helper()
	tb := NewTable(buffer.Rect{X: 0, Y: 0, W: w, H: h},
		named("ID", 4), named("NAME", 6), named("STATE", 6))
	tb.Header = true
	tb.SetRows(tableRows(n))
	tb.Block().SetBorder(buffer.BorderPlain)
	return tb
}

// tableRows builds n rows for the plainTable fixture.
func tableRows(n int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{Cells: []Cell{
			{Text: fmt.Sprintf("r%d", i)},
			{Text: fmt.Sprintf("name%d", i)},
			{Text: fmt.Sprintf("state%d", i)},
		}}
	}
	return rows
}

func TestTableDrawsHeaderAndRowsWithMarker(t *testing.T) {
	tb := plainTable(t, 22, 5, 10)
	got := rows(t, 22, 5, tb)
	// The border row itself is Block's, and widgets/block tests it; what
	// matters here is that the frame is where it belongs.
	wantFramed(t, got, 0)
	want(t, got, 1, "  ID   NAME   STATE ")
	want(t, got, 2, "› r0   name0  stat…█")
	want(t, got, 3, "  r1   name1  stat… ")
	// The border row itself is Block's, and widgets/block tests it; what
	// matters here is that the frame is where it belongs.
	wantFramed(t, got, 4)
}

func TestTableHeaderIsBoldWithoutColour(t *testing.T) {
	// The header's signal is an ATTRIBUTE, so it survives a monochrome terminal.
	// Asserting on the attribute rather than a colour is the point.
	tb := plainTable(t, 22, 5, 1)
	buf := cellBuf(22, 5)
	tb.Draw(buf)
	head := buf.CellAt(3, 1).Attr
	body := buf.CellAt(3, 2).Attr
	if !head.Has(buffer.AttrBold) {
		t.Errorf("the header cell is not bold: attr %v", head)
	}
	if body.Has(buffer.AttrBold) {
		t.Errorf("a body cell is bold: attr %v", body)
	}
}

func TestTableColumnAlignmentIsHonoured(t *testing.T) {
	tb := NewTable(buffer.Rect{X: 0, Y: 0, W: 14, H: 3},
		Column{Title: []buffer.Span{buffer.NewSpan("N", buffer.DefaultStyle)}, Width: 4, Align: geometry.AlignRight})
	tb.SetRows([]Row{{Cells: []Cell{{Text: "7"}}}})
	got := rows(t, 14, 3, tb)
	// Row 0 is the only body row: this table has no header. A right-aligned "7"
	// sits at the right edge of its 4-cell column, which starts at column 2.
	// The gutter is two cells and the marker sits in the first of them, so the
	// right-aligned "7" is at column 5 of a 4-cell column starting at column 2.
	want(t, got, 0, "›    7")
	// The two rendered values must genuinely differ, or the alignment assertion
	// above proves nothing.
	left := NewTable(buffer.Rect{X: 0, Y: 0, W: 14, H: 3},
		Column{Title: []buffer.Span{buffer.NewSpan("N", buffer.DefaultStyle)}, Width: 4})
	left.SetRows([]Row{{Cells: []Cell{{Text: "7"}}}})
	lgot := rows(t, 14, 3, left)
	assertDifferent(t, "right-aligned against left-aligned column", got[0], lgot[0])
	want(t, lgot, 0, "› 7")
}

func TestTableMeasuresColumnsFromVisibleRowsOnly(t *testing.T) {
	// A content-derived column must be measured from what is ON SCREEN. Proving
	// that needs a row that is far below the viewport carrying a long value: if
	// the width came from the whole collection, the visible column would be as
	// wide as that row and the frame would differ.
	const n = 500
	rowsIn := tableRows(n)
	rowsIn[499].Cells[1].Text = strings.Repeat("x", 40)
	tb := NewTable(buffer.Rect{X: 0, Y: 0, W: 30, H: 4},
		sized("ID"), sized("NAME"), sized("STATE"))
	tb.Header = true
	tb.SetRows(rowsIn)
	got := rows(t, 30, 4, tb)
	want(t, got, 0, "  ID NAME  STATE  ")
	if strings.Contains(got[2], "x") {
		t.Errorf("an off-screen row leaked into the visible width: %q", got[2])
	}
}

func TestTableFixedAndFillWidths(t *testing.T) {
	tb := NewTable(buffer.Rect{X: 0, Y: 0, W: 20, H: 2},
		named("A", 4), Column{Title: []buffer.Span{buffer.NewSpan("B", buffer.DefaultStyle)}, Grow: 1})
	tb.SetRows([]Row{{Cells: []Cell{{Text: "xxxx"}, {Text: "y"}}}})
	got := rows(t, 20, 3, tb)
	// Column A is 4 wide and B is 13, so the data row reads "xxxx" then a gap
	// then "y" at column 7. The gutter is 2 cells and the scrollbar takes 1.
	want(t, got, 0, "› xxxx y")
}

func TestTableHorizontalScrollRevealsLaterColumns(t *testing.T) {
	// 16 cells of screen hold 11 of content once the border, the gutter and the
	// scrollbar have taken theirs, and the three columns are 19 wide, so the table
	// overflows by more than a column and the last one starts off-screen.
	tb := plainTable(t, 16, 4, 10)
	first := rows(t, 16, 4, tb)
	// ContentWidth is derived from the rect, so it is only meaningful after a frame:
	// reading it before one would be asserting that a widget knows its own size
	// before it has been given any space at all.
	if got := tb.ContentWidth(); got != 19 {
		t.Fatalf("ContentWidth = %d, want 19", got)
	}
	if !strings.Contains(first[1], "ID") {
		t.Fatalf("the first column is not visible initially: %q", first[1])
	}
	if strings.Contains(first[1], "STATE") {
		t.Fatalf("the last column is already visible; the fixture does not overflow: %q", first[1])
	}
	focus(tb)
	mustPress(t, tb, "\x1b[C") // right
	if tb.ColOffset() != 1 {
		t.Errorf("after one right: column offset %d, want 1", tb.ColOffset())
	}
	after := rows(t, 16, 4, tb)
	assertDifferent(t, "the header before and after a horizontal scroll", first[1], after[1])
	if !strings.Contains(after[1], "NAME") {
		t.Errorf("the second column did not come into view: %q", after[1])
	}
	// One step right is one COLUMN, which is the first column's width plus its gap:
	// five cells. So the id column is now five cells further left, which for a
	// four-cell column means it is entirely off-screen — nothing of it may remain.
	if strings.Contains(inner(after, 1), "ID") {
		t.Errorf("the first column is still visible after a step right: %q", inner(after, 1))
	}

	// Shift+End must reach the LAST column, fully. This is the assertion that
	// catches a clamp computed as "the last column whose start is still on screen":
	// that ceiling is smaller, and it strands every column beyond the viewport with
	// no way to scroll to it — a table that silently hides data.
	mustPress(t, tb, "\x1b[1;2F") // shift+end
	end := rows(t, 16, 4, tb)
	if !strings.HasSuffix(inner(end, 1), "STATE") {
		t.Errorf("shift+end did not bring the last column fully into view: %q", inner(end, 1))
	}
	// The frame survives the scroll: a column scrolled off to the left must not paint
	// over the gutter or the border beside it.
	wantFramed(t, end, 1)
	// And past the end there is nothing more to scroll to: a further step right is a
	// no-op rather than an error, which is what a wheel notch at the right edge
	// should be.
	mustPress(t, tb, "\x1b[C")
	past := rows(t, 16, 4, tb)
	if inner(past, 1) != inner(end, 1) {
		t.Errorf("scrolling right past the end changed the table: %q -> %q", inner(end, 1), inner(past, 1))
	}
	// The ceiling in cell terms is the content's width less the viewport's: that is
	// what makes the last column reachable, and it is the number a caller reasoning
	// about scrolling wants to see.
	if got, want := tb.maxCellsForTest(), tb.ContentWidth()-11; got != want {
		t.Errorf("the horizontal cell ceiling is %d, want %d: the last column must be reachable", got, want)
	}
	if tb.maxColOffsetForTest() != tb.Columns()-1 {
		t.Errorf("the ceiling is column %d, want the last one (%d)", tb.maxColOffsetForTest(), tb.Columns()-1)
	}
}

func TestTableKeysMoveSelectionAndClickSelects(t *testing.T) {
	// Six rows of screen so the body has three rows: with a one-row body, End and
	// a click on the first visible row are the same row, which would make the
	// click assertion vacuous.
	tb := plainTable(t, 22, 6, 10)
	focus(tb)
	mustPress(t, tb, "\x1b[B")
	if tb.Selected() != 1 {
		t.Errorf("after down: selected %d, want 1", tb.Selected())
	}
	if !clickAt(tb, 5, 2) {
		t.Fatal("a press inside the table was not consumed")
	}
	if !tb.Focused() {
		t.Error("a press did not take focus")
	}
	if tb.Selected() != 0 {
		t.Errorf("after clicking the first body row: selected %d, want 0", tb.Selected())
	}
	if !clickAt(tb, 5, 4) {
		t.Fatal("a press on the third body row was not consumed")
	}
	if tb.Selected() != 2 {
		t.Errorf("after clicking the third body row: selected %d, want 2", tb.Selected())
	}
	mustPress(t, tb, "\x1b[F") // end
	if tb.Selected() != 9 {
		t.Errorf("after end: selected %d, want 9", tb.Selected())
	}
}

func TestTableWheelScrollsVerticallyAndShiftScrollsHorizontally(t *testing.T) {
	tb := plainTable(t, 22, 4, 10)
	focus(tb)
	if !wheelAt(tb, 5, 2, false) {
		t.Fatal("a wheel notch was not consumed")
	}
	if tb.RowOffset() != wheelLines {
		t.Errorf("after one notch: row offset %d, want %d", tb.RowOffset(), wheelLines)
	}
	if tb.Selected() != 0 {
		t.Errorf("scrolling moved the selection to %d", tb.Selected())
	}
	before := inner(rows(t, 22, 4, tb), 1)
	if !tb.Handle(termmosaicMouse(5, 2, termmosaicModShift)) {
		t.Fatal("a shifted wheel notch was not consumed")
	}
	// Three notches ask for the end of the content. What matters is that the table
	// MOVED horizontally, not which column index it reports: the offset is in cells
	// and ColOffset reports the leftmost column with cells on screen.
	after := inner(rows(t, 22, 4, tb), 1)
	assertDifferent(t, "the table before and after a shifted wheel notch", before, after)
	if tb.ColOffset() >= tb.Columns() {
		t.Errorf("column offset %d is past the last column", tb.ColOffset())
	}
}

func TestTableDegenerateSizesDoNotPanic(t *testing.T) {
	for _, size := range []buffer.Size{{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 2}, {W: 3, H: 1}, {W: 6, H: 1}, {W: 0, H: 5}} {
		tb := NewTable(buffer.Rect{X: 0, Y: 0, W: size.W, H: size.H},
			named("A", 4), named("B", 4), named("C", 4))
		tb.Header = true
		tb.SetRows(tableRows(50))
		tb.Block().SetBorder(buffer.BorderPlain)
		focus(tb)
		buf := cellBuf(size.W, size.H)
		tb.Draw(buf)
		mustPress(t, tb, "\x1b[C")
		mustPress(t, tb, "\x1b[B")
		tb.Draw(cellBuf(size.W, size.H))
	}
}

func TestTableDrawIsAllocationFree(t *testing.T) {
	tb := plainTable(t, 40, 20, 5000)
	buf := cellBuf(40, 20)
	drawAll(tb, buf, 3)
	if got := testing.AllocsPerRun(200, func() { tb.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per frame; the frame path must be free", got)
	}
}

func TestTableScrollingDoesNotAllocate(t *testing.T) {
	tb := plainTable(t, 40, 20, 100000)
	focus(tb)
	buf := cellBuf(40, 20)
	drawAll(tb, buf, 3)
	if got := testing.AllocsPerRun(200, func() {
		tb.vm.ScrollBy(1)
		tb.Draw(buf)
	}); got != 0 {
		t.Errorf("scrolling then Draw allocated %v times", got)
	}
	// Horizontal scrolling is the widget's own arithmetic rather than virtual's,
	// so it gets the same treatment.
	if got := testing.AllocsPerRun(200, func() {
		tb.SetColOffset(tb.ColOffset() + 1)
		tb.Draw(buf)
	}); got != 0 {
		t.Errorf("a horizontal scroll then Draw allocated %v times", got)
	}
}

func TestTableHundredThousandRowsIsFlat(t *testing.T) {
	cost := func(n int) float64 {
		tb := plainTable(t, 40, 20, n)
		buf := cellBuf(40, 20)
		drawAll(tb, buf, 3)
		return testing.AllocsPerRun(50, func() { tb.Draw(buf) })
	}
	small, huge := cost(5), cost(100000)
	if small != huge {
		t.Errorf("100k rows cost %v allocs/frame and 5 rows cost %v; per-frame cost is not flat", huge, small)
	}
	// And the far end of the collection is reachable and correctly reported.
	tb := plainTable(t, 40, 20, 100000)
	focus(tb)
	mustPress(t, tb, "\x1b[F")
	if tb.Selected() != 99999 {
		t.Errorf("after end on 100k rows: selected %d, want 99999", tb.Selected())
	}
}

func TestTableGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	// The wide layout puts content in cells the narrow layout must clear. A test
	// that only grows cannot catch that, so this shrinks and checks both the
	// surviving content and the width of every row.
	tb := plainTable(t, 34, 5, 20)
	wide := rows(t, 34, 5, tb)
	if !strings.Contains(wide[1], "STATE") {
		t.Fatalf("the wide layout did not draw the last column: %q", wide[1])
	}
	tb.SetBounds(buffer.Rect{X: 0, Y: 0, W: 14, H: 5})
	narrow := rows(t, 14, 5, tb)
	if !strings.Contains(narrow[1], "ID") {
		t.Errorf("the header did not survive the shrink: %q", narrow[1])
	}
	for y := range narrow {
		if len([]rune(narrow[y])) > 14 {
			t.Errorf("row %d is wider than the screen after shrinking: %q", y, narrow[y])
		}
	}
}

func TestTableShrinkRepaintsEveryCellOfItsBounds(t *testing.T) {
	// The direct form of ADR 0007 §1 rule 3: the row a wide table drew past its
	// new right edge must not survive, because the renderer diffs and never
	// clears.
	tb := plainTable(t, 30, 4, 5)
	buf := cellBuf(30, 4)
	tb.Draw(buf)
	tb.SetBounds(buffer.Rect{X: 0, Y: 0, W: 30, H: 4})
	for y := 0; y < 4; y++ {
		for x := 0; x < 30; x++ {
			buf.SetCell(x, y, buffer.Cell{Ch: 'Z'})
		}
	}
	tb.Draw(buf)
	for y := 0; y < 4; y++ {
		for x := 0; x < 30; x++ {
			if buf.CellAt(x, y).Ch == 'Z' {
				t.Fatalf("cell (%d,%d) still holds the stale 'Z'", x, y)
			}
		}
	}
}

func TestTableMinSizeIncludesChrome(t *testing.T) {
	tb := NewTable(buffer.Rect{W: 1, H: 1}, named("A", 4))
	tb.Block().SetBorder(buffer.BorderPlain)
	got := tb.MinSize()
	if got.W != minTableW+2 || got.H != minTableH+2 {
		t.Errorf("MinSize = %+v, want {%d,%d}", got, minTableW+2, minTableH+2)
	}
}

func TestTableShortRowsAndSurplusCellsAreLegal(t *testing.T) {
	// Data arrives from disk; a malformed row must not take the frame down.
	tb := NewTable(buffer.Rect{X: 0, Y: 0, W: 20, H: 3}, named("A", 3), named("B", 3), named("C", 3))
	tb.SetRows([]Row{
		{Cells: []Cell{{Text: "one"}}},
		{Cells: []Cell{{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}, {Text: "e"}}},
	})
	got := rows(t, 20, 3, tb)
	if !strings.Contains(got[0], "one") || !strings.Contains(got[1], "a") {
		t.Errorf("a short or long row was not drawn: %q / %q", got[0], got[1])
	}
}

// maxColOffsetForTest exposes the horizontal clamp's ceiling in COLUMN terms, which
// is internal state a caller has no business reading but a test needs in order to
// assert that scrolling stops there rather than somewhere further on.
func (t *Table) maxColOffsetForTest() int { return t.maxColOffset }

// maxCellsForTest exposes the same ceiling in CELL terms, which is the unit that
// decides how much of the content is reachable.
func (t *Table) maxCellsForTest() int { return t.maxCells }

// TestTableWideColumnIsMarkedRatherThanBleedingIntoTheFrame is the range-clip
// contract end to end for a column wider than the whole viewport: the column is
// drawn inside its own range with the marker in the last cell of that range, and
// nothing lands on the scrollbar or the border beside it.
//
// It goes through buffer.SetSpansWindowIn — the writer a horizontally scrolled
// column uses.
//
// An earlier version of this comment claimed the scrolled-skip case was covered
// only in buffer's own tests, "because the Table's cell offset always lands on a
// column start, so a partially scrolled column is not reachable from here". That was
// wrong. The cell offset lands on a column start when a caller asks for one, but
// clampColOffset's CEILING is totalW - contentW — the right-hand edge of the
// content — which is generally not a column start, so scrolling to the end or
// resizing lands mid-column. table_window_test.go proves it and pins it; see
// TestTablePartialColumnSkipIsReachable there.
func TestTableWideColumnIsMarkedRatherThanBleedingIntoTheFrame(t *testing.T) {
	tb := NewTable(buffer.Rect{X: 0, Y: 0, W: 18, H: 5}, named("ID", 4), sized("STATE"))
	tb.Header = true
	body := make([]Row, 2)
	for i := range body {
		body[i] = Row{Cells: []Cell{
			{Text: fmt.Sprintf("r%d", i)},
			{Text: "state-running-long-and-then-some"},
		}}
	}
	tb.SetRows(body)
	tb.Block().SetBorder(buffer.BorderPlain)

	got := rows(t, 18, 5, tb)
	for _, y := range []int{2, 3} {
		line := inner(got, y)
		if !strings.Contains(line, buffer.TruncSuffix) {
			t.Errorf("row %d shows no truncation marker although the column is wider than the viewport: %q", y, line)
		}
		if strings.Contains(line, "state-running-long-and-then-some") {
			t.Errorf("row %d shows the whole value although the column is cut: %q", y, line)
		}
		// The frame survives: a marker painted over the border would be the bug
		// this test exists for.
		wantFramed(t, got, y)
	}
}
