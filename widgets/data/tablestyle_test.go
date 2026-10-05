package data

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// This file pins the table's row renditions reaching the CELL TEXT.
//
// drawRow filled the row with ItemStyle or SelectedStyle and then wrote every
// cell through drawCell with the ZERO style, meaning "write the spans' own styles
// verbatim". So the row's rendition reached the background and nothing else: a
// selected row whose SelectedStyle carries a background and an attribute showed
// those on the fill and left the text in whatever the cell's own style said. That
// is List's stated reason for passing the row style to paintRow — "a selection
// that only recoloured the background would leave the text unreadable, which is
// worse than no selection at all" — applied to Table and not to it.
//
// The header is the same defect one row up, and the doc says so: HeaderStyle is
// documented as resolving to HeadingStyle "patched with ItemStyle so the header
// sits on the row background", and no Patch was ever applied. HeadingStyle is
// attribute-only, so the header text resolved to the terminal's own background
// inside a row that had just been filled with ItemStyle.

// cellOfRuneInRow returns the first cell in row y holding r.
func cellOfRuneInRow(buf *buffer.Buffer, r rune, y int) (buffer.Cell, bool) {
	for _, c := range buf.Row(y) {
		if c.Ch == r {
			return c, true
		}
	}
	return buffer.Cell{}, false
}

func TestTableSelectedStyleReachesTheCellText(t *testing.T) {
	sel := buffer.NewStyle(buffer.NewColour(255, 255, 255), buffer.NewColour(0, 0, 128), buffer.AttrReverse)
	tb := plainTable(t, 22, 5, 3)
	tb.SelectedStyle = sel
	tb.Select(1)
	buf := cellBuf(22, 5)
	tb.Draw(buf)

	// Row 1 of the data, which is screen row 3 with a border and a header.
	c, ok := cellOfRuneInRow(buf, 'r', 3)
	if !ok {
		t.Fatalf("the selected row's first cell is not on row 3:\n%q", string(bufRow(buf, 3)))
	}
	if c.FG != sel.FG || c.BG != sel.BG || c.Attr != sel.Attr {
		t.Errorf("selected row's text cell = FG %s BG %s Attr %d, want SelectedStyle FG %s BG %s Attr %d",
			c.FG, c.BG, c.Attr, sel.FG, sel.BG, sel.Attr)
	}
}

func TestTableItemStyleReachesTheCellText(t *testing.T) {
	// The unselected half of the same contract: ItemStyle is documented as the
	// rendition of an ordinary row, and it reached only its background.
	item := buffer.NewStyle(buffer.NewColour(0, 200, 0), buffer.NewColour(0, 0, 40), buffer.AttrFaint)
	tb := plainTable(t, 22, 5, 3)
	tb.ItemStyle = item
	tb.Select(-1)
	buf := cellBuf(22, 5)
	tb.Draw(buf)

	c, ok := cellOfRuneInRow(buf, 'r', 3)
	if !ok {
		t.Fatalf("the row's first cell is not on row 3:\n%q", string(bufRow(buf, 3)))
	}
	if c.FG != item.FG || c.BG != item.BG || c.Attr != item.Attr {
		t.Errorf("ordinary row's text cell = FG %s BG %s Attr %d, want ItemStyle FG %s BG %s Attr %d",
			c.FG, c.BG, c.Attr, item.FG, item.BG, item.Attr)
	}
}

func TestTableUnselectedRowIsNotStampedWithSelectedStyle(t *testing.T) {
	// The complement, so the fix cannot be "every row takes SelectedStyle".
	sel := buffer.NewStyle(buffer.NewColour(255, 255, 255), buffer.NewColour(0, 0, 128), buffer.AttrReverse)
	tb := plainTable(t, 22, 6, 3)
	tb.SelectedStyle = sel
	tb.Select(0)
	buf := cellBuf(22, 6)
	tb.Draw(buf)

	c, ok := cellOfRuneInRow(buf, 'r', 4)
	if !ok {
		t.Fatalf("the second row's first cell is not on row 4:\n%q", string(bufRow(buf, 4)))
	}
	if c.Attr&buffer.AttrReverse != 0 {
		t.Errorf("the unselected row's text carries AttrReverse from SelectedStyle")
	}
}

func TestTableMultiSpanCellKeepsItsOwnStyles(t *testing.T) {
	// drawCell's documented exception, restated at the call site: a cell carrying
	// several spans keeps them whatever the row rendition is, because flattening it
	// would have to build a string on the frame path. TestTreeMultiSpanLabelKeepsItsOwnStyles
	// is the List/Tree half of this same rule.
	sel := buffer.NewStyle(buffer.NewColour(255, 255, 255), buffer.NewColour(0, 0, 128), buffer.AttrReverse)
	tb := NewTable(buffer.Rect{W: 22, H: 5}, named("ID", 10))
	tb.SelectedStyle = sel
	tb.SetRows([]Row{{Cells: []Cell{{Spans: []buffer.Span{
		{Text: "cc", Style: buffer.NewStyle(buffer.NewColour(255, 0, 0), buffer.DefaultColour, 0)},
		{Text: "dd", Style: buffer.NewStyle(buffer.NewColour(0, 255, 0), buffer.DefaultColour, 0)},
	}}}}})
	tb.Select(0)
	buf := cellBuf(22, 5)
	tb.Draw(buf)

	red, okR := cellOfRuneInRow(buf, 'c', 0)
	green, okG := cellOfRuneInRow(buf, 'd', 0)
	if !okR || !okG {
		t.Fatalf("the multi-span cell is not on row 0: c %v, d %v", okR, okG)
	}
	wantRed := buffer.NewColour(255, 0, 0)
	wantGreen := buffer.NewColour(0, 255, 0)
	if red.FG != wantRed || green.FG != wantGreen {
		t.Errorf("multi-span cell = FG %s and %s, want its own %s and %s",
			red.FG, green.FG, wantRed, wantGreen)
	}
}

func TestTableHeaderStyleIsPatchedWithItemStyle(t *testing.T) {
	// The doc's Patch, applied. A header whose text resolves to the terminal's own
	// background sits on the terminal background inside a row filled with
	// ItemStyle, so with a non-default ItemStyle background the header text was on
	// the wrong surface.
	itemBG := buffer.NewColour(0, 16, 64)
	tb := plainTable(t, 22, 5, 3)
	tb.ItemStyle = buffer.NewStyle(buffer.DefaultColour, itemBG, 0)
	buf := cellBuf(22, 5)
	tb.Draw(buf)

	// The header text "ID" is on screen row 1.
	c, ok := cellOfRuneInRow(buf, 'I', 1)
	if !ok {
		t.Fatalf("the header text is not on row 1:\n%q", string(bufRow(buf, 1)))
	}
	if c.BG != itemBG {
		t.Errorf("header text cell BG = %s, want ItemStyle's %s", c.BG, itemBG)
	}
	if c.Attr&buffer.AttrBold == 0 {
		t.Errorf("header text cell Attr = %d, want HeadingStyle's AttrBold", c.Attr)
	}
}

func TestTableHeaderStyleSetByTheAuthorIsUsedVerbatim(t *testing.T) {
	// The complement for the header: an explicit HeaderStyle is the author's
	// choice and is not patched over.
	hdr := buffer.NewStyle(buffer.NewColour(1, 2, 3), buffer.NewColour(4, 5, 6), buffer.AttrUnderline)
	tb := NewTable(buffer.Rect{W: 22, H: 5},
		Column{Title: []buffer.Span{buffer.NewSpan("ID", buffer.DefaultStyle)}, Width: 6, HeaderStyle: hdr})
	tb.Header = true
	tb.ItemStyle = buffer.NewStyle(buffer.DefaultColour, buffer.NewColour(0, 16, 64), 0)
	tb.SetRows(tableRows(2))
	buf := cellBuf(22, 5)
	tb.Draw(buf)

	c, ok := cellOfRuneInRow(buf, 'I', 0)
	if !ok {
		t.Fatalf("the header text is not on row 0:\n%q", string(bufRow(buf, 0)))
	}
	if c.FG != hdr.FG || c.BG != hdr.BG || c.Attr != hdr.Attr {
		t.Errorf("header text cell = FG %s BG %s Attr %d, want HeaderStyle FG %s BG %s Attr %d",
			c.FG, c.BG, c.Attr, hdr.FG, hdr.BG, hdr.Attr)
	}
}

func TestTableSelectionAndHeaderBothReadOnTheirOwnBackground(t *testing.T) {
	// The F3/F4 interaction, asserted as a whole frame rather than as two cells:
	// with the row rendition now reaching the text, the header must still sit on
	// the ItemStyle surface and the selected row must still take SelectedStyle
	// outright — neither fix may swallow the other.
	item := buffer.NewStyle(buffer.DefaultColour, buffer.NewColour(0, 16, 64), 0)
	sel := buffer.NewStyle(buffer.NewColour(255, 255, 255), buffer.NewColour(0, 0, 128), buffer.AttrReverse)
	tb := plainTable(t, 22, 6, 3)
	tb.ItemStyle = item
	tb.SelectedStyle = sel
	tb.Select(0)
	buf := cellBuf(22, 6)
	tb.Draw(buf)

	hdr, ok := cellOfRuneInRow(buf, 'I', 1)
	if !ok {
		t.Fatalf("the header text is not on row 1:\n%q", string(bufRow(buf, 1)))
	}
	if hdr.BG != item.BG {
		t.Errorf("header BG = %s, want ItemStyle's %s", hdr.BG, item.BG)
	}
	body, ok := cellOfRuneInRow(buf, 'r', 2)
	if !ok {
		t.Fatalf("a body row's text is not on row 2:\n%q", string(bufRow(buf, 2)))
	}
	if body.BG != sel.BG || body.Attr != sel.Attr {
		t.Errorf("selected row BG %s Attr %d, want SelectedStyle BG %s Attr %d",
			body.BG, body.Attr, sel.BG, sel.Attr)
	}
	assertDifferent(t, "header background and selected-row background", hdr.BG.String(), body.BG.String())
}

// bufRow is row y of buf as a string, for a failure message that should show the
// whole row rather than one cell of it.
func bufRow(buf *buffer.Buffer, y int) string {
	var b strings.Builder
	for _, c := range buf.Row(y) {
		b.WriteRune(c.Ch)
	}
	return b.String()
}
