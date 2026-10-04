package data

import (
	"fmt"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// prose is a paragraph long enough to wrap several times at the widths these
// tests use.
const prose = "the quick brown fox jumps over the lazy dog and keeps on running " +
	"until the far side of the meadow"

// shortPager returns a bordered pager showing text, with the status line on.
func shortPager(t *testing.T, w, h int, text string) *Pager {
	t.Helper()
	p := NewPager(buffer.Rect{X: 0, Y: 0, W: w, H: h})
	p.SetText(text)
	p.Block().SetBorder(buffer.BorderPlain)
	return p
}

func TestPagerWrapsToWidth(t *testing.T) {
	// Twenty cells of screen leaves 18 for text after the border; the prose wraps
	// into words, and no word is broken.
	p := shortPager(t, 20, 8, prose)
	got := rows(t, 20, 8, p)
	// The border row itself is Block's, and widgets/block tests it; what
	// matters here is that the frame is where it belongs.
	wantFramed(t, got, 0)
	want(t, got, 1, "Ln 1/1            ")
	// Eighteen cells hold "the quick brown" and no more: "fox" would need a
	// twentieth cell, so the row breaks at the space before it.
	if !strings.Contains(got[2], "the quick brown") || strings.Contains(got[2], "fox") {
		t.Errorf("the first text row is not the wrapped start of the prose: %q", got[2])
	}
	// A row that is cut mid-document shows no ellipsis: the ellipsis belongs to
	// truncation, and a wrap point is not a truncation.
	if strings.Contains(got[2], "…") {
		t.Errorf("a wrapped row was marked as truncated: %q", got[2])
	}
	if strings.Contains(got[3], "…") {
		t.Errorf("a wrapped row was marked as truncated: %q", got[3])
	}
}

func TestPagerWrappingAgreesWithDrawing(t *testing.T) {
	// The row COUNT and the rows DRAWN come from one function, so they cannot
	// disagree — but only if that function actually STORES its ranges. This is the
	// regression test for the bug where it counted rows while writing nothing:
	// every range below must be distinct and in order, and the last must end at the
	// end of the line, or text would be dropped between rows.
	for _, width := range []int{1, 2, 3, 7, 18, 40} {
		n := splitRows(make([]textRow, 256), prose, width)
		if n < 1 {
			t.Fatalf("width %d: splitRows reported %d rows", width, n)
		}
		rowsIn := make([]textRow, n)
		if got := splitRows(rowsIn, prose, width); got != n {
			t.Fatalf("width %d: two calls disagreed: %d and %d rows", width, n, got)
		}
		prev := 0
		for i := 0; i < n; i++ {
			r := rowsIn[i]
			// Ranges advance monotonically but need NOT be contiguous: the spaces a
			// break consumes are exactly the bytes between one row's end and the
			// next one's start. What must hold is that no row is empty — a dropped
			// word would render as a blank line in the middle of a paragraph — and
			// that the last row reaches the end of the line, or text at the end
			// would be unreachable.
			if r.from < prev {
				t.Errorf("width %d row %d starts at %d, before the previous row ended at %d: text is out of order", width, i, r.from, prev)
			}
			if r.to <= r.from {
				t.Errorf("width %d row %d is the empty range [%d,%d): a word would be lost", width, i, r.from, r.to)
			}
			if r.to > len(prose) {
				t.Errorf("width %d row %d ends at %d, past the %d-byte line", width, i, r.to, len(prose))
			}
			prev = r.to
		}
		if prev != len(prose) {
			t.Errorf("width %d: the rows reach %d of %d bytes, so %d are unreachable", width, prev, len(prose), len(prose)-prev)
		}
	}
}

func TestPagerDoubleSpaceDoesNotProduceABlankRow(t *testing.T) {
	// The break consumes the whole run of spaces, so a double space is one break
	// rather than two rows with an empty one between them. At width 1 this is the
	// difference between a document and a mostly-blank one.
	rowsIn := make([]textRow, 64)
	n := splitRows(rowsIn, "a  b  c", 1)
	for i := 0; i < n; i++ {
		if rowsIn[i].to <= rowsIn[i].from {
			t.Errorf("row %d is empty: %+v", i, rowsIn[i])
		}
	}
	// Three letters, three rows: the space runs are consumed by the breaks rather
	// than becoming rows of their own.
	if n != 3 {
		t.Errorf("splitRows gave %d rows for %q at width 1, want 3", n, "a  b  c")
	}
}

func TestPagerBlankLinesAreRows(t *testing.T) {
	// A document of empty lines has as many rows as it has lines: dropping them
	// would make a log file lose its shape.
	// Seven rows of screen: a border, a status line and four body rows, one per
	// source line.
	p := shortPager(t, 12, 7, "a\n\n\nb")
	got := rows(t, 12, 7, p)
	// The body starts below the status line, and each blank source line is a blank
	// row of the interior — not a missing one.
	if inner(got, 2) != "a" {
		t.Errorf("row 2 is not the first line: %q", got[2])
	}
	if inner(got, 3) != "" || inner(got, 4) != "" {
		t.Errorf("blank lines did not produce blank rows: %q / %q", got[3], got[4])
	}
	if inner(got, 5) != "b" {
		t.Errorf("row 5 is not the last line: %q", got[5])
	}
	if inner(got, 4) != "" {
		t.Errorf("row 4 should be the third, blank line: %q", got[4])
	}
}

func TestPagerStatusReportsPositionWithoutPrinting(t *testing.T) {
	p := shortPager(t, 20, 8, "one\ntwo\nthree\nfour")
	p.Status = false
	p.Invalidate()
	off := rows(t, 20, 8, p)
	if strings.Contains(strings.Join(off, ""), "Ln") {
		t.Errorf("the status line was drawn although Status is false: %q", off)
	}
	p.Status = true
	p.Invalidate()
	on := rows(t, 20, 8, p)
	if !strings.Contains(on[1], "Ln 1/4") {
		t.Errorf("the status line does not report the position: %q", on[1])
	}
	assertDifferent(t, "the pager with and without a status line", strings.Join(off, ""), strings.Join(on, ""))
}

func TestPagerScrollKeysMoveTheViewport(t *testing.T) {
	p := shortPager(t, 20, 5, "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8")
	focus(p)
	if !strings.Contains(rows(t, 20, 5, p)[2], "l1") {
		t.Errorf("the first body row is not the first line: %q", rows(t, 20, 5, p)[2])
	}
	mustPress(t, p, "\x1b[B")
	if p.TopLine() != 1 {
		t.Errorf("after down: top line %d, want 1", p.TopLine())
	}
	mustPress(t, p, "\x1b[F") // end
	got := rows(t, 20, 5, p)
	// Five lines of document in a three-row body: End shows the last screenful, so
	// the top line is 6 - 3 + 1... measured on ROWS, so it is the line that puts
	// "l8" in the last body row.
	if p.TopLine() != 6 {
		t.Errorf("after end: top line %d, want 6", p.TopLine())
	}
	// Five rows of screen with a border and a status line leave two body rows.
	if inner(got, 3) != "l8" {
		t.Errorf("the last line is not the last visible row: %q", got[3])
	}
	mustPress(t, p, "\x1b[H") // home
	if p.TopLine() != 0 {
		t.Errorf("after home: top line %d, want 0", p.TopLine())
	}
}

func TestPagerEndIsTheLastScreenNotTheLastLineAlone(t *testing.T) {
	// End must show a SCREEN of the tail: a pager that put the last line at the top
	// would show two rows of content and three of nothing.
	p := shortPager(t, 20, 6, strings.Repeat("line\n", 20))
	focus(p)
	mustPress(t, p, "\x1b[F")
	got := rows(t, 20, 6, p)
	filled := 0
	for y := 2; y < 6; y++ {
		if strings.TrimSpace(got[y]) != "" {
			filled++
		}
	}
	if filled < 3 {
		t.Errorf("End showed only %d filled rows of a 4-row body: %q", filled, got)
	}
}

func TestPagerScrollInsideAWrappedLine(t *testing.T) {
	// A single long line wraps into several visual rows, and the sub-row position
	// is what scrolling has to move when there is only one line to scroll.
	p := shortPager(t, 16, 5, prose)
	focus(p)
	if p.TopLine() != 0 {
		t.Fatalf("fixture: top line %d", p.TopLine())
	}
	before := rows(t, 16, 5, p)
	mustPress(t, p, "\x1b[B")
	if p.TopLine() != 0 {
		t.Errorf("scrolling within one line changed the line: %d", p.TopLine())
	}
	after := rows(t, 16, 5, p)
	// Three body rows of 14 cells each, and the prose wraps into more rows than
	// that: the second row is a CONTINUATION, so it must not repeat the first.
	first, second := inner(before, 2), inner(after, 3)
	if !strings.HasPrefix(first, "the quick") {
		t.Errorf("the first row is not the start of the prose: %q", first)
	}
	assertDifferent(t, "the first wrapped row and the one after a scroll", first, inner(before, 3))
	if strings.HasPrefix(second, "the quick") {
		t.Errorf("the second row repeats the first: %q", second)
	}
}

func TestPagerSearchHighlightsWithARenditionNotAColour(t *testing.T) {
	p := shortPager(t, 20, 6, "alpha beta\nbeta gamma\ndelta beta")
	p.SetQuery("beta")
	if p.Matches() != 3 {
		t.Errorf("Matches = %d, want 3", p.Matches())
	}
	buf := cellBuf(20, 6)
	p.Draw(buf)
	// The default MatchStyle is reverse video, which is an attribute: it survives a
	// monochrome terminal, which a hue shift would not.
	if !buf.CellAt(2, 3).Attr.Has(buffer.AttrReverse) {
		t.Errorf("the matched word is not reversed: attr %v", buf.CellAt(2, 3).Attr)
	}
	if buf.CellAt(2, 2).Attr.Has(buffer.AttrReverse) {
		t.Errorf("an unmatched word is reversed: attr %v", buf.CellAt(2, 2).Attr)
	}
	// And the status line announces that a query is live, by glyph rather than by
	// colour.
	got := rows(t, 20, 6, p)
	if !strings.Contains(got[1], string(matchGlyph)) || !strings.Contains(got[1], "beta") {
		t.Errorf("the status line does not announce the query: %q", got[1])
	}
}

func TestPagerNextAndPrevMatch(t *testing.T) {
	p := shortPager(t, 20, 5, "one\ntwo\nthree\ntarget\nfive")
	focus(p)
	p.SetQuery("target")
	if !p.NextMatch() {
		t.Fatal("NextMatch found nothing")
	}
	if p.TopLine() != 3 {
		t.Errorf("after NextMatch: top line %d, want 3", p.TopLine())
	}
	if !p.NextMatch() {
		t.Fatal("the second NextMatch found nothing; it must wrap round")
	}
	// It wrapped past the end of the document and came back to the only match,
	// which is what "next" means when there is one.
	if p.TopLine() != 3 {
		t.Errorf("NextMatch did not wrap round to the only match: %d", p.TopLine())
	}
	if !p.PrevMatch() {
		t.Fatal("PrevMatch found nothing")
	}
	if p.TopLine() != 3 {
		t.Errorf("after PrevMatch: top line %d, want 3", p.TopLine())
	}
	p.SetQuery("absent")
	if p.NextMatch() {
		t.Error("NextMatch reported a match that does not exist")
	}
}

func TestPagerKeysAreIgnoredWhileUnfocused(t *testing.T) {
	p := shortPager(t, 20, 5, "a\nb\nc\nd")
	if press(t, p, "\x1b[B") {
		t.Error("down was consumed while the pager did not have focus")
	}
	if p.TopLine() != 0 {
		t.Errorf("the viewport moved to line %d while unfocused", p.TopLine())
	}
}

func TestPagerWheelScrolls(t *testing.T) {
	p := shortPager(t, 20, 5, strings.Repeat("line\n", 30))
	focus(p)
	if !wheelAt(p, 5, 2, false) {
		t.Fatal("a wheel notch was not consumed")
	}
	if p.TopLine() != scrollLines {
		t.Errorf("after a notch: top line %d, want %d", p.TopLine(), scrollLines)
	}
}

func TestPagerDegenerateSizesDoNotPanic(t *testing.T) {
	for _, size := range []buffer.Size{{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 2}, {W: 3, H: 1}, {W: 8, H: 0}, {W: 0, H: 4}} {
		p := shortPager(t, size.W, size.H, prose+"\n\n"+prose)
		p.Block().SetBorder(buffer.BorderPlain)
		focus(p)
		buf := cellBuf(size.W, size.H)
		p.Draw(buf)
		for _, seq := range []string{"\x1b[B", "\x1b[A", "\x1b[F", "\x1b[H", "\x1b[5~", "\x1b[6~"} {
			mustPress(t, p, seq)
		}
		p.SetQuery("the")
		mustPress(t, p, "n")
		p.Draw(cellBuf(size.W, size.H))
	}
}

func TestPagerDrawIsAllocationFree(t *testing.T) {
	p := shortPager(t, 40, 20, strings.Repeat(prose+"\n", 500))
	buf := cellBuf(40, 20)
	drawAll(p, buf, 3)
	if got := testing.AllocsPerRun(200, func() { p.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per frame; the frame path must be free", got)
	}
	// And with a query live, because the highlight path is the one that builds
	// substrings.
	p.SetQuery("lazy")
	drawAll(p, buf, 3)
	if got := testing.AllocsPerRun(200, func() { p.Draw(buf) }); got != 0 {
		t.Errorf("Draw with a query allocated %v times per frame", got)
	}
}

func TestPagerScrollingDoesNotAllocate(t *testing.T) {
	p := shortPager(t, 40, 20, strings.Repeat(prose+"\n", 2000))
	focus(p)
	buf := cellBuf(40, 20)
	drawAll(p, buf, 3)
	if got := testing.AllocsPerRun(100, func() {
		p.ScrollBy(1)
		p.Draw(buf)
	}); got != 0 {
		t.Errorf("scrolling then Draw allocated %v times", got)
	}
}

func TestPagerScrollsAHugeDocument(t *testing.T) {
	// The claim is that cost is per screen rather than per document: a 4 MB pager
	// must cost what a small one does, and End must still be instant because it
	// walks backwards from the last line.
	big := strings.Repeat(prose+"\n", 60000)
	p := shortPager(t, 40, 20, big)
	// The document ends with a newline, which makes an empty final line: a pager
	// that hid it would report one line fewer than a reader counting with wc -l
	// expects, and would make "the last line" unreachable.
	if p.Lines() != 60001 {
		t.Fatalf("Lines = %d, want 60001", p.Lines())
	}
	focus(p)
	buf := cellBuf(40, 20)
	drawAll(p, buf, 3)
	if got := testing.AllocsPerRun(50, func() { p.Draw(buf) }); got != 0 {
		t.Errorf("a 4 MB pager allocated %v times per frame", got)
	}
	mustPress(t, p, "\x1b[F")
	if p.TopLine() < 59990 {
		t.Errorf("End on a 60000-line document landed on line %d", p.TopLine())
	}
}

func TestPagerGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	p := shortPager(t, 40, 6, prose)
	wide := rows(t, 40, 6, p)
	if strings.TrimSpace(wide[2]) == "" {
		t.Fatalf("the wide layout drew nothing: %q", wide[2])
	}
	p.SetBounds(buffer.Rect{X: 0, Y: 0, W: 14, H: 6})
	narrow := rows(t, 14, 6, p)
	if strings.TrimSpace(narrow[2]) == "" {
		t.Errorf("the text did not survive the shrink: %q", narrow[2])
	}
	for y := range narrow {
		if len([]rune(narrow[y])) > 14 {
			t.Errorf("row %d is wider than the screen after shrinking: %q", y, narrow[y])
		}
	}
	// Growing again must also work, and must re-wrap rather than keep the narrow
	// layout's rows.
	p.SetBounds(buffer.Rect{X: 0, Y: 0, W: 40, H: 6})
	again := rows(t, 40, 6, p)
	if again[2] == narrow[2] {
		t.Errorf("growing back did not re-wrap: %q", again[2])
	}
}

func TestPagerEmptyDocumentIsLegal(t *testing.T) {
	p := shortPager(t, 12, 5, "")
	got := rows(t, 12, 5, p)
	if p.Lines() != 1 {
		t.Errorf("an empty document has %d lines, want 1", p.Lines())
	}
	focus(p)
	for _, seq := range []string{"\x1b[B", "\x1b[F", "\x1b[6~"} {
		mustPress(t, p, seq)
	}
	p.Draw(cellBuf(12, 5))
	if inner(got, 2) != "" {
		t.Errorf("an empty document drew text: %q", got[2])
	}
}

func TestPagerMinSizeIncludesChrome(t *testing.T) {
	p := NewPager(buffer.Rect{W: 1, H: 1})
	p.Block().SetBorder(buffer.BorderPlain)
	got := p.MinSize()
	if got.W != minPagerW+2 || got.H != minPagerH+2 {
		t.Errorf("MinSize = %+v, want {%d,%d}", got, minPagerW+2, minPagerH+2)
	}
}

func TestPagerLongWordIsCutWithoutSplittingARune(t *testing.T) {
	// A word longer than the width is cut at the cell boundary. Proving the rune
	// is not split: the rendered rows must decode as valid UTF-8 with no
	// replacement characters.
	p := shortPager(t, 10, 5, strings.Repeat("日", 20))
	got := rows(t, 10, 5, p)
	for y := range got {
		if strings.Contains(got[y], "�") {
			t.Errorf("row %d contains a replacement character: %q", y, got[y])
		}
	}
	if p.Line(0) != strings.Repeat("日", 20) {
		t.Error("Line(0) did not return the source line verbatim")
	}
	_ = fmt.Sprint(got)
}
