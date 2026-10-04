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
// # Why the helpers below exist instead of buffer.Truncate
//
// A truncated row has to be drawn every frame, and buffer.Truncate allocates
// because it builds a new span slice. The renderer cannot afford one allocation
// per visible row per frame, so this package writes clipped spans itself with
// drawSpans / drawSpansCapped.
//
// That is a deliberate, bounded exception rather than a re-implementation of the
// buffer's text path: drawSpans handles exactly three cases — zero-width runes
// dropped, a double-width rune written as a glyph cell plus a buffer.ContinuationCell
// in the SAME style, and a rune that does not fit ending the run — which are the
// rules buffer.SetSpans already documents, and it calls the same exported
// buffer.ContinuationCell so the two halves cannot disagree. The reason it
// exists at all is that buffer.SetSpans clips to the BUFFER's right edge, and a
// widget row ends before the screen does. If a fourth widget set ever needs the
// same thing, this belongs in buffer as a SetSpansIn(buf, x0, x1, y, spans); it
// is here until then rather than in shared code this package does not own.
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

// drawSpans writes spans left to right on row y, starting at x0 and stopping
// before x1, and returns the column it stopped at.
//
// It is the clipped, allocation-free counterpart of buffer.SetSpans, which stops
// at the BUFFER's right edge rather than at a widget row's. It returns x1 when
// the content did not fit, which is how a caller tells truncation from fitting.
func drawSpans(buf *buffer.Buffer, x0, x1, y int, spans []buffer.Span) int {
	if x1 <= x0 {
		return x0
	}
	x := x0
	for i := range spans {
		if x >= x1 {
			return x
		}
		st := spans[i].Style.Resolved()
		for _, r := range spans[i].Text {
			w := buffer.RuneWidth(r)
			switch {
			case w == 0:
				// Zero-width runes occupy no cell and are dropped, matching
				// buffer.SetSpans and buffer.Wrap.
			case x+w > x1:
				// A wide glyph is never split and a narrow one is never half
				// taken: the run ends and the caller sees x < x1.
				return x
			case w == 2:
				buf.SetCell(x, y, st.Cell(r))
				buf.SetCell(x+1, y, buffer.ContinuationCell(st))
			default:
				buf.SetCell(x, y, st.Cell(r))
			}
			x += w
		}
	}
	return x
}

// drawSpansCapped writes spans into the cells [x0, x1) and, when the content is
// wider than that span, ends with the one-cell truncation marker in the final
// cell.
//
// It is the allocation-free counterpart of buffer.Truncate for the draw path,
// with the same rules: the marker takes the style of the last span kept (or of
// the first non-empty span when nothing fit), content already narrow enough is
// drawn whole, and a double-width rune is never split.
func drawSpansCapped(buf *buffer.Buffer, x0, x1, y int, spans []buffer.Span, mark rune) {
	if x1 <= x0 {
		return
	}
	if buffer.SpansWidth(spans) <= x1-x0 {
		drawSpans(buf, x0, x1, y, spans)
		return
	}
	// Reserve the last cell for the marker. Truncate cannot exceed maxWidth
	// either, so the result is never wider than the span it was given.
	if x1-x0 == 1 {
		buf.SetCell(x0, y, lastSpanStyle(spans).Resolved().Cell(mark))
		return
	}
	drawSpans(buf, x0, x1-1, y, spans)
	buf.SetCell(x1-1, y, lastSpanStyle(spans).Resolved().Cell(mark))
}

// lastSpanStyle returns the style a truncation marker wears: that of the last
// non-empty input span, since there is no "last kept span" to take it from
// without having built the truncated slice this function exists to avoid.
func lastSpanStyle(spans []buffer.Span) buffer.Style {
	for i := len(spans) - 1; i >= 0; i-- {
		if spans[i].Text != "" {
			return spans[i].Style
		}
	}
	return buffer.DefaultStyle
}

// drawSpansWindow writes spans into the cells [x0, x1) on row y, starting at the
// given CELL OFFSET into the content, and ends with the truncation marker when the
// rest does not fit.
//
// It exists for the horizontally scrolled column: a column whose first cells are off
// screen to the left has to show its TAIL, and neither drawSpans nor drawSpansCapped
// can express a window that does not start at the content's beginning. Without it, a
// scrolled table either repeats the column's head or paints it over the gutter and
// the border to its left.
//
// skip is clamped at zero and a negative or zero width window draws nothing.
func drawSpansWindow(buf *buffer.Buffer, x0, x1, y int, spans []buffer.Span, skip int, mark rune) int {
	if x1 <= x0 {
		return x0
	}
	if skip < 0 {
		skip = 0
	}
	seen, x := 0, x0
	stopped := false
	for i := range spans {
		if stopped {
			break
		}
		st := spans[i].Style.Resolved()
		for _, r := range spans[i].Text {
			w := buffer.RuneWidth(r)
			if w == 0 {
				continue
			}
			if seen+w <= skip {
				seen += w
				continue
			}
			if seen < skip {
				// A wide rune straddling the window's left edge is dropped rather
				// than half taken, exactly as buffer.SetSpans drops one that does not
				// fit at the right edge.
				seen += w
				stopped = true
				break
			}
			if x+w > x1 {
				stopped = true
				break
			}
			if w == 2 {
				buf.SetCell(x, y, st.Cell(r))
				buf.SetCell(x+1, y, buffer.ContinuationCell(st))
			} else {
				buf.SetCell(x, y, st.Cell(r))
			}
			seen += w
			x += w
		}
	}
	if stopped && x1-x0 > 1 {
		// The marker replaces the LAST cell of the window even when the window
		// filled exactly: "state0" in five cells is "stat…", not "state", which is
		// what a reader needs to see to know the cell is cut.
		buf.SetCell(x1-1, y, lastSpanStyle(spans).Resolved().Cell(mark))
		x = x1
	}
	return x
}

// drawText writes s into the cells [x0, x1) on row y in st and returns the
// column it stopped at. It is drawSpans for the common uniform-text case and
// allocates nothing.
func drawText(buf *buffer.Buffer, x0, x1, y int, s string, st buffer.Style) int {
	var one [1]buffer.Span
	one[0] = buffer.NewSpan(s, st)
	return drawSpans(buf, x0, x1, y, one[:])
}

// paintRow fills row in bg and then writes spans into it, clipped to the row and
// offset by lead cells.
//
// It is the body of every data widget's row painter: repaint the whole row
// first (ADR 0007 §1 rule 3), then write the content over it. The fill is what
// makes a selected row readable without colour when SelectedStyle carries a
// background.
func paintRow(buf *buffer.Buffer, row buffer.Rect, spans []buffer.Span, lead int, mark rune, bg buffer.Style) {
	if row.Empty() {
		return
	}
	buf.FillRect(row, bg.Resolved().Blank())
	drawSpansCapped(buf, row.X+lead, row.Right(), row.Y, spans, mark)
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
