// Package data provides the catalog's scrolling data widgets: List, Table,
// Tree and Pager.
//
// # What the four have in common, and why they share a package
//
// All four show a window of a much larger collection, and none of the arithmetic
// involved is about lists, tables, trees or pagers. So the arithmetic lives in
// virtual/ and this package owns only what differs: what a row is, what happens
// on a key, and what a row looks like. ADR 0007 §6 requires a virtualized widget
// to be O(visible rows) per frame rather than O(item count), and List, Table and
// Tree all get that from virtual.Model for free — BenchmarkList100K in this
// package's tests is the proof rather than the claim.
//
// # The rules every widget here obeys
//
//   - Its space is Bounds(), never buf.Size() (ADR 0007 §1 rule 1). The only
//     place a screen dimension is read is inside a test.
//   - It repaints its whole Bounds() before drawing content (rule 3). Every
//     widget here does that by composing widgets/block.Block, whose Draw fills
//     the rect in Background before it draws anything else — the sanctioned way
//     to express it (ADR 0008 §2). No widget in this package draws a border, a
//     corner or a title itself.
//   - Its Draw is allocation-free (ADR 0008 §4). Anything derived from a widget's
//     own size — which columns are visible, which chrome survives the budget,
//     which rows map to which items — is computed in the size-change check,
//     cached against the rect it was computed for, and only read in Draw.
//   - Draw is total. Every rect including 0x0 and 1x1 is defined, and a widget
//     below its own MinSize draws its minimum layout clipped rather than
//     blanking itself (ADR 0007 §4).
//   - Colour is never the only signal. Selection is carried by a marker column
//     as well as by SelectedStyle, the tree's expansion state by a glyph, and
//     the pager's matches by a rendition attribute rather than a hue.
//
// # Why the span writers live in buffer
//
// A truncated row has to be drawn every frame, and buffer.Truncate allocates
// because it builds a new span slice. The renderer cannot afford one allocation
// per visible row per frame. That used to be solved here, with private copies of
// buffer's wide-glyph rules in this package and in widgets/viz, because
// buffer.SetSpans clips to the BUFFER's right edge and a widget row ends before
// the screen does. Two copies of the continuation-cell rule is two chances for
// one of them to put the next span's style on the right half of a glyph, and that
// mismatch never compares equal to the previous frame, so the row flickers
// forever.
//
// So the writers are buffer's: SetSpansIn, SetSpansCappedIn, SetSpansWindowIn and
// SetStringIn, all allocation-free, all owning the zero-width, wide-glyph and
// clipping rules in one place. This package chooses the RANGE and the marker —
// which is the ASCII rung, whether the row has a selection gutter — and buffer
// does the writing.
package data

import (
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// truncMark returns the one-cell truncation marker for the given ASCII rung.
//
// Both rungs are one cell wide — that invariant is buffer's and is pinned by
// TestTruncSuffixesAreOneCellWide — so picking between them cannot shift a
// layout, only which glyph the reader sees.
func truncMark(ascii bool) rune {
	if ascii {
		return firstRune(buffer.AscTruncSuffix)
	}
	return firstRune(buffer.TruncSuffix)
}

// paintRow fills row in bg and then writes spans into it, clipped to the row and
// offset by lead cells.
//
// It is the body of every data widget's row painter: repaint the whole row
// first (ADR 0007 §1 rule 3), then write the content over it. The fill is what
// makes a selected row readable without colour when SelectedStyle carries a
// background. The writing itself — the truncation marker and the wide-glyph rules
// — is buffer's SetSpansCappedIn: this function decides the row and the lead
// cells, not how a glyph is placed.
//
// st is the row's CONTENT rendition: it overrides the spans' own styles, which
// is how a widget's ItemStyle reaches text it did not build and how the selected
// row takes SelectedStyle outright. Pass the zero Style to write the spans'
// styles verbatim, and note that a multi-span row keeps them regardless —
// flattening it would have to build a string on the frame path, and a row that
// deliberately carries several styles is the author saying so.
func paintRow(buf *buffer.Buffer, row buffer.Rect, spans []buffer.Span, lead int, mark rune, bg, st buffer.Style) {
	if row.Empty() {
		return
	}
	buf.FillRect(row, bg.Resolved().Blank())
	if !st.IsUnset() && len(spans) == 1 {
		// The common case is one span, so the override is a field write into a
		// stack array rather than a slice built per row per frame.
		var one [1]buffer.Span
		one[0] = buffer.Span{Text: spans[0].Text, Style: st}
		spans = one[:]
	}
	buf.SetSpansCappedIn(row.X+lead, row.Right(), row.Y, spans, mark)
}

// intDigits writes v's decimal representation into dst and returns the filled
// prefix of dst. It exists so a widget can print a number without fmt, and
// therefore without the allocation fmt.Sprintf performs on the frame path.
//
// v is clamped at zero: every quantity a widget here prints is a count, a
// position or a non-negative measurement, and a negative one printed as "-3"
// would be a bug the caller cannot see rather than an honest report.
func intDigits(dst []byte, v int) []byte {
	if v < 0 {
		v = 0
	}
	if v == 0 {
		return append(dst, '0')
	}
	var scratch [20]byte
	i := len(scratch)
	for v > 0 {
		i--
		scratch[i] = byte('0' + v%10)
		v /= 10
	}
	return append(dst, scratch[i:]...)
}

// putDigits writes d into row y starting at x in st and returns the column just
// past the last digit. One cell per byte, which is what intDigits guarantees.
func putDigits(buf *buffer.Buffer, x, y int, d []byte, st buffer.Style) int {
	c := st.Resolved()
	for i := range d {
		buf.SetCell(x+i, y, c.Cell(rune(d[i])))
	}
	return x + len(d)
}

// chromeInset returns how many cells blk consumes on each side: its border, if
// it has one it can actually draw, plus its padding.
//
// MinSize needs it to convert an interior minimum into a whole-widget minimum
// (ADR 0007 §2: MinSize includes chrome), and it is the one place in this
// package that reasons about the border's size. The thresholds themselves are
// block's — this is arithmetic over what block reports, not a second copy of
// them.
func chromeInset(blk *block.Block) int {
	inset := blk.Padding
	if blk.Border != buffer.BorderNone {
		inset++
	}
	return inset
}

// minWhole returns the whole-widget size carrying chrome of inset cells on each
// side and an interior of at least w by h cells.
func minWhole(blk *block.Block, w, h int) buffer.Size {
	inset := chromeInset(blk)
	s := blk.MinSize()
	if want := w + 2*inset; s.W < want {
		s.W = want
	}
	if want := h + 2*inset; s.H < want {
		s.H = want
	}
	return s
}

// firstRune returns the first rune of s, or 0 when s is empty.
//
// It is for a one-cell marker string: a caller may write a multi-byte glyph such
// as "▸" and must not have to index its bytes, which would split the UTF-8
// sequence and write a replacement character.
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}
