package dialog

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// This package's tests must not spell a box-drawing rune: buffer/border.go is the
// only file in TermMosaic allowed to, and TestBoxDrawingRunesLiveInOneFile
// enforces that by scanning every .go file in the module, tests included. Every
// expectation below is therefore assembled from the shared glyph table, which is
// also why a change to that table shows up here as a change in meaning rather
// than in spelling.

// edge returns the glyphs of the catalog's ONLY border table, so a test can name
// the chrome without writing a box-drawing rune.
func edge() buffer.BorderGlyphs { return buffer.BorderRounded.Glyphs(false) }

// rows renders root on a w-by-h screen and returns its rows with trailing blanks
// trimmed, so an expected string need not count the background a widget paints.
func rows(t *testing.T, w, h int, root termmosaic.Widget) []string {
	t.Helper()
	sink := widgettest.Render(t, w, h, 1, root)
	out := make([]string, h)
	for y := 0; y < h; y++ {
		out[y] = widgettest.Row(sink, y)
	}
	return out
}

// unframe strips the dialog's frame from both ends of a row and trims the
// trailing padding.
//
// LEADING spaces are kept: the choice marker column and the action gap are
// content, and an expectation that threw them away would assert less than it
// looks like it asserts.
func unframe(row string) string {
	return strings.TrimRight(strings.Trim(row, string(edge().Vertical)), " ")
}

// want asserts that row y's CONTENT equals expect, with the frame removed from
// both sides and trailing blanks trimmed.
func want(t *testing.T, got []string, y int, expect string) {
	t.Helper()
	if y < 0 || y >= len(got) {
		t.Fatalf("screen has %d rows, wanted row %d", len(got), y)
	}
	g, e := unframe(got[y]), strings.TrimRight(expect, " ")
	if g != e {
		t.Errorf("row %d\n got %q\nwant %q", y, g, e)
	}
}

// wantFramed asserts that row y starts and ends with the frame glyphs of the
// catalog's border table, which is how a test checks the CHROME without spelling
// a box-drawing rune.
func wantFramed(t *testing.T, got []string, y int) {
	t.Helper()
	if y < 0 || y >= len(got) {
		t.Fatalf("screen has %d rows, wanted row %d", len(got), y)
	}
	g := edge()
	r := []rune(got[y])
	if len(r) == 0 {
		t.Fatalf("row %d is empty", y)
	}
	leftOK := r[0] == g.Vertical || r[0] == g.TopLeft || r[0] == g.BottomLeft
	rightOK := r[len(r)-1] == g.Vertical || r[len(r)-1] == g.TopRight || r[len(r)-1] == g.BottomRight
	if !leftOK || !rightOK {
		t.Errorf("row %d is not framed: %q", y, got[y])
	}
}

// cellBuf returns a bare buffer for the tests that inspect cells and measure
// allocations, where a renderer in the way would be what was measured.
func cellBuf(w, h int) *buffer.Buffer { return buffer.NewBuffer(w, h) }

// rowOf returns row y of buf as a string, which is how a cell-level test reads
// back what a Draw wrote without involving the renderer.
func rowOf(buf *buffer.Buffer, y int) string {
	if y < 0 || y >= buf.Height() {
		return ""
	}
	var b strings.Builder
	for _, c := range buf.Row(y) {
		b.WriteRune(c.Rune())
	}
	return strings.TrimRight(b.String(), " ")
}

// rowText is rowOf without the trim, for the assertions that care about padding.
func rowText(buf *buffer.Buffer, y int) string {
	if y < 0 || y >= buf.Height() {
		return ""
	}
	var b strings.Builder
	for _, c := range buf.Row(y) {
		b.WriteRune(c.Rune())
	}
	return b.String()
}

// countRowsWithText returns how many rows contain needle, which is how a test
// counts what a Draw actually put on screen rather than what it meant to.
func countRowsWithText(buf *buffer.Buffer, needle string) int {
	n := 0
	for y := 0; y < buf.Height(); y++ {
		if strings.Contains(rowText(buf, y), needle) {
			n++
		}
	}
	return n
}

// snapshot returns every cell of buf as one string, so "the same state twice must
// produce the same cells" is an equality check on the whole grid rather than on a
// row that happened to change.
func snapshot(buf *buffer.Buffer) string {
	var b strings.Builder
	for y := 0; y < buf.Height(); y++ {
		for _, c := range buf.Row(y) {
			b.WriteRune(c.Rune())
			b.WriteByte('|')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// drawAll calls Draw n times, which is how an allocation test separates the first
// frame — which adapts, and therefore allocates by design — from the steady
// state, which must not.
func drawAll(w termmosaic.Widget, buf *buffer.Buffer, n int) {
	for i := 0; i < n; i++ {
		w.Draw(buf)
	}
}

// assertDifferent fails when a and b are equal.
//
// Every assertion here that compares one rendered value against another goes
// through this: a test that compares a widget's output with itself proves
// nothing, and that is the failure mode this helper exists to prevent.
func assertDifferent(t *testing.T, what, a, b string) {
	t.Helper()
	if a == b {
		t.Fatalf("%s: both values are %q, so the assertion cannot fail; the test is vacuous", what, a)
	}
}

// goldenDir is where the golden files live. testdata is skipped by the module-wide
// source scan, so a golden is free to hold the frame as the screen shows it.
var goldenDir = filepath.Join("testdata")

// goldenFor names a golden file. Sizes are in the name because the point of the
// golden set is the same dialog at different sizes.
func goldenFor(name string) string { return filepath.Join(goldenDir, name) }

// renderNoColor renders root at w by h with colour suppressed at encode time,
// which is what a terminal with NO_COLOR set does.
func renderNoColor(t *testing.T, w, h int, root termmosaic.Widget) *headless.MemorySink {
	t.Helper()
	sink := headless.NewMemorySink(w, h)
	r := newRenderer(t, sink, w, h, true)
	r.SetRoot(root)
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if got := sink.UnknownSequences(); got != 0 {
		t.Fatalf("%d unknown sequences with NO_COLOR:\n%s", got, sink.RawString())
	}
	return sink
}

// noColorAttrs returns the set of SGR attribute codes in a raw frame, which is how
// a test checks that something an attribute conveys SURVIVED NO_COLOR rather than
// being encoded away with the colours.
func noColorAttrs(raw string) map[string]bool {
	out := map[string]bool{}
	for _, code := range []string{"\x1b[1m", "\x1b[4m", "\x1b[7m"} {
		if strings.Contains(raw, code) {
			out[code] = true
		}
	}
	return out
}

// countRowsStartingWith returns how many rows begin with prefix after the frame is
// removed. It is the probe that can tell two WRAPPED layouts apart: a substring
// match cannot, because the narrow layout's words are all still present in the
// wide one.
func countRowsStartingWith(buf *buffer.Buffer, prefix string) int {
	n := 0
	for y := 0; y < buf.Height(); y++ {
		if strings.HasPrefix(unframe(rowText(buf, y)), prefix) {
			n++
		}
	}
	return n
}

// renderScreen renders root through the whole stack and returns the sink, for the
// tests that locate a control by looking at the screen rather than by computing
// where it ought to be.
func renderScreen(t *testing.T, root termmosaic.Widget) *headless.MemorySink {
	t.Helper()
	_, h := root.Bounds().Bottom(), root.Bounds().H
	sink := widgettest.Render(t, root.Bounds().W, root.Bounds().H, 1, root)
	_ = h
	return sink
}

// screenRow returns row y of the rendered screen with trailing blanks trimmed.
func screenRow(sink *headless.MemorySink, y int) string { return widgettest.Row(sink, y) }

// indexOf returns the index of the first occurrence of sub in r, or -1. It is
// strings.Index over runes, because a label found in a row of runes has to be
// located in runes rather than in bytes or the column would be wrong for any
// multi-byte glyph.
func indexOf(r, sub []rune) int {
	for i := 0; i+len(sub) <= len(r); i++ {
		match := true
		for j := range sub {
			if r[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// cellOf finds the column at which want starts on screen row y, and reports
// whether it is there. It is how the mouse tests locate a control the way a user
// does — by looking — instead of by re-deriving the widget's arithmetic.
func cellOf(t *testing.T, sink *headless.MemorySink, y int, want string) (int, bool) {
	t.Helper()
	at := indexOf([]rune(screenRow(sink, y)), []rune(want))
	return at, at >= 0
}

// actionRowOf returns the screen row the dialog's action block starts on, found by
// looking for the first button label rather than by recomputing the layout. It is
// what lets the mouse tests press a control the way a user would.
func actionRowOf(t *testing.T, d *Dialog) int {
	t.Helper()
	sink := renderScreen(t, d)
	_, h := sink.Size()
	for y := 0; y < h; y++ {
		for _, a := range d.Actions() {
			if _, ok := cellOf(t, sink, y, a.Label); ok {
				return y
			}
		}
	}
	t.Fatalf("no action label is on screen at all:\\n%s", sink.String())
	return -1
}

// choiceRowOf returns the screen row showing choice i, found by locating the choice
// whose label begins at that index. Looking rather than counting is what keeps the
// mouse tests honest when the layout changes.
func choiceRowOf(t *testing.T, d *Dialog, i int) int {
	t.Helper()
	sink := renderScreen(t, d)
	_, h := sink.Size()
	choices := d.Choices()
	if i < 0 || i >= len(choices) {
		t.Fatalf("choice %d is outside the %d choices", i, len(choices))
	}
	// The choices are contiguous, so the row of choice i is the row of choice 0
	// plus i — and both are read off the screen.
	first := -1
	for y := 0; y < h; y++ {
		if at, ok := cellOf(t, sink, y, choices[0]); ok && strings.Contains(screenRow(sink, y)[at:], ChoiceMarker) {
			first = y
			break
		}
	}
	if first < 0 {
		t.Fatalf("choice 0 is not on screen:\n%s", sink.String())
	}
	y := first + i
	if _, ok := cellOf(t, sink, y, choices[i]); !ok {
		t.Fatalf("choice %d is not on row %d as the contiguous layout requires:\n%s", i, y, sink.String())
	}
	return y
}

// keyPageDown is the page-down key, named with the rest of the contract's keys.
const keyPageDown = "\x1b[6~"

// renderIn renders root at w by h through the whole stack and returns the sink.
func renderIn(t *testing.T, w, h int, root termmosaic.Widget) *headless.MemorySink {
	t.Helper()
	return widgettest.Render(t, w, h, 1, root)
}

// widgetScreen returns the sink's screen as runes per row, for the tests that check
// that a widget wrote nothing outside its rect.
func widgetScreen(sink *headless.MemorySink) [][]rune {
	_, h := sink.Size()
	out := make([][]rune, h)
	for y := 0; y < h; y++ {
		out[y] = []rune(sink.Line(y))
	}
	return out
}

// buttonRows returns how many rows the dialog's action block occupies on a rendered
// screen, found by looking for the labels rather than by recomputing the layout.
func buttonRows(t *testing.T, got []string) int {
	labels := []string{DefaultCancelLabel, DefaultOKLabel, DefaultDismissLabel}
	first, last := -1, -1
	for y, row := range got {
		for _, l := range labels {
			if indexOf([]rune(row), []rune(l)) < 0 {
				continue
			}
			if first < 0 {
				first = y
			}
			last = y
		}
	}
	if first < 0 {
		t.Fatalf("no button label is on screen:\n%s", strings.Join(got, "\n"))
	}
	return last - first + 1
}

// bodyTitle is the title the body-counting helper excludes, because the title row is
// frame with a word in it and would otherwise be counted as body.
const bodyTitle = "Body"

// bodyRows returns how many rows carry NON-BLANK content that is not a button
// label and not frame — which is the body, counted off the SCREEN rather than read
// off the widget's own field, so the test cannot pass by the widget agreeing with
// itself.
//
// The blank rows matter: the stack is centred, so a dialog with a one-line body has
// blank rows above and below it, and counting those would make every height look
// the same and the whole assertion vacuous.
func bodyRows(t *testing.T, got []string) int {
	n := 0
	for _, row := range got {
		r := strings.TrimSpace(unframe(row))
		if r == "" || isFrameRow(row) {
			continue
		}
		if strings.Contains(r, DefaultDismissLabel) || strings.Contains(r, DefaultCancelLabel) ||
			strings.Contains(r, DefaultOKLabel) || strings.Contains(r, ChoiceMarker) {
			continue
		}
		// The TITLE row is frame with a word in it, so isFrameRow cannot catch it and
		// it would be counted as body. Excluding it by the title the caller set is
		// why this helper takes the dialog rather than only the screen.
		if strings.Contains(row, bodyTitle) {
			continue
		}
		n++
	}
	if n == 0 {
		t.Fatalf("no body row is on screen:\n%s", strings.Join(got, "\n"))
	}
	return n
}

// isFrameRow reports whether a row is entirely frame.
func isFrameRow(row string) bool {
	t := strings.TrimSpace(row)
	if t == "" {
		return false
	}
	g := edge()
	for _, r := range t {
		switch r {
		case g.TopLeft, g.TopRight, g.BottomLeft, g.BottomRight, g.Horizontal, g.Vertical:
		default:
			return false
		}
	}
	return true
}

// choiceRowCount returns how many rows carry a choice, counted from the MARKER
// COLUMN rather than from the labels' text — which is what makes it a measure of
// the layout instead of a measure of what the widget was configured with.
//
// Every choice row opens the same two cells: the marker and its pad. The FOCUSED row
// has the marker and the rest have the pad, and both count: a helper that matched
// only the marker would report one choice however many were on screen, which is
// exactly the bug this comment is here to prevent.
func choiceRowCount(t *testing.T, got []string) int {
	mark, pad := []rune(ChoiceMarker)[0], ' '
	n := 0
	for _, row := range got {
		r := []rune(unframe(row))
		if len(r) < 2 || strings.TrimSpace(string(r)) == "" {
			continue
		}
		if r[0] == mark && r[1] == pad {
			n++
			continue
		}
		if r[0] == pad && r[1] == pad {
			n++
		}
	}
	return n
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// longBody returns an info dialog of size w by h whose message is long enough to
// wrap at any reasonable width, for the tests about how the body uses its rows.
func longBody(t *testing.T, w, h int) *Dialog {
	t.Helper()
	d := New(rect(w, h), VariantInfo)
	d.SetTitle("Body", buffer.PlainStyle)
	// Sized against the WIDEST interior the responsive test uses — 58 cells at 60
	// wide — so this fits on ONE line there and needs three at 24 wide. The two row
	// counts therefore differ because of the LAYOUT, not because of where the
	// sentence happens to break, which is the whole claim.
	const text = "alpha beta gamma delta epsilon zeta eta theta"
	if got := buffer.StringWidth(text); got > 58 {
		t.Fatalf("setup: the message is %d cells and will not fit on one line at 60 wide", got)
	}
	d.SetBodyString(text)
	return d
}

// sentinelRune is the character the "wrote nothing outside its bounds" tests fill a
// buffer with first. It has to be distinguishable from a space, because a space is
// exactly what a widget paints when it means nothing.
const sentinelRune = '·'

// hasSGR reports whether raw contains an SGR sequence setting parameter p.
//
// It parses rather than substring-matches, because the encoder emits a reset plus
// its parameters ("ESC [ 0 ; 7 m") rather than a bare "ESC [ 7 m", and a test that
// looked for the bare form would fail against a correct frame. What it must NOT do
// is check for "7" as a substring: that would match a cursor-move row or a colour
// index, and the assertion would pass on a frame with no attribute at all.
func hasSGR(raw, p string) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != 0x1b || i+1 >= len(raw) || raw[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(raw) && raw[j] != 'm' {
			j++
		}
		if j >= len(raw) {
			continue
		}
		for _, param := range strings.Split(raw[i+2:j], ";") {
			if param == p {
				return true
			}
		}
		i = j
	}
	return false
}

// joinRows joins a screen's rows with newlines, which is how the failure messages
// here print a whole screen without each one re-deriving the join.
func joinRows(rows []string) string { return strings.Join(rows, "\n") }
