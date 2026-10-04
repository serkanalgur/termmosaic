package data

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// rows renders root on a w-by-h screen and returns the screen with trailing
// spaces trimmed per row, so an expected string need not count the background
// blanks a widget paints.
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
// A golden expectation in this package is written as the CONTENT of a row, never as
// a bordered row spelled out in box-drawing runes: those runes live in
// buffer/border.go and nowhere else, which vocabulary_test.go enforces over every
// file in the module, tests included. Asking the table for them here keeps these
// expectations honest about what they are testing — the widget's content — and keeps
// the one border implementation in the repository.
func edge() buffer.BorderGlyphs { return buffer.BorderPlain.Glyphs(false) }

// unframe strips a bordered widget's edge glyphs from both ends of a row and trims
// the trailing background padding, leaving the content between the border and the
// padding.
//
// LEADING spaces are kept: a marker gutter and a tree's indent are content, and an
// expectation of "  i1" is saying something a leading-space trim would throw away.
func unframe(row string) string {
	return strings.TrimRight(strings.Trim(row, string(edge().Vertical)), " ")
}

// want asserts that row y's CONTENT equals expect, with the frame removed from both
// sides and trailing blanks trimmed.
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

// wantFramed asserts that row y starts and ends with the border glyphs of the
// catalog's border table, which is how a test checks the CHROME without spelling a
// box-drawing rune.
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

// cellBuf returns a bare buffer l cells wide by h tall for the direct-Draw tests
// that measure allocations, where a renderer in the way would be what is being
// measured.
func cellBuf(w, h int) *buffer.Buffer { return buffer.NewBuffer(w, h) }

// drawAll calls Draw n times on a fresh buffer of the same size, which is how an
// allocation test separates the first frame (which adapts and therefore allocates
// by design) from the steady state (which must not).
func drawAll(w termmosaic.Widget, buf *buffer.Buffer, n int) {
	for i := 0; i < n; i++ {
		w.Draw(buf)
	}
}

// bordered wraps a widget in a plain border of one cell, which is what most of
// these tests want to exercise: the chrome is part of what is being tested.
func bordered(w, h int) (buffer.Rect, *block.Block) {
	r := buffer.Rect{X: 0, Y: 0, W: w, H: h}
	blk := block.New(r)
	blk.SetBorder(buffer.BorderPlain)
	return r, blk
}

// press offers the decoded key sequence seq to w and reports whether it was
// consumed, going through the real decoder so no test can pass on an event the
// decoder would never emit.
func press(t *testing.T, w termmosaic.Widget, seq string) bool {
	t.Helper()
	return w.Handle(keyFromSeq(t, seq))
}

// mustPress is press for a key the documented contract says the widget consumes.
// A key that is in the contract but not consumed is a test failure, never a
// silent pass.
func mustPress(t *testing.T, w termmosaic.Widget, seq string) {
	t.Helper()
	if !press(t, w, seq) {
		t.Errorf("%q was not consumed by %T", seq, w)
	}
}

// focus gives w focus, for the tests whose keys are only consumed while focused.
func focus(w termmosaic.Widget) {
	if f, ok := w.(termmosaic.Focusable); ok {
		f.SetFocused(true)
	}
}

// inner returns row y's content with the frame removed, so an assertion about a
// widget's CONTENT need not spell out the chrome the test did not ask about.
func inner(rs []string, y int) string {
	if y < 0 || y >= len(rs) {
		return ""
	}
	return unframe(rs[y])
}
