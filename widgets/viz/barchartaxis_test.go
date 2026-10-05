package viz

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// This file pins a category label staying inside its own COLUMN on the vertical
// axis.
//
// drawAxis computed the centring offset and threw it away (`_ = lx`), then wrote
// the label with a cap at the end of the whole axis row. So a label wider than
// its column ran over the next category's: with "alphabetical" in an eight-cell
// column next to "b", the first category rendered as "bcal" — the neighbour's
// first cell followed by this label's truncated tail. The horizontal path has
// always capped at the column, and this is the same defect with layout as its
// victim rather than style.

// axisRowText returns row y of buf as text.
func axisRowText(buf *buffer.Buffer, y int) string {
	var b strings.Builder
	for _, c := range buf.Row(y) {
		b.WriteRune(c.Ch)
	}
	return b.String()
}

func TestBarChartAxisLabelStaysInItsOwnColumn(t *testing.T) {
	// Three categories in a 24-cell chart means eight cells each, so
	// "alphabetical" cannot fit and must be truncated with the marker rather than
	// reaching into its neighbour.
	c := NewBarChart(buffer.Rect{W: 24, H: 6})
	c.SetData([]Datum{
		{Label: "alphabetical", Value: 3},
		{Label: "b", Value: 5},
		{Label: "ccc", Value: 2},
	})
	buf := cellBuf(24, 6)
	c.Draw(buf)

	got := axisRowText(buf, c.axisRow.Y)
	if !strings.Contains(got, "b") {
		t.Fatalf("the axis row has no second category in it:\n%q", got)
	}
	// Every cell of the first label lies inside the FIRST column. Before the fix the
	// label was capped at the end of the whole axis row, so its tail landed on the
	// second category's cells and this row read "bcal".
	runes := []rune(got)
	x0, w0 := c.plot.X, c.widths[0]
	// Rebuild the row column by column and compare: each category's label may only
	// occupy its OWN column. Anything else is a spill, whatever glyph it is.
	var want []rune
	for i, d := range c.Data {
		w := c.widths[i]
		// buffer.Truncate is the same rule the axis uses, so the expectation is
		// built by the primitive rather than by a second guess at it.
		kept := buffer.Truncate([]buffer.Span{buffer.NewSpan(d.Label, buffer.DefaultStyle)}, w)
		lw := buffer.SpansWidth(kept)
		off := (w - lw) / 2
		col := make([]rune, w)
		for j := range col {
			col[j] = axisRune
		}
		x := 0
		for _, s := range kept {
			for _, r := range s.Text {
				col[off+x] = r
				x++
			}
		}
		want = append(want, col...)
	}
	for len(want) < len(runes) {
		want = append(want, axisRune)
	}
	if string(runes[x0:min(len(runes), x0+len(want))]) != string(want) {
		t.Errorf("axis row from column 0:\n got %q\nwant %q", string(runes[x0:]), string(want))
	}
	// The second category's own label is still there, which is the cell the first
	// label used to overwrite.
	foundB := false
	for i := x0 + w0; i < len(runes) && i < x0+w0+c.widths[1]; i++ {
		if runes[i] == 'b' {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("category 2's label is not in its own column:\n%q", got)
	}
	// The first category keeps a truncation marker, which is what says the label did
	// not fit — that is the alternative to overwriting the neighbour.
	first := runes[x0:]
	if first[0] == ' ' {
		t.Errorf("the first category's column is blank: %q", got)
	}
	if !strings.ContainsRune(got, c.mark) {
		t.Errorf("the axis row carries no truncation marker, so the clipped label does not say so:\n%q", got)
	}
}

func TestBarChartAxisLabelIsCentredInItsColumn(t *testing.T) {
	// The other half: the offset that used to be discarded is what CENTRES a label
	// narrower than its column. An odd slack in an eight-cell column puts a
	// two-cell label one cell in, and dropping the offset put it hard against the
	// axis.
	c := NewBarChart(buffer.Rect{W: 24, H: 6})
	c.SetData([]Datum{
		{Label: "aaaa", Value: 3},
		{Label: "b", Value: 5},
		{Label: "ccc", Value: 2},
	})
	buf := cellBuf(24, 6)
	c.Draw(buf)

	got := []rune(axisRowText(buf, c.axisRow.Y))
	x0, w := c.plot.X, c.widths[0]
	if w < 5 {
		t.Fatalf("the first column is %d cells, too narrow for this test", w)
	}
	off := -1
	for i := range got {
		if got[i] == 'a' {
			off = i - x0
			break
		}
	}
	if off < 0 {
		t.Fatalf("the first label is not on the axis row:\n%q", string(got))
	}
	if off == 0 {
		t.Errorf("a %d-cell label in a %d-cell column starts at the column's left edge, want it centred", 4, w)
	}
	if off != (w-4)/2 {
		t.Errorf("the label sits %d cells into a %d-cell column, want %d", off, w, (w-4)/2)
	}
}

func TestBarChartAxisLabelsDoNotSpillAtTheRightEdge(t *testing.T) {
	// The same cap seen from the other side: the last category's label must stop at
	// the row's right edge rather than being written past it.
	c := NewBarChart(buffer.Rect{W: 24, H: 6})
	c.SetData([]Datum{
		{Label: "aaa", Value: 3},
		{Label: "bbb", Value: 5},
		{Label: "alphabetical", Value: 2},
	})
	buf := cellBuf(24, 6)
	c.Draw(buf)

	got := axisRowText(buf, c.axisRow.Y)
	if len([]rune(got)) > 24 {
		t.Errorf("the axis row is %d cells wide, want at most the chart's 24", len([]rune(got)))
	}
	if !strings.ContainsRune(got, c.mark) {
		t.Errorf("the clipped last label does not carry a truncation marker:\n%q", got)
	}
	// The last label is also read as the row's SUFFIX, which is the one case where
	// the old cap at the end of the axis row and the new cap at the column edge are
	// the same cell — so this half of the test passes with or without the fix. It is
	// kept because a fix that capped at x+w without clamping to the plot's right edge
	// would break it, and a reader should not have to work that out.
	last := c.plot.X + c.widths[0] + c.widths[1]
	runes := []rune(got)
	if last >= len(runes) {
		t.Fatalf("the last category's column is off the row: %q", got)
	}
	kept := buffer.Truncate([]buffer.Span{buffer.NewSpan("alphabetical", buffer.DefaultStyle)}, c.widths[2])
	var wantLabel []rune
	for _, sp := range kept {
		wantLabel = append(wantLabel, []rune(sp.Text)...)
	}
	if tail := string(runes[len(runes)-len(wantLabel):]); tail != string(wantLabel) {
		t.Errorf("the last label reads %q at the row's end, want %q", tail, string(wantLabel))
	}
}
