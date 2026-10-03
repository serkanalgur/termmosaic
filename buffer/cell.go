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

// ContinuationCell returns the right half of a double-width glyph in the given
// style: a cell that occupies no column of its own and carries no rune.
//
// SetString is the supported way to write a wide glyph, and it is the only code
// that sets the private continuation flag. This constructor exists for the
// headless screen model, which must reproduce that cell when it interprets a
// frame written by someone else's terminal.
func ContinuationCell(fg, bg Colour, attr Attr) Cell {
	return Cell{Ch: continuationRune, FG: fg, BG: bg, Attr: attr, flags: flagContinuation}
}

// NewCell returns a Cell with the given rune and styling.
func NewCell(ch rune, fg, bg Colour, attr Attr) Cell {
	return Cell{Ch: ch, FG: fg, BG: bg, Attr: attr}
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

// cellBytes returns the cell's backing memory as bytes for the row-wise
// comparison in the diff.
//
// This is the only place in the codebase where a []Cell is reinterpreted as
// []byte, as ADR 0002 requires. It is sound only because TestCellHasNoPadding
// guarantees there is no undefined padding to compare.
func cellBytes(c *Cell) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(c)), unsafe.Sizeof(*c))
}

// RowBytes returns n consecutive cells' backing memory, starting at index from.
// Cells in the buffer's top level are contiguous in a single slice, so a row
// segment is one contiguous span and a single bytes.Equal covers it.
//
// from is an index into the flat cell slice, i.e. row*stride + x. Only a
// top-level buffer (stride == Width) has a row as a single contiguous span;
// sub-buffers carry a stride, so callers must pass already-flattened
// coordinates and must not use RowBytes for them. The diff only ever runs
// against top-level buffers.
func RowBytes(cells []Cell, from, n int) []byte {
	if n <= 0 || from < 0 || from+n > len(cells) {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&cells[from])), n*int(unsafe.Sizeof(Cell{})))
}
