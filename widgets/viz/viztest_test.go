package viz

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// rows renders root on a w-by-h screen and returns its rows with trailing blanks
// trimmed, so an expected string need not count the blanks a widget paints.
func rows(t *testing.T, w, h int, root termmosaic.Widget) []string {
	t.Helper()
	sink := widgettest.Render(t, w, h, 1, root)
	out := make([]string, h)
	for y := 0; y < h; y++ {
		out[y] = widgettest.Row(sink, y)
	}
	return out
}

// edge returns the glyphs of the catalog's ONLY border table.
//
// A golden expectation here is the CONTENT of a row, never a bordered row spelled
// out in box-drawing runes: those runes live in buffer/border.go and nowhere else,
// which vocabulary_test.go enforces across the whole module including tests. Asking
// the table for them keeps these expectations honest about what they assert — the
// widget's content — and keeps the one border implementation in the repository.
func edge() buffer.BorderGlyphs { return buffer.BorderPlain.Glyphs(false) }

// unframe strips a bordered widget's edge glyphs from both ends of a row and trims
// the trailing background padding. Leading spaces are kept: a label gutter is
// content, and trimming it would make an expectation vacuous.
func unframe(row string) string {
	return strings.TrimRight(strings.Trim(row, string(edge().Vertical)), " ")
}

// want asserts that row y's CONTENT equals expect, with the frame removed and
// trailing blanks trimmed.
func want(t *testing.T, got []string, y int, expect string) {
	t.Helper()
	if y >= len(got) {
		t.Fatalf("screen has %d rows, wanted row %d", len(got), y)
	}
	g, e := unframe(got[y]), strings.TrimRight(expect, " ")
	if g != e {
		t.Errorf("row %d\n got %q\nwant %q", y, g, e)
	}
}

// wantFramed asserts that row y carries the border glyphs of the catalog's border
// table, which is how a test checks the CHROME without spelling a box-drawing rune.
func wantFramed(t *testing.T, got []string, y int) {
	t.Helper()
	if y >= len(got) {
		t.Fatalf("screen has %d rows, wanted row %d", len(got), y)
	}
	g := edge()
	row := []rune(got[y])
	if len(row) == 0 {
		t.Fatalf("row %d is empty", y)
	}
	leftOK := row[0] == g.Vertical || row[0] == g.TopLeft || row[0] == g.BottomLeft
	rightOK := row[len(row)-1] == g.Vertical || row[len(row)-1] == g.TopRight || row[len(row)-1] == g.BottomRight
	if !leftOK || !rightOK {
		t.Errorf("row %d is not framed: %q", y, got[y])
	}
}

// cellBuf returns a bare buffer for the direct-Draw tests that measure allocations
// and inspect cells, where the renderer in the way would be what is measured.
func cellBuf(w, h int) *buffer.Buffer { return buffer.NewBuffer(w, h) }

// drawAll draws n frames into the same buffer, which separates the first frame —
// which adapts, and therefore allocates by design — from the steady state.
func drawAll(w termmosaic.Widget, buf *buffer.Buffer, n int) {
	for i := 0; i < n; i++ {
		w.Draw(buf)
	}
}

// cellsOf returns w cells of row y starting at x0 as a string.
func cellsOf(buf *buffer.Buffer, y, x0, w int) string {
	row := buf.Row(y)
	var b strings.Builder
	for x := x0; x < x0+w && x < len(row); x++ {
		if x < 0 {
			continue
		}
		b.WriteRune(row[x].Ch)
	}
	return b.String()
}

// countRune returns how many times r appears in s, which is how the fill tests
// count filled cells without depending on which glyph the terminal uses.
func countRune(s string, r rune) int {
	n := 0
	for _, c := range s {
		if c == r {
			n++
		}
	}
	return n
}

// assertDifferent fails the test when a and b are equal.
//
// Every assertion here that compares a rendered value against another rendered
// value goes through this: a test that compares a widget's output with itself
// proves nothing, which is the failure mode this exists to prevent.
func assertDifferent(t *testing.T, what, a, b string) {
	t.Helper()
	if a == b {
		t.Fatalf("%s: both values are %q, so the assertion cannot fail; the test is vacuous", what, a)
	}
}

// termmosaicEvent returns a zero Event, which is what a widget must ignore: it
// carries no kind and no key, so consuming it would be a widget swallowing the
// absence of an event.
func termmosaicEvent() termmosaic.Event { return termmosaic.Event{} }

// cellDump renders w by h cells of buf as rows, for a failure message that has to
// show the whole frame rather than one cell of it.
func cellDump(buf *buffer.Buffer, w, h int) string {
	lines := make([]string, 0, h)
	for y := 0; y < h; y++ {
		lines = append(lines, cellsOf(buf, y, 0, w))
	}
	return strings.Join(lines, "\n")
}
