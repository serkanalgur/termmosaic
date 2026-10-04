// Package viz provides the catalog's measurement and display widgets: ProgressBar,
// Gauge, Meter, Sparkline and BarChart.
//
// # Why this package is the differentiator
//
// Data widgets are table stakes: every comparable TUI framework ships a list, a
// table and a scrollbar. None of them ships a sparkline with sub-cell resolution,
// a meter with named zones, or a gauge that survives a monochrome terminal. That
// gap is the reason this package exists, and it is why these five are built as
// widgets rather than as drawing examples: a sparkline you have to hand-roll is a
// sparkline nobody ships.
//
// # The three rules every widget here obeys
//
//  1. Its space is Bounds(), never buf.Size() (ADR 0007 §1 rule 1).
//  2. It repaints its whole Bounds() before drawing content (rule 3), by composing
//     widgets/block.Block, whose Draw fills the rect in Background before anything
//     else. No widget here draws a border, a corner or a title itself.
//  3. Its Draw is allocation-free (ADR 0008 §4). Everything derived from the size
//     — which regions survived the budget, where the axis labels go, the glyph
//     ramp for a sparkline — is computed in the size-change check, cached against
//     the rect, and only read in Draw.
//
// # Colour is never the only signal
//
// This is a hard accessibility requirement here rather than a nicety, because a
// bar chart whose only distinction between "fine" and "critical" is a hue is
// unreadable to a colour-blind reader and to every monochrome terminal. So:
//
//   - ProgressBar and Gauge print the number, as digits, next to the bar.
//   - Meter prints the ACTIVE ZONE'S NAME and marks the threshold with a glyph,
//     so "critical" is a word as well as a shade.
//   - Sparkline encodes its data in GEOMETRY — the height of a column is the value
//     — and offers styles that add emphasis rather than carry it.
//   - BarChart prints the value above or beside every bar.
//   - Every widget offers an ASCII rung, selected through the Block, so a terminal
//     without Unicode degrades to '#', '=' and '|' rather than to nothing.
//
// # Values degrade, and say so
//
// Values are floats arriving from the world: a division that produced NaN, a
// percentage over 100, a negative delta. Every widget here clamps rather than
// panicking and documents which way it clamps:
//
//   - a NaN or infinite value is treated as zero (ratioOf)
//   - a value below the minimum clamps to the minimum, above the maximum to the
//     maximum
//   - a maximum of zero or less gives a zero-filled widget rather than a division
//     by zero
//
// # Glyph vocabulary
//
// buffer/border.go owns the box-drawing table and this package adds none of it.
// What it does use is the two texture blocks, which are legitimate texture rather
// than borders and are explicitly outside that rule: Block Elements (U+2580–259F,
// the eighths, quadrants and shades used by bars and gauges) and Braille Patterns
// (U+2800–28FF, used by the gauge dial and the high-resolution sparkline). The
// tables below are indexed by level and are the only place in the package that
// names those runes.
package viz

import (
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Eighth-block ramp, U+2581 through U+2588: eight levels of vertical fill, which
// is what gives a one-row sparkline its sub-cell resolution.
var eighths = [...]rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// fullBlock is U+2588 FULL BLOCK, one cell wide, the solid fill of a bar.
const fullBlock = '█'

// halfBlockLeft and halfBlockRight are the two half-block cells used to fill the
// final, partial cell of a bar or a column: U+258C and U+2590.
const (
	halfBlockLeft  = '▌'
	halfBlockRight = '▐'
)

// brailleBits maps a position within a Braille cell to the bit that carries it.
// A Braille cell is a 2-column by 4-row dot grid, and U+2800 is the empty one, so
// drawing a shape at sub-cell resolution is a bitwise OR into one rune.
//
// Indexed [column][row]; the values are the Unicode dot numbers minus one, which
// is why they are in this order and not row-major.
var brailleBits = [2][4]int{
	{0, 3}, // column 0, rows 0..3
	{1, 4}, // column 1, rows 0..3
}

// brailleBase is the empty Braille cell.
const brailleBase = '⠀'

// Local thresholds, per ADR 0007 §1 rule 5: a widget's own constants are its own
// policy, not framework vocabulary.
const (
	// dialMinW and dialMinH are the interior sizes below which Gauge stops drawing
	// a dial and draws a bar instead. The thresholds are in CELLS because a Braille
	// cell is 2 dots wide by 4 tall, so a round dial needs roughly twice as many
	// cells across as it has down.
	dialMinW = 9
	dialMinH = 5
	// dialInnerRatio is the dial's hole as a fraction of its outer radius, which is
	// what makes it a ring rather than a disc.
	dialInnerRatio = 0.55
	// dialGapRatio is the gap between the ring and the needle hub.
	dialGapRatio = 0.22
	// minBarW is the narrowest interior a bar-shaped widget draws something in.
	minBarW = 4
	// minBarH is the smallest height at which a bar-shaped widget still shows a
	// bar rather than collapsing to nothing.
	minBarH = 1
)

// drawSpans writes spans left to right on row y between x0 and x1, stopping
// before x1, and returns the column it stopped at.
//
// It is the clipped, allocation-free counterpart of buffer.SetSpans, which stops
// at the BUFFER's right edge rather than at a widget's. A measurement widget's
// label must not be able to spill into its neighbour, and buffer.SetSpans cannot
// express that.
//
// The wide-glyph rules are buffer's: zero-width runes are dropped, a double-width
// rune is written as a glyph cell plus a buffer.ContinuationCell in the SAME
// style, and a rune that does not fit ends the run.
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
			case x+w > x1:
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
// wider, ends with the one-cell truncation marker in the final cell. It returns the
// column just past what it wrote, so a caller can continue after a clipped run.
//
// It is the allocation-free counterpart of buffer.Truncate for the frame path,
// with the same rules: the marker takes the style of the last span, content that
// already fits is drawn whole, and a double-width rune is never split.
func drawSpansCapped(buf *buffer.Buffer, x0, x1, y int, spans []buffer.Span, mark rune) int {
	if x1 <= x0 {
		return x0
	}
	if buffer.SpansWidth(spans) <= x1-x0 {
		return drawSpans(buf, x0, x1, y, spans)
	}
	if x1-x0 == 1 {
		buf.SetCell(x0, y, lastSpanStyle(spans).Resolved().Cell(mark))
		return x1
	}
	drawSpans(buf, x0, x1-1, y, spans)
	buf.SetCell(x1-1, y, lastSpanStyle(spans).Resolved().Cell(mark))
	return x1
}

// drawText writes s into the cells [x0, x1) on row y in st and returns the column
// it stopped at.
func drawText(buf *buffer.Buffer, x0, x1, y int, s string, st buffer.Style) int {
	var one [1]buffer.Span
	one[0] = buffer.NewSpan(s, st)
	return drawSpans(buf, x0, x1, y, one[:])
}

// lastSpanStyle returns the style a truncation marker wears: that of the last
// non-empty input span.
func lastSpanStyle(spans []buffer.Span) buffer.Style {
	for i := len(spans) - 1; i >= 0; i-- {
		if spans[i].Text != "" {
			return spans[i].Style
		}
	}
	return buffer.DefaultStyle
}

// paintRow fills row in bg and then writes spans into it, offset by lead cells.
// Every row-shaped widget in this package starts here, because ADR 0007 §1 rule 3
// requires the whole rect to be repainted before content goes into it.
func paintRow(buf *buffer.Buffer, row buffer.Rect, spans []buffer.Span, lead int, mark rune, bg buffer.Style) {
	if row.Empty() {
		return
	}
	buf.FillRect(row, bg.Resolved().Blank())
	drawSpansCapped(buf, row.X+lead, row.Right(), row.Y, spans, mark)
}

// fillRow fills row in st, which is how a bar's unfilled track is painted.
func fillRow(buf *buffer.Buffer, row buffer.Rect, st buffer.Style) {
	if row.Empty() {
		return
	}
	buf.FillRect(row, st.Resolved().Blank())
}

// ratioOf returns v as a fraction of max, clamped to [0, 1].
//
// It is the whole of this package's value-degradation policy, in one function,
// because five widgets each writing their own clamp is how three of them end up
// disagreeing about what a NaN does:
//
//   - v that is NaN, or max that is NaN, or max <= 0: 0. A zero or negative
//     maximum means "there is no scale to measure against", and drawing an empty
//     bar is honest where dividing would be a lie.
//   - v below zero: 0, and v above max: 1.
//   - v infinite: 1 if max is positive, because an infinite measurement is at
//     least as large as any finite maximum.
func ratioOf(v, max float64) float64 {
	if max <= 0 || v != v {
		return 0
	}
	if v <= 0 {
		return 0
	}
	r := v / max
	if r > 1 {
		return 1
	}
	if r != r {
		return 0
	}
	return r
}

// numDigits writes v into dst with the given number of decimal places and returns
// the filled prefix.
//
// It exists so a widget can print a number without fmt: fmt.Sprintf allocates, and
// every widget here has to print numbers on the frame path. A NaN is written as
// "nan" rather than as a cell of nonsense, and a negative value keeps its sign —
// a bar clamps, but a printed number reports what was actually measured.
func numDigits(dst []byte, v float64, decimals int) []byte {
	if v != v {
		return append(dst, 'n', 'a', 'n')
	}
	if v < 0 {
		dst = append(dst, '-')
		v = -v
	}
	scale := 1.0
	for i := 0; i < decimals; i++ {
		scale *= 10
	}
	// The float64 -> int64 conversion of an infinite value is undefined in Go, so an
	// infinite measurement is printed as a large finite one rather than as
	// whatever the conversion happened to produce.
	if v > 1e15 {
		v = 1e15
	}
	scaled := int64(v*scale + 0.5)
	whole := scaled / int64(scale)
	frac := scaled % int64(scale)

	var scratch [24]byte
	i := len(scratch)
	for whole > 0 {
		i--
		scratch[i] = byte('0' + whole%10)
		whole /= 10
	}
	if i == len(scratch) {
		i--
		scratch[i] = '0'
	}
	dst = append(dst, scratch[i:]...)
	if decimals > 0 {
		dst = append(dst, '.')
		div := int64(scale)
		for d := decimals - 1; d > 0; d-- {
			div /= 10
			dst = append(dst, byte('0'+frac/div%10))
		}
		dst = append(dst, byte('0'+frac%10))
	}
	return dst
}

// putNum writes v with the given number of decimals into the cells [x0, x1) on
// row y and returns the column just past the last cell written. It allocates
// nothing: the digits go into a stack array and each is written as one cell.
func putNum(buf *buffer.Buffer, x0, x1, y int, v float64, decimals int, st buffer.Style) int {
	if x1 <= x0 {
		return x0
	}
	var dst [32]byte
	d := numDigits(dst[:0], v, decimals)
	return putDigits(buf, x0, x1, y, d, st)
}

// putDigits writes the ASCII bytes d into the cells [x0, x1) on row y and returns
// the column just past the last one. One cell per byte, which is what numDigits
// guarantees.
func putDigits(buf *buffer.Buffer, x0, x1, y int, d []byte, st buffer.Style) int {
	c := st.Resolved()
	x := x0
	for i := range d {
		if x >= x1 {
			break
		}
		buf.SetCell(x, y, c.Cell(rune(d[i])))
		x++
	}
	return x
}

// chromeInset returns how many cells blk consumes on each side: its border, if it
// can draw one, plus its padding.
func chromeInset(blk *block.Block) int {
	inset := blk.Padding
	if blk.Border != buffer.BorderNone {
		inset++
	}
	return inset
}

// minWhole returns the whole-widget size carrying chrome of blk's thickness on
// each side and an interior of at least w by h cells.
//
// MinSize is the WHOLE widget including its own chrome (ADR 0007 §2), and a bar
// widget's minimum is meaningless without it: a bordered bar needs four more cells
// than a borderless one on each axis.
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

// eighthsRune returns the block glyph for a fill level in [0, 1], which is what
// gives a one-row chart its vertical resolution: eight levels of eighths.
func eighthsRune(level float64) rune {
	if level <= 0 {
		return eighths[0]
	}
	if level >= 1 {
		return eighths[len(eighths)-1]
	}
	// Round to nearest eighth: a chart that truncates reads as lower than it is.
	i := int(level*float64(len(eighths)) + 0.5)
	if i < 0 {
		i = 0
	}
	if i >= len(eighths) {
		i = len(eighths) - 1
	}
	return eighths[i]
}

// barRune returns the glyph for a bar's fractional cell, which is how a bar ends
// mid-cell: a full block, or the left half of one for the last quarter-cell.
//
// A bar drawn with whole cells alone would quantise to eight levels, which is
// enough for a progress bar and visibly wrong for a 200-cell one.
func barRune(frac float64) rune {
	switch {
	case frac >= 0.75:
		return fullBlock
	case frac >= 0.5:
		return halfBlockLeft
	case frac >= 0.25:
		return eighths[1]
	default:
		return eighths[0]
	}
}
