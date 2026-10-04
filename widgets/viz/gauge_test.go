package viz

import (
	"math"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// brailleCount returns how many cells of row y lie within [x0, x1) and are Braille
// cells — that is, how much of the dial was actually drawn.
func brailleCount(buf *buffer.Buffer, y, x0, x1 int) int {
	n := 0
	for x := x0; x < x1; x++ {
		if r := buf.CellAt(x, y).Ch; r >= brailleBase && r < brailleBase+256 {
			if r != brailleBase {
				n++
			}
		}
	}
	return n
}

func TestGaugeDrawsABrailleDial(t *testing.T) {
	g := NewGauge(buffer.Rect{X: 0, Y: 0, W: 21, H: 9})
	g.SetLabel("cpu", buffer.DefaultStyle)
	g.Set(50)
	buf := cellBuf(21, 9)
	g.Draw(buf)
	// A dial is Braille cells: that is what gives it 40 dots of resolution across
	// 20 cells, and no block-element drawing gets there.
	drawn := 0
	for y := 0; y < 9; y++ {
		drawn += brailleCount(buf, y, 0, 21)
	}
	if drawn < 20 {
		t.Errorf("only %d Braille cells were drawn; that is not a dial", drawn)
	}
	// And the value is printed, which is the non-colour reading of the dial.
	if !strings.Contains(cellsOf(buf, 8, 0, 21), "50") {
		t.Errorf("the reading is not on screen: %q", cellsOf(buf, 8, 0, 21))
	}
	if !strings.Contains(cellsOf(buf, 8, 0, 21), "cpu") {
		t.Errorf("the label is not on screen: %q", cellsOf(buf, 8, 0, 21))
	}
}

func TestGaugeSweepGrowsWithTheReading(t *testing.T) {
	// The arc's cell COUNT must grow with the reading: a dial whose arc does not
	// move is a decoration, not a measurement. The two values are far enough apart
	// that the counts cannot coincide.
	g := NewGauge(buffer.Rect{X: 0, Y: 0, W: 21, H: 9})
	g.ArcStyle = buffer.ReverseStyle
	count := func(v float64) int {
		g.Set(v)
		buf := cellBuf(21, 9)
		g.Draw(buf)
		n := 0
		for y := 0; y < 9; y++ {
			for x := 0; x < 21; x++ {
				if buf.CellAt(x, y).Attr.Has(buffer.AttrReverse) {
					n++
				}
			}
		}
		return n
	}
	quarter, threeQuarters := count(25), count(75)
	if quarter == 0 {
		t.Error("a quarter reading drew no arc")
	}
	if threeQuarters <= quarter {
		t.Errorf("the arc did not grow: %d cells at 25%%, %d at 75%%", quarter, threeQuarters)
	}
	// A zero reading draws the needle at twelve o'clock and no arc, and a full one
	// fills the ring. The two extremes must differ as FRAMES — comparing their cell
	// counts as characters, which an earlier version of this test did, is a
	// comparison that can pass while both frames are identical.
	frame := func(v float64) string {
		g.Set(v)
		buf := cellBuf(21, 9)
		g.Draw(buf)
		return cellDump(buf, 21, 9)
	}
	zero, full := frame(0), frame(100)
	assertDifferent(t, "the dial at 0 against the dial at 100", zero, full)
	if count(100) <= count(0) {
		t.Errorf("the full dial did not exceed the empty one: %d vs %d cells", count(100), count(0))
	}
}

func TestGaugeFallsBackToABarBelowItsMinimum(t *testing.T) {
	// Three cells of height cannot hold a circle, so the gauge draws a bar. This is
	// the "clip, never blank" contract applied to a measurement widget: the same
	// reading, in a form the space allows.
	g := NewGauge(buffer.Rect{X: 0, Y: 0, W: 20, H: 3})
	g.SetLabel("cpu", buffer.DefaultStyle)
	g.Set(50)
	got := rows(t, 20, 3, g)
	joined := strings.Join(got, "")
	if strings.ContainsRune(joined, brailleBase+1) {
		t.Errorf("a dial was drawn in a three-row rect: %q", got)
	}
	if !strings.Contains(got[0], "50") {
		t.Errorf("the fallback bar lost the reading: %q", got[0])
	}
	if countRune(got[0], fillGlyph(false)) == 0 {
		t.Errorf("the fallback bar has no fill: %q", got[0])
	}
	// The reading's edge is a GLYPH, not just a hue: the needle replaces the fill's
	// last cell, so on a monochrome terminal the exact position is still marked.
	if !strings.ContainsRune(got[0], '▸') {
		t.Errorf("the fallback bar has no reading marker: %q", got[0])
	}
	// And on an ASCII terminal a dial is impossible, so the fallback is chosen even
	// when there is room: Braille outside U+2800 renders as nothing at all.
	a := NewGauge(buffer.Rect{X: 0, Y: 0, W: 21, H: 9})
	a.Block().Ascii = true
	a.Set(50)
	asciiRows := rows(t, 21, 9, a)
	if strings.ContainsRune(strings.Join(asciiRows, ""), brailleBase+1) {
		t.Errorf("a dial was drawn on an ASCII terminal: %q", asciiRows)
	}
}

func TestGaugeValueBoundariesClampRatherThanPanic(t *testing.T) {
	g := NewGauge(buffer.Rect{X: 0, Y: 0, W: 21, H: 9})
	cases := []struct {
		name string
		set  float64
		want float64
	}{
		{"zero", 0, 0},
		{"at the top", 100, 100},
		{"negative", -10, 0},
		{"over the top", 500, 100},
		{"NaN", math.NaN(), 0},
	}
	for _, c := range cases {
		g.Set(c.set)
		if g.Reading() != c.want {
			t.Errorf("%s: Set(%v) leaves Reading at %v, want %v", c.name, c.set, g.Reading(), c.want)
		}
		g.Draw(cellBuf(21, 9))
	}
	// A degenerate scale reads as empty rather than dividing by zero.
	g.ScaleMax = 0
	g.ScaleMin = 0
	g.Set(5)
	g.Draw(cellBuf(21, 9))
	if r := g.Ratio(); r < 0 || r > 1 {
		t.Errorf("Ratio = %v, want it within [0,1]", r)
	}
}

func TestGaugeDegenerateSizesDoNotPanic(t *testing.T) {
	for _, size := range []buffer.Size{{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 2}, {W: 5, H: 3}, {W: 9, H: 4}, {W: 0, H: 9}, {W: 21, H: 0}} {
		g := NewGauge(buffer.Rect{X: 0, Y: 0, W: size.W, H: size.H})
		g.SetLabel("a label wider than most gauges", buffer.DefaultStyle)
		g.Set(50)
		g.Draw(cellBuf(size.W, size.H))
		g.Set(math.NaN())
		g.Draw(cellBuf(size.W, size.H))
	}
}

func TestGaugeGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	g := NewGauge(buffer.Rect{X: 0, Y: 0, W: 25, H: 11})
	g.Set(50)
	wide := rows(t, 25, 11, g)
	buf := cellBuf(25, 11)
	// Dirty the whole buffer the way a stale frame would, shrink, and check that
	// every cell of the widget's own rect was rewritten.
	g.Draw(buf)
	for y := 0; y < 11; y++ {
		for x := 0; x < 25; x++ {
			buf.SetCell(x, y, buffer.Cell{Ch: 'Z'})
		}
	}
	g.SetBounds(buffer.Rect{X: 0, Y: 0, W: 25, H: 11})
	g.Draw(buf)
	for y := 0; y < 11; y++ {
		for x := 0; x < 25; x++ {
			if buf.CellAt(x, y).Ch == 'Z' {
				t.Fatalf("cell (%d,%d) still holds the stale 'Z'; the widget did not repaint its whole bounds", x, y)
			}
		}
	}
	narrow := rows(t, 25, 11, g)
	if len(narrow) != 11 {
		t.Fatalf("screen has %d rows", len(narrow))
	}
	// A different size must actually produce a different dial, or the shrink proved
	// nothing about adaptation.
	g2 := NewGauge(buffer.Rect{X: 0, Y: 0, W: 11, H: 5})
	g2.Set(50)
	small := rows(t, 11, 5, g2)
	if strings.Join(wide, "") == strings.Join(small, "") {
		t.Error("a 25-by-11 dial and an 11-by-5 one rendered identically")
	}
}

func TestGaugeDrawIsAllocationFree(t *testing.T) {
	g := NewGauge(buffer.Rect{X: 0, Y: 0, W: 21, H: 9})
	g.SetLabel("cpu", buffer.DefaultStyle)
	g.Set(50)
	buf := cellBuf(21, 9)
	drawAll(g, buf, 3)
	if got := testing.AllocsPerRun(200, func() { g.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per frame; the frame path must be free", got)
	}
}

func TestGaugeMinSizeIncludesChromeAndIsTheDialNotTheBar(t *testing.T) {
	g := NewGauge(buffer.Rect{W: 1, H: 1})
	g.Block().SetBorder(buffer.BorderPlain)
	got := g.MinSize()
	if got.W != dialMinW+2 || got.H != dialMinH+2 {
		t.Errorf("MinSize = %+v, want {%d,%d}: a gauge that reported the bar's minimum would be handed a rect too small for a dial", got, dialMinW+2, dialMinH+2)
	}
}

func TestGaugeHandlesNothing(t *testing.T) {
	g := NewGauge(buffer.Rect{X: 0, Y: 0, W: 21, H: 9})
	if g.Handle(termmosaicEvent()) {
		t.Error("a gauge consumed a zero event")
	}
}
