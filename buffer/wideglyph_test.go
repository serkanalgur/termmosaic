package buffer

import "testing"

// internal/diff's wideglyph_test.go measures the read side (a wide scene through
// the two-tier diff); this file measures the write side, which is where ADR 0008
// risk 5 names three unbenchmarked paths: SetString, SetSpans and Wrap.
//
// # Why the pairing is n:n
//
// Each benchmark writes the same CELL WIDTH with narrow runes and with
// double-width runes, into the same rectangle. Narrow therefore writes twice as
// many runes for the same width, so the two ns/op figures are NOT interchangeable
// and the doc comment on each says so. What IS directly comparable:
//
//   - allocs/op, which must be 0 for the frame-path writers and non-zero for
//     Wrap by its documented contract;
//   - ns/op PER CELL written, which is what a 200x60 screen of text actually
//     scales with and is reported here as ns/cell.
//
// # What the numbers turned out to be
//
// The wide path is not slower. A double-width rune costs one extra branch and a
// second 16-byte cell write; it saves the loop iterations and the RuneWidth calls
// that two narrow runes would have needed. The results are in ADR 0008's risk 5
// amendment.

// wideBenchSpans is a CJK sentence as a single span, and narrowBenchSpans is the
// narrow-glyph sentence of exactly the same cell width. They are declared as vars
// rather than rebuilt per iteration because building them inside the timed loop
// would measure string construction.
var (
	wideBenchSpans   = []Span{NewSpan("日本語のテキストはERICorrectly二Cell進", DefaultStyle)}
	narrowBenchSpans = []Span{NewSpan("abcdefghijklmnopqrstuvwxyz0123456789AB", DefaultStyle)}
)

// benchText is the widest of the two, in cells, and every benchmark writes
// exactly this many.
func benchTextWidth() int {
	w := SpansWidth(wideBenchSpans)
	if n := SpansWidth(narrowBenchSpans); n != w {
		panic("the wide and narrow benchmark texts must have the same cell width, got " +
			itoa(w) + " and " + itoa(n))
	}
	return w
}

// itoa is a tiny local helper so the init-time check needs no fmt.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// benchH is the height every write benchmark fills.
const benchH = 40

// benchWriteNarrow and benchWriteWide are the like-for-like pair for
// SetSpansIn: the same rectangle, the same cell count, half the runes.
//
// This is the path ADR 0008 §1 rule for wide glyphs lives on — a wide rune writes
// a glyph cell AND a continuation cell, and both take this span's style — so it
// is the write-side equivalent of what internal/diff measures on the read side.
func benchWriteNarrow(b *testing.B) { benchWrite(b, narrowBenchSpans) }
func benchWriteWide(b *testing.B)   { benchWrite(b, wideBenchSpans) }

func benchWrite(b *testing.B, spans []Span) {
	b.Helper()
	w := benchTextWidth()
	buf := NewBuffer(w, benchH)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.SetSpansIn(0, w, i%benchH, spans)
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*w*benchH), "ns/cell")
}

// BenchmarkSetSpansInNarrow is the narrow half of the SetSpansIn pair. See
// benchWriteWide's comment for what the two figures mean.
func BenchmarkSetSpansInNarrow(b *testing.B) { benchWriteNarrow(b) }

// BenchmarkSetSpansInWide is the wide half, and the number ADR 0008 risk 5 was
// missing: the per-cell cost of writing double-width runes through the frame-path
// writer.
func BenchmarkSetSpansInWide(b *testing.B) { benchWriteWide(b) }

// BenchmarkSetSpansWindowInWide measures the horizontally scrolled writer, the
// one whose `skip` argument walks a partially-visible column. It is the only
// writer that touches every rune in a long row twice — once to skip past it and
// once to write it — so it is where a wide glyph would cost the most if it cost
// anything at all.
//
// 12 is the skip: six narrow runes or three wide ones off the left edge.
func BenchmarkSetSpansWindowInWide(b *testing.B) {
	w := benchTextWidth()
	buf := NewBuffer(w, benchH)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.SetSpansWindowIn(0, w, i%benchH, wideBenchSpans, 12, '~')
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*w*benchH), "ns/cell")
}

// BenchmarkWrapNarrow is the narrow half of the pair below. Both are ADR 0008's
// other named unmeasured path: Wrap ALLOCATES by contract — it returns a Wrapped
// with one Span per line, which is why ADR 0008 §4 forbids calling it from Draw —
// so the allocs/op column is the expected one and is not a failure.
//
// The interesting number is per LINE, not per rune: Wrap's cost is dominated by the
// line breaking rather than by glyph width, so wide text should cost about half the
// lines of narrow text of the same cell width and therefore about half the time. If
// it does not, the width arithmetic in Wrap is doing more work per rune than the
// design says it should.
func BenchmarkWrapNarrow(b *testing.B) { benchWrap(b, narrowBenchSpans, 20) }

// BenchmarkWrapWide is the wide half of the pair. See BenchmarkWrapNarrow.
func BenchmarkWrapWide(b *testing.B) { benchWrap(b, wideBenchSpans, 20) }

func benchWrap(b *testing.B, spans []Span, width int) {
	b.Helper()
	// Warm: the first Wrap may size an internal scratch.
	_ = Wrap(spans, width)
	n := len(Wrap(spans, width).Lines)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Wrap(spans, width)
	}
	b.StopTimer()
	b.ReportMetric(float64(n), "lines/iter")
}
