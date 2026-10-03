package diff

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/internal/ansi"
)

// sceneW and sceneH are ADR 0002's synthetic scene: 200x60 = 12,000 cells.
const (
	sceneW = 200
	sceneH = 60
)

// ADR 0002's headline targets for that scene, reproduced as assertions so a
// regression cannot be merged silently:
//
//	42 changed cells of 12,000 (0.35%), 4 dirty rows of 60 (6.7%)
//	two-tier diff: 107 bytes; full repaint: 23,240 bytes
//
// The byte counts depend on the exact scene, so TestSceneShape asserts the
// shape (cell and row counts) and TestDiffIsMuchSmallerThanFullRepaint
// asserts the ratio, which is the property that actually matters.
const (
	targetChangedCells = 42
	targetDirtyRows    = 4
)

// Scene is the benchmark scene: 99% static chrome (panel borders and static
// filler) plus one progress-bar row and three numeric readouts that change per
// frame.
type Scene struct {
	Prev *buffer.Buffer
	Cur  *buffer.Buffer
	// Rects is the dirty region the renderer would report.
	Rects []buffer.Rect
}

// staticScene builds frame t of the scene. Frame t-1 is produced by the same
// builder, so the delta between them is exactly what the renderer sees between
// two consecutive frames -- there is no hand-written "previous" state to get
// wrong.
func staticScene(t int) (prev, cur *buffer.Buffer, rects []buffer.Rect) {
	prev = buildFrame(t - 1)
	cur = buildFrame(t)

	// Dirty rectangles: the bar row and the three readout rows.
	rects = make([]buffer.Rect, 0, 4)
	rects = append(rects, buffer.Rect{X: barX0, Y: barY, W: barW, H: 1})
	for i := 0; i < 3; i++ {
		rects = append(rects, buffer.Rect{X: readoutX, Y: readoutY0 + i*2, W: readoutW, H: 1})
	}
	return prev, cur, rects
}

// Geometry of the scene's dynamic region.
const (
	barX0 = 44
	barW  = 36
	barY  = 20

	readoutX  = 44
	readoutY0 = 22
	readoutW  = 2
)

// buildFrame renders frame t: static chrome plus a progress bar and three
// numeric readouts whose contents depend on t. Everything else is identical in
// every frame, which is the 99%-static property the benchmark depends on.
func buildFrame(t int) *buffer.Buffer {
	w, h := sceneW, sceneH
	fg := buffer.NewColour(0xd0, 0xd0, 0xd0)
	panel := buffer.NewColour(0x30, 0x36, 0x40)
	fill := buffer.NewColour(0x18, 0x1c, 0x24)

	// A Buffer rather than a raw []buffer.Cell: Frame carries buffers, so the
	// scene must be built as the same shape the renderer hands the diff
	// (ADR 0006). Row writes go through Row, which is stride-correct, so the
	// scene builder is exercising the public bulk accessor too.
	buf := buffer.NewBuffer(w, h)
	set := func(x, y int, c buffer.Cell) { buf.SetCell(x, y, c) }
	rowFill := buffer.NewCell(' ', fill, fill, 0)
	for y := 0; y < h; y++ {
		row := buf.Row(y)
		for x := range row {
			row[x] = rowFill
		}
	}

	// --- static chrome: three panel borders ------------------------------
	for _, p := range []struct{ x0, y0, x1, y1 int }{
		{0, 0, w - 1, 8},
		{0, 10, 39, h - 2},
		{41, 10, w - 1, h - 2},
	} {
		for x := p.x0; x <= p.x1; x++ {
			set(x, p.y0, buffer.NewCell('─', panel, fill, 0))
			set(x, p.y1, buffer.NewCell('─', panel, fill, 0))
		}
		for y := p.y0; y <= p.y1; y++ {
			set(p.x0, y, buffer.NewCell('│', panel, fill, 0))
			set(p.x1, y, buffer.NewCell('│', panel, fill, 0))
		}
		set(p.x0, p.y0, buffer.NewCell('┌', panel, fill, 0))
		set(p.x1, p.y0, buffer.NewCell('┐', panel, fill, 0))
		set(p.x0, p.y1, buffer.NewCell('└', panel, fill, 0))
		set(p.x1, p.y1, buffer.NewCell('┘', panel, fill, 0))
	}

	// --- static sidebar text ----------------------------------------------
	for i := 0; i < 29; i++ {
		y := 11 + i
		if y >= h-2 {
			break
		}
		label := fmt.Sprintf("  - item %02d static", i)
		for x := 0; x < len(label) && 1+x < 39; x++ {
			set(1+x, y, buffer.NewCell(rune(label[x]), fg, fill, 0))
		}
	}

	// --- dynamic region ----------------------------------------------------
	// The bar advances AND its unfilled cells animate, so every one of the 36
	// bar cells changes each frame. That makes the bar row the one expensive
	// row, which is what a real dashboard frame looks like.
	filled := 18 + (t*3)%19
	// The bar advances and every cell's glyph animates, so all 36 bar cells
	// differ between consecutive frames -- while keeping only two styles in the
	// row. That is the point: a changed cell costs one byte, not one SGR
	// sequence, unless its style genuinely changed.
	//
	// The glyphs are single-byte ASCII deliberately. ADR 0002's 107-byte
	// figure is only comparable to a scene whose dynamic glyphs are ASCII: a
	// block-drawing glyph such as '█' is 3 bytes in UTF-8, which more than
	// triples the cost of a changed cell. See
	// BenchmarkDiffDefaultSceneUnicode for the same scene with realistic
	// block glyphs, and the note above the benchmark for the comparison.
	accent := buffer.NewColour(0x30, 0xc0, 0x80)
	for i := 0; i < barW; i++ {
		var c buffer.Cell
		switch {
		case i < filled:
			if (i+t)%2 == 0 {
				c = buffer.NewCell('#', accent, fill, 0)
			} else {
				c = buffer.NewCell('=', accent, fill, 0)
			}
		case (i+t)%2 == 0:
			c = buffer.NewCell('.', fill, fill, 0)
		default:
			c = buffer.NewCell(',', fill, fill, 0)
		}
		set(barX0+i, barY, c)
	}

	// Three numeric readouts, two cells each, both digits changing every
	// frame. The arithmetic is chosen so no digit ever coincides between
	// consecutive frames, which keeps the scene's shape stable at exactly 42
	// changed cells rather than drifting with t.
	for i := 0; i < 3; i++ {
		y := readoutY0 + i*2
		d1 := rune('0' + (t+i)%10)
		d2 := rune('0' + (t*3+i)%10)
		set(readoutX+0, y, buffer.NewCell(d1, fg, fill, 0))
		set(readoutX+1, y, buffer.NewCell(d2, fg, fill, 0))
	}
	return buf
}

func changedCells(prev, cur *buffer.Buffer) (cells, rows int) {
	rowsTouched := make(map[int]bool)
	for y := 0; y < sceneH; y++ {
		curRow, prevRow := cur.Row(y), prev.Row(y)
		for x := 0; x < sceneW; x++ {
			if curRow[x] != prevRow[x] {
				cells++
				rowsTouched[y] = true
			}
		}
	}
	return cells, len(rowsTouched)
}

// TestSceneShape asserts the benchmark scene matches ADR 0002's description, so
// the benchmark is comparable to the number in the ADR.
func TestSceneShape(t *testing.T) {
	prev, cur, rects := staticScene(1)
	cells, rows := changedCells(prev, cur)
	if cells != targetChangedCells {
		t.Errorf("changed cells = %d, want %d (ADR 0002 scene)", cells, targetChangedCells)
	}
	if rows != targetDirtyRows {
		t.Errorf("dirty rows = %d, want %d (ADR 0002 scene)", rows, targetDirtyRows)
	}
	if len(rects) != targetDirtyRows {
		t.Errorf("rect count = %d, want %d", len(rects), targetDirtyRows)
	}
}

// TestDiffIsMuchSmallerThanFullRepaint is the byte-count assertion ADR 0002
// makes: 107 bytes for the diff against 23,240 for a full repaint.
func TestDiffIsMuchSmallerThanFullRepaint(t *testing.T) {
	prev, cur, rects := staticScene(1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}

	diffed := NewDiffer(64 * 1024)
	// Seed the previous frame so the diff does not pay first-frame SGR costs.
	diffed.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH,
		ForceFull: true, Encoder: enc})
	n := len(diffed.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH,
		Rects: rects, PrevCursor: Cursor{Valid: true}, Encoder: enc}))

	full := NewDiffer(64 * 1024)
	m := len(full.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH,
		ForceFull: true, Encoder: enc}))

	t.Logf("diff = %d bytes, full repaint = %d bytes (%.1fx reduction)", n, m, float64(m)/float64(n))
	const adrDiff, adrFull = 107, 23240
	if n > adrDiff*2 {
		t.Errorf("diff wrote %d bytes, ADR 0002 measured %d; more than 2x is a regression", n, adrDiff)
	}
	if m < adrFull/2 {
		t.Errorf("full repaint wrote %d bytes, ADR 0002 measured %d; suspiciously low", m, adrFull)
	}
	if n*100 >= m {
		t.Errorf("diff is not meaningfully smaller: %d vs %d", n, m)
	}
}

// TestDiffNeverCostsMoreThanFullRepaint is ADR 0002's all-dynamic adversarial
// property: the two-tier diff never writes more than a full repaint, because
// tier 1 can only ever save bytes.
func TestDiffNeverCostsMoreThanFullRepaint(t *testing.T) {
	prev := buffer.NewBuffer(sceneW, sceneH)
	cur := buffer.NewBuffer(sceneW, sceneH)
	for i := 0; i < sceneW*sceneH; i++ {
		x, y := i%sceneW, i/sceneW
		prev.SetCell(x, y, buffer.NewCell(' ', buffer.DefaultColour, buffer.DefaultColour, 0))
		cur.SetCell(x, y, buffer.NewCell('x', buffer.NewColour(uint8(i%256), 0, 0), buffer.DefaultColour, buffer.AttrBold))
	}
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	rects := []buffer.Rect{{W: sceneW, H: sceneH}}

	d1 := NewDiffer(64 * 1024)
	d1.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
	n := len(d1.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc}))
	m := len(d1.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc}))
	t.Logf("all-dynamic: diff = %d bytes, full repaint = %d bytes (%.2fx)", n, m, float64(m)/float64(n))
	if n > m {
		t.Errorf("diff wrote %d bytes against a full repaint's %d", n, m)
	}
}

// BenchmarkDiffDefaultSceneUnicode measures the same scene with realistic block
// glyphs instead of ASCII. The result is reported alongside the ADR's 107 bytes
// because the byte count depends on the scene's glyphs more than on the
// algorithm: '#' is one byte, '█' is three. Anything reading the ADR's 107 as
// a property of the diff should read this too.
func BenchmarkDiffDefaultSceneUnicode(b *testing.B) {
	prev, cur, rects := staticScene(1)
	prev, cur = blockGlyphs(prev), blockGlyphs(cur)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(64 * 1024)
	d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc})
	}
}

// blockGlyphs rewrites the scene's dynamic-region ASCII glyphs as the block
// glyphs a real progress bar would use, so the two benchmarks differ only in
// encoding width.
func blockGlyphs(src *buffer.Buffer) *buffer.Buffer {
	out := buffer.NewBuffer(sceneW, sceneH)
	for y := 0; y < sceneH; y++ {
		srcRow, dstRow := src.Row(y), out.Row(y)
		copy(dstRow, srcRow)
		for i := range dstRow {
			switch dstRow[i].Ch {
			case '#':
				dstRow[i].Ch = '█'
			case '=':
				dstRow[i].Ch = '▊'
			case '.':
				dstRow[i].Ch = '░'
			case ',':
				dstRow[i].Ch = '·'
			}
		}
	}
	return out
}

// TestUnicodeGlyphSceneCostsMoreBytes pins the glyph-width observation above so
// it is a recorded fact rather than a comment that drifts.
func TestUnicodeGlyphSceneCostsMoreBytes(t *testing.T) {
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	measure := func(prev, cur *buffer.Buffer, rects []buffer.Rect) int {
		d := NewDiffer(64 * 1024)
		d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
		return len(d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc}))
	}
	p, c, r := staticScene(1)
	asciiBytes := measure(p, c, r)
	uniBytes := measure(blockGlyphs(p), blockGlyphs(c), r)
	t.Logf("ASCII dynamic glyphs: %d bytes; block glyphs: %d bytes", asciiBytes, uniBytes)
	if uniBytes <= asciiBytes {
		t.Errorf("block glyphs should cost more bytes: %d vs %d", uniBytes, asciiBytes)
	}
}

// BenchmarkDiffDefaultSceneAllRows is the apples-to-apples comparison with ADR
// 0002's 7,133 ns/op: it compares every one of the 60 rows rather than only
// the 4 the dirty rectangles name.
//
// BenchmarkDiffDefaultScene is the number a real frame costs, because the
// renderer only ever hands the diff the dirty regions. This one is what ADR
// 0002 measured, and the two should not be quoted interchangeably.
func BenchmarkDiffDefaultSceneAllRows(b *testing.B) {
	prev, cur, _ := staticScene(1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	rects := []buffer.Rect{{W: sceneW, H: sceneH}}
	d := NewDiffer(64 * 1024)
	d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc})
	}
}

func BenchmarkDiffDefaultScene(b *testing.B) {
	prev, cur, rects := staticScene(1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(64 * 1024)
	// Warm: one full repaint to prime the scratch and the tracked style.
	d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc})
	}
}

// BenchmarkRowSkipAllRows isolates tier 1: every row compared, none skipped.
func BenchmarkRowSkipAllRows(b *testing.B) {
	prev := buffer.NewBuffer(sceneW, sceneH)
	cur := buffer.NewBuffer(sceneW, sceneH)
	for y := 0; y < sceneH; y++ {
		for x := 0; x < sceneW; x++ {
			prev.SetCell(x, y, buffer.NewCell(' ', buffer.DefaultColour, buffer.DefaultColour, 0))
			cur.SetCell(x, y, buffer.NewCell('x', buffer.DefaultColour, buffer.DefaultColour, 0))
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for y := 0; y < sceneH; y++ {
			if bytes.Equal(
				cur.RowBytes(y, 0, sceneW),
				prev.RowBytes(y, 0, sceneW),
			) {
				continue
			}
		}
	}
}

// BenchmarkRowSkipAllIdentical isolates tier 1 when the skip fires: the
// static-chrome case that dominates a real frame.
func BenchmarkRowSkipAllIdentical(b *testing.B) {
	prev := buffer.NewBuffer(sceneW, sceneH)
	for y := 0; y < sceneH; y++ {
		for x := 0; x < sceneW; x++ {
			prev.SetCell(x, y, buffer.NewCell(' ', buffer.DefaultColour, buffer.DefaultColour, 0))
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for y := 0; y < sceneH; y++ {
			if bytes.Equal(
				prev.RowBytes(y, 0, sceneW),
				prev.RowBytes(y, 0, sceneW),
			) {
				continue
			}
		}
	}
}

// BenchmarkFullRepaint is the baseline ADR 0002 compares against.
func BenchmarkFullRepaint(b *testing.B) {
	prev, cur, _ := staticScene(1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(64 * 1024)
	d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
	}
}

// BenchmarkDiffAllDynamic is ADR 0002's adversarial scene: every cell changes.
func BenchmarkDiffAllDynamic(b *testing.B) {
	prev := buffer.NewBuffer(sceneW, sceneH)
	cur := buffer.NewBuffer(sceneW, sceneH)
	for i := 0; i < sceneW*sceneH; i++ {
		x, y := i%sceneW, i/sceneW
		prev.SetCell(x, y, buffer.NewCell(' ', buffer.DefaultColour, buffer.DefaultColour, 0))
		cur.SetCell(x, y, buffer.NewCell('x', buffer.NewColour(uint8(i%256), 0, 0), buffer.DefaultColour, buffer.AttrBold))
	}
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	rects := []buffer.Rect{{W: sceneW, H: sceneH}}
	d := NewDiffer(256 * 1024)
	d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc})
	}
}

// BenchmarkEncodeSGROnly isolates the SGR encoder from the diff.
func BenchmarkEncodeSGROnly(b *testing.B) {
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	s := ansi.Style{
		FG:   buffer.NewColour(0x30, 0xc0, 0x80),
		BG:   buffer.NewColour(0x18, 0x1c, 0x24),
		Attr: buffer.AttrBold,
	}
	dst := make([]byte, 0, 64)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dst = enc.AppendSGR(dst[:0], s)
	}
	_ = dst
}

// TestDiffIsZeroAllocation is the explicit proof ADR 0002's "0 allocs/op"
// requires, as an assertion rather than a benchmark line that has to be read
// carefully. testing.AllocsPerRun returns a float, so this fails loudly rather
// than depending on someone noticing a column in benchmark output.
//
// A single allocation here would mean the per-frame path puts something on the
// heap every frame, which at 60 Hz is the difference between a flat and a
// sawtooth-shaped GC profile.
func TestDiffIsZeroAllocation(t *testing.T) {
	prev, cur, rects := staticScene(1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(64 * 1024)
	frame := Frame{
		Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc,
	}
	// Warm the scratch buffer and the tracked style, as the renderer does on its
	// first frame. AllocsPerRun performs its own warm-up run, but the scratch has
	// to survive between calls for this to measure the steady state.
	d.Diff(frame)

	if got := testing.AllocsPerRun(200, func() { d.Diff(frame) }); got != 0 {
		t.Errorf("the diff allocated %.1f objects per run, want 0", got)
	}

	// The same must hold for a forced full repaint, which is the largest output
	// the steady state can produce.
	full := Frame{
		Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc,
	}
	d.Diff(full)
	if got := testing.AllocsPerRun(20, func() { d.Diff(full) }); got != 0 {
		t.Errorf("a full repaint allocated %.1f objects per run, want 0", got)
	}
}
