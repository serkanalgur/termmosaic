package data

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/virtual"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds and constants for Table, per ADR 0007 §1 rule 5.
const (
	// colGap is the one space between adjacent columns. Without it a truncated
	// cell and its neighbour run together and the table becomes unreadable.
	colGap = 1
	// maxAutoWidth caps a content-derived column, in cells. Without a cap, one
	// pathological cell would set the width of its column for every row below
	// it; the column then scrolls like any other and nothing is lost.
	maxAutoWidth = 32
	// minAutoWidth is the narrowest content-derived column: enough for one
	// glyph rather than nothing at all.
	minAutoWidth = 1
	// headerPrio is the header's drop priority. It is high rather than low on
	// purpose: the scrollbar is the thing that goes first, because a column
	// label carries more information than a position indicator.
	headerPrio = geometry.PrioHigh
	// scrollbarPrio is the vertical scrollbar's priority against the selection
	// gutter.
	scrollbarPrio = geometry.PrioLow
	// minTableW is the narrowest interior showing a marker, one column of a
	// readable width and a scrollbar.
	minTableW = 10
	// minTableH is the smallest body a table shows a header and a row in.
	minTableH = 2
	// headerCellsPrio is the header row's own priority within the height budget.
	headerCellsPrio = geometry.PrioHigh
)

// Column describes one column of a Table: how wide it is, how its cells align,
// and how its header and cells are styled.
//
// The three width modes are mutually exclusive and are read in this order: a
// positive Width wins, then a positive Grow, then a measurement of the visible
// rows.
type Column struct {
	// Title is the header cell's content, drawn in HeaderStyle and truncated to
	// the column width like any other cell.
	Title []buffer.Span

	// Width is a FIXED width in cells. Zero or negative means "not fixed", which
	// leaves Grow and then content measurement in charge.
	Width int

	// Grow is this column's weight in the FILL share of whatever cells are left
	// after the fixed and content-derived columns have taken theirs. Zero means
	// the column does not participate in the fill.
	//
	// Fill is the mode that survives a wide terminal: a column with Grow > 0
	// absorbs the extra cells rather than leaving a gap at the right edge.
	Grow int

	// Align places a cell's content within its column. AlignRight is what makes a
	// column of numbers line up, and it is worth saying out loud because the
	// default is AlignLeft.
	Align geometry.Align

	// HeaderStyle is the rendition of the header cell. An unset value resolves to
	// HeadingStyle, which is the framework's named style for exactly this, patched
	// with ItemStyle so the header sits on the row background.
	HeaderStyle buffer.Style

	// CellStyle is the default rendition of this column's cells. It is applied
	// when the cells are NORMALISED, which is SetRows: changing it afterwards has
	// no effect until SetRows is called again, because re-styling per frame would
	// mean rebuilding a span slice per cell per frame (ADR 0008 §4).
	CellStyle buffer.Style
}

// Cell is one table cell's content: a string, or styled spans, or neither.
type Cell struct {
	// Text is the cell's content when Spans is empty.
	Text string
	// Spans overrides Text when it has any cell width.
	Spans []buffer.Span
	// Style is the cell's rendition. An unset value takes the column's CellStyle.
	Style buffer.Style
}

// Row is one table row: one Cell per column, in column order.
//
// A row with fewer cells than there are columns is legal — the missing columns
// draw empty — and so is a row with more, the surplus being unreachable rather
// than a panic. Data arrives from disk, and a malformed row must not take the
// frame down.
type Row struct {
	// Cells are the row's cells in column order.
	Cells []Cell
}

// tableRow is a Row normalised once, at SetRows time.
type tableRow struct {
	cells []tableCell
}

// tableCell is one normalised cell: its spans, with the column's style already
// applied, and their cell width.
type tableCell struct {
	spans []buffer.Span
	width int
}

// Table shows rows in columns, with a header, a selection and horizontal
// scrolling.
//
// It is Focusable on the same terms as List: keys are consumed only while it has
// focus, and a press inside it selects a row and takes focus.
//
// # Column widths
//
// Three modes, resolved per rect in this order:
//
//	Width > 0    fixed, exactly that many cells
//	Grow > 0     a share of the cells left over, solved by layout.Solve rather
//	             than by a private distribution loop
//	otherwise    measured from the VISIBLE ROWS ONLY
//
// Measuring from the visible rows is not an approximation, it is the only
// affordable answer: a 100,000-row table that measured its content would be
// O(item count) on every resize, which ADR 0007 §6 rules out outright. The
// consequence worth stating plainly is that a content-derived column can change
// width as different content scrolls through, so a caller who needs stable widths
// gives explicit Width or Grow values. That is the honest answer rather than a
// hidden cache over the whole collection.
//
// # Cost
//
// O(visible rows × visible columns) per frame, and the same on a resize. It is
// the horizontal axis, the header, the gutter and the scrollbar that are each
// O(1) or O(columns), never O(rows).
//
// # Key contract
//
// Consumed only while focused.
//
//	up / down        move the selection by one row
//	page up/down     move it by a screen, less one row of overlap
//	home / end       first / last row
//	shift+home/end   first / last column
//	left / right     scroll one column horizontally
//	enter, space     activate the selection, calling OnActivate
//	wheel up/down    scroll vertically WITHOUT moving the selection
//	wheel + shift    scroll horizontally
//	press            select the pressed row and take focus
//
// KeyTab is NOT consumed, for the same reason List does not consume it.
type Table struct {
	blk    *block.Block
	bounds buffer.Rect

	cols []Column
	rows []tableRow

	// vm is the vertical scroll engine. Horizontal scrolling is the widget's own
	// arithmetic over columns, because virtual/ is a row engine and bending a
	// shared package to one widget's second axis would be the collision ADR 0007
	// §1 rule 4 exists to prevent.
	vm *virtual.Model

	// hOffset is the index of the leftmost column that has any cell on screen, and
	// hCells the CELL offset the content is scrolled by. Both exist because a column index and a cell offset are
	// different units and drawing needs the cells: a column is not always as wide as
	// the one before it, so subtracting an index from a cell position would scroll by
	// the wrong number of cells for every column but the first. The index is kept
	// because it is the meaningful thing to report and to set.
	hOffset  int
	hCells   int
	selected int
	focused  bool

	// Marker is the selection gutter glyph, as in List, and MarkerStyle its
	// rendition.
	Marker      string
	MarkerStyle buffer.Style

	// Header draws the header row. It is a field because a table with no header
	// is a legitimate thing — a detail pane, a key/value list — and a caller
	// should not have to fake one to get the rows.
	Header bool

	// ItemStyle is the background of an unselected row and the base the header is
	// patched with; SelectedStyle is the selected row's. Both should carry an
	// attribute as well as a colour, because the gutter marker is the
	// colour-independent signal.
	ItemStyle     buffer.Style
	SelectedStyle buffer.Style

	// Scrollbar draws the vertical thumb on the rightmost interior column, and
	// ScrollbarStyle is its rendition. The thumb's POSITION is the signal.
	Scrollbar      bool
	ScrollbarStyle buffer.Style

	// OnActivate, when set, is called with the selected row index.
	OnActivate func(int)

	// widthRegions and heightRegions are the two geometry.Budget inputs. They are
	// built once at construction and refreshed only where their size depends on
	// content (the gutter), per ADR 0007 §2.
	widthRegions  []geometry.Region
	heightRegions []geometry.Region
	keepW, keepH  []bool

	// Derived-from-size state, all recomputed in adapt:
	//
	//	colX      absolute x of each column, indexed by column
	//	colW      width of each column, indexed by column
	//	titleW    cell width of each column's header, indexed by column
	//	visCols   the columns intersecting the viewport, left to right
	//	body      the rectangle body rows are painted into
	//	rowH      body height available to the row engine
	//	totalW    total width of all columns including gaps
	colX   []int
	colW   []int
	titleW []int
	// visCols aliases visBuf and is rebuilt by refreshVisible; it is a slice
	// field rather than a fresh allocation so scrolling horizontally costs
	// nothing.
	visCols []int
	visBuf  []int
	body    buffer.Rect
	gutter  buffer.Rect
	rowH    int
	totalW  int

	// markerW, barW, headerH and showHeader are the budget answers resolved into
	// the numbers the painters use.
	markerW, barW, headerH int
	showHeader             bool

	// cachedRect is the interior the above was computed for; maxColOffset is the
	// horizontal clamp's ceiling in column terms and maxCells the same ceiling in
	// cell terms, which is the unit that decides what is reachable.
	cachedRect   buffer.Rect
	maxColOffset int
	maxCells     int
	contentW     int
	mark         rune
	markerRune   rune
	thumbRune    rune
	fillScratch  []int
	fillWeights  []layout.Constraint
}

// NewTable returns a Table sized r with the given columns and no rows.
func NewTable(r buffer.Rect, cols ...Column) *Table {
	t := &Table{
		blk:       block.New(r),
		bounds:    r,
		cols:      cols,
		vm:        virtual.New(0),
		selected:  -1,
		Marker:    defaultMarker,
		Scrollbar: true,
		widthRegions: []geometry.Region{
			{Size: buffer.StringWidth(defaultMarker) + markerPad, Prio: geometry.PrioHigh},
			{Size: scrollbarW, Prio: scrollbarPrio},
		},
		heightRegions: []geometry.Region{
			{Size: 1, Prio: headerCellsPrio},
			{Size: 0, Prio: geometry.PrioAlways},
		},
	}
	t.blk.SetBounds(r)
	return t
}

// SetRows replaces the collection and re-clamps both offsets.
//
// It is the only way to change content, so cell spans are normalised here — with
// the column's CellStyle folded in — rather than per frame, and the measured
// widths are invalidated so the next Draw re-measures against the rows that are
// now visible.
func (t *Table) SetRows(rows []Row) {
	out := make([]tableRow, len(rows))
	for i := range rows {
		row := tableRow{cells: make([]tableCell, len(rows[i].Cells))}
		for j, c := range rows[i].Cells {
			st := c.Style
			if st.IsUnset() && j < len(t.cols) {
				st = t.cols[j].CellStyle
			}
			var spans []buffer.Span
			switch {
			case buffer.SpansWidth(c.Spans) > 0:
				// The cell's own spans win, styles and all.
				spans = c.Spans
			case c.Text != "":
				spans = []buffer.Span{buffer.NewSpan(c.Text, st)}
			}
			row.cells[j] = tableCell{spans: spans, width: buffer.SpansWidth(spans)}
		}
		out[i] = row
	}
	t.rows = out
	t.vm.SetCount(len(out))
	if t.selected >= len(out) {
		t.selected = len(out) - 1
	}
	if t.selected < 0 && len(out) > 0 {
		t.selected = 0
	}
	t.vm.ScrollIntoView(t.selected)
	t.clampColOffset()
	t.cachedRect = buffer.Rect{}
}

// Rows returns the number of rows.
func (t *Table) Rows() int { return t.vm.Count() }

// Columns returns the number of columns.
func (t *Table) Columns() int { return len(t.cols) }

// Bounds returns the table's rectangle, safe to call before the first Draw.
func (t *Table) Bounds() buffer.Rect { return t.bounds }

// SetBounds sets the table's rectangle.
func (t *Table) SetBounds(r buffer.Rect) {
	t.bounds = r
	t.blk.SetBounds(r)
}

// Block returns the block that draws this table's chrome, so a caller can
// configure the border, title, padding, background and ASCII rung without Table
// re-exporting every Block method.
func (t *Table) Block() *block.Block { return t.blk }

// Focused reports whether the table has focus.
func (t *Table) Focused() bool { return t.focused }

// SetFocused gives or removes focus. The selection survives either way.
func (t *Table) SetFocused(v bool) { t.focused = v }

// MinSize returns the smallest table that shows a marker, one column of a
// readable width and at least one row: a whole-widget size including chrome.
func (t *Table) MinSize() buffer.Size { return minWhole(t.blk, minTableW, minTableH) }

// Selected returns the selected row index, or -1 when there is no selection.
func (t *Table) Selected() int { return t.selected }

// Select moves the selection to row i, clamped into the collection.
func (t *Table) Select(i int) {
	if t.vm.Count() == 0 {
		t.selected = -1
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= t.vm.Count() {
		i = t.vm.Count() - 1
	}
	t.selected = i
	t.vm.ScrollIntoView(i)
}

// ColOffset returns the index of the leftmost column with any cell on screen.
//
// It is not the same as "the column SetColOffset was given": scrolling to the end of
// a table puts the last column flush against the right edge while the one before it
// is still partly visible, so the leftmost visible column is the one before. The
// distinction is why ColOffset reports what is on screen and SetColOffset takes what
// a caller wants at the left edge.
func (t *Table) ColOffset() int { return t.hOffset }

// SetColOffset scrolls horizontally so column i is the leftmost, clamped into
// [0, the last column] and then into the cell range the content occupies.
func (t *Table) SetColOffset(i int) {
	if len(t.colX) < len(t.cols) {
		// Before the first adapt the column positions are unknown, so the only safe
		// offset is zero.
		t.hOffset, t.hCells = 0, 0
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(t.cols) {
		i = len(t.cols) - 1
	}
	t.hOffset = i
	t.hCells = t.colX[i]
	t.clampColOffset()
	t.refreshVisible()
}

// RowOffset returns the index of the first visible body row.
func (t *Table) RowOffset() int { return t.vm.Offset() }

// SetRowOffset scrolls vertically to body row i.
func (t *Table) SetRowOffset(i int) { t.vm.SetOffset(i) }

// ContentWidth returns the total width of all columns including the gaps between
// them, which is what decides whether the table scrolls horizontally at all.
func (t *Table) ContentWidth() int { return t.totalW }

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
func (t *Table) Invalidate() {
	t.cachedRect = buffer.Rect{}
}

// Handle consumes navigation keys while focused and mouse events inside its
// rectangle.
func (t *Table) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventKey:
		if !t.focused {
			return false
		}
		return t.handleKey(ev)
	case termmosaic.EventMouse:
		return t.handleMouse(ev)
	default:
		return false
	}
}

// handleKey is the key contract, one case per documented binding.
func (t *Table) handleKey(ev termmosaic.Event) bool {
	t.syncChrome()
	shift := ev.Mod.Has(termmosaic.ModShift)
	switch ev.Key {
	case termmosaic.KeyUp:
		t.move(-1)
		return true
	case termmosaic.KeyDown:
		t.move(1)
		return true
	case termmosaic.KeyPageUp:
		t.move(-t.vm.Visible() + 1)
		return true
	case termmosaic.KeyPageDown:
		t.move(t.vm.Visible() - 1)
		return true
	case termmosaic.KeyHome:
		if shift {
			t.SetColOffset(0)
			return true
		}
		t.Select(0)
		return true
	case termmosaic.KeyEnd:
		if shift {
			t.SetColOffset(t.maxColOffset)
			return true
		}
		t.Select(t.vm.Count() - 1)
		return true
	case termmosaic.KeyLeft:
		t.scrollCols(-1)
		return true
	case termmosaic.KeyRight:
		t.scrollCols(1)
		return true
	case termmosaic.KeyEnter, termmosaic.KeySpace:
		if t.OnActivate != nil && t.selected >= 0 {
			t.OnActivate(t.selected)
		}
		return true
	}
	return false
}

// scrollCols moves the horizontal offset by n columns.
func (t *Table) scrollCols(n int) { t.SetColOffset(t.hOffset + n) }

// move moves the selection by delta rows, treating zero as one.
func (t *Table) move(delta int) {
	if delta == 0 {
		delta = 1
	}
	if t.selected < 0 {
		t.selected = 0
	}
	t.Select(t.selected + delta)
}

// handleMouse consumes a press or a wheel notch inside the table. Shift with the
// wheel scrolls horizontally, because a table is the one data widget with a
// second axis to scroll.
func (t *Table) handleMouse(ev termmosaic.Event) bool {
	if !t.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
		return false
	}
	t.syncChrome()
	// A mouse event's modifiers live in Mouse.Mod, not in Event.Mod: the decoder
	// fills one or the other depending on the kind, and reading the wrong one
	// would make this modifier silently do nothing.
	shift := ev.Mouse.Mod.Has(termmosaic.ModShift) || ev.Mod.Has(termmosaic.ModShift)
	switch ev.Mouse.Button {
	case termmosaic.MouseWheelUp:
		t.scrollWheel(-wheelLines, shift)
		return true
	case termmosaic.MouseWheelDown:
		t.scrollWheel(wheelLines, shift)
		return true
	}
	if ev.Mouse.Action != termmosaic.MousePress || ev.Mouse.Button != termmosaic.MouseLeft {
		return false
	}
	i, ok := t.vm.ItemAt(ev.Mouse.Y - t.body.Y)
	if !ok {
		return false
	}
	t.focused = true
	t.Select(i)
	return true
}

// scrollWheel scrolls one axis by n rows or columns.
func (t *Table) scrollWheel(n int, horizontal bool) {
	if horizontal {
		t.scrollCols(n)
		return
	}
	t.vm.ScrollBy(n)
}

// syncChrome adapts if the interior has changed, so a key or a click handled
// before the first Draw lands on the row the user can see rather than on one
// computed without the header.
//
// It allocates, because geometry.Budget allocates, which is exactly why it is not
// on the frame path: it is only reached from Handle, and from Draw only after
// adapt has already run for this rect.
func (t *Table) syncChrome() {
	in := t.blk.Interior()
	if in.Empty() || in == t.cachedRect {
		t.vm.Resize(t.bodyRect())
		return
	}
	t.adapt(in)
}

// bodyRect returns the rectangle body rows are painted into: the block's interior
// minus the gutter, the scrollbar and the header.
//
// It reads the widths the last adapt computed, so it is meaningful only after the
// first Draw — which is exactly when it is used, and why the engine is resized
// again inside Handle with a possibly stale rect. The engine clamps that rect, so
// a stale one is off by a row rather than wrong.
func (t *Table) bodyRect() buffer.Rect {
	in := t.blk.Interior()
	if in.Empty() {
		return in
	}
	h := in.H - t.headerH
	if h < 0 {
		h = 0
	}
	w := in.W - t.markerW - t.barW
	if w < 0 {
		w = 0
	}
	return buffer.Rect{X: in.X + t.markerW, Y: in.Y + t.headerH, W: w, H: h}
}

// Draw paints the chrome, the header, and the visible rows and columns.
//
// It is total and allocation-free: the whole layout — which chrome survived the
// budget, which columns are visible and how wide each is — is derived in adapt,
// which runs once per distinct interior.
func (t *Table) Draw(buf *buffer.Buffer) {
	r := t.bounds
	if r.Empty() {
		return
	}
	t.blk.Draw(buf)
	in := t.blk.Interior()
	if in.Empty() {
		return
	}
	if in != t.cachedRect {
		t.adapt(in)
	}

	t.drawHeader(buf, in)
	if t.rowH > 0 && t.body.W > 0 {
		view := t.body
		t.vm.Resize(view)
		table := t
		t.vm.ForEach(view, buf, table.drawRow)
	}
	if t.barW > 0 {
		t.drawScrollbar(buf, in)
	}
}

// drawHeader paints the header row across the whole interior, gutter included, so
// that the rows below form a block of one background with the selection's row
// highlighted inside it.
func (t *Table) drawHeader(buf *buffer.Buffer, in buffer.Rect) {
	if t.headerH == 0 || !t.showHeader {
		return
	}
	row := buffer.Rect{X: in.X, Y: in.Y, W: in.W, H: 1}
	buf.FillRect(row, t.ItemStyle.Resolved().Blank())
	for _, c := range t.visCols {
		col := t.cols[c]
		if t.titleW[c] <= 0 {
			continue
		}
		st := col.HeaderStyle
		if st.IsUnset() {
			st = buffer.HeadingStyle
		}
		t.drawCell(buf, row, c, col.Title, t.titleW[c], st)
	}
}

// drawRow paints one body row: the row background across the gutter and the
// content columns, the marker for the selected row, and every visible cell
// clipped to its column.
func (t *Table) drawRow(dst *buffer.Buffer, row buffer.Rect, i int) {
	selected := i == t.selected
	bg := t.ItemStyle
	if selected {
		bg = t.SelectedStyle
	}
	// The row engine is given the BODY only, so the gutter is painted here with
	// the same background: a selection has to cover the whole row, marker column
	// included, or it reads as a highlighted set of cells rather than a row.
	dst.FillRect(row, bg.Resolved().Blank())
	if t.markerW > 0 {
		g := buffer.Rect{X: t.gutter.X, Y: row.Y, W: t.gutter.W, H: 1}
		dst.FillRect(g, bg.Resolved().Blank())
		if selected && t.markerRune != 0 {
			mst := t.MarkerStyle
			if mst.IsUnset() {
				mst = t.SelectedStyle
			}
			dst.SetCell(g.X, g.Y, mst.Resolved().Cell(t.markerRune))
		}
	}

	cells := t.rows[i].cells
	for _, c := range t.visCols {
		if c < len(cells) {
			t.drawCell(dst, row, c, cells[c].spans, cells[c].width, buffer.DefaultStyle)
		}
	}
}

// drawCell writes one cell's spans into column c, aligned within the part of the
// column that is on screen.
//
// width is the cell's own content width and st overrides the spans' styles when
// it is not the zero Style — which is how the header gets HeadingStyle without
// the caller's spans having to carry it. Pass DefaultStyle to write a cell's own
// styles verbatim.
func (t *Table) drawCell(buf *buffer.Buffer, row buffer.Rect, c int, spans []buffer.Span, width int, st buffer.Style) {
	if len(spans) == 0 || t.colW[c] <= 0 {
		return
	}
	// Where the column's first cell WOULD be at the current horizontal offset. It is
	// frequently off-screen to the LEFT, and drawing from it unclipped paints the
	// column's head over the selection gutter and the border beside it — a scrolled
	// table overwriting its own frame.
	origin := t.body.X + t.colX[c] - t.hCells
	full := t.colW[c]
	left, right := t.body.X, t.body.Right()
	start, end := origin, origin+full
	if end <= left || start >= right {
		// Entirely off-screen in one direction: nothing to draw, and nothing to clip
		// against.
		return
	}
	skip := 0
	if start < left {
		skip = left - start
		start = left
	}
	if end > right {
		end = right
	}
	if !st.IsUnset() && len(spans) == 1 {
		// One span on the stack: no slice is built on the frame path. A cell with
		// several spans keeps its own styles, since flattening it would have to build
		// a string.
		var one [1]buffer.Span
		one[0] = buffer.Span{Text: spans[0].Text, Style: st}
		spans = one[:]
	}
	// The visible part of the CONTENT is what is left after the skipped cells, and the
	// alignment applies to that: a right-aligned number scrolled half off screen lines
	// up with the visible edge rather than with a cell that is not there.
	visW := end - start
	content := width - skip
	if content > visW {
		content = visW
	}
	if content < 0 {
		content = 0
	}
	x := start + t.cols[c].Align.Offset(visW, content)
	buf.SetSpansWindowIn(x, end, row.Y, spans, skip, t.mark)
}

// drawScrollbar paints the vertical position thumb over the body rows, as in
// List.
func (t *Table) drawScrollbar(buf *buffer.Buffer, in buffer.Rect) {
	if t.vm.MaxOffset() <= 0 || t.rowH <= 0 {
		return
	}
	h := t.rowH
	x := in.Right() - 1
	thumbH := h * t.vm.Visible() / t.vm.Count()
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > h {
		thumbH = h
	}
	start := 0
	if slack := h - thumbH; slack > 0 {
		start = slack * t.vm.Offset() / t.vm.MaxOffset()
	}
	c := t.ScrollbarStyle.Resolved().Cell(t.thumbRune)
	for y := 0; y < thumbH; y++ {
		buf.SetCell(x, t.body.Y+start+y, c)
	}
}

// adapt recomputes everything derived from the interior.
//
// This is the rect-keyed cache of ADR 0007 §3: a steady frame recomputes nothing,
// and a drag that produces eighteen heights recomputes eighteen times.
func (t *Table) adapt(in buffer.Rect) {
	t.cachedRect = in
	t.mark = truncMark(t.blk.Ascii)
	t.markerRune = firstRune(t.Marker)
	t.thumbRune = thumbRune
	if t.blk.Ascii {
		t.thumbRune = asciiThumb
	}
	if len(t.widthRegions) > 0 {
		t.widthRegions[0].Size = buffer.StringWidth(t.Marker) + markerPad
	}

	// Width budget: the gutter and the scrollbar compete for the same cells, and
	// the gutter wins because losing selection would be worse than losing position.
	t.keepW = geometry.Budget(t.widthRegions, in.W)
	t.markerW, t.barW = 0, 0
	if len(t.keepW) == len(t.widthRegions) {
		if t.keepW[0] && t.Marker != "" {
			t.markerW = t.widthRegions[0].Size
		}
		if t.keepW[1] && t.Scrollbar {
			t.barW = scrollbarW
		}
	}
	if t.markerW >= in.W {
		t.markerW = 0
	}

	// Height budget: the header row against the body.
	if len(t.heightRegions) > 1 {
		// The body is measured as WHAT IS LEFT after the header, because
		// geometry.Budget compares declared sizes against the available cells: a
		// body declared as the whole height would leave nothing for the header
		// and the header would always be dropped.
		t.heightRegions[1].Size = in.H - 1
		if t.heightRegions[1].Size < 0 {
			t.heightRegions[1].Size = 0
		}
	}
	t.keepH = geometry.Budget(t.heightRegions, in.H)
	t.headerH, t.showHeader = 0, false
	if len(t.keepH) == len(t.heightRegions) && t.Header && t.keepH[0] {
		t.headerH, t.showHeader = 1, true
	}

	t.contentW = in.W - t.markerW - t.barW
	if t.contentW < 0 {
		t.contentW = 0
	}
	h := in.H - t.headerH
	if h < 0 {
		h = 0
	}
	t.body = buffer.Rect{X: in.X + t.markerW, Y: in.Y + t.headerH, W: t.contentW, H: h}
	t.gutter = buffer.Rect{X: in.X, Y: in.Y + t.headerH, W: t.markerW, H: h}
	t.rowH = h
	// The engine is resized BEFORE the columns are measured, because a content
	// column's width comes from the rows that fit: measuring against a viewport of
	// zero would size every column to its minimum on the first frame and keep that
	// width until the rect changed.
	t.vm.Resize(t.body)
	t.measureColumns()
	t.clampColOffset()
	t.refreshVisible()
}

// measureColumns resolves every column's width and the x it lands at.
//
// The measured widths come from the visible body rows only, and the fill share is
// solved by layout.Solve so that the distribution rule is the framework's rather
// than a second one living in a widget (ADR 0004's argument, applied to width).
func (t *Table) measureColumns() {
	n := len(t.cols)
	if cap(t.colX) < n {
		t.colX = make([]int, n)
		t.colW = make([]int, n)
		t.titleW = make([]int, n)
	} else {
		t.colX = t.colX[:n]
		t.colW = t.colW[:n]
		t.titleW = t.titleW[:n]
	}

	first, last := t.vm.Range()
	fixed, fills := 0, 0
	for i := range t.cols {
		col := t.cols[i]
		t.titleW[i] = buffer.SpansWidth(col.Title)
		switch {
		case col.Width > 0:
			t.colW[i] = col.Width
			fixed += col.Width + colGap
		case col.Grow > 0:
			fills++
		default:
			w := minAutoWidth
			for r := first; r < last; r++ {
				if i >= len(t.rows[r].cells) {
					continue
				}
				if cw := t.rows[r].cells[i].width; cw > w {
					if cw > maxAutoWidth {
						cw = maxAutoWidth
					}
					w = cw
				}
			}
			t.colW[i] = w
			fixed += w + colGap
		}
	}

	// Whatever is left goes to the Fill columns, in proportion to their weights.
	if fills > 0 {
		left := t.contentW - fixed
		if left < 0 {
			left = 0
		}
		if cap(t.fillWeights) < fills {
			t.fillWeights = make([]layout.Constraint, fills)
		}
		cs := t.fillWeights[:fills]
		k := 0
		for i := range t.cols {
			if t.cols[i].Width <= 0 && t.cols[i].Grow > 0 {
				cs[k] = layout.Fill(t.cols[i].Grow)
				k++
			}
		}
		if cap(t.fillScratch) < fills {
			t.fillScratch = make([]int, fills)
		}
		sizes := t.fillScratch[:fills]
		copy(sizes, layout.Solve(layout.Horizontal, cs, 0, left))
		k = 0
		for i := range t.cols {
			if t.cols[i].Width <= 0 && t.cols[i].Grow > 0 {
				t.colW[i] = sizes[k]
				k++
			}
		}
	}

	// Absolute x per column, including the gap that follows it.
	x := 0
	for i := range t.cols {
		t.colX[i] = x
		x += t.colW[i] + colGap
	}
	t.totalW = x
}

// refreshVisible rebuilds which columns are on screen and recomputes the
// horizontal clamp's ceiling.
//
// It is separate from measureColumns so that a horizontal scroll — the one thing
// that does not change the rect — costs no re-measurement and no allocation.
func (t *Table) refreshVisible() {
	if len(t.colX) < len(t.cols) {
		// Before the first adapt the column positions are unknown.
		t.visCols = nil
		return
	}
	if cap(t.visBuf) < len(t.cols) {
		t.visBuf = make([]int, 0, len(t.cols))
	}
	// Clamping first is what guarantees at least one column is visible: the
	// ceiling is derived from the LAST column's x, so no legal offset can scroll
	// every column off the left edge.
	t.clampColOffset()
	t.visCols = t.visBuf[:0]
	for i := range t.cols {
		x := t.colX[i] - t.hCells
		if x >= t.contentW {
			break
		}
		if x+t.colW[i] > 0 {
			t.visCols = append(t.visCols, i)
		}
	}
	t.visBuf = t.visCols
}

// clampColOffset pins the horizontal scroll into range. It is the horizontal twin of
// virtual's clamp do not recentre rule (ADR 0007 §6 rule 2), written here because
// the horizontal axis belongs to the widget rather than to virtual/.
//
// The ceiling is the cell offset at which the END of the content reaches the right
// edge, which is what makes every column reachable — including the ones whose start
// lies past the viewport. Clamping to "the last column that still starts on screen"
// instead would be a smaller number, and it would strand every column beyond the
// viewport with no way to scroll to it: a table that silently hides data.
//
// It also cannot show an empty table: at this offset the final column's right edge
// is exactly at the viewport's right edge, so it always has cells on screen.
func (t *Table) clampColOffset() {
	if len(t.cols) == 0 || len(t.colX) < len(t.cols) {
		// Before the first adapt the column positions are unknown, so there is
		// nothing to clamp against and the only safe offset is zero.
		t.hOffset, t.hCells, t.maxColOffset = 0, 0, 0
		return
	}
	maxCells := t.totalW - t.contentW
	if maxCells < 0 {
		maxCells = 0
	}
	if t.hCells > maxCells {
		t.hCells = maxCells
	}
	if t.hCells < 0 {
		t.hCells = 0
	}
	// Re-derive the column index from the clamped cell offset, so ColOffset reports
	// what is actually on screen rather than what was asked for.
	idx := 0
	for i := range t.cols {
		if t.colX[i] <= t.hCells {
			idx = i
		}
	}
	t.hOffset = idx
	t.maxCells = maxCells
	// The ceiling in COLUMN terms is the last column: scrolling to it means showing
	// it, and the cell clamp above decides how much of what comes before it is still
	// on screen. Reporting "the last column whose start fits" instead would make
	// shift+End stop short of the last column, which is the one thing a reader asking
	// for the end of a table wants.
	t.maxColOffset = len(t.cols) - 1
}

var (
	_ termmosaic.Widget      = (*Table)(nil)
	_ termmosaic.Focusable   = (*Table)(nil)
	_ termmosaic.Minimizable = (*Table)(nil)
)
