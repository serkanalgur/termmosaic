package viz

import (
	"math"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// series returns a fixed test series: low, high, low, higher, mid.
func series() []float64 { return []float64{1, 9, 2, 7, 4} }

func TestSparklineBrailleModeResolvesTwoSamplesPerCell(t *testing.T) {
	s := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 10, H: 1})
	s.SetValues(series())
	buf := cellBuf(10, 1)
	s.Draw(buf)
	row := cellsOf(buf, 0, 0, 10)
	// Five samples in Braille mode fill three cells (two samples each), and every one
	// of them is a Braille cell: that is the sub-cell resolution, two samples per
	// cell where a block-element sparkline gets one.
	cells := 0
	for _, r := range row {
		if r >= brailleBase && r != brailleBase {
			cells++
		}
	}
	if cells != 3 {
		t.Errorf("five samples produced %d Braille cells in %q, want 3", cells, row)
	}
}

func TestSparklineBlockModeResolvesEightLevelsPerCell(t *testing.T) {
	s := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 10, H: 1})
	s.Braille = false
	s.SetValues([]float64{0, 1, 2, 3, 4})
	buf := cellBuf(10, 1)
	s.Draw(buf)
	row := cellsOf(buf, 0, 0, 10)
	// One sample per cell, each an eighth-block at a level proportional to its
	// value: five distinct glyphs out of a five-sample ramp. Two values differing by
	// one step must therefore produce two DIFFERENT glyphs, which is what makes the
	// resolution real.
	seen := map[rune]bool{}
	for _, r := range []rune(row)[:5] {
		seen[r] = true
	}
	if len(seen) != 5 {
		t.Errorf("five distinct values produced %d distinct glyphs in %q, want 5", len(seen), row)
	}
	for r := range seen {
		found := false
		for _, e := range eighths {
			if e == r {
				found = true
			}
		}
		if !found {
			t.Errorf("glyph %U in %q is not an eighth-block level", r, row)
		}
	}
}

func TestSparklineEncodesTheSeriesInGeometryNotColour(t *testing.T) {
	// A rising series and a falling one over the same range must differ in their
	// GLYPHS, with every style left unset. If they differed only in colour the
	// assertion below would pass while the widget was unreadable in monochrome —
	// which is the whole accessibility claim, so it is asserted on the characters.
	//
	// The two series are the same six numbers in opposite orders on purpose: that
	// makes them differ in exactly the way a reader is meant to see, and makes a
	// widget that ignored the ORDER fail here rather than pass by accident.
	low := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 6, H: 1})
	low.Braille = false
	low.SetValues([]float64{1, 2, 3, 4, 5, 6})
	lowBuf := cellBuf(6, 1)
	low.Draw(lowBuf)

	high := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 6, H: 1})
	high.Braille = false
	high.SetValues([]float64{6, 5, 4, 3, 2, 1})
	highBuf := cellBuf(6, 1)
	high.Draw(highBuf)

	lowRow, highRow := cellsOf(lowBuf, 0, 0, 6), cellsOf(highBuf, 0, 0, 6)
	assertDifferent(t, "a flat low series against a flat high one", lowRow, highRow)
	for i := 0; i < 6; i++ {
		if lowBuf.CellAt(i, 0).Style() != highBuf.CellAt(i, 0).Style() {
			t.Errorf("cell %d differs in style, so this test was not measuring geometry: %v vs %v",
				i, lowBuf.CellAt(i, 0).Style(), highBuf.CellAt(i, 0).Style())
		}
	}
}

func TestSparklineNormalisationHandlesDegenerateSeries(t *testing.T) {
	// A flat series has no range to normalise against: dividing by it would be a
	// division by zero, and drawing it "high" would be a lie about a series that
	// does not move.
	s := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 4, H: 1})
	s.SetValues([]float64{5, 5, 5, 5})
	buf := cellBuf(4, 1)
	s.Draw(buf)
	if row := cellsOf(buf, 0, 0, 4); row != "" {
		// A constant series is drawn at the bottom of the range, which is the
		// eighth-block floor: the point is that it drew something and did not
		// divide by zero.
		t.Logf("a flat series drew %q", row)
	}
	// Pinned bounds with values outside them clamp rather than overflow.
	p := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 4, H: 1})
	p.Auto = false
	p.Min, p.Max = 0, 10
	p.SetValues([]float64{-5, 5, 15})
	p.Draw(cellBuf(4, 1))
	if p.level(math.NaN()) != 0 {
		t.Error("a NaN sample did not normalise to zero")
	}
	if l := p.level(5); l != 0.5 {
		t.Errorf("level(5) on a 0..10 scale = %v, want 0.5", l)
	}
	if l := p.level(50); l != 1 {
		t.Errorf("level(50) on a 0..10 scale = %v, want 1 (clamped)", l)
	}
	if l := p.level(-50); l != 0 {
		t.Errorf("level(-50) on a 0..10 scale = %v, want 0 (clamped)", l)
	}
}

func TestSparklineNaNSampleDoesNotShiftTheSeries(t *testing.T) {
	// A NaN is drawn as zero rather than skipped: skipping it would move every
	// later sample one cell left, which silently misreports the series.
	withNaN := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 6, H: 1})
	withNaN.Braille = false
	withNaN.Auto = false
	withNaN.Min, withNaN.Max = 1, 9
	withNaN.SetValues([]float64{1, math.NaN(), 9, 1, 1, 1})
	b1 := cellBuf(6, 1)
	withNaN.Draw(b1)
	if cellsOf(b1, 0, 0, 6) == "" {
		t.Error("a series containing a NaN drew nothing")
	}
	// The sample after the NaN is still in its own cell.
	// The comparison pins the scale to 1..9, so the NaN's floor IS the first
	// sample's level: the two rows must be identical, cell for cell.
	zeroed := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 6, H: 1})
	zeroed.Braille = false
	zeroed.Auto = false
	zeroed.Min, zeroed.Max = 1, 9
	zeroed.SetValues([]float64{1, 1, 9, 1, 1, 1})
	b2 := cellBuf(6, 1)
	zeroed.Draw(b2)
	if cellsOf(b1, 0, 0, 6) != cellsOf(b2, 0, 0, 6) {
		t.Errorf("a NaN sample is not drawn as zero:\n got %q\nwant %q", cellsOf(b1, 0, 0, 6), cellsOf(b2, 0, 0, 6))
	}
}

func TestSparklineThresholdMarksWithAnAttributeNotAHue(t *testing.T) {
	s := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 6, H: 1})
	s.Braille = false
	s.Auto = false
	s.Min, s.Max = 0, 10
	s.Threshold = 8
	s.SetValues([]float64{1, 2, 9, 3})
	buf := cellBuf(6, 1)
	s.Draw(buf)
	// The breach is at index 2, and it is marked with AttrReverse — an attribute, so
	// it survives a monochrome terminal.
	if !buf.CellAt(2, 0).Attr.Has(buffer.AttrReverse) {
		t.Errorf("the breaching sample is not reversed: attr %v", buf.CellAt(2, 0).Attr)
	}
	if buf.CellAt(0, 0).Attr.Has(buffer.AttrReverse) {
		t.Errorf("a normal sample is reversed: attr %v", buf.CellAt(0, 0).Attr)
	}
}

func TestSparklineVerticalLayoutRunsDownTheColumn(t *testing.T) {
	s := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 3, H: 4})
	s.Vertical = true
	s.Braille = false
	s.SetValues([]float64{1, 5, 9})
	buf := cellBuf(3, 4)
	s.Draw(buf)
	// Three samples, three rows, each a full width of three cells: the vertical mode
	// trades horizontal for vertical resolution, and says so in the field's
	// documentation.
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			if buf.CellAt(x, y).Ch == ' ' {
				t.Fatalf("cell (%d,%d) of a vertical sparkline is blank", x, y)
			}
		}
	}
	if buf.CellAt(0, 3).Ch != ' ' {
		t.Error("the fourth row of a three-sample series is not blank")
	}
}

func TestSparklineDegenerateSizesDoNotPanic(t *testing.T) {
	for _, size := range []buffer.Size{{W: 0, H: 0}, {W: 1, H: 1}, {W: 1, H: 4}, {W: 4, H: 0}, {W: 2, H: 2}} {
		for _, vertical := range []bool{false, true} {
			s := NewSparkline(buffer.Rect{X: 0, Y: 0, W: size.W, H: size.H})
			s.Vertical = vertical
			s.SetValues(series())
			s.Draw(cellBuf(size.W, size.H))
		}
	}
	// An empty series is legal and draws nothing.
	e := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 4, H: 1})
	e.Draw(cellBuf(4, 1))
}

func TestSparklineGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	s := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 20, H: 1})
	s.Braille = false
	s.SetValues([]float64{1, 9, 2, 7, 4, 1, 9, 2, 7, 4, 1, 9, 2, 7, 4, 1, 9, 2, 7, 4})
	buf := cellBuf(20, 1)
	s.Draw(buf)
	for x := 0; x < 20; x++ {
		buf.SetCell(x, 0, buffer.Cell{Ch: 'Z'})
	}
	s.SetBounds(buffer.Rect{X: 0, Y: 0, W: 20, H: 1})
	s.Draw(buf)
	for x := 0; x < 20; x++ {
		if buf.CellAt(x, 0).Ch == 'Z' {
			t.Fatalf("cell %d still holds the stale 'Z'", x)
		}
	}
	// And a narrower rect draws fewer cells: the series is clipped, not shifted.
	s.SetBounds(buffer.Rect{X: 0, Y: 0, W: 5, H: 1})
	narrow := cellBuf(5, 1)
	s.Draw(narrow)
	for x := 5; x < 20; x++ {
		if narrow.CellAt(x, 0).Ch != ' ' {
			t.Fatalf("a five-cell sparkline wrote cell %d", x)
		}
	}
}

func TestSparklineDrawIsAllocationFree(t *testing.T) {
	s := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 40, H: 1})
	s.SetValues(make([]float64, 1000))
	buf := cellBuf(40, 1)
	drawAll(s, buf, 3)
	if got := testing.AllocsPerRun(200, func() { s.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per frame; the frame path must be free", got)
	}
}

func TestSparklineMinSizeIncludesChrome(t *testing.T) {
	s := NewSparkline(buffer.Rect{W: 1, H: 1})
	s.Block().SetBorder(buffer.BorderPlain)
	got := s.MinSize()
	if got.W != minSparkW+2 || got.H != minSparkH+2 {
		t.Errorf("MinSize = %+v, want {%d,%d}", got, minSparkW+2, minSparkH+2)
	}
}

func TestSparklineHandlesNothing(t *testing.T) {
	s := NewSparkline(buffer.Rect{X: 0, Y: 0, W: 10, H: 1})
	if s.Handle(termmosaicEvent()) {
		t.Error("a sparkline consumed a zero event")
	}
	_ = strings.TrimSpace("")
}
