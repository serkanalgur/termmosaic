package geometry

// Align is horizontal placement within a span of cells.
//
// It lives in geometry rather than in buffer because it is a pure enumeration
// over a cell range with no dependency on anything, and geometry is the leaf
// both buffer and layout import. It is the exception that proves the rule the
// rest of the vocabulary follows: shared widget vocabulary lives in the lowest
// package that can hold it without an import cycle — geometry for cell-free
// geometry, buffer for anything that touches Colour, Attr, Cell or a rune.
type Align uint8

const (
	// AlignLeft places content against the leading edge. It is the default, and
	// is the zero value so a zero Align needs no special-casing.
	AlignLeft Align = iota
	// AlignCenter centres content within the span, with any odd cell left of
	// centre.
	AlignCenter
	// AlignRight places content against the trailing edge.
	AlignRight
)

// Offset returns the x offset, within a span of avail cells, at which content of
// cell width w begins.
//
// It is the shared answer to "where does this go", so that a Block's title and a
// Paragraph's line cannot round differently and leave a composed screen visibly
// misaligned. Three widget authors would otherwise each write the switch.
//
// Rules:
//   - w >= avail gives 0, which clips rather than scrolling content off the left.
//   - AlignCenter leaves any odd cell on the left, which is the reading that
//     matches the glyph tables' own geometry and keeps the arithmetic symmetric
//     with AlignRight.
//   - An out-of-range Align is treated as AlignLeft, matching String: it is
//     layout-derived data and must not panic.
func (a Align) Offset(avail, w int) int {
	if avail <= 0 || w <= 0 {
		return 0
	}
	switch a {
	case AlignCenter:
		return (avail - w) / 2
	case AlignRight:
		if avail-w < 0 {
			return 0
		}
		return avail - w
	default:
		return 0
	}
}

// String returns the alignment's name, for diagnostics and golden-test failure
// messages: "left", "center", "right". An out-of-range value renders in decimal
// rather than panicking, because an Align is layout-derived data and a TUI that
// panics on a rounding error is unusable.
func (a Align) String() string {
	switch a {
	case AlignLeft:
		return "left"
	case AlignCenter:
		return "center"
	case AlignRight:
		return "right"
	default:
		return "align(" + itoa(int(a)) + ")"
	}
}

// itoa is strconv.Itoa without the import, for a function called only on the
// error path. A string conversion of a small int cannot allocate here because
// the result never escapes.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
