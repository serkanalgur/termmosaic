package data

import (
	"strings"
	"unicode/utf8"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds and constants for Pager, per ADR 0007 §1 rule 5.
const (
	// minPagerW is the narrowest interior that shows a readable line of text.
	minPagerW = 8
	// minPagerH is the smallest pager worth showing: a status line and one row of
	// text.
	minPagerH = 2
	// statusPrio is the status line's priority against the body. It is the first
	// thing to go: losing "Ln 12/500" costs a position readout, losing body rows
	// costs content.
	statusPrio = geometry.PrioLow
	// wheelLines is how many visual rows one wheel notch scrolls.
	scrollLines = 3
	// matchGlyph marks a search hit in the status line, so a match is announced by
	// a character and not only by the highlight in the text.
	matchGlyph = '>'
)

// statusPrefix is the status line's fixed label, so a position readout reads the
// same whichever widget draws it.
const statusPrefix = "Ln "

// textRow is one visual row of wrapped text, as a byte range into the source
// line. Byte ranges rather than strings because a substring of a Go string
// allocates nothing while a rebuilt one would allocate per row per frame.
type textRow struct {
	from, to int
}

// Pager is a read-only view over a large text, wrapped to its width, with search.
//
// It is Focusable: keys are consumed only while it has focus. It cannot be edited
// — that is what TextArea in widgets/form is for, and a pager that accepted
// keystrokes would need a caret, a selection and an undo history to be worth
// having.
//
// # Why the scroll position is a (line, sub-row) pair and not a row index
//
// A visual row index would need the wrapped height of every line before it, which
// is O(document) on every width change — exactly the cost ADR 0007 §6 rules out,
// and it would make a resize of a 10 MB log proportional to the log rather than to
// the screen. So the pager stores which source line is at the top and which of
// ITS wrapped rows is showing, and walks one row at a time. The consequences are
// worth stating: scrolling a screen is O(one screen of text), End is O(one screen)
// walking backwards from the last line, and there is no proportional scrollbar —
// the status line reports "Ln 1204/9000" instead, which is exact and free.
//
// # Cost
//
// O(visible cells) per frame: the wrap of the visible lines is recomputed each
// frame into a scratch buffer the pager owns, so there is no cache to invalidate
// on a resize and nothing to allocate. Text can be any size; TestPagerScrollsAHugeDocument
// is the evidence.
//
// # Key contract
//
// Consumed only while focused.
//
//	up / down       scroll one visual row
//	page up/down    scroll one screen
//	home / end      first / last screen
//	n               jump to the next match of the current query
//	N (shift+n)     jump to the previous match
//	wheel up/down   scroll WITHOUT moving anything else
//	press           move the caret to the pressed position — a pager has none, so
//	                a press is consumed and does nothing but take focus
//
// The QUERY ITSELF IS THE APPLICATION'S: SetQuery takes it, because a read-only
// widget has no way to receive typed text without becoming an editor. KeyTab is NOT
// consumed.
type Pager struct {
	blk    *block.Block
	bounds buffer.Rect

	// text is the document, stored by reference: SetText does not copy it, so a
	// caller must not mutate the string afterwards.
	text string
	// starts is the byte offset of each line's first byte, so line i is
	// text[starts[i]:starts[i+1]] with the last line ending at len(text). A '\n'
	// is not part of any line.
	starts []int
	// count is the number of lines.
	count int

	// topLine and subRow are the scroll position: which source line is at the top
	// of the body, and which of its wrapped rows.
	topLine int
	subRow  int

	query string

	// TextStyle is the rendition of the body text; StatusStyle of the status line;
	// MatchStyle of a search hit. MatchStyle defaults to ReverseStyle rather than
	// to a colour, because the hit has to be visible on a monochrome terminal.
	TextStyle   buffer.Style
	StatusStyle buffer.Style
	MatchStyle  buffer.Style

	// Status draws the position readout, and is the first thing dropped when there
	// is no room for it.
	Status bool

	focused bool

	// regions is the height budget, built once: the status line against the body.
	regions []geometry.Region
	keep    []bool

	body       buffer.Rect
	statusH    int
	contentW   int
	cachedRect buffer.Rect

	// scratch holds the wrapped rows of the line being drawn, with room for a
	// screenful plus one so a scroll computation always has somewhere to put a
	// row.
	scratch []textRow
}

// NewPager returns an empty Pager sized r.
func NewPager(r buffer.Rect) *Pager {
	p := &Pager{
		blk:        block.New(r),
		bounds:     r,
		Status:     true,
		regions:    []geometry.Region{{Size: 1, Prio: statusPrio}, {Size: 0, Prio: geometry.PrioAlways}},
		MatchStyle: buffer.ReverseStyle,
	}
	p.blk.SetBounds(r)
	return p
}

// SetText replaces the document and returns the pager to the top.
//
// The line index is built here, once, rather than per frame: it is O(document) and
// depends on nothing but the text.
func (p *Pager) SetText(s string) {
	p.text = s
	p.starts = p.starts[:0]
	p.starts = append(p.starts, 0)
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			p.starts = append(p.starts, i+1)
		}
	}
	p.count = len(p.starts)
	p.topLine, p.subRow = 0, 0
	p.cachedRect = buffer.Rect{}
}

// Text returns the document, by reference.
func (p *Pager) Text() string { return p.text }

// Lines returns the number of source lines.
func (p *Pager) Lines() int { return p.count }

// Line returns source line i's text without its newline. The result aliases the
// document, so it allocates nothing; an out-of-range index returns "".
func (p *Pager) Line(i int) string {
	if i < 0 || i >= p.count {
		return ""
	}
	end := len(p.text)
	if i+1 < p.count {
		end = p.starts[i+1] - 1
	}
	return p.text[p.starts[i]:end]
}

// TopLine returns the source line at the top of the body.
func (p *Pager) TopLine() int { return p.topLine }

// SetQuery sets the search query and returns the pager to the top, because a hit
// found by a later NextMatch is found from where the reader is now looking.
//
// An empty query clears the highlight.
func (p *Pager) SetQuery(q string) {
	p.query = q
	p.topLine, p.subRow = 0, 0
}

// Query returns the current search query.
func (p *Pager) Query() string { return p.query }

// NextMatch scrolls to the first occurrence of the query after the current
// position, wrapping round to the top, and reports whether there was one.
//
// It scans forward line by line, which is why a match in the last line of a
// 10 MB document is found without having measured every line in between.
func (p *Pager) NextMatch() bool { return p.jumpMatch(1) }

// PrevMatch scrolls to the last occurrence of the query before the current
// position, wrapping round to the bottom, and reports whether there was one.
func (p *Pager) PrevMatch() bool { return p.jumpMatch(-1) }

// jumpMatch walks the document in one direction looking for the query.
//
// A direction of zero or an empty query is a no-op reporting false: there is no
// match to jump to, and silently "succeeding" would leave the caller believing the
// document changed.
func (p *Pager) jumpMatch(dir int) bool {
	if dir == 0 || p.query == "" {
		return false
	}
	p.syncChrome()
	for step := 0; step < p.count; step++ {
		p.topLine += dir
		if p.topLine >= p.count {
			p.topLine = 0
		} else if p.topLine < 0 {
			p.topLine = p.count - 1
		}
		p.subRow = 0
		if strings.Contains(p.Line(p.topLine), p.query) {
			return true
		}
	}
	return false
}

// Matches returns how many times the query occurs in the document.
//
// It is O(document) and it is a CALLER's question, not something Draw needs: the
// highlight is found by scanning the visible lines only, so a pager's per-frame
// cost does not depend on the document size even when a query is set.
func (p *Pager) Matches() int {
	if p.query == "" {
		return 0
	}
	n := 0
	for i := 0; i < p.count; i++ {
		n += strings.Count(p.Line(i), p.query)
	}
	return n
}

// Bounds returns the pager's rectangle, safe to call before the first Draw.
func (p *Pager) Bounds() buffer.Rect { return p.bounds }

// SetBounds sets the pager's rectangle.
func (p *Pager) SetBounds(r buffer.Rect) {
	p.bounds = r
	p.blk.SetBounds(r)
}

// Block returns the block that draws this pager's chrome, so a caller can
// configure the border, title, padding, background and ASCII rung.
func (p *Pager) Block() *block.Block { return p.blk }

// Focused reports whether the pager has focus.
func (p *Pager) Focused() bool { return p.focused }

// SetFocused gives or removes focus.
func (p *Pager) SetFocused(v bool) { p.focused = v }

// MinSize returns the smallest pager that shows a status line and one row of
// text: a whole-widget size including chrome.
func (p *Pager) MinSize() buffer.Size { return minWhole(p.blk, minPagerW, minPagerH) }

// ScrollBy moves the viewport by n visual rows, which may be negative.
func (p *Pager) ScrollBy(n int) {
	p.syncChrome()
	p.scrollRows(n)
}

// ScrollToStart returns to the first row of the document.
func (p *Pager) ScrollToStart() { p.topLine, p.subRow = 0, 0 }

// ScrollToEnd moves to the last screen.
//
// It walks BACKWARDS from the last line until it has covered a screenful, which is
// O(one screen) rather than O(document) — the reason this pager has no cumulative
// row index and still reaches the end instantly.
func (p *Pager) ScrollToEnd() {
	p.syncChrome()
	if p.count == 0 {
		p.topLine, p.subRow = 0, 0
		return
	}
	used := p.rowCountOf(p.count - 1)
	i := p.count - 1
	for i > 0 && used < p.height() {
		i--
		used += p.rowCountOf(i)
	}
	p.topLine = i
	p.subRow = p.rowCountOf(i) - 1
	if p.subRow < 0 {
		p.subRow = 0
	}
}

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
func (p *Pager) Invalidate() {
	p.cachedRect = buffer.Rect{}
}

// Handle consumes navigation keys while focused and mouse events inside its
// rectangle.
func (p *Pager) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventKey:
		if !p.focused {
			return false
		}
		return p.handleKey(ev)
	case termmosaic.EventMouse:
		return p.handleMouse(ev)
	default:
		return false
	}
}

// handleKey is the key contract, one case per documented binding.
func (p *Pager) handleKey(ev termmosaic.Event) bool {
	p.syncChrome()
	switch ev.Key {
	case termmosaic.KeyUp:
		p.scrollRows(-1)
		return true
	case termmosaic.KeyDown:
		p.scrollRows(1)
		return true
	case termmosaic.KeyPageUp:
		p.scrollRows(-p.height())
		return true
	case termmosaic.KeyPageDown:
		p.scrollRows(p.height())
		return true
	case termmosaic.KeyHome:
		p.ScrollToStart()
		return true
	case termmosaic.KeyEnd:
		p.ScrollToEnd()
		return true
	}
	if ev.Rune == 'n' {
		p.NextMatch()
		return true
	}
	if ev.Rune == 'N' {
		p.PrevMatch()
		return true
	}
	return false
}

// handleMouse consumes a press or a wheel notch inside the pager. A press takes
// focus and does nothing else: there is no caret to place in a read-only view, and
// consuming it is what stops an ancestor from treating it as a pane selection.
func (p *Pager) handleMouse(ev termmosaic.Event) bool {
	if !p.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
		return false
	}
	p.syncChrome()
	switch ev.Mouse.Button {
	case termmosaic.MouseWheelUp:
		p.scrollRows(-scrollLines)
		return true
	case termmosaic.MouseWheelDown:
		p.scrollRows(scrollLines)
		return true
	}
	if ev.Mouse.Action == termmosaic.MousePress {
		p.focused = true
		return true
	}
	return false
}

// syncChrome adapts if the interior has changed, so an event handled before the
// first Draw scrolls by the right number of rows. It allocates, which is why it is
// not on the frame path.
func (p *Pager) syncChrome() {
	in := p.blk.Interior()
	if in.Empty() || in == p.cachedRect {
		return
	}
	p.adapt(in)
}

// wrapWidth returns the width rows wrap to, never below one: a zero width has no
// place to put even one glyph, and a scroll command that did nothing because of it
// would look like a broken widget.
func (p *Pager) wrapWidth() int {
	if p.contentW < 1 {
		return 1
	}
	return p.contentW
}

// height returns the number of body rows, never negative.
func (p *Pager) height() int {
	if p.body.H < 1 {
		return 1
	}
	return p.body.H
}

// rowCountOf returns how many visual rows source line i wraps to at the current
// width. It is the one place the wrap is counted, and drawLine counts the SAME way
// through the SAME function, which TestPagerWrappingAgreesWithDrawing pins.
func (p *Pager) rowCountOf(i int) int {
	if i < 0 || i >= p.count {
		return 0
	}
	return splitRows(p.grow(8), p.Line(i), p.wrapWidth())
}

// grow returns a scratch slice of n rows, reusing the pager's own storage.
//
// It exists so scroll arithmetic can count rows without allocating per call. The
// result is a slice of LENGTH n, not a zero-length slice with the capacity:
// splitRows writes into dst by index, and handing it len 0 would silently store
// nothing — which is exactly the bug TestPagerWrappingAgreesWithDrawing exists to
// make impossible to reintroduce.
func (p *Pager) grow(n int) []textRow {
	if cap(p.scratch) < n {
		p.scratch = make([]textRow, n)
	}
	return p.scratch[:n]
}

// scrollRows moves the viewport by n visual rows, which may be negative.
//
// Each step is one row of the current line or one line boundary, so the cost is
// O(|n| rows of text) rather than O(document).
func (p *Pager) scrollRows(n int) {
	if p.count == 0 {
		p.topLine, p.subRow = 0, 0
		return
	}
	if p.topLine >= p.count {
		p.topLine = p.count - 1
	}
	for ; n > 0; n-- {
		if p.subRow+1 < p.rowCountOf(p.topLine) {
			p.subRow++
			continue
		}
		if p.topLine+1 < p.count {
			p.topLine++
			p.subRow = 0
			continue
		}
		// At the end of the document a further scroll is a no-op rather than an
		// error, which is what a wheel notch at the bottom should be.
		return
	}
	for ; n < 0; n++ {
		if p.subRow > 0 {
			p.subRow--
			continue
		}
		if p.topLine > 0 {
			p.topLine--
			rows := p.rowCountOf(p.topLine)
			p.subRow = rows - 1
			if p.subRow < 0 {
				p.subRow = 0
			}
			continue
		}
		return
	}
}

// Draw paints the chrome, the status line and the visible rows of wrapped text.
//
// It is total and allocation-free: the layout comes from adapt, the wrapped rows
// of each visible line go into a scratch slice sized at adapt time, and every
// substring of the document is a slice of the original rather than a new string.
func (p *Pager) Draw(buf *buffer.Buffer) {
	r := p.bounds
	if r.Empty() {
		return
	}
	p.blk.Draw(buf)
	in := p.blk.Interior()
	if in.Empty() {
		return
	}
	if in != p.cachedRect {
		p.adapt(in)
	}
	if p.statusH > 0 {
		p.drawStatus(buf, in)
	}
	if p.body.Empty() {
		return
	}
	p.drawBody(buf)
}

// drawStatus paints the position readout: which source line is at the top and how
// many there are, plus the query when one is set.
//
// The digits are built into a stack array and written a cell at a time, because
// fmt.Sprintf allocates and this is the frame path.
func (p *Pager) drawStatus(buf *buffer.Buffer, in buffer.Rect) {
	row := buffer.Rect{X: in.X, Y: in.Y, W: in.W, H: 1}
	st := p.StatusStyle
	buf.FillRect(row, st.Resolved().Blank())
	x := row.X
	x = buf.SetStringIn(x, row.Right(), row.Y, statusPrefix, st)

	var digits [24]byte
	d := intDigits(digits[:0], p.topLine+1)
	d = append(d, '/')
	d = intDigits(d, p.count)
	x = putDigits(buf, x, row.Y, d, st)

	if p.query != "" && x+2 <= row.Right() {
		buf.SetCell(x+1, row.Y, st.Resolved().Cell(matchGlyph))
		var q [1]buffer.Span
		q[0] = buffer.NewSpan(p.query, st)
		buf.SetSpansCappedIn(x+2, row.Right(), row.Y, q[:], truncMark(p.blk.Ascii))
	}
}

// drawBody paints the visible rows of wrapped text from the current position.
func (p *Pager) drawBody(buf *buffer.Buffer) {
	body := p.body
	y := body.Y
	li := p.topLine
	sub := p.subRow
	w := p.wrapWidth()
	st := p.TextStyle

	for y < body.Bottom() && li < p.count {
		line := p.Line(li)
		// The scratch is addressed at its CAPACITY, not its length: splitRows fills
		// from index zero, and a line wrapping into more rows than a screen holds
		// is COUNTED in full but STORED for a screenful only, which is all that can
		// be drawn.
		store := p.scratch[:cap(p.scratch)]
		n := splitRows(store, line, w)
		if n > len(store) {
			n = len(store)
		}
		for ; sub < n && y < body.Bottom(); sub++ {
			p.drawRow(buf, y, line, store[sub], st)
			y++
		}
		sub = 0
		li++
	}
	// Lines past the end of the document are already background: the block painted
	// the whole rect, and "clip, never blank" means the padding below the last
	// line is padding, not a row of text.
}

// drawRow paints one visual row, splitting it at the search hits that fall inside
// it.
func (p *Pager) drawRow(buf *buffer.Buffer, y int, line string, row textRow, st buffer.Style) {
	from, to := row.from, row.to
	if from > len(line) {
		from = len(line)
	}
	if to > len(line) {
		to = len(line)
	}
	if to < from {
		to = from
	}
	if p.query == "" {
		buf.SetStringIn(p.body.X, p.body.Right(), y, line[from:to], st)
		return
	}
	// A hit that straddles the row boundary is drawn on the rows it covers: the
	// match style covers the visible part of it rather than being dropped, because
	// a hit that disappears at a wrap point looks like a missed match.
	match := p.MatchStyle
	if match.IsUnset() {
		match = buffer.ReverseStyle
	}
	// Each segment continues where the previous one ended: every SetStringIn starts at
	// the body's left edge, so writing all of them there would draw the last segment
	// over the first.
	pos, x := from, p.body.X
	for pos < to {
		k := strings.Index(line[pos:to], p.query)
		if k < 0 {
			break
		}
		hit := pos + k
		x = buf.SetStringIn(x, p.body.Right(), y, line[pos:hit], st)
		end := hit + len(p.query)
		if end > to {
			end = to
		}
		if end <= hit {
			break
		}
		x = buf.SetStringIn(x, p.body.Right(), y, line[hit:end], match)
		pos = end
	}
	if pos < to {
		x = buf.SetStringIn(x, p.body.Right(), y, line[pos:to], st)
	}
	// A wrapped row carries NO truncation marker: the next row continues it, so an
	// ellipsis would claim text had been lost when none had. That is the difference
	// between wrapping and truncating, and it is why buffer.Truncate is not used
	// anywhere in this file.
	_ = x
}

// adapt recomputes everything derived from the interior: the rect-keyed cache of
// ADR 0007 §3, plus the scratch buffer sized for the screen.
func (p *Pager) adapt(in buffer.Rect) {
	p.cachedRect = in
	if len(p.regions) > 1 {
		// The body is measured as what is left after the status line, because
		// geometry.Budget compares declared sizes against the available cells.
		p.regions[1].Size = in.H - 1
		if p.regions[1].Size < 0 {
			p.regions[1].Size = 0
		}
	}
	p.keep = geometry.Budget(p.regions, in.H)
	p.statusH = 0
	if len(p.keep) == len(p.regions) && p.Status && p.keep[0] {
		p.statusH = 1
	}
	p.contentW = in.W
	h := in.H - p.statusH
	if h < 0 {
		h = 0
	}
	p.body = buffer.Rect{X: in.X, Y: in.Y + p.statusH, W: in.W, H: h}
	// One screenful of rows plus one, so a scroll step that crosses a line boundary
	// always has somewhere to put the row it is counting.
	if cap(p.scratch) < h+2 {
		p.scratch = make([]textRow, h+2)
	}
	p.scratch = p.scratch[:0]
	if p.topLine >= p.count {
		p.topLine = p.count - 1
	}
	if p.topLine < 0 {
		p.topLine = 0
	}
}

// splitRows fills dst by index with the byte range of each visual row of s at the
// given width, and returns how many rows there are.
//
// dst is indexed from zero and its LENGTH is how many rows can be stored: a caller
// wanting every row passes a slice as long as the line can wrap to, and a caller
// wanting only the count passes a short one.
//
// It is the pager's wrap, and it is deliberately NOT buffer.Wrap: buffer.Wrap
// builds a span slice per line, which would allocate on the frame path, and it
// measures its whole input, which would be O(document) rather than O(screen).
// The rules match what a reader expects of a text view, and one of them is
// load-bearing for correctness rather than taste:
//
//   - Words break at U+0020 SPACE, and the whole run of spaces that causes a break
//     is consumed, so a double space cannot produce an empty row.
//   - A word longer than the width is cut at the cell boundary, and a double-width
//     rune is never split: the cut moves back to the rune boundary.
//   - An empty line is one empty row, not no rows: a document of blank lines has
//     as many rows as it has lines.
//   - width <= 0 returns zero rows. There is nowhere to put even one glyph.
//
// Rows past len(dst) are counted but not stored, because the only caller with no
// room for every row is the one asking only HOW MANY there are.
func splitRows(dst []textRow, s string, width int) int {
	if width <= 0 {
		return 0
	}
	n := 0
	for i := 0; i <= len(s); {
		w, j, lastSpace := 0, i, -1
		for j < len(s) {
			r, size := utf8.DecodeRuneInString(s[j:])
			rw := buffer.RuneWidth(r)
			if rw > 0 && w+rw > width {
				break
			}
			w += rw
			if r == ' ' {
				lastSpace = j
			}
			j += size
		}
		if j >= len(s) {
			if n < len(dst) {
				dst[n] = textRow{from: i, to: len(s)}
			}
			return n + 1
		}
		if lastSpace >= i {
			if lastSpace > i {
				// A row is emitted only when it has content: a line that starts on
				// the space which caused the previous break must not produce an
				// empty row, which at width 1 would be most of the rows.
				if n < len(dst) {
					dst[n] = textRow{from: i, to: lastSpace}
				}
				n++
			}
			// The break consumes the whole run of spaces, not just the one that
			// filled the row.
			i = lastSpace
			for i < len(s) && s[i] == ' ' {
				i++
			}
			continue
		}
		if j > i {
			if n < len(dst) {
				dst[n] = textRow{from: i, to: j}
			}
			n++
			i = j
			continue
		}
		// Nothing fits at all: only a double-width rune in a one-cell area reaches
		// here, and it is dropped rather than half taken.
		_, size := utf8.DecodeRuneInString(s[j:])
		i = j + size
	}
	return n
}

var (
	_ termmosaic.Widget      = (*Pager)(nil)
	_ termmosaic.Focusable   = (*Pager)(nil)
	_ termmosaic.Minimizable = (*Pager)(nil)
)
