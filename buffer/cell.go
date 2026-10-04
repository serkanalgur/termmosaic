// Package buffer implements TermMosaic's cell buffer: the padding-free 16-byte
// Cell, the packed Colour type, the Attr bitmask, and the row-major Buffer that
// widgets draw into.
//
// The representation is fixed by ADR 0002. In short: array-of-structs, not
// struct-of-arrays, and Cell must remain exactly 16 bytes with no interior or
// tail padding, because the tier-1 row skip in the diff compares whole rows as
// bytes. Go does not define the contents of tail padding, so a padded Cell
// would produce phantom dirty rows and visible flicker. TestCellHasNoPadding
// enforces the invariant.
package buffer

import "unsafe"

// Attr is a bitmask of character rendition attributes.
//
// The bits are stable and may be combined with the bitwise operators. Attr
// occupies 2 of Cell's 16 bytes; the remaining 2 carry Cell's flags.
type Attr uint16

// Character rendition attributes. These are the SGR attributes TermMosaic
// emits; they are deliberately the common subset rather than every SGR code
// (no blink, no fraktur, no ideogram).
const (
	// AttrBold requests increased intensity (SGR 1).
	AttrBold Attr = 1 << iota
	// AttrFaint requests decreased intensity (SGR 2).
	AttrFaint
	// AttrItalic requests italic rendition (SGR 3).
	AttrItalic
	// AttrUnderline requests underline (SGR 4).
	AttrUnderline
	// AttrReverse swaps the foreground and background colours (SGR 7).
	AttrReverse
	// AttrStrike requests strikethrough (SGR 9).
	AttrStrike
)

// attrNames maps bits to their short names, used by Attr.String. Index by
// Attr >> iota, so it must stay in sync with the constants above.
var attrNames = [...]string{"bold", "faint", "italic", "underline", "reverse", "strike"}

// Has reports whether every bit in other is set in a.
func (a Attr) Has(other Attr) bool { return a&other == other }

// String returns a stable, human-readable rendering of the set attributes, for
// use in test failure messages and debugging. Bits with no name render as hex.
func (a Attr) String() string {
	if a == 0 {
		return "none"
	}
	var out []byte
	for i, name := range attrNames {
		bit := Attr(1) << i
		if a&bit == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, '|')
		}
		out = append(out, name...)
	}
	return string(out)
}

// flagContinuation marks a Cell as the right half of a double-width glyph.
//
// A wide glyph (CJK, most emoji) occupies two terminal cells: the left cell
// holds the rune and the right cell holds nothing but this flag. Both cells
// are written, and — critically — both must compare equal to the previous
// frame's cells or the row skip will not fire and the glyph will flicker.
// ADR 0002 lists wide characters as unmeasured and deferred; the semantics are
// implemented here, but no benchmark exercises them. Treat as unverified.
const flagContinuation uint16 = 1 << 0

// Cell is one terminal cell: a rune, two colours, an attribute mask, and a
// flags word. It is exactly 16 bytes with no padding, which is the whole point
// — see the package documentation and ADR 0002.
//
// INVARIANT: any change to these fields must preserve unsafe.Sizeof(Cell) ==
// 16, must keep every byte meaningful, and must keep `flags` last and
// 2 bytes wide. TestCellHasNoPadding fails otherwise. A reserved-but-unused
// byte is unsound: bytes.Equal over a row would compare undefined padding.
//
// Style is deliberately NOT embedded here. Style is the 10-byte flat projection
// of the three styling fields; embedding it would make the cell 28+ bytes with
// interior padding and reintroduce the hazard above. Cell.Style and Style.Cell
// convert, and TestCellRemainsSixteenBytesWithStyle fails if that is undone.
//
// ADR 0002 spells the final two bytes as `_ [2]byte`. We spend them on the
// wide-glyph continuation flag instead, at zero extra bytes, because a
// continuation cell has to be distinguishable from an empty one for the diff
// to be correct.
type Cell struct {
	// Ch is the rune to display, or 0 for a continuation cell.
	Ch rune
	// FG is the foreground colour.
	FG Colour
	// BG is the background colour.
	BG Colour
	// Attr is the rendition attribute mask.
	Attr Attr
	// flags is private so the invariant cannot be broken from outside the
	// package without a size change; see flagContinuation.
	flags uint16
}

// DefaultCell is the value of an untouched cell: a space in the terminal's
// default foreground and background, with no attributes.
var DefaultCell = Cell{Ch: ' ', FG: DefaultColour, BG: DefaultColour}

// ContinuationCell returns the right half of a double-width glyph in st: a cell
// that occupies no column of its own and carries no rune.
//
// Its style MUST match the glyph cell it follows, or the pair will not compare
// equal to the previous frame and the row skip will never fire. SetSpans and
// SetString are the supported way to write a wide glyph, and they are the only
// code that sets the private continuation flag with that guarantee. This
// constructor exists for the headless screen model, which must reproduce the
// cell when it interprets a frame written by someone else's terminal.
func ContinuationCell(st Style) Cell {
	return Cell{Ch: continuationRune, FG: st.FG, BG: st.BG, Attr: st.Attr, flags: flagContinuation}
}

// NewCell returns a Cell with the given rune in st. It is the inverse of
// Cell.Style, and neither direction allocates.
func NewCell(ch rune, st Style) Cell {
	return Cell{Ch: ch, FG: st.FG, BG: st.BG, Attr: st.Attr}
}

// Rune returns the rune the cell displays, or 0 if the cell is the continuation
// half of a double-width glyph.
func (c Cell) Rune() rune { return c.Ch }

// IsContinuation reports whether the cell is the right half of a double-width
// glyph, which occupies no cell of its own and must never be written to
// directly.
func (c Cell) IsContinuation() bool { return c.flags&flagContinuation != 0 }

// asContinuation returns c marked as the continuation half of a wide glyph.
// The rune is preserved so a round-tripped cell compares equal to itself, which
// is what keeps the diff from flickering.
func (c Cell) asContinuation() Cell {
	c.flags |= flagContinuation
	return c
}

// continuationRune is the rune a continuation cell reports. Zero is not a
// displayable glyph, so a stray write of it is visually inert.
const continuationRune rune = 0

// cellBytes returns n consecutive cells' backing memory, starting at c, as
// bytes for the row-wise comparison in the diff.
//
// This is the only place in the codebase where cells are reinterpreted as
// bytes, as ADR 0002 requires. It is sound only because TestCellHasNoPadding
// guarantees there is no undefined padding to compare; contiguity is the
// caller's precondition, which is why RowBytes — the only caller — refuses a
// sub-buffer (ADR 0006).
func cellBytes(c *Cell, n int) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(c)), n*int(unsafe.Sizeof(Cell{})))
}

// RowBytes returns n consecutive cells' backing memory, starting at (x0, y) in
// the row y of b, as bytes. It exists for the diff's byte-wise row comparison
// (ADR 0002) and for nothing else.
//
// On a top-level buffer stride == Width, so a row segment is one contiguous
// span and a single bytes.Equal covers it.
//
// It panics if b is a sub-buffer. A view's rows are strided by its parent, so no
// range of its cells is contiguous and there is nothing correct to hand back.
// The correct answer is not to diff a view: compose with SubBuffer, diff at the
// top. This is a TermMosaic bug rather than a user error — the diff is the only
// caller and it only ever receives the renderer's top-level front and back
// buffers — which is why it panics instead of returning nil. A nil return would
// make bytes.Equal(nil, nil) report two different strided rows as equal: a wrong
// answer behind a safe-looking signature. Note the deliberate contrast with
// SetCell, which does not panic on an out-of-range write because its coordinates
// come from widget layout.
//
// Returns nil for an out-of-range or empty range.
func (b *Buffer) RowBytes(y, x0, n int) []byte {
	if b.stride != b.w {
		panic("buffer: RowBytes called on a sub-buffer; a view's rows are strided and are not byte-comparable. Diff top-level buffers. To read a view's cells use Row, or Clip for a dense copy; to stop getting one, do not compose with SubBuffer.")
	}
	if n <= 0 || x0 < 0 || x0+n > b.w || y < 0 || y >= b.h {
		return nil
	}
	// cellBytes is the single place cells are reinterpreted as bytes. The range
	// is contiguous here precisely because the sub-buffer case panicked above.
	return cellBytes(&b.cx[y*b.stride+x0], n)
}
