package viz

import (
	"math"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// labelledBar returns a bordered progress bar with a label, 30 cells wide.
func labelledBar(w, h int) *ProgressBar {
	p := NewProgressBar(buffer.Rect{X: 0, Y: 0, W: w, H: h})
	p.Block().SetBorder(buffer.BorderPlain)
	p.SetLabel("build", buffer.DefaultStyle)
	p.SetPercentage(true)
	return p
}

func TestProgressBarDrawsLabelFillAndPercentage(t *testing.T) {
	p := labelledBar(30, 3)
	p.Set(0.5)
	got := rows(t, 30, 3, p)
	// The border row is Block's, and widgets/block tests it; what matters here
	// is that the frame is where it belongs.
	wantFramed(t, got, 0)
	// The bar is what is left between the label and the number: 28 interior cells,
	// less "build" and its two spaces, less "50%" and its two spaces. Half of it is
	// seven whole cells and a half-cell, which is why the last glyph is U+258C.
	want(t, got, 1, "build  ███████▌         50%")
	// The border row is Block's, and widgets/block tests it; what matters here
	// is that the frame is where it belongs.
	wantFramed(t, got, 2)
}

func TestProgressBarFillGrowsMonotonicallyWithTheValue(t *testing.T) {
	// The two compared values must REALLY differ: 0.25 and 0.75 over a 20-cell bar
	// are five and fifteen filled cells, so a widget that ignored Value would
	// produce identical frames and this test would fail.
	p := labelledBar(30, 3)
	p.Set(0.25)
	low := rows(t, 30, 3, p)
	p.Set(0.75)
	high := rows(t, 30, 3, p)
	assertDifferent(t, "the bar at 25% against the bar at 75%", low[1], high[1])

	lowFill := countRune(innerRow(low[1]), fillGlyph(false))
	highFill := countRune(innerRow(high[1]), fillGlyph(false))
	if highFill <= lowFill {
		t.Errorf("filled cells did not grow: %d at 25%%, %d at 75%%", lowFill, highFill)
	}
	// And the printed numbers differ, which is the non-colour reading.
	if !strings.Contains(low[1], "25%") || !strings.Contains(high[1], "75%") {
		t.Errorf("the percentage is missing from one of the frames: %q / %q", low[1], high[1])
	}
}

func TestProgressBarValueBoundariesClampRatherThanPanic(t *testing.T) {
	p := labelledBar(30, 3)
	cases := []struct {
		name  string
		set   float64
		value float64
	}{
		{"zero", 0, 0},
		{"one", 1, 1},
		{"negative", -5, 0},
		{"over one", 4.2, 1},
		{"NaN", math.NaN(), 0},
		{"positive infinity", math.Inf(1), 1},
		{"negative infinity", math.Inf(-1), 0},
	}
	for _, c := range cases {
		p.Set(c.set)
		if p.Value() != c.value {
			t.Errorf("%s: Set(%v) left Value at %v, want %v", c.name, c.set, p.Value(), c.value)
		}
		p.Draw(cellBuf(30, 3))
	}
	// A zero-width bar draws its track and nothing else rather than panicking.
	zero := NewProgressBar(buffer.Rect{X: 0, Y: 0, W: 1, H: 1})
	zero.Set(0.5)
	zero.Draw(cellBuf(1, 1))
}

func TestProgressBarTracksAnEmptyAndAFullValueDifferently(t *testing.T) {
	p := labelledBar(30, 3)
	p.Set(0)
	empty := rows(t, 30, 3, p)
	p.Set(1)
	full := rows(t, 30, 3, p)
	assertDifferent(t, "an empty bar against a full one", empty[1], full[1])
	if countRune(innerRow(empty[1]), fillGlyph(false)) != 0 {
		t.Errorf("an empty bar has fill: %q", empty[1])
	}
	if countRune(innerRow(full[1]), fillGlyph(false)) == 0 {
		t.Errorf("a full bar has no fill: %q", full[1])
	}
}

func TestProgressBarDegenerateSizesDoNotPanic(t *testing.T) {
	for _, size := range []buffer.Size{{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 1}, {W: 4, H: 2}, {W: 0, H: 5}, {W: 9, H: 0}} {
		p := NewProgressBar(buffer.Rect{X: 0, Y: 0, W: size.W, H: size.H})
		p.Block().SetBorder(buffer.BorderPlain)
		p.SetLabel("a long label that cannot fit", buffer.DefaultStyle)
		p.SetPercentage(true)
		p.Set(0.5)
		p.Draw(cellBuf(size.W, size.H))
		p.Set(math.NaN())
		p.Draw(cellBuf(size.W, size.H))
	}
}

func TestProgressBarGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	// A resize test that only grows cannot catch a stale cell, so this shrinks: the
	// wide frame's percentage and fill sat in cells the narrow frame must clear.
	p := labelledBar(40, 3)
	p.Set(0.8)
	wide := rows(t, 40, 3, p)
	if !strings.Contains(wide[1], "80%") {
		t.Fatalf("the wide frame did not draw the percentage: %q", wide[1])
	}
	p.SetBounds(buffer.Rect{X: 0, Y: 0, W: 18, H: 3})
	narrow := rows(t, 18, 3, p)
	if len([]rune(narrow[1])) > 18 {
		t.Errorf("row 1 is wider than the screen after shrinking: %q", narrow[1])
	}
	// Growing back must restore the wide layout rather than keeping the narrow one.
	p.SetBounds(buffer.Rect{X: 0, Y: 0, W: 40, H: 3})
	again := rows(t, 40, 3, p)
	if again[1] != wide[1] {
		t.Errorf("growing back did not restore the layout:\n got %q\nwant %q", again[1], wide[1])
	}
}

func TestProgressBarDrawIsAllocationFree(t *testing.T) {
	p := labelledBar(40, 3)
	p.Set(0.42)
	buf := cellBuf(40, 3)
	drawAll(p, buf, 3)
	if got := testing.AllocsPerRun(200, func() { p.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per frame; the frame path must be free", got)
	}
}

func TestProgressBarMinSizeIncludesChrome(t *testing.T) {
	p := NewProgressBar(buffer.Rect{W: 1, H: 1})
	p.Block().SetBorder(buffer.BorderPlain)
	got := p.MinSize()
	if got.W != minProgressW+2 || got.H != minProgressH+2 {
		t.Errorf("MinSize = %+v, want {%d,%d}", got, minProgressW+2, minProgressH+2)
	}
}

func TestProgressBarHandlesNothing(t *testing.T) {
	p := labelledBar(30, 3)
	if p.Handle(termmosaicEvent()) {
		t.Error("a progress bar consumed an event; a bar has no focus and no interaction")
	}
}

// innerRow strips a row's frame so a test can count the fill without depending on
// the chrome.
func innerRow(row string) string { return unframe(row) }
