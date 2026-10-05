package data

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/virtual"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds and constants for List, per ADR 0007 §1 rule 5: thresholds
// are local named constants rather than framework vocabulary.
const (
	// markerPad is the one space between the selection marker and the item
	// text. It exists so the marker reads as a gutter rather than as part of the
	// first item's label.
	markerPad = 1
	// wheelLines is how many rows one wheel notch scrolls. Three is the
	// convention every scroller converges on and, more importantly, it is a
	// List POLICY rather than something virtual/ should decide for four
	// widgets (ADR 0007 §2).
	wheelLines = 3
	// scrollbarW is the width of the vertical scrollbar, in cells.
	scrollbarW = 1
	// minListW is the narrowest interior that shows a marker, a little text and a
	// scrollbar.
	minListW = 8
	// minListH is the number of interior rows below which a list is not worth
	// showing at all.
	minListH = 1
	// thumbRune is the scrollbar thumb glyph: U+2588 FULL BLOCK, one cell wide.
	// It is a Block Elements character rather than a box-drawing one, which is
	// why it is legal here — buffer/border.go owns the border table, not the
	// texture table. An ASCII terminal gets a '#' from List.Ascii instead.
	thumbRune = '█'
	// asciiThumb is the scrollbar thumb on a terminal without Unicode. '#' is
	// one cell wide like thumbRune, so the scrollbar's arithmetic is identical on
	// both rungs.
	asciiThumb = '#'
)

// defaultMarker is the glyph in the selection gutter. It is U+203A, one cell
// wide, and it is the reason a selected row is identifiable with no colour at
// all: colour is an enhancement here, never the signal.
var defaultMarker = "›"

// ListItem is one row of a List: a label, optional styled spans, and a style.
//
// A plain string is the common case and Spans is the escape hatch, so an
// application styles one row differently without giving up the convenient
// literal for the other ninety-nine.
type ListItem struct {
	// Label is the item's text. It is used when Spans is empty, and an empty
	// Label with empty Spans is a legal blank row.
	Label string
	// Spans overrides Label when it has any cell width, which is what lets one
	// row carry several styles.
	Spans []buffer.Span
	// Style is the item's rendition. An unset value resolves to ItemStyle.
	Style buffer.Style
}

// listRow is a ListItem normalised once, at SetItems time.
//
// Normalising here rather than in Draw is the whole of ADR 0008 §4 for this
// widget: building a one-span slice per item per frame would allocate once per
// visible row per frame, so the content is prepared when the DATA changes and
// Draw only reads it.
type listRow struct {
	spans []buffer.Span
	style buffer.Style
}

// List is a selectable, scrollable list of items.
//
// It is Focusable: keys are consumed only while it has focus, and a press
// inside its rectangle takes focus as well as selecting, so a click is a
// complete interaction without the application writing a click handler.
//
// # Cost
//
// O(visible rows) per frame, whatever the item count. The scroll arithmetic is
// virtual.Model's and the row count is derived from the interior height with
// geometry.ClampCount; nothing in Draw or Handle touches the items slice beyond
// the rows it is painting. BenchmarkList100K and TestListHundredThousandItems
// are the evidence.
//
// # Key contract
//
// Consumed only while focused. Every key also scrolls the selection into view,
// which is virtual's ScrollIntoView rather than a per-widget re-centring rule
// (ADR 0007 §6 rule 2: clamp, do not recentre).
//
//	up / down       move the selection by one row
//	page up/down    move it by a screen, less one row of overlap
//	home / end      first / last row
//	enter, space    activate the selection, calling OnActivate
//	wheel up/down   scroll WITHOUT moving the selection
//	press           select the pressed row and take focus
//
// KeyTab is NOT consumed, so a form can move focus out of a list with the
// keyboard exactly as it moves out of a text input.
type List struct {
	blk    *block.Block
	bounds buffer.Rect
	rows   []listRow

	// vm is the scroll engine. It owns the count, the offset and the viewport
	// arithmetic; List owns nothing about it beyond when to resize it.
	vm *virtual.Model

	// selected is the selected item index, or -1 when there is no selection.
	// A negative value is legal and means every row renders unselected, which is
	// what a read-only list wants.
	selected int

	// focused reports whether this list has focus.
	focused bool

	// Marker is the selection gutter glyph, and MarkerStyle its rendition. An
	// empty Marker disables the gutter entirely, which costs two cells and is
	// sometimes the right trade at a narrow width.
	Marker      string
	MarkerStyle buffer.Style

	// ItemStyle is the rendition of an ordinary row; a ListItem's own Style wins
	// when set.
	ItemStyle buffer.Style
	// SelectedStyle is the rendition of the selected row's background and of its
	// text. It should carry an attribute as well as a colour, because the marker
	// is the colour-independent signal and the style is only reinforcement.
	SelectedStyle buffer.Style
	// ScrollbarStyle is the rendition of the scrollbar thumb. The thumb's
	// POSITION is the signal; its colour is not.
	ScrollbarStyle buffer.Style

	// Scrollbar draws the vertical position indicator. It is a field rather than
	// always-on because the gutter and the scrollbar are the two things a
	// narrow list gives up first.
	Scrollbar bool

	// OnActivate, when set, is called with the selected index on Enter or Space.
	// It is called with an index in [0, Count).
	OnActivate func(int)

	// regions is the width budget: the gutter and the scrollbar compete for the
	// same cells, and the gutter wins because losing selection would be worse
	// than losing position. Built once, per ADR 0007 §2.
	regions []geometry.Region
	// keep is geometry.Budget's cached answer for the current rect. It is
	// recomputed only when the rect differs, because Budget allocates (ADR 0007
	// §3).
	keep []bool
	// markerW and barW are keep resolved into the two widths the row painter
	// uses, so Draw reads integers instead of indexing a slice of booleans.
	markerW, barW int
	// cachedRect is the interior the budget above was computed for.
	cachedRect buffer.Rect
	// mark and markerRune are the truncation marker and the gutter glyph for the
	// current ASCII rung, resolved once per adapt rather than per row. thumbRune
	// is the scrollbar's, which follows the same rung.
	mark, markerRune, thumbRune rune
}

// NewList returns a List sized r holding items.
func NewList(r buffer.Rect, items ...ListItem) *List {
	l := &List{
		blk:      block.New(r),
		bounds:   r,
		vm:       virtual.New(len(items)),
		selected: 0,
		Marker:   defaultMarker,
		regions: []geometry.Region{
			{Size: buffer.StringWidth(defaultMarker) + markerPad, Prio: geometry.PrioHigh},
			{Size: scrollbarW, Prio: geometry.PrioLow},
		},
	}
	l.blk.SetBounds(r)
	l.SetItems(items)
	return l
}

// SetItems replaces the collection and re-clamps the offset.
//
// It is the only way to change the content, so the row styles are normalised
// here rather than in Draw, and so a count change is always followed by the
// clamp virtual.Model owns.
func (l *List) SetItems(items []ListItem) {
	rows := make([]listRow, len(items))
	for i := range items {
		row := listRow{style: items[i].Style}
		if buffer.SpansWidth(items[i].Spans) > 0 {
			row.spans = items[i].Spans
		} else if items[i].Label != "" {
			row.spans = []buffer.Span{buffer.NewSpan(items[i].Label, items[i].Style)}
		}
		rows[i] = row
	}
	l.rows = rows
	l.vm.SetCount(len(rows))
	if l.selected >= len(rows) {
		l.selected = len(rows) - 1
	}
	if l.selected < 0 && len(rows) > 0 {
		l.selected = 0
	}
	l.vm.ScrollIntoView(l.selected)
}

// Items returns the number of items. It is the count the engine works from, so
// it is O(1) whatever the collection size.
func (l *List) Items() int { return l.vm.Count() }

// Item returns item i's normalised content for a caller that wants to read the
// rows back, or nil when i is out of range. The result aliases List's storage.
func (l *List) Item(i int) []buffer.Span {
	if i < 0 || i >= len(l.rows) {
		return nil
	}
	return l.rows[i].spans
}

// Bounds returns the list's rectangle, safe to call before the first Draw.
func (l *List) Bounds() buffer.Rect { return l.bounds }

// SetBounds sets the list's rectangle. The budget cache is keyed on it, so the
// next Draw re-decides which chrome survives.
func (l *List) SetBounds(r buffer.Rect) {
	l.bounds = r
	l.blk.SetBounds(r)
}

// Block returns the block that draws this list's border, title, padding and
// background, so a caller can configure the chrome without List re-exporting
// every Block method. It is the documented composition point of ADR 0008 §2: this
// widget spells no border rune and invents no title threshold.
func (l *List) Block() *block.Block { return l.blk }

// Focused reports whether the list has focus.
func (l *List) Focused() bool { return l.focused }

// SetFocused gives or removes focus. A list keeps its selection either way:
// losing focus is not a reason to deselect.
func (l *List) SetFocused(v bool) { l.focused = v }

// MinSize returns the smallest list that shows a marker, some text and a
// scrollbar: a whole-widget size including the border and padding this list is
// configured with.
func (l *List) MinSize() buffer.Size { return minWhole(l.blk, minListW, minListH) }

// Selected returns the selected index, or -1 when there is no selection.
func (l *List) Selected() int { return l.selected }

// Select moves the selection to i, clamped into the collection, and scrolls it
// into view. A count of zero leaves no selection.
func (l *List) Select(i int) {
	if l.vm.Count() == 0 {
		l.selected = -1
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= l.vm.Count() {
		i = l.vm.Count() - 1
	}
	l.selected = i
	l.vm.ScrollIntoView(i)
}

// Offset returns the index of the first visible row.
func (l *List) Offset() int { return l.vm.Offset() }

// SetOffset scrolls to row i, clamped to what the collection allows.
func (l *List) SetOffset(i int) { l.vm.SetOffset(i) }

// Invalidate satisfies termmosaic.Widget: it drops the layout cache so the next
// Draw re-derives it.
//
// The widget repaints its whole rectangle every frame, so there is no dirty region
// to mark — but there IS state derived from the rect, and this is the documented
// way to say it is stale. A caller that changes a field the layout depends on
// (Scrollbar, Header, Status, Marker) calls Invalidate; without it the change lands
// whenever the rect next changes, which is the "broken for exactly one frame and
// repaired by the next" shape ADR 0007 §3 describes for a cache keyed on the wrong
// thing.
func (l *List) Invalidate() {
	l.cachedRect = buffer.Rect{}
}

// Handle consumes navigation keys while focused and mouse events inside its
// own rectangle, and reports whether it took them.
func (l *List) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventKey:
		if !l.focused {
			return false
		}
		return l.handleKey(ev)
	case termmosaic.EventMouse:
		return l.handleMouse(ev)
	default:
		return false
	}
}

// handleKey is the key contract, one case per documented binding.
func (l *List) handleKey(ev termmosaic.Event) bool {
	// The engine needs to know the viewport before it can scroll a selection into
	// it, and an application may handle a key before the first Draw. Resize is
	// O(1) and allocates nothing, so doing it here costs nothing and removes the
	// ordering dependency.
	l.vm.Resize(l.interior())
	switch ev.Key {
	case termmosaic.KeyUp:
		l.move(-1)
		return true
	case termmosaic.KeyDown:
		l.move(1)
		return true
	case termmosaic.KeyPageUp:
		l.move(-l.vm.Visible() + 1)
		return true
	case termmosaic.KeyPageDown:
		l.move(l.vm.Visible() - 1)
		return true
	case termmosaic.KeyHome:
		l.Select(0)
		return true
	case termmosaic.KeyEnd:
		l.Select(l.vm.Count() - 1)
		return true
	case termmosaic.KeyEnter, termmosaic.KeySpace:
		if l.OnActivate != nil && l.selected >= 0 {
			l.OnActivate(l.selected)
		}
		return true
	}
	return false
}

// move moves the selection by delta rows, which may be negative. A delta of zero
// is treated as one, because "the key did nothing" is never what a navigation key
// means.
func (l *List) move(delta int) {
	if delta == 0 {
		delta = 1
	}
	if l.selected < 0 {
		l.selected = 0
	}
	l.Select(l.selected + delta)
}

// handleMouse consumes a press inside the list and a wheel notch over it.
//
// A press selects the pressed row and takes focus, so the application needs no
// click handler of its own; a press OUTSIDE the list is left for an ancestor.
// A wheel notch scrolls without moving the selection, which is the difference
// between scrolling and navigating.
func (l *List) handleMouse(ev termmosaic.Event) bool {
	if !l.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
		return false
	}
	l.vm.Resize(l.interior())
	switch ev.Mouse.Button {
	case termmosaic.MouseWheelUp:
		l.vm.LineUp(wheelLines)
		return true
	case termmosaic.MouseWheelDown:
		l.vm.LineDown(wheelLines)
		return true
	}
	if ev.Mouse.Action != termmosaic.MousePress || ev.Mouse.Button != termmosaic.MouseLeft {
		return false
	}
	i, ok := l.vm.ItemAt(ev.Mouse.Y - l.interior().Y)
	if !ok {
		return false
	}
	l.focused = true
	l.Select(i)
	return true
}

// interior returns the rectangle rows are painted into: the block's interior,
// minus the scrollbar column when one is drawn.
func (l *List) interior() buffer.Rect { return l.blk.Interior() }

// Draw paints the block's chrome and then the visible rows.
//
// It is total and allocation-free: the budget answer is cached against the rect
// it was computed for (ADR 0007 §3), the rows are read from the data prepared by
// SetItems, and nothing here builds a slice or formats a string.
func (l *List) Draw(buf *buffer.Buffer) {
	r := l.bounds
	if r.Empty() {
		return
	}
	l.blk.Draw(buf)
	in := l.interior()
	if in.Empty() {
		return
	}

	if in != l.cachedRect {
		l.adapt(in)
	}
	l.mark = truncMark(l.blk.Ascii)

	// Resize the engine every frame: it is O(1) and it is what clamps the offset
	// when the terminal shrinks (ADR 0007 §6 rule 2).
	l.vm.Resize(in)

	view := buffer.Rect{X: in.X, Y: in.Y, W: in.W - l.barW, H: in.H}
	list := l
	l.vm.ForEach(view, buf, list.drawRow)

	if l.barW > 0 {
		l.drawScrollbar(buf, in)
	}
}

// adapt recomputes everything derived from the interior: which chrome survives
// the budget, and where the rows go.
//
// It runs at most once per distinct interior, so a drag producing eighteen
// different heights recomputes eighteen times and a steady frame computes
// nothing (ADR 0007 §3).
func (l *List) adapt(in buffer.Rect) {
	l.cachedRect = in
	// The gutter's cost is a function of the Marker, which is content rather than
	// configuration, so the region statement is refreshed with it.
	if len(l.regions) > 0 {
		l.regions[0].Size = buffer.StringWidth(l.Marker) + markerPad
	}
	l.mark = truncMark(l.blk.Ascii)
	l.thumbRune = thumbRune
	if l.blk.Ascii {
		l.thumbRune = asciiThumb
	}
	l.markerRune = firstRune(l.Marker)
	l.keep = geometry.Budget(l.regions, in.W)
	l.markerW, l.barW = 0, 0
	if len(l.keep) == len(l.regions) {
		if l.keep[0] && l.Marker != "" {
			l.markerW = l.regions[0].Size
		}
		if l.keep[1] && l.Scrollbar {
			l.barW = scrollbarW
		}
	}
	// A gutter wider than the interior would leave no room for content at all,
	// so an interior too narrow for it drops it however the budget voted.
	if l.markerW >= in.W {
		l.markerW = 0
	}
}

// drawRow paints one row: the selected row's background across the whole row,
// the marker in the gutter, and the item's content clipped to what is left.
//
// The item's own style wins over ItemStyle when it is set, and the selected row
// takes SelectedStyle outright — a selection that only recoloured the background
// would leave the text unreadable, which is worse than no selection at all.
func (l *List) drawRow(dst *buffer.Buffer, row buffer.Rect, i int) {
	item := l.rows[i]
	bg := l.ItemStyle
	st := item.style
	if st.IsUnset() {
		st = l.ItemStyle
	}
	selected := i == l.selected
	if selected {
		bg = l.SelectedStyle
		st = l.SelectedStyle
	}
	// st reaches the text of a SINGLE-span item; an item carrying several spans keeps
	// its own styles on this row, which is paintRow's documented exception and the
	// reason a deliberately multi-styled row is the author's to keep.
	paintRow(dst, row, item.spans, l.markerW, l.mark, bg, st)
	if l.markerW > 0 && selected && l.markerRune != 0 {
		mst := l.MarkerStyle
		if mst.IsUnset() {
			// An unset marker style follows the selected row rather than
			// resolving to the terminal defaults, which on a highlighted row
			// would be dark text on a dark background.
			mst = l.SelectedStyle
		}
		dst.SetCell(row.X, row.Y, mst.Resolved().Cell(l.markerRune))
	}
}

// drawScrollbar paints the vertical position thumb on the rightmost column of
// the interior.
//
// The thumb is a run of full-block glyphs and the track is left as the background
// the block painted, so the position is legible with no colour at all. The thumb
// is sized by the visible fraction of the collection and placed by the offset,
// both computed here with integer arithmetic so no float ever reaches a cell.
// A collection that fits has no thumb, which is itself the "everything is
// shown" signal.
func (l *List) drawScrollbar(buf *buffer.Buffer, in buffer.Rect) {
	if l.vm.MaxOffset() <= 0 {
		return
	}
	h := in.H
	if h < 1 {
		return
	}
	x := in.Right() - 1

	thumbH := h * l.vm.Visible() / l.vm.Count()
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > h {
		thumbH = h
	}
	start := 0
	if slack := h - thumbH; slack > 0 {
		start = slack * l.vm.Offset() / l.vm.MaxOffset()
	}
	c := l.ScrollbarStyle.Resolved().Cell(l.thumbRune)
	for y := 0; y < thumbH; y++ {
		buf.SetCell(x, in.Y+start+y, c)
	}
}

var (
	_ termmosaic.Widget      = (*List)(nil)
	_ termmosaic.Focusable   = (*List)(nil)
	_ termmosaic.Minimizable = (*List)(nil)
)
