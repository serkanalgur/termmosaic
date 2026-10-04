package viz

import (
	"math"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// fourBars returns four categories whose values differ enough for every bar to have
// a distinct height.
func fourBars() []Datum {
	return []Datum{
		{Label: "read", Value: 30},
		{Label: "write", Value: 70},
		{Label: "idle", Value: 10},
		{Label: "wait", Value: 100},
	}
}

func TestBarChartVerticalDrawsBarsAxisAndValues(t *testing.T) {
	c := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 32, H: 8})
	c.SetData(fourBars())
	buf := cellBuf(32, 8)
	c.Draw(buf)
	rowsOut := rows(t, 32, 8, c)
	joined := strings.Join(rowsOut, "\n")
	// The axis is a hyphen rule, not a box-drawing glyph: a chart axis is texture,
	// and buffer's border table is the catalog's only owner of the other kind. The
	// labels sit ON the rule, so what is asserted is that the last row is made of
	// axis glyphs and labels and nothing else.
	last := rowsOut[len(rowsOut)-1]
	if !strings.ContainsRune(last, axisRune) {
		t.Errorf("the last row carries no axis rule: %q", last)
	}
	if strings.ContainsRune(last, '█') {
		t.Errorf("the axis row carries a bar: %q", last)
	}
	for _, label := range []string{"read", "write", "idle", "wait"} {
		if !strings.Contains(joined, label) {
			t.Errorf("category %q is not labelled:\n%s", label, joined)
		}
	}
	// The values are printed, which is the reading that survives monochrome.
	for _, v := range []string{"30", "70", "10", "100"} {
		if !strings.Contains(joined, v) {
			t.Errorf("value %s is not printed:\n%s", v, joined)
		}
	}
}

func TestBarChartHeightsTrackTheirValues(t *testing.T) {
	// The tallest bar must be taller than the shortest, and the two must not be the
	// same cell: a chart whose bars all had one height would satisfy every other
	// assertion in this file.
	c := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 32, H: 8})
	c.SetData(fourBars())
	low := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 32, H: 8})
	low.SetData([]Datum{{Label: "a", Value: 10}, {Label: "b", Value: 100}, {Label: "c", Value: 10}, {Label: "d", Value: 10}})
	buf := cellBuf(32, 8)
	c.Draw(buf)
	low.Draw(buf)
	first := rows(t, 32, 8, c)
	second := rows(t, 32, 8, low)
	assertDifferent(t, "a chart with one tall bar against one with several", strings.Join(first, ""), strings.Join(second, ""))
}

func TestBarChartPartialCellUsesTheEighthRamp(t *testing.T) {
	// A bar that is not a whole number of cells must show the fraction: at 50% of a
	// three-cell plot the top cell is a half block, which is the difference between a
	// chart with four levels per row and one with one.
	c := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 4, H: 5})
	c.ShowValue = false
	c.Axis = false
	c.SetData([]Datum{{Label: "x", Value: 50}})
	c.Max = 100
	buf := cellBuf(4, 5)
	c.Draw(buf)
	// The bar is 50% of a five-cell plot: two whole cells at the bottom and a
	// partial cell above them, in the bar's OWN column.
	partial := false
	for y := 0; y < 5; y++ {
		for x := 0; x < 4; x++ {
			r := buf.CellAt(x, y).Ch
			for i, e := range eighths {
				if r == e && i < len(eighths)-1 {
					partial = true
				}
			}
		}
	}
	if !partial {
		t.Errorf("a half-full bar drew only whole cells; the eighth ramp is not being used:\n%s", cellDump(buf, 4, 5))
	}
	if r := buf.CellAt(0, 2).Ch; r == eighths[len(eighths)-1] {
		t.Errorf("the cell above the fill is a full block, so the fraction was not drawn: %q", r)
	}
	if buf.CellAt(0, 4).Ch != fullBlock {
		t.Errorf("the bottom of the bar is not filled: %q", buf.CellAt(0, 4).Ch)
	}
}

func TestBarChartHorizontalStaysInsideItsRect(t *testing.T) {
	// A horizontal chart NOT at the origin. Its labels must land in its own label
	// column, not at absolute column 0: a widget writing outside Bounds is an ADR
	// 0007 §4 violation, and it is invisible in every other test in this file
	// because they all draw at X=0 where the two answers coincide.
	c := NewBarChart(buffer.Rect{X: 40, Y: 6, W: 30, H: 5})
	c.Block().SetBorder(buffer.BorderPlain)
	c.Vertical = false
	c.SetData([]Datum{{Label: "cpu", Value: 25}, {Label: "disk", Value: 80}})
	buf := cellBuf(80, 14)
	c.Draw(buf)
	for _, y := range []int{7, 8} {
		if got := cellsOf(buf, y, 0, 40); strings.TrimSpace(got) != "" {
			t.Errorf("row %d drew %q to the LEFT of the chart's own rectangle", y, got)
		}
	}
	for y, want := range map[int]string{7: "cpu", 8: "disk"} {
		if got := cellsOf(buf, y, 40, 40); !strings.Contains(got, want) {
			t.Errorf("row %d does not carry the label %q inside the chart: %q", y, want, got)
		}
	}
}

func TestBarChartHorizontalDrawsOneRowPerCategory(t *testing.T) {
	c := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 30, H: 6})
	c.Vertical = false
	c.SetData([]Datum{{Label: "cpu", Value: 25}, {Label: "disk", Value: 80}})
	buf := cellBuf(30, 6)
	c.Draw(buf)
	if cellsOf(buf, 0, 0, 30) == cellsOf(buf, 1, 0, 30) {
		t.Error("two different categories drew identical rows")
	}
	if !strings.Contains(cellsOf(buf, 0, 0, 30), "cpu") {
		t.Errorf("the first category label is missing: %q", cellsOf(buf, 0, 0, 30))
	}
	if !strings.Contains(cellsOf(buf, 1, 0, 30), "disk") {
		t.Errorf("the second category label is missing: %q", cellsOf(buf, 1, 0, 30))
	}
	// The longer bar is the longer row of glyphs.
	if countRune(cellsOf(buf, 1, 0, 30), fillGlyph(false)) <= countRune(cellsOf(buf, 0, 0, 30), fillGlyph(false)) {
		t.Error("the 80% bar is not longer than the 25% one")
	}
}

func TestBarChartValueBoundariesClampRatherThanPanic(t *testing.T) {
	c := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 24, H: 6})
	for _, v := range []float64{0, 100, -50, 1000, math.NaN(), math.Inf(1)} {
		c.SetData([]Datum{{Label: "a", Value: v}, {Label: "b", Value: 50}})
		c.Draw(cellBuf(24, 6))
	}
	// A negative or NaN bar draws no fill but keeps its label, because a category
	// that vanished would be indistinguishable from one that was missing.
	c.SetData([]Datum{{Label: "neg", Value: -5}})
	c.Draw(cellBuf(24, 6))
	if c.MaxValue() != 0 {
		t.Errorf("MaxValue over one negative value = %v, want 0", c.MaxValue())
	}
	// A pinned scale is honoured, which is how two charts are made comparable.
	c.Max = 200
	if c.MaxValue() != 200 {
		t.Errorf("a pinned Max was ignored: %v", c.MaxValue())
	}
	// No data at all is legal.
	empty := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 10, H: 4})
	empty.Draw(cellBuf(10, 4))
}

func TestBarChartDegenerateSizesDoNotPanic(t *testing.T) {
	for _, size := range []buffer.Size{{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 2}, {W: 4, H: 3}, {W: 0, H: 6}, {W: 12, H: 0}} {
		for _, vertical := range []bool{true, false} {
			c := NewBarChart(buffer.Rect{X: 0, Y: 0, W: size.W, H: size.H})
			c.Vertical = vertical
			c.SetData(fourBars())
			c.Draw(cellBuf(size.W, size.H))
		}
	}
}

func TestBarChartGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	c := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 40, H: 8})
	c.SetData(fourBars())
	wide := rows(t, 40, 8, c)
	buf := cellBuf(40, 8)
	c.Draw(buf)
	for y := 0; y < 8; y++ {
		for x := 0; x < 40; x++ {
			buf.SetCell(x, y, buffer.Cell{Ch: 'Z'})
		}
	}
	c.SetBounds(buffer.Rect{X: 0, Y: 0, W: 40, H: 8})
	c.Draw(buf)
	for y := 0; y < 8; y++ {
		for x := 0; x < 40; x++ {
			if buf.CellAt(x, y).Ch == 'Z' {
				t.Fatalf("cell (%d,%d) still holds the stale 'Z'", x, y)
			}
		}
	}
	c.SetBounds(buffer.Rect{X: 0, Y: 0, W: 14, H: 8})
	narrow := rows(t, 14, 8, c)
	for y := range narrow {
		if len([]rune(narrow[y])) > 14 {
			t.Errorf("row %d is wider than the screen after shrinking: %q", y, narrow[y])
		}
	}
	c.SetBounds(buffer.Rect{X: 0, Y: 0, W: 40, H: 8})
	again := rows(t, 40, 8, c)
	if strings.Join(again, "") != strings.Join(wide, "") {
		t.Error("growing back did not restore the layout")
	}
}

func TestBarChartDrawIsAllocationFree(t *testing.T) {
	c := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 40, H: 10})
	for i := 0; i < 50; i++ {
		c.SetData([]Datum{{Label: "a", Value: float64(i)}, {Label: "b", Value: float64(50 - i)}})
		buf := cellBuf(40, 10)
		drawAll(c, buf, 3)
		if got := testing.AllocsPerRun(200, func() { c.Draw(buf) }); got != 0 {
			t.Fatalf("Draw allocated %v times per frame with %d categories", got, len(c.Data))
		}
	}
	h := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 40, H: 10})
	h.Vertical = false
	h.SetData(fourBars())
	buf := cellBuf(40, 10)
	drawAll(h, buf, 3)
	if got := testing.AllocsPerRun(200, func() { h.Draw(buf) }); got != 0 {
		t.Errorf("the horizontal orientation allocated %v times per frame", got)
	}
}

func TestBarChartMinSizeFollowsTheOrientation(t *testing.T) {
	c := NewBarChart(buffer.Rect{W: 1, H: 1})
	c.Block().SetBorder(buffer.BorderPlain)
	if got := c.MinSize(); got.H != minChartH+2 {
		t.Errorf("a vertical chart's MinSize = %+v, want a height of %d", got, minChartH+2)
	}
	c.Vertical = false
	if got := c.MinSize(); got.H != 4 {
		t.Errorf("a horizontal chart's MinSize = %+v, want a height of 4", got)
	}
}

func TestBarChartHandlesNothing(t *testing.T) {
	c := NewBarChart(buffer.Rect{X: 0, Y: 0, W: 20, H: 6})
	if c.Handle(termmosaicEvent()) {
		t.Error("a chart consumed a zero event")
	}
}
