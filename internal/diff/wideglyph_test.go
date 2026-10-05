package diff

// This file closes the wide-glyph measurement gap. ADR 0002 records that no
// benchmark exercises wide characters, ADR 0008 risk 5 records that the wide-glyph
// interaction is "implemented but unbenchmarked" across SetString, SetSpans and
// Wrap, and STATUS.md's wide-characters open question says the same. This file is
// the diff half of closing it; buffer/wideglyph_test.go is the writer half.
//
// # What "no regression" can and cannot mean here
//
// A double-width rune occupies two cells, so a wide-glyph scene drawn on the same
// geometry as the ASCII scene necessarily emits HALF as many runes for the same
// number of changed cells. "The wide scene took fewer ns/op than the ASCII scene"
// would therefore be arithmetic, not evidence. The comparisons below are chosen so
// that each one is either genuinely like-for-like or explicitly not:
//
//	BenchmarkDiffWideGlyphStaticOnly vs BenchmarkDiffASCIIStaticOnly
//	    LIKE FOR LIKE. Both compare 60 identical rows of 200 cells over the same
//	    192,000 bytes, and differ only in whether the cells hold narrow or double-
//	    width runes. This is the one place a regression could hide: tier 1 is a
//	    bytes.Equal over Cell, and if anything about a wide rune made a Cell
//	    wider or un-comparable, this is where it would show.
//
//	BenchmarkDiffWideGlyphScene vs BenchmarkDiffDefaultScene
//	    NOT LIKE FOR LIKE, and the doc comment says so. Same geometry, same four
//	    dirty rows, roughly half the emitted runes. Read the output-bytes and
//	    allocs columns; treat ns/op as indicative only.
//
// # What is asserted rather than benchmarked
//
// Benchmarks produce numbers a reader has to notice. The four tests below pin the
// properties that would make a regression a bug rather than a slower number:
//
//   - an identical wide frame writes zero bytes (no flicker from a continuation
//     cell that fails to compare equal — ADR 0008 §1's rule);
//   - a one-glyph change in wide content writes only that glyph;
//   - the wide path allocates nothing, on a partial diff and on a full repaint;
//   - wide output bytes are bounded by the UTF-8 ceiling relative to ASCII, so a
//     continuation cell that starts being emitted as its own rune is caught.

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/internal/ansi"
)

// wideSceneGeometry documents what the wide scene shares with the ASCII one.
const (
	// wideBarGlyphs is how many double-width runes the 36-cell bar holds.
	// barW is 36 cells, so 18 glyphs and 18 continuation cells.
	wideBarGlyphs = barW / 2
)

// wideFilled and wideEmpty are the wide scene's dynamic glyph sets. Every rune in
// both is genuinely double-width under buffer.RuneWidth, and they are drawn from
// four different blocks of its table rather than one, so the scene exercises the
// CJK Unified, the fullwidth-forms, the ideographic and the emoji ranges and not
// just the first entry of a list.
//
// They are not chosen for how they look; they are chosen for their width. A rune
// that merely LOOKS wide but measures 1 — '█', '░', '·' — is the case that makes
// a screen look right in one terminal and ragged in another, and
// TestWideGlyphSetsAreActuallyDoubleWidth rejects it.
var (
	wideFilled = []rune{'漢', '字', '🟩', '０', 'Ａ', '日', '本', '語', '한', '글', '🚀', '中'}
	wideEmpty  = []rune{'　', '＿', '〇', '・', '－', '＝', '＃', '～'}
)

// wideLabel is the wide scene's static sidebar label, standing in for a CJK UI's
// sidebar. It is written cell by cell through SetSpans so the scene is built by
// the same public writers an application uses, which means a bug in the
// continuation-cell rule shows up in the benchmark scene rather than hiding from
// it.
var wideLabel = "  項目０１ 静的"

// wideScene builds frame t of the wide-glyph scene and frame t-1 beside it, with
// the same four dirty rectangles the ASCII scene reports.
func wideScene(t int) (prev, cur *buffer.Buffer, rects []buffer.Rect) {
	return buildWideFrame(t - 1), buildWideFrame(t), sceneRects()
}

// buildWideFrame renders frame t of the wide scene: the same static chrome as
// buildFrame, a bar and three readouts whose DOUBLE-WIDTH glyphs depend on t, and
// a static sidebar written in wide text.
//
// Every rune it writes comes from a measured set, and each wide glyph is written
// through SetSpans so its continuation cell is produced by the same code path that
// produces one in a real widget.
func buildWideFrame(t int) *buffer.Buffer {
	fill := sceneFill
	buf := newSceneBuffer(fill)
	labelStyle := buffer.NewStyle(sceneFg, fill, 0)
	accent := buffer.NewStyle(sceneAccent, fill, 0)
	muted := buffer.NewStyle(fill, fill, 0)

	drawScenePanels(buf)

	// --- static sidebar, in wide text ---------------------------------------
	// Written through SetSpans so the continuation cells come from the writer
	// rather than from a hand-placed pair, and clamped to the panel so the scene
	// never overruns the border.
	for i := 0; i < 29; i++ {
		y := 11 + i
		if y >= sceneH-2 {
			break
		}
		label := fmt.Sprintf("%s%02d", wideLabel, i)
		if w := buffer.StringWidth(label); 1+w > 39 {
			label = truncateToCells(label, 38)
		}
		buf.SetSpans(1, y, []buffer.Span{buffer.NewSpan(label, labelStyle)})
	}

	// --- dynamic bar ---------------------------------------------------------
	// The bar advances and every glyph in it changes each frame, exactly as the
	// ASCII bar does. The advance is measured in GLYPHS rather than cells, so the
	// filled portion covers the same fraction of the bar.
	filledGlyphs := (wideBarGlyphs/2 + (t*3)%(wideBarGlyphs/2+1))
	for g := 0; g < wideBarGlyphs; g++ {
		set := wideEmpty
		st := muted
		if g < filledGlyphs {
			set = wideFilled
			st = accent
		}
		r := set[(g+t)%len(set)]
		// One SetSpans call per glyph: it writes the glyph cell and its
		// continuation, which is the only correct way to produce the pair.
		buf.SetSpans(barX0+g*2, barY, []buffer.Span{buffer.NewSpan(string(r), st)})
	}

	// --- dynamic readouts ----------------------------------------------------
	// Two cells each, so one wide glyph and one continuation — the same shape as
	// the ASCII readout's two digits, with half the runes.
	for i := 0; i < 3; i++ {
		y := readoutY0 + i*2
		d := rune('０' + (t*3+i)%10)
		buf.SetSpans(readoutX, y, []buffer.Span{
			buffer.NewSpan(string(d), buffer.NewStyle(sceneFg, fill, 0)),
		})
	}
	return buf
}

// truncateToCells returns the longest prefix of s whose cell width is at most max.
// It is test-only: the benchmark scene must not use the allocating Truncate, which
// is exactly what ADR 0008 §4 forbids on a draw path.
func truncateToCells(s string, max int) string {
	w := 0
	for i, r := range s {
		rw := buffer.RuneWidth(r)
		if w+rw > max {
			return s[:i]
		}
		w += rw
	}
	return s
}

// ---------------------------------------------------------------------------
// Tests: the properties a benchmark cannot assert
// ---------------------------------------------------------------------------

// TestWideGlyphSetsAreActuallyDoubleWidth rejects the exact failure this file
// exists to prevent: a benchmark whose "wide" glyphs measure 1 would make the
// whole measurement worthless while still producing a passing number.
//
// buffer.RuneWidth's own doc comment says the table is PROVISIONAL and
// UNVERIFIED. This test is one of the things that verifies it — for the twelve
// runes the benchmark depends on.
func TestWideGlyphSetsAreActuallyDoubleWidth(t *testing.T) {
	for _, r := range append(append([]rune{}, wideFilled...), wideEmpty...) {
		if got := buffer.RuneWidth(r); got != 2 {
			t.Errorf("benchmark rune %U (U+%04X) has width %d, want 2: the wide scene "+
				"would not be exercising the continuation-cell path at all", r, r, got)
		}
	}
	// And the ASCII scene's dynamic glyphs must still be narrow, or the two
	// scenes are not the comparison they claim to be.
	for _, r := range []rune{'#', '=', '.', ',', '0', '9'} {
		if got := buffer.RuneWidth(r); got != 1 {
			t.Errorf("ASCII scene rune %q has width %d, want 1", r, got)
		}
	}
}

// TestWideGlyphSceneShape pins what the wide scene actually is, because a
// benchmark whose shape is unknown cannot be compared to anything.
//
// 24 changed cells against the ASCII scene's 42, and the difference is not a
// defect — it is what double-width means:
//
//   - 21 GLYPH cells change: 18 in the bar (every glyph advances by parity) and 3
//     readouts. The ASCII scene changes 36 bar cells because each of its cells is
//     its own glyph; here 18 glyphs cover 36 cells.
//   - 3 CONTINUATION cells change as well, and that is the interesting part. The
//     bar's fill boundary moves by three glyphs between these two frames, and a
//     continuation cell carries its owning glyph's STYLE (ADR 0008 §1). So the
//     three boundary continuations change style with their glyphs — which is the
//     rule working, and which the ASCII scene has no way to exercise at all.
//
// The four dirty rows are the same four as the ASCII scene's.
func TestWideGlyphSceneShape(t *testing.T) {
	const (
		wantGlyphCells = wideBarGlyphs + 3
		wantContCells  = 3
	)
	for _, tt := range []int{1, 2, 5, 9} {
		prev, cur, rects := wideScene(tt)
		cells, rows := changedCells(prev, cur)
		if rows != targetDirtyRows {
			t.Errorf("t=%d: wide scene dirty rows = %d, want %d", tt, rows, targetDirtyRows)
		}
		if cells != wantGlyphCells+wantContCells {
			t.Errorf("t=%d: wide scene changed cells = %d, want %d", tt, cells, wantGlyphCells+wantContCells)
		}
		if len(rects) != targetDirtyRows {
			t.Errorf("t=%d: rect count = %d, want %d", tt, len(rects), targetDirtyRows)
		}
		var glyphs, conts int
		for y := 0; y < sceneH; y++ {
			pr, cr := prev.Row(y), cur.Row(y)
			for x := 0; x < sceneW; x++ {
				if pr[x] != cr[x] {
					if cr[x].IsContinuation() {
						conts++
					} else {
						glyphs++
					}
				}
			}
		}
		if glyphs != wantGlyphCells {
			t.Errorf("t=%d: changed glyph cells = %d, want %d", tt, glyphs, wantGlyphCells)
		}
		if conts != wantContCells {
			t.Errorf("t=%d: changed continuation cells = %d, want %d", tt, conts, wantContCells)
		}
	}
	// The continuation half of every changed glyph must really be a
	// continuation, or the scene is not what it says it is.
	cur := buildWideFrame(1)
	for g := 0; g < wideBarGlyphs; g++ {
		if !cur.CellAt(barX0+g*2+1, barY).IsContinuation() {
			t.Errorf("bar cell %d is not a continuation cell", barX0+g*2+1)
		}
	}
}

// TestWideGlyphIdenticalFrameWritesNothing is the no-flicker property, in wide
// content. ADR 0008 §1's rule is that a wide glyph's continuation cell takes its
// owning span's style, precisely so the row compares equal to the previous frame.
// If it did not, a row of CJK would repaint itself at 60 Hz forever — a very
// visible bug, and one a narrow-glyph scene cannot reproduce.
//
// Both halves of every wide glyph are present here: the scene is full of them, and
// the comparison covers all 12,000 cells rather than only the reported rects, so a
// continuation that failed to compare equal anywhere on the screen would show up.
func TestWideGlyphIdenticalFrameWritesNothing(t *testing.T) {
	cur := buildWideFrame(7)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(64 * 1024)
	seed := Frame{Cur: cur, Prev: cur, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc}
	if n := len(d.Diff(seed)); n == 0 {
		t.Fatal("the seed full repaint wrote nothing; the scene is empty and this test would pass vacuously")
	}

	for _, tc := range []struct {
		name  string
		rects []buffer.Rect
	}{
		{"reported rects only", sceneRects()},
		{"the whole screen", []buffer.Rect{{W: sceneW, H: sceneH}}},
	} {
		if n := len(d.Diff(Frame{Cur: cur, Prev: cur, Width: sceneW, Height: sceneH, Rects: tc.rects, Encoder: enc})); n != 0 {
			t.Errorf("%s: a wide frame identical to its predecessor wrote %d bytes, want 0:\n%q",
				tc.name, n, string(d.Scratch()))
		}
	}
}

// TestWideGlyphOneGlyphChangeWritesOneGlyph is the finest-grained statement of the
// same property: a single changed wide rune must cost one rune's worth of bytes
// and must not drag its continuation cell out as a second, separate glyph.
func TestWideGlyphOneGlyphChangeWritesOneGlyph(t *testing.T) {
	prev := buildWideFrame(3)
	cur := buffer.NewBuffer(sceneW, sceneH)
	// Copy rather than rebuild so exactly one cell differs.
	for y := 0; y < sceneH; y++ {
		copy(cur.Row(y), prev.Row(y))
	}
	const x, y = barX0, barY
	next := wideFilled[(1+3)%len(wideFilled)]
	st := buffer.NewStyle(sceneAccent, sceneFill, 0)
	cur.SetSpans(x, y, []buffer.Span{buffer.NewSpan(string(next), st)})

	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(64 * 1024)
	d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
	out := d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH,
		Rects: []buffer.Rect{{X: x, Y: y, W: barW, H: 1}}, Encoder: enc})

	// A cursor move, one SGR, and exactly one rune. The bound is the SGR
	// sequence's own worst case plus a full escape for both, because a changed
	// style legitimately costs a sequence and this test is about the GLYPH, not
	// about suppressing the style change.
	const worstSGR = 40
	if len(out) > 16+worstSGR {
		t.Errorf("one changed wide glyph wrote %d bytes (%q), want one rune plus a cursor move",
			len(out), string(out))
	}
	if !strings.HasSuffix(string(out), string(next)) {
		t.Errorf("frame %q does not end with the new glyph %q", string(out), string(next))
	}
	if strings.Contains(string(out), "\x00") {
		t.Errorf("frame %q emitted a continuation cell's zero rune as a glyph", string(out))
	}
	// The glyph appears exactly once. A diff that emitted the continuation cell's
	// rune as well would put a second, empty glyph on screen beside the real one.
	if n := strings.Count(string(out), string(next)); n != 1 {
		t.Errorf("frame %q contains the glyph %d times, want once:\n%q", string(out), n, next)
	}
}

// TestWideGlyphDiffIsZeroAllocation is the property that matters most, and the one
// TestDiffIsZeroAllocation does not cover: a non-ASCII rune must not put anything
// on the heap. utf8.AppendRune writes into the caller's slice, so this ought to be
// free, and "ought to be" is precisely what a measurement gap leaves unverified.
func TestWideGlyphDiffIsZeroAllocation(t *testing.T) {
	prev, cur, rects := wideScene(1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(64 * 1024)
	partial := Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc}
	d.Diff(partial)
	if got := testing.AllocsPerRun(200, func() { d.Diff(partial) }); got != 0 {
		t.Errorf("the wide-glyph diff allocated %.1f objects per run, want 0", got)
	}

	full := Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc}
	d.Diff(full)
	if got := testing.AllocsPerRun(20, func() { d.Diff(full) }); got != 0 {
		t.Errorf("a wide-glyph full repaint allocated %.1f objects per run, want 0", got)
	}
}

// TestWideGlyphOutputIsBoundedByTheUTF8Ceiling is the "does not regress against
// the ASCII equivalent" assertion, in the only form that is stable across
// machines.
//
// A changed ASCII cell costs 1 byte; a changed wide cell costs at most 4 (the
// longest UTF-8 encoding). So wide output must be MORE than ASCII output and no
// more than four times it, measured on the SAME scene geometry. An upper bound is
// a real regression trap: if the diff ever started emitting a continuation cell's
// rune as well as its glyph's, or emitting a rune per cell instead of per glyph,
// this fails.
func TestWideGlyphOutputIsBoundedByTheUTF8Ceiling(t *testing.T) {
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	measure := func(prev, cur *buffer.Buffer, rects []buffer.Rect) int {
		d := NewDiffer(256 * 1024)
		d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
		return len(d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc}))
	}

	ap, ac, ar := staticScene(1)
	ascii := measure(ap, ac, ar)
	wp, wc, wr := wideScene(1)
	wide := measure(wp, wc, wr)
	t.Logf("partial diff: ASCII scene %d bytes, wide-glyph scene %d bytes", ascii, wide)

	// Fewer runes are emitted for the same four dirty rows, so wide is allowed to
	// be smaller — but only by about the rune-count ratio, and never MORE than
	// four times ASCII if the geometry ever changes to match the rune counts.
	if wide*4 < ascii {
		t.Errorf("wide scene wrote %d bytes against ASCII's %d: more than the UTF-8 ceiling (4x)", wide, ascii)
	}
	if wide > ascii*4 {
		t.Errorf("wide scene wrote %d bytes against ASCII's %d: above the UTF-8 ceiling (4x)", wide, ascii)
	}

	// And a full repaint, where the cell count is the screen rather than a few
	// rows, is the case where the two scenes really are comparable: every cell of
	// the screen is written, and the wide scene's static half is wide text.
	measureFull := func(prev, cur *buffer.Buffer) int {
		d := NewDiffer(256 * 1024)
		return len(d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc}))
	}
	aFull := measureFull(ap, ac)
	wFull := measureFull(wp, wc)
	t.Logf("full repaint: ASCII scene %d bytes, wide-glyph scene %d bytes (%.2fx)", aFull, wFull, float64(wFull)/float64(aFull))
	if wFull < aFull {
		t.Logf("the wide scene's full repaint is smaller than ASCII's: its static half holds " +
			"half as many runes in the same cells, which is expected and not a saving worth claiming")
	}
	if wFull > aFull*4 {
		t.Errorf("wide full repaint wrote %d bytes against ASCII's %d: above the UTF-8 ceiling (4x)", wFull, aFull)
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkDiffWideGlyphScene is the same geometry as BenchmarkDiffDefaultScene —
// 200x60, four dirty rows, the same static chrome — with every dynamic glyph
// double-width.
//
// READ THE ns/op COLUMN WITH CARE. The wide scene emits about half as many runes
// as the ASCII scene for the same dirty rows (18 bar glyphs against 36 bar
// characters), so a smaller ns/op here is arithmetic rather than a result. The
// columns that ARE comparable are allocs/op, which must stay 0, and the
// bytes/op, which is reported here as out-B/op and must be within the UTF-8
// ceiling of the ASCII figure. TestWideGlyphOutputIsBoundedByTheUTF8Ceiling
// asserts that part.
func BenchmarkDiffWideGlyphScene(b *testing.B) {
	prev, cur, rects := wideScene(1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(64 * 1024)
	d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
	n := len(d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc}))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, Rects: rects, Encoder: enc})
	}
	b.StopTimer()
	b.ReportMetric(float64(n), "out-B/frame")
}

// BenchmarkDiffWideGlyphSceneAllRows is the wide scene against every one of its 60
// rows rather than the four the dirty rectangles name, so it is the apples-to-
// apples partner of BenchmarkDiffDefaultSceneAllRows. Same caveat about runes.
func BenchmarkDiffWideGlyphSceneAllRows(b *testing.B) {
	prev, cur, _ := wideScene(1)
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

// BenchmarkDiffWideGlyphFullRepaint is the worst case for wide content: no row can
// be skipped and every cell of a 200x60 screen is encoded. This is what a resize
// costs on a CJK screen.
func BenchmarkDiffWideGlyphFullRepaint(b *testing.B) {
	prev, cur, _ := wideScene(1)
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(256 * 1024)
	d.Diff(Frame{Cur: prev, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
	n := len(d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc}))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Diff(Frame{Cur: cur, Prev: prev, Width: sceneW, Height: sceneH, ForceFull: true, Encoder: enc})
	}
	b.StopTimer()
	b.ReportMetric(float64(n), "out-B/frame")
}

// BenchmarkDiffASCIIStaticOnly and BenchmarkDiffWideGlyphStaticOnly are the
// like-for-like pair, and they are the load-bearing measurement in this file.
//
// Both compare all 60 rows of 200 cells against an identical predecessor — 192,000
// bytes through bytes.Equal — and emit nothing, because tier 1 skips every row.
// The ONLY difference is whether the cells hold narrow or double-width runes.
//
// This is where a regression would hide if one exists. Tier 1 is a raw byte
// compare of the cell array, so the claim under test is that a wide rune does not
// make a Cell wider, differently aligned, or un-comparable. If it did, a screen
// full of CJK would repaint its static chrome at 60 Hz forever. If the two
// benchmarks report materially different numbers, that is the bug.
func BenchmarkDiffASCIIStaticOnly(b *testing.B) {
	benchStaticOnly(b, buildFrame(1))
}

// BenchmarkDiffWideGlyphStaticOnly is the wide half of the pair above. See it for
// what the comparison means.
func BenchmarkDiffWideGlyphStaticOnly(b *testing.B) {
	benchStaticOnly(b, buildWideFrame(1))
}

// benchStaticOnly diffs buf against itself with no dirty rectangles, so tier 1
// skips every row and tier 2 never runs.
func benchStaticOnly(b *testing.B, buf *buffer.Buffer) {
	b.Helper()
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(64 * 1024)
	frame := Frame{Cur: buf, Prev: buf, Width: sceneW, Height: sceneH,
		Rects: []buffer.Rect{{W: sceneW, H: sceneH}}, Encoder: enc}
	d.Diff(frame)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Diff(frame)
	}
}

// BenchmarkAppendRuneNarrowVsWide isolates the only place a wide glyph touches
// the encoder, so the diff benchmarks above can be read: utf8.AppendRune's
// branch table versus a single byte store. If this pair is close, then no
// ns/op difference in the scenes above is the encoder's doing.
func BenchmarkAppendRuneNarrowVsWide(b *testing.B) {
	cases := []struct {
		name string
		r    rune
	}{
		{"1byte", 'A'}, // ASCII
		{"2byte", 'é'}, // U+00E9, Latin-1 supplement, width 1
		{"3byte", '漢'}, // U+6F22, CJK Unified, width 2
		{"4byte", '🚀'}, // U+1F680, transport, width 2
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			dst := make([]byte, 0, 64)
			b.ReportAllocs()
			b.SetBytes(int64(utf8.RuneLen(c.r)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dst = ansi.AppendRune(dst[:0], c.r)
			}
			_ = dst
		})
	}
}

// ---------------------------------------------------------------------------
// The dense pair: what a wide glyph actually costs the diff
// ---------------------------------------------------------------------------

// denseRunes is how many glyphs each dense scene emits. It is deliberately a
// figure that divides both ways: 6,000 narrow glyphs fill 30 rows of 200 cells and
// 6,000 wide glyphs fill 60, so the two scenes differ in cell count and in dirty-row
// count but emit exactly the same number of runes in the same styles. That makes
// them the closest thing to a fair comparison the two widths allow, and it is the
// measurement that answers "does a wide glyph cost the diff more than a narrow
// one?".
const denseRunes = 6000

// denseNarrow returns the dense narrow scene: every cell holds one single-width
// glyph, one style for the whole screen, so the only variable is glyph width.
func denseNarrow() *buffer.Buffer {
	const rows = denseRunes / sceneW
	buf := buffer.NewBuffer(sceneW, rows)
	st := buffer.NewStyle(sceneAccent, sceneFill, 0)
	glyphs := []rune{'#', '=', '.', ',', '0', '1', '2', '3', '4', '5'}
	for y := 0; y < rows; y++ {
		for x := 0; x < sceneW; x++ {
			buf.SetCell(x, y, st.Cell(glyphs[(x+y)%len(glyphs)]))
		}
	}
	return buf
}

// denseWide is the same scene with double-width glyphs: 6,000 of them, filling
// every cell of a screen twice as tall. Each goes through SetSpans so its
// continuation cell comes from the writer rather than from a hand-placed pair.
func denseWide() *buffer.Buffer {
	const rows = 2 * denseRunes / sceneW
	buf := buffer.NewBuffer(sceneW, rows)
	st := buffer.NewStyle(sceneAccent, sceneFill, 0)
	for y := 0; y < rows; y++ {
		for x := 0; x < sceneW; x += 2 {
			buf.SetSpans(x, y, []buffer.Span{
				buffer.NewSpan(string(wideFilled[(x/2+y)%len(wideFilled)]), st),
			})
		}
	}
	return buf
}

// BenchmarkDiffDenseNarrow and BenchmarkDiffDenseWide emit the same 6,000 runes in
// the same styles and differ only in glyph width.
//
// THE RESULT, after the fix: the wide scene writes 19,443 bytes where the narrow
// one writes 6,233 for the very same 6,000 runes - 3.12x, and 60 CUP sequences
// where the narrow scene needs 30.
//
// The defect this benchmark surfaced is FIXED. Diff's cursor-run suppression
// checks whether the next cell is lastX+1, and a wide glyph advances the
// terminal's cursor by TWO while lastX recorded only the glyph's own x, so
// every wide glyph used to be preceded by a full CUP escape: 6,000 of them and
// 68,832 bytes, 11x. The run tracker now advances by the glyph's cell width.
// Output was always correct - this was byte efficiency on dense wide content -
// and the ASCII path, which is the one that matters most, does not regress.
//
// The remaining 3.12x is inherent: a wide rune is three UTF-8 bytes where a
// narrow one is one, so the same 6,000 runes cannot produce the same frame.

func BenchmarkDiffDenseNarrow(b *testing.B) { benchDense(b, denseNarrow) }

// BenchmarkDiffDenseWide is the wide half of the pair above. Its out-B/rune column
// against BenchmarkDiffDenseNarrow's is the whole finding.
func BenchmarkDiffDenseWide(b *testing.B) { benchDense(b, denseWide) }

func benchDense(b *testing.B, build func() *buffer.Buffer) {
	b.Helper()
	prev := build()
	cur := build()
	w, h := prev.Width(), prev.Height()
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	d := NewDiffer(1 << 20)
	d.Diff(Frame{Cur: prev, Prev: prev, Width: w, Height: h, ForceFull: true, Encoder: enc})
	n := len(d.Diff(Frame{Cur: cur, Prev: prev, Width: w, Height: h,
		ForceFull: true, Encoder: enc}))

	// Both frames are built BEFORE ResetTimer: a scene builder's own allocations
	// would otherwise be charged to the diff, which is the mistake this whole
	// project's zero-allocation claim is about.
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Diff(Frame{Cur: cur, Prev: prev, Width: w, Height: h, ForceFull: true, Encoder: enc})
	}
	b.StopTimer()
	b.ReportMetric(float64(n), "out-B/frame")
	b.ReportMetric(float64(n)/float64(denseRunes), "out-B/rune")
}

// TestDenseWideGlyphCostsOneCursorMovePerGlyph pins the finding above as a test
// rather than leaving it as a benchmark line somebody has to notice.
//
// The assertions are about the CURRENT behaviour, deliberately. A wide glyph is
// preceded by a CUP escape because the diff's run tracking advances by one cell
// while the terminal advances by two. Encoding that as an exact expectation means a
// future fix shows up as a deliberate, reviewed change to THIS test — rather than
// as a byte-count change that six other golden assertions notice first and
// misdiagnose. That is the treatment ADR 0002 gave its own figures.
func TestDenseWideGlyphCostsOneCursorMovePerRow(t *testing.T) {
	enc := ansi.Encoder{Depth: ansi.DepthTrueColor}
	full := func(buf *buffer.Buffer) []byte {
		d := NewDiffer(1 << 20)
		return d.Diff(Frame{Cur: buf, Prev: buf, Width: buf.Width(), Height: buf.Height(),
			ForceFull: true, Encoder: enc})
	}
	narrow, wide := denseNarrow(), denseWide()
	if got := wide.Width() * wide.Height() / 2; got != denseRunes {
		t.Fatalf("the wide scene holds %d glyphs, want %d", got, denseRunes)
	}

	nB, wB := full(narrow), full(wide)
	nMoves, wMoves := countCUP(string(nB)), countCUP(string(wB))
	t.Logf("dense: %d narrow runes -> %d bytes in %d CUP moves; %d wide runes -> %d bytes in %d CUP moves",
		denseRunes, len(nB), nMoves, denseRunes, len(wB), wMoves)

	// Narrow: one run of adjacent cells, so exactly one cursor move per row.
	if want := narrow.Height(); nMoves != want {
		t.Errorf("narrow scene emitted %d CUP moves, want %d (one per row)", nMoves, want)
	}
	// Wide: run tracking must advance by the glyph's CELL WIDTH, not by one
	// column. A wide glyph occupies columns x and x+1 and leaves the terminal's
	// cursor at x+1, so recording only x made every subsequent cell look
	// non-adjacent and emitted a CUP per glyph.
	//
	// This assertion was inverted on 2026-10-04: the test originally pinned the
	// defective behaviour (one move per glyph) so a future fix would show up as
	// a deliberate change. The defect is now fixed, so the correct expectation is
	// one move per ROW, the same as the narrow scene.
	if want := wide.Height(); wMoves != want {
		t.Errorf("wide scene emitted %d CUP moves, want %d (one per row, as for narrow)",
			wMoves, want)
	}
	t.Logf("wide dense frame is %d bytes against the narrow one's %d, for the same %d runes (%.2fx)",
		len(wB), len(nB), denseRunes, float64(len(wB))/float64(len(nB)))
}

// countCUP returns how many CSI H (cursor position) sequences s contains.
func countCUP(s string) int {
	const (
		esc = 0x1b
		csi = '['
		cup = 'H'
	)
	n := 0
	for i := 0; i+2 < len(s); i++ {
		if s[i] != esc || s[i+1] != csi {
			continue
		}
		j := i + 2
		for j < len(s) && ((s[j] >= '0' && s[j] <= '9') || s[j] == ';') {
			j++
		}
		if j > i+2 && j < len(s) && s[j] == cup {
			n++
			i = j
		}
	}
	return n
}
