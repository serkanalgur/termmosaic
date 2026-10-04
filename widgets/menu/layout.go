// The layout: which open levels get a column, how the interior's cells divide
// between the marker, check, label, hint and submenu gutters, and every string
// cut to the width it will be drawn at.
//
// It is one file because it is one cache, and it is a separate file from menu.go
// because the distinction is the one that matters for cost: everything in HERE
// allocates, and nothing in here runs on a steady frame.

package menu

import (
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds and constants, per ADR 0007 §1 rule 5: a widget switches
// variant at its own named constants and shares nothing.
const (
	// markW is the width of the selection marker column: one cell. It is NOT
	// dropped while the column has room for it, because the marker is the signal
	// that survives a monochrome terminal and a reader who cannot tell two of the
	// application's own colours apart.
	markW = 1
	// checkW is the width of the check gutter: one cell, present only when some
	// item in the column is checkable.
	checkW = 1
	// submenuW is the width of the right gutter holding the submenu marker: one
	// cell, present only when some item in the column has a submenu.
	submenuW = 1
	// hintGap is the cells between a hint and the submenu marker beside it. Two,
	// because a hint one cell from the marker reads as part of it.
	hintGap = 2
	// minLabelW is the narrowest label the hint column may sit beside. Below it
	// the hint is dropped rather than sharing three cells with the text, which is
	// what keeps the hint from being what makes a label unreadable.
	minLabelW = 4
	// headerH is the height of a level header row: one cell.
	headerH = 1
	// minColumnW is the narrowest column that may be shown at all. Below it a
	// label has no cells left once the marker is paid for, so showing the column
	// would show a marker and nothing else.
	minColumnW = 3
	// minMenuW is the narrowest whole menu worth showing: two columns, each wide
	// enough for a marker and a couple of label cells, plus a hint's gap.
	minMenuW = 14
	// minMenuH is the shortest menu worth showing: a header row and one item.
	minMenuH = headerH + 1
	// ellipsis is the prefix in a header whose shallower levels were dropped for
	// want of width. It says so, rather than letting a level appear silently
	// missing from a menu that is supposed to be showing everything.
	ellipsis = "…"
	// asciiEllipsis is ellipsis on a terminal with no Unicode. One cell, like the
	// Unicode form, so a header's arithmetic is identical on both rungs.
	asciiEllipsis = "~"
	// headerMarkOpen and headerMarkClose bracket the ACTIVE level's header, and
	// headerMarkIdle space-pads every other one.
	//
	// This is the same bracket-means-selected convention Tabs uses, and the
	// brackets are one cell each so a header's width does not change when the
	// selection moves between levels — a header that reflows on every arrow key
	// is a header that makes the whole row jump.
	headerMarkOpen  = "["
	headerMarkClose = "]"
	headerMarkIdle  = " "
	// wheelRows is how many items one wheel notch scrolls.
	wheelRows = 3
)

// column is one open level's cached geometry and cached text.
//
// The rects are absolute, because a menu draws into the root buffer rather than
// into a sub-buffer per column: the columns are side by side in one interior,
// and a second clipping layer per column would be one more thing to get wrong.
type column struct {
	// rect is the column's whole rectangle, including its header row.
	rect buffer.Rect
	// headRect is the header row, or empty when there is no header. Empty rather
	// than zero-height, so the draw loop has one test rather than two.
	headRect buffer.Rect
	// itemRect is the rows region: every cell of rect below the header.
	itemRect buffer.Rect

	// header is the level header's text, already cut to the column's width. It is
	// cached because building it needs an allocation per level per rebuild.
	header string
	// headerSpans is header as a styled run, cached for the same reason. Draw uses
	// this rather than wrapping `header` in a slice at draw time, which would
	// allocate once per level per frame.
	headerSpans []buffer.Span
	// labels[i] is item i's text cut to the column's label width, and hints[i]
	// its keybinding cut to the hint width. Both are cached because truncation
	// allocates.
	labels []string
	hints  []string

	// active reports whether this column is the one the next key acts on. It is
	// cached rather than derived from len(m.levels) so a test can read off which
	// column the widget believes is active, and so Draw does no path arithmetic.
	active bool

	// The x positions of the five regions of a row, cached so Draw reads integers
	// instead of re-deriving the gutter arithmetic for every visible row.
	markerX, checkX, labelX, hintX, submenuX int
	// labelW is the width of the label region, which is what the cached labels
	// were cut to.
	labelW int
	// hasCheck reports whether the check column exists: a statement about the
	// items rather than about the size.
	hasCheck bool
	// hasSubmenu reports whether the submenu marker column exists.
	hasSubmenu bool
	// showHint reports whether the hint column survived the width budget.
	showHint bool
	// labelPad is the cells of blank label between a truncated label and the
	// region's right edge, which is what stops a truncated label reading as a
	// complete word.
	labelPad int
}

// layoutCol is a column plus the level it shows.
//
// They are one type rather than two parallel slices because a column whose depth
// is not recorded is a column nobody can hit-test, and a slice index that has to
// be translated back into a level is the bug this removes.
type layoutCol struct {
	column
	depth int
}

// layout is the cached per-size layout.
//
// It is keyed on the block's INTERIOR rather than on Bounds, because the interior
// is what every derived number is a function of: two bounds differing only in the
// block's padding produce the same layout and should not rebuild it.
type layout struct {
	// in is the interior the columns were built for.
	in buffer.Rect
	// ok reports whether cols describes the current interior.
	ok bool
	// cols holds one entry per SHOWN column in level order, each recording the
	// level it shows.
	cols []layoutCol
	// firstDepth is the level the leftmost shown column stands for. It is greater
	// than zero when shallower levels were dropped for want of width, which is
	// what puts the ellipsis in that column's header.
	firstDepth int
	// hasCheck and hasSubmenu decide whether the two gutter columns exist at all.
	// They are properties of the open PATH rather than of one level, because a
	// submenu whose rows had a different shape from its parent's would not read as
	// part of the same menu.
	hasCheck, hasSubmenu bool
	// wide is the width of the widest hint on the path, cached because both the
	// gutter budget and the truncation need it and both run in the rebuild.
	wide int
}

// invalidate drops the cache. SetItems, Open, Close and a push or a pop all call
// it, because each of them changes the SHAPE of the result rather than the size
// it was derived from.
func (l *layout) invalidate() {
	l.ok = false
	l.in = buffer.Rect{}
}

// stale reports whether the cache describes in.
func (l *layout) stale(in buffer.Rect) bool { return !l.ok || l.in != in }

// ensureLayout rebuilds the layout when it is stale, so a hit test arriving
// before the first Draw still has geometry to hit against.
//
// The alternative is an ordering dependency the caller has to know about: a
// layout hands out rectangles, input arrives, and Handle is asked which item is
// under the cursor before anything has been drawn. An allocation here is not a
// concern because Handle is not the frame path.
func (m *Menu) ensureLayout() {
	in := m.blk.Interior()
	if m.lay.stale(in) {
		m.rebuild(in)
	}
}

// rebuild recomputes the whole cached layout for an interior.
//
// It runs at most once per distinct interior, so a drag producing eighteen
// different widths recomputes eighteen times and a steady frame recomputes
// nothing (ADR 0007 §3). Everything in it may allocate; nothing outside it does.
func (m *Menu) rebuild(in buffer.Rect) {
	m.lay.in, m.lay.ok = in, true
	m.lay.cols = m.lay.cols[:0]
	m.lay.wide = 0
	if in.Empty() {
		return
	}

	// The selection at each level is what says which items a deeper level HAS, so
	// it is clamped before anything reads the path.
	m.clampSelections()

	active := m.active()

	// Which levels get a column. The DEEPEST win, because the active level must
	// be visible and a level with no column still keeps its state. Losing the
	// shallowest is the honest trade: the user is looking at the deepest.
	depth := m.Depth()
	kept := depth
	for kept > 1 && in.W < kept*(minColumnW+columnGap) {
		kept--
	}
	m.lay.firstDepth = depth - kept
	m.lay.hasCheck, m.lay.hasSubmenu, m.lay.wide = m.scanAll()
	m.resolveRunes()
	m.placeColumns(in, kept, active-m.lay.firstDepth)
	m.resizeViews()
}

// resizeViews gives every SHOWN level's scroll engine its item rect.
//
// It runs in the rebuild rather than only in Draw so that a hit test or a key
// arriving before the first frame already has a viewport: an engine resized to a
// zero-height viewport reports Visible() == 0, and ScrollIntoView against that
// does nothing at all — a menu that ignored End until it had been drawn once is a
// menu with an ordering dependency its caller has to know about.
func (m *Menu) resizeViews() {
	for i := range m.lay.cols {
		c := &m.lay.cols[i]
		if c.depth < len(m.levels) {
			m.levels[c.depth].vm.Resize(c.itemRect)
		}
	}
}

// columnGap is the blank cells between two columns.
//
// It is one cell rather than a rule on purpose. A vertical rule would have to be a
// box-drawing rune, and buffer/border.go is the only file allowed to hold one — a
// menu that spelled its own would be exactly the collision ADR 0008 exists to
// prevent. A blank cell does the separating job for a fraction of a rule, and it
// survives an ASCII terminal with no second degradation path.
const columnGap = 1

// placeColumns writes a column layout per shown level.
//
// The interior is divided into `kept` equal widths and `kept-1` gaps, with the
// remainder going to the leftmost columns so no cell is left unpainted. Both
// decisions are fixed rather than random, because a layout that wobbles by a cell
// between two renders is one a resize test cannot assert on.
//
// The width each column receives is recomputed from what is LEFT rather than
// divided up front, so the last column is exactly as wide as its neighbours and
// the columns do not creep inward by a cell each time one is added.
func (m *Menu) placeColumns(in buffer.Rect, kept, activeCol int) {
	free := in.W - (kept-1)*columnGap
	share, rem := free/kept, free%kept
	if share < 1 {
		// Below one cell per column there is nothing to divide. Every column still
		// gets one cell of address, and the ones past the edge are clipped by the
		// buffer — a partly visible menu beats a division by a zero-ish count.
		share, rem = 1, 0
	}
	offX := 0
	for i := 0; i < kept; i++ {
		w := share
		if i < rem {
			w++
		}
		m.lay.cols = append(m.lay.cols, m.buildColumn(
			m.lay.firstDepth+i, offX, w, in, i == 0 && m.lay.firstDepth > 0, i == activeCol))
		offX += w
		if i < kept-1 {
			offX += columnGap
		}
	}
}

// scanAll reports the content facts every column's gutters depend on: whether any
// reachable item is a toggle, whether any has a submenu, and the widest hint.
//
// It is one pass over the open path rather than one per level because the
// gutters are a property of the MENU as drawn — a check column in a column of
// plain items would assert that those items are toggles, and one column's gutter
// must not depend on which level happens to be open beside it. Scoping the
// gutters to the menu rather than the level also means a submenu cannot appear
// with a different row shape from its parent, which is what makes the columns read
// as one widget.
func (m *Menu) scanAll() (hasCheck, hasSubmenu bool, wide int) {
	for d := 0; d < m.Depth(); d++ {
		checks, branches, hint := scanLevel(m.levelItems(d))
		hasCheck = hasCheck || checks
		hasSubmenu = hasSubmenu || branches
		if hint > wide {
			wide = hint
		}
	}
	return hasCheck, hasSubmenu, wide
}

// resolveRunes picks the marker glyphs for the current rung and caches them as
// runes, which is what Draw writes.
//
// A field the application has changed is left alone on BOTH rungs. Only the
// catalog DEFAULT is swapped for its ASCII form, because a caller who assigned
// SubmenuMarker chose that glyph deliberately and a degradation step that
// overrode it would be the widget second-guessing an explicit choice.
//
// It runs in the rebuild rather than in Draw because comparing four strings per
// visible row per frame is exactly the sort of per-frame work ADR 0007 §3 is
// written against.
func (m *Menu) resolveRunes() {
	m.markRune = runeOf(m.Marker, DefaultMarker)
	m.submenuRune = runeOf(m.SubmenuMarker, DefaultSubmenuMarker)
	m.checkRune = runeOf(m.CheckMark, DefaultCheckMark)
	m.checkEmptyRune = runeOf(m.CheckMarkEmpty, DefaultCheckMarkEmpty)
	m.truncMark = firstRuneOf(buffer.TruncSuffix)
	if !m.Ascii {
		return
	}
	if m.Marker == DefaultMarker {
		m.markRune = firstRuneOf(asciiMarker)
	}
	if m.SubmenuMarker == DefaultSubmenuMarker {
		m.submenuRune = firstRuneOf(asciiSubmenuMarker)
	}
	if m.CheckMark == DefaultCheckMark {
		m.checkRune = firstRuneOf(asciiCheckMark)
	}
	m.truncMark = firstRuneOf(buffer.AscTruncSuffix)
}

// runeOf returns the first rune of got, or of fallback when got is empty. An
// empty marker field means "draw a space", not "draw nothing", because the row has
// just been filled and a zero rune would cost a branch to explain for no visible
// difference.
func runeOf(got, fallback string) rune {
	if got == "" {
		got = fallback
	}
	return firstRuneOf(got)
}

// firstRuneOf returns s's first rune, or a space when s is empty.
func firstRuneOf(s string) rune {
	for _, r := range s {
		return r
	}
	return ' '
}

// scanLevel reports whether a level has a checkable item, whether it has a
// branch, and the width of its widest hint.
func scanLevel(items []Item) (hasCheck bool, hasSubmenu bool, wide int) {
	for i := range items {
		if items[i].Checkable {
			hasCheck = true
		}
		if len(items[i].Items) > 0 {
			hasSubmenu = true
		}
		if n := buffer.StringWidth(items[i].Hint); n > wide {
			wide = n
		}
	}
	return hasCheck, hasSubmenu, wide
}

// buildColumn lays out one level's column of width w at offX cells into in.
//
// headTruncated says this is the leftmost kept column AND shallower levels were
// dropped for want of width, which is what puts the ellipsis in its header.
func (m *Menu) buildColumn(d, offX, w int, in buffer.Rect, headTruncated, active bool) layoutCol {
	c := layoutCol{depth: d}
	c.column.active = active
	c.column.hasCheck = m.lay.hasCheck
	c.column.hasSubmenu = m.lay.hasSubmenu
	c.rect = buffer.Rect{X: in.X + offX, Y: in.Y, W: w, H: in.H}
	// A header is shown only when a row is left for an item after it: a menu that
	// spends its last row on a caption and shows no item is a frame with a title.
	if c.rect.H > headerH {
		c.headRect = buffer.Rect{X: c.rect.X, Y: c.rect.Y, W: c.rect.W, H: headerH}
		c.itemRect = buffer.Rect{X: c.rect.X, Y: c.rect.Y + headerH, W: c.rect.W, H: c.rect.H - headerH}
	} else {
		c.itemRect = c.rect
	}
	m.placeGutters(&c, w, m.lay.wide)
	m.cutStrings(&c, m.levelItems(d), headTruncated)
	return c
}

// placeGutters decides where the five regions of a row start, in one pass.
//
// The order is fixed and it is the whole of the row's layout: marker, check,
// label, hint, submenu marker. The label flexes, and the two states that must
// never be signalled by colour alone — the marker and the check — are paid for
// BEFORE the hint, which is therefore the first thing to go.
//
// The submenu marker is never dropped: a branch that does not look like a branch
// is a menu the user cannot navigate by reading it.
func (m *Menu) placeGutters(c *layoutCol, w, wide int) {
	x := c.rect.X
	c.markerX = x
	x += markW

	c.checkX = x
	if c.hasCheck {
		x += checkW
	}

	c.submenuX = 0
	if c.hasSubmenu && c.rect.W > markW+1 {
		c.submenuX = c.rect.Right() - submenuW
	}

	// The hint region is asked for only when the label would still be legible
	// with it present, which is the budget expressed as one comparison.
	c.showHint = wide > 0 && c.submenuX > 0 && x+minLabelW+hintGap+wide <= c.submenuX
	if c.showHint {
		c.hintX = c.submenuX - hintGap - wide
	} else {
		c.hintX = 0
	}

	c.labelX = x
	labelEnd := c.rect.Right()
	if c.submenuX > 0 {
		labelEnd = c.submenuX
	}
	if c.showHint {
		labelEnd = c.hintX
	}
	if labelEnd < x {
		// A column too narrow for a label still gets its gutter cells: "clip,
		// never blank" means the marker survives a width that cannot hold the
		// text, not that the row empties.
		labelEnd = x
	}
	c.labelW = labelEnd - x
	_ = w
}

// cutStrings truncates every item's label and hint to the width its column will
// draw it at, and caches the results.
//
// This is where the package's one allocating operation on the frame path lives,
// which is why it is here rather than in Draw and why the whole of it is skipped
// when the interior has not changed.
func (m *Menu) cutStrings(c *layoutCol, items []Item, headTruncated bool) {
	c.labels = resizeStrings(c.labels, len(items))
	c.hints = resizeStrings(c.hints, len(items))
	for i := range items {
		c.labels[i] = cutText(m.Ascii, items[i].Label, c.labelW)
		c.hints[i] = cutText(m.Ascii, items[i].Hint, buffer.StringWidth(items[i].Hint))
	}
	// The header's style is resolved HERE rather than in Draw because whether a
	// column is active is fixed for the life of this cached layout: it changes only
	// when the cursor path does, and every operation that changes the path
	// invalidates the layout. So a header's style is as much a property of the
	// cached layout as its text is.
	c.header = cutText(m.Ascii, m.headerText(c.depth, headTruncated), c.rect.W)
	c.headerSpans = c.headerSpans[:0]
	c.headerSpans = append(c.headerSpans, buffer.NewSpan(c.header, m.headerStyle(c.active).Resolved()))
}

// clampSelections puts every frame's selection back inside its own level.
//
// It is the guard that makes a stale path harmless: a reload that removed an item,
// a parent that moved, and a submenu opened on an item that has none all produce
// an out-of-range index, and the alternative to clamping here is an index panic in
// the middle of a draw.
func (m *Menu) clampSelections() {
	for d := 0; d < len(m.levels); d++ {
		items := m.levelItems(d)
		m.levels[d].vm.SetCount(len(items))
		sel := m.levels[d].sel
		if len(items) == 0 {
			m.levels[d].sel = -1
			continue
		}
		if sel < 0 || sel >= len(items) {
			// A level with nothing selectable keeps -1, which is a real answer and
			// a distinct one: a caller storing the index must be able to tell
			// "nothing here can be selected" from "the first is selected". Drawing
			// tolerates it because a selection of -1 matches no row index.
			sel = m.firstSelectable(d)
		}
		m.levels[d].sel = sel
	}
}

// headerText builds a level's header: the level NUMBER, then the label of the
// item that opened it, then an ellipsis prefix when shallower levels were dropped
// for width.
//
// The NUMBER is what makes the level readable. Once two columns are side by side,
// position alone says which is deeper but not which is the third level, and a
// header that omits the number is a header a screen reader has to infer from
// arithmetic.
func (m *Menu) headerText(d int, headTruncated bool) string {
	s := itoa(d + 1)
	if d > 0 {
		parent := m.levelItems(d - 1)
		i := m.SelectedAt(d - 1)
		if i >= 0 && i < len(parent) {
			if l := parent[i].Label; l != "" {
				s += " " + l
			}
		}
	}
	if headTruncated {
		mark := ellipsis
		if m.Ascii {
			mark = asciiEllipsis
		}
		s = mark + s
	}
	return s
}

// cutText returns text truncated to at most w cells, choosing the truncation
// marker from the ASCII rung so a terminal with no Unicode gets "~" and the
// layout is identical either way — both markers are one cell wide.
//
// It delegates to buffer's truncation rather than cutting here, because three
// widgets each re-deriving "how many cells is this rune" is how one of them ends
// up splitting a double-width glyph.
func cutText(ascii bool, text string, w int) string {
	if w <= 0 {
		return ""
	}
	if buffer.StringWidth(text) <= w {
		return text
	}
	spans := []buffer.Span{buffer.NewSpan(text, buffer.PlainStyle)}
	if ascii {
		return buffer.TruncateASCII(spans, w)[0].Text
	}
	return buffer.Truncate(spans, w)[0].Text
}

// resizeStrings returns a string slice of length n, reusing s's storage.
//
// Reuse rather than reallocation is the point: a steady frame must not touch this
// at all, and a resize sweep must not grow the heap on every step of the sweep.
func resizeStrings(s []string, n int) []string {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]string, n)
}

// minWhole returns the whole-widget size carrying chrome of blk's thickness on
// each side and an interior of at least w by h cells.
//
// MinSize is the WHOLE widget including its own chrome (ADR 0007 §2), so a menu
// reporting a content size would leave every caller's arithmetic wrong by the
// border's thickness.
func minWhole(blk *block.Block, w, h int) buffer.Size {
	inset := 0
	if blk.Border != buffer.BorderNone {
		inset++
	}
	inset += blk.Padding
	s := blk.MinSize()
	if want := w + 2*inset; s.W < want {
		s.W = want
	}
	if want := h + 2*inset; s.H < want {
		s.H = want
	}
	return s
}

// itoa returns the decimal form of v without strconv, for the header row.
//
// It allocates, because building a string does, but it is called from the rebuild
// and never from Draw.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [12]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
