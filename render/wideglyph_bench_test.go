package render

// This file measures a full frame whose content is double-width, end to end:
// widget Draw into the back buffer, dirty-rect accumulation, the two-tier diff and
// the encoder. It is the third piece of the wide-glyph measurement ADR 0008 risk 5
// records as missing, after internal/diff/wideglyph_test.go (a wide scene through
// the diff) and buffer/wideglyph_test.go (the writers and Wrap).
//
// # What is comparable, and what is not
//
// wideBlock and asciiBlock paint the SAME cells: the same 200x60 rectangle, the
// same per-row cell count, the same number of SetSpans calls, the same styles, and
// the same one changing cell per frame. The wide one writes half as many runes for
// the same cells, so the ns/op figures are not interchangeable — which is why
// every benchmark here also reports ns/cell.
//
// What that leaves is the honest comparison: two scenes doing identical structural
// work, differing only in how many runes it takes to fill the same cells. A
// material divergence in ns/cell would mean the wide path costs something per
// glyph that the narrow one does not.
//
// # What the force-repaint numbers show
//
// The two ForceRepaint benchmarks are the ones to look at, and they are not the
// result anyone would have guessed. The wide frame writes ~145 KB where the narrow
// one writes ~3 KB, and it is not the glyphs: it is that a wide glyph advances the
// terminal cursor by two cells while the diff's cursor-run suppression assumes one,
// so every wide glyph is preceded by a CUP escape. internal/diff/wideglyph_test.go
// isolates and pins that; see BenchmarkDiffDenseWide.
//
// FIXED 2026-10-04: the run tracker now advances by the glyph's cell width, so
// the wide scene writes 19,443 bytes against the narrow scene's 6,233 - 3.12x
// rather than 11x, with 60 cursor moves instead of 6,000. The residual 3.12x is
// inherent: a wide rune is three UTF-8 bytes where a narrow one is one. The
// ASCII path, which matters far more, is unchanged.

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
)

// wideCells and narrowCells are the two content sets, chosen to cover the same
// number of cells per row. wideRunes is six double-width runes (twelve cells) and
// narrowRunes is twelve single-width ones (twelve cells).
var (
	wideRunes   = []rune{'漢', '字', '🟩', '０', '語', '🚀'}
	narrowRunes = []rune{'A', 'B', 'C', '0', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K'}
)

// glyphScreenW and glyphScreenH are the whole-screen benchmark size. They are
// named rather than inlined because the ns/cell metric divides by their product.
const (
	glyphScreenW = 200
	glyphScreenH = 60
)

// wideGlyphBlock is the benchmark widget: it fills its whole rectangle with a
// repeated rune set through the public span writer, draws one static label, and
// changes a single cell every frame. That is the steady state of a dashboard with
// a live readout, which is what the diff's row skip exists for.
//
// Every span is built ONCE at construction. A fixture that built a []Span and a
// string per glyph inside Draw would put its own allocations on the frame path and
// then report them as the renderer's cost — which is the mistake ADR 0008 §4
// forbids a real widget, and which would have made every number here meaningless.
type wideGlyphBlock struct {
	bounds buffer.Rect
	// spans holds one span per two-cell glyph position across a row, so Draw
	// neither builds nor bounds anything.
	spans []buffer.Span
	label []buffer.Span
	// anim is the style of the one cell that changes each frame.
	anim    buffer.Style
	counter int
}

func (w *wideGlyphBlock) Bounds() buffer.Rect          { return w.bounds }
func (w *wideGlyphBlock) Invalidate()                  {}
func (w *wideGlyphBlock) Handle(termmosaic.Event) bool { return false }

// Draw fills the rectangle, then writes the label and the one animated cell.
//
// Each glyph goes through its own SetSpans call so the continuation cell is
// produced by the writer rather than by a hand-placed pair: a benchmark scene that
// placed the pair itself would not be measuring the code a widget runs.
func (w *wideGlyphBlock) Draw(buf *buffer.Buffer) {
	st := buffer.NewStyle(white, buffer.DefaultColour, 0)
	buf.FillRect(w.bounds, st.Blank())
	w.counter++
	for y := w.bounds.Y; y < w.bounds.Bottom(); y++ {
		for i := range w.spans {
			buf.SetSpans(w.bounds.X+i*2, y, w.spans[i:i+1])
		}
	}
	if len(w.label) > 0 && w.bounds.H > 0 {
		buf.SetSpans(w.bounds.X, w.bounds.Y, w.label)
	}
	if w.bounds.W > 0 && w.bounds.H > 1 {
		buf.Set(w.bounds.X, w.bounds.Y+1, rune('0'+w.counter%10), w.anim)
	}
}

// newGlyphBlock returns the whole-screen widget for one of the two rune sets.
//
// Both cover the same cells: wideRunes has six double-width runes and narrowRunes
// twelve single-width ones, and the span list is sized in CELLS, so the two
// variants write an identical 200-cell row with half and twice the runes.
func newGlyphBlock(wide bool) *wideGlyphBlock {
	runes, label := narrowRunes, "dashboard       "
	if wide {
		runes, label = wideRunes, "ダッシュボード"
	}
	st := buffer.NewStyle(white, buffer.DefaultColour, 0)
	spans := make([]buffer.Span, 0, glyphScreenW/2)
	for i := 0; i+1 < glyphScreenW; i += 2 {
		spans = append(spans, buffer.NewSpan(string(runes[(i/2)%len(runes)]), st))
	}
	return &wideGlyphBlock{
		bounds: buffer.Rect{W: glyphScreenW, H: glyphScreenH},
		spans:  spans,
		label:  []buffer.Span{buffer.NewSpan(label, st)},
		anim:   buffer.NewStyle(green, buffer.DefaultColour, 0),
	}
}

// glyphRenderer returns a renderer drawing w over the whole screen, with its first frame
// already painted so every benchmark measures the steady state.
func glyphRenderer(tb testing.TB, w *wideGlyphBlock) *Renderer {
	tb.Helper()
	sink := headless.NewMemorySink(glyphScreenW, glyphScreenH)
	// Bound the byte history: without this the sink accumulates every frame ever
	// written and its buffer growth dominates the measurement.
	sink.KeepBytes(1 << 20)
	r := New(sink, Config{Width: glyphScreenW, Height: glyphScreenH, Caps: termmosaic.DefaultCaps()})
	r.SetRoot(w)
	if _, err := r.Render(); err != nil {
		tb.Fatal(err)
	}
	return r
}

// reportPerCell reports the per-cell cost, which is the figure that survives the
// differing rune counts.
func reportPerCell(b *testing.B) {
	b.Helper()
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*glyphScreenW*glyphScreenH), "ns/cell")
}

// BenchmarkRenderWideGlyphSteadyState is the steady state of a screen whose
// content is double-width: static chrome plus one changing cell.
//
// Its narrow twin is BenchmarkRenderSteadyState; read the ns/cell columns, not the
// ns/op ones.
func BenchmarkRenderWideGlyphSteadyState(b *testing.B) {
	r := glyphRenderer(b, newGlyphBlock(true))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render(); err != nil {
			b.Fatal(err)
		}
		r.Invalidate(buffer.Rect{X: 100, Y: 30, W: 1, H: 1})
	}
	reportPerCell(b)
}

// BenchmarkRenderASCIISteadyState is the narrow twin of the benchmark above, built
// here so the pair sits in one file and cannot drift apart.
func BenchmarkRenderASCIISteadyState(b *testing.B) {
	r := glyphRenderer(b, newGlyphBlock(false))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render(); err != nil {
			b.Fatal(err)
		}
		r.Invalidate(buffer.Rect{X: 100, Y: 30, W: 1, H: 1})
	}
	reportPerCell(b)
}

// BenchmarkRenderWideGlyphForceRepaint is the worst case for wide content: no row
// can be skipped and all 12,000 cells are encoded. This is what a resize costs on a
// screen full of CJK, and it is the number ADR 0007 §6's drag-resize table does
// not have.
//
// The allocs/op column here is NOT the renderer's, and reading it as such would be
// wrong. The diff, the encoder and this widget are all 0 allocs/op — that is
// asserted directly in internal/diff's TestWideGlyphDiffIsZeroAllocation and is
// visible as the 0 allocs/op on every BenchmarkDiff* line. What allocates is
// headless.MemorySink, the test-only screen model, which re-parses the emitted byte
// stream into cells and whose SGR parameter parser allocates a slice per styling
// change. A profile attributes it to headless.parseParams and nothing else. The
// wide figure is higher than the ASCII one only because the wide frame emits more
// bytes for the same screen, and therefore more SGRs to parse.
func BenchmarkRenderWideGlyphForceRepaint(b *testing.B) {
	r := glyphRenderer(b, newGlyphBlock(true))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Reset(); err != nil {
			b.Fatal(err)
		}
		if _, err := r.Render(); err != nil {
			b.Fatal(err)
		}
	}
	reportPerCell(b)
}

// BenchmarkRenderASCIIForceRepaint is the narrow twin of the benchmark above.
func BenchmarkRenderASCIIForceRepaint(b *testing.B) {
	r := glyphRenderer(b, newGlyphBlock(false))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Reset(); err != nil {
			b.Fatal(err)
		}
		if _, err := r.Render(); err != nil {
			b.Fatal(err)
		}
	}
	reportPerCell(b)
}
