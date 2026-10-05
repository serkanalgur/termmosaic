package menu

import (
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// This file pins the check glyph and the submenu arrow following the SELECTED
// row when CheckStyle is unset.
//
// The file already identifies this hazard for the marker three lines above
// drawRow's end — "an unset marker style follows the selected row, because a
// marker in ItemStyle on a reversed row would be the one unreadable thing here" —
// and handles it there. The check glyph and the submenu arrow share the same
// checkStyle resolution and were left out of it, so with SelectedStyle carrying a
// background the selected row showed its marker and its label in the focus
// rendition and its check glyph in ItemStyle: the one cell of the highlighted row
// that could not be read.

// checkableMenu returns an open, focused menu over items, sized to leave the
// check column visible.
func checkableMenu(t *testing.T, items ...Item) *Menu {
	t.Helper()
	m := newMenu(t, 40, 12, items...)
	return m
}

// cellAt returns the cell at (x, y) of buf.
func cellAt(buf *buffer.Buffer, x, y int) buffer.Cell { return buf.CellAt(x, y) }

// topColumn returns the leftmost shown column after a Draw, which is the top
// level's. Reading the widget's own layout rather than recomputing it is what
// keeps these assertions about the GLYPH and not about the arithmetic.
func topColumn(t *testing.T, m *Menu) layoutCol {
	t.Helper()
	if len(m.lay.cols) == 0 {
		t.Fatalf("the menu laid out no columns")
	}
	return m.lay.cols[0]
}

// topRowY returns the screen row of the top level's item i, found from the
// column's own item rect and the engine's window rather than counted from zero.
func topRowY(m *Menu, c layoutCol, i int) int {
	return c.itemRect.Y + (i - m.levels[c.depth].vm.Offset())
}

func TestMenuCheckGlyphFollowsTheSelectedRow(t *testing.T) {
	sel := buffer.NewStyle(buffer.NewColour(255, 255, 255), buffer.NewColour(0, 0, 128), buffer.AttrReverse)
	m := checkableMenu(t, Item{Label: "Wrap", Checkable: true, Checked: true})
	m.SelectedStyle = sel
	buf := drawInto(m, 40, 12)

	// The selected row is the top-level row the menu opened on.
	col := topColumn(t, m)
	y := topRowY(m, col, m.SelectedAt(col.depth))
	glyph := cellAt(buf, col.checkX, y)
	if glyph.Ch != m.checkRune {
		t.Fatalf("cell at the check column holds %q, want the checked glyph %q", glyph.Ch, m.checkRune)
	}
	if glyph.FG != sel.FG || glyph.BG != sel.BG || glyph.Attr != sel.Attr {
		t.Errorf("check glyph on the selected row = FG %s BG %s Attr %d, want SelectedStyle FG %s BG %s Attr %d",
			glyph.FG, glyph.BG, glyph.Attr, sel.FG, sel.BG, sel.Attr)
	}
}

func TestMenuUnselectedCheckGlyphKeepsItemStyle(t *testing.T) {
	// The complement: an UNSELECTED row must not wear SelectedStyle's rendition,
	// so the fix is not "always the selected style".
	sel := buffer.NewStyle(buffer.NewColour(255, 255, 255), buffer.NewColour(0, 0, 128), buffer.AttrReverse)
	item := buffer.NewStyle(buffer.NewColour(0, 200, 0), buffer.DefaultColour, 0)
	m := checkableMenu(t,
		Item{Label: "First", Checkable: true, Checked: true},
		Item{Label: "Wrap", Checkable: true, Checked: true},
	)
	m.SelectedStyle = sel
	m.ItemStyle = item
	buf := drawInto(m, 40, 12)

	col := topColumn(t, m)
	y := topRowY(m, col, 1)
	glyph := cellAt(buf, col.checkX, y)
	if glyph.Attr != item.Attr {
		t.Errorf("check glyph on an unselected row carries Attr %d, want ItemStyle's %d", glyph.Attr, item.Attr)
	}
}

func TestMenuCheckStyleSetByTheAuthorAppliesToEveryRow(t *testing.T) {
	// An explicit CheckStyle is the author's choice and is not overridden by the
	// row, exactly as the marker's rule states.
	sel := buffer.NewStyle(buffer.NewColour(255, 255, 255), buffer.NewColour(0, 0, 128), buffer.AttrReverse)
	chk := buffer.NewStyle(buffer.NewColour(9, 9, 9), buffer.NewColour(8, 8, 8), buffer.AttrUnderline)
	m := checkableMenu(t, Item{Label: "Wrap", Checkable: true, Checked: true})
	m.SelectedStyle = sel
	m.CheckStyle = chk
	buf := drawInto(m, 40, 12)

	col := topColumn(t, m)
	y := topRowY(m, col, m.SelectedAt(col.depth))
	glyph := cellAt(buf, col.checkX, y)
	if glyph.FG != chk.FG || glyph.Attr != chk.Attr {
		t.Errorf("check glyph = FG %s Attr %d, want the author's CheckStyle FG %s Attr %d",
			glyph.FG, glyph.Attr, chk.FG, chk.Attr)
	}
}

func TestMenuSubmenuArrowFollowsTheSelectedRow(t *testing.T) {
	sel := buffer.NewStyle(buffer.NewColour(255, 255, 255), buffer.NewColour(0, 0, 128), buffer.AttrReverse)
	m := checkableMenu(t, Item{Label: "New", Items: []Item{{Label: "Deep"}}})
	m.SelectedStyle = sel
	buf := drawInto(m, 40, 12)

	col := topColumn(t, m)
	y := topRowY(m, col, m.SelectedAt(col.depth))
	if col.submenuX <= 0 {
		t.Fatalf("the layout gave no submenu column for a branch item")
	}
	arrow := cellAt(buf, col.submenuX, y)
	if arrow.Ch != m.submenuRune {
		t.Fatalf("cell at the submenu column holds %q, want the submenu glyph %q", arrow.Ch, m.submenuRune)
	}
	if arrow.FG != sel.FG || arrow.BG != sel.BG || arrow.Attr != sel.Attr {
		t.Errorf("submenu arrow on the selected row = FG %s BG %s Attr %d, want SelectedStyle FG %s BG %s Attr %d",
			arrow.FG, arrow.BG, arrow.Attr, sel.FG, sel.BG, sel.Attr)
	}
}
