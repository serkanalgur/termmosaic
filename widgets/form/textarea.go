package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// TextArea is a multi-line editable text region that wraps rather than scrolls
// horizontally.
//
// The wrapping is buffer's, not this widget's: the content is handed to
// buffer.Wrap and the resulting Wrapped is cached against the rect it was built
// for. Wrap allocates twice over, so calling it in Draw would add an allocation
// to every frame — the failure ADR 0008 §4 exists to prevent. The first Draw
// after a width change wraps; every Draw after that reads the cache.
//
// # Key contract
//
// Keys are consumed only while focused, and the caret moves by VISUAL line and
// cell column, so Up and Down follow the wrapping rather than the newline
// characters.
//
//	insert         any printable rune
//	newline        KeyEnter inserts U+000A
//	backspace      KeyBackspace; ModCtrl or ModAlt deletes the word before
//	delete         KeyDelete; ModCtrl or ModAlt deletes the word after
//	left / right   KeyLeft / KeyRight; ModShift extends the selection, ModCtrl
//	               or ModAlt moves by word
//	up / down      KeyUp / KeyDown, one visual line
//	home / end     KeyHome / KeyEnd within the visual line; with ModCtrl, within
//	               the whole text
//	page up/down   KeyPageUp / KeyPageDown, one screen
//	select all     ModCtrl+'a'
//	undo           ModCtrl+'z'
//	paste          EventPaste, inserted verbatim INCLUDING newlines
//
// KeyTab is NOT consumed: in a form, tab moves between fields, and swallowing it
// here would make a text area impossible to leave with the keyboard.
type TextArea struct {
	ed     editor
	bounds buffer.Rect

	// topLine is the first visible wrapped line. Scrolling is vertical only,
	// because wrapping means there is no horizontal overflow to scroll to.
	topLine int

	// focused reports whether the area has focus.
	focused bool

	// TextStyle is the style of the text. There is no selection style: a
	// multi-line selection is not in this widget's contract, and half a
	// selection is worse than none.
	TextStyle buffer.Style

	// CursorStyle is the style of the rune under the caret and of the block
	// drawn past the last character. An unset value means "the text style with
	// AttrReverse and AttrUnderline", the same rule TextInput uses, so the two
	// widgets' carets look alike.
	CursorStyle buffer.Style

	// Background is painted across Bounds before anything else.
	Background buffer.Style

	// OnChange is called after every accepted edit, once per operation — never
	// once per pasted character.
	OnChange func(string)

	// starts is the rune index of each wrapped line's first rune and ends the
	// index just past its last rune, both copied from buffer.Wrap's LineRange
	// result. They are kept as two slices because the caret arithmetic below reads
	// one or the other on its own: ends[i] is NOT starts[i+1], since the space Wrap
	// consumed at a break sits between them.
	starts []int
	ends   []int
	// lines holds the cached display runs for each wrapped line, each already
	// carrying the caret style where the caret is.
	lines [][]buffer.Span

	cache       visibleCache
	cacheStyles areaStyles
}

// areaStyles is the set of resolved styles a TextArea's cached display depends
// on. It is comparable, so one equality test covers all of them.
type areaStyles struct {
	text, cursor buffer.Style
}

// NewTextArea returns an empty TextArea sized r.
func NewTextArea(r buffer.Rect) *TextArea {
	a := &TextArea{bounds: r}
	a.ed.multiLine = true
	a.ed.stale = true
	return a
}

// NewTextAreaString returns a TextArea holding s, sized r.
func NewTextAreaString(r buffer.Rect, s string) *TextArea {
	a := NewTextArea(r)
	a.ed.setText(s)
	return a
}

// Bounds returns the area's rectangle, safe to call before the first Draw.
func (a *TextArea) Bounds() buffer.Rect { return a.bounds }

// SetBounds sets the area's rectangle. The wrap cache is keyed on it, so the
// next Draw re-wraps at the new width.
func (a *TextArea) SetBounds(r buffer.Rect) { a.bounds = r }

// Focused reports whether the area has focus.
func (a *TextArea) Focused() bool { return a.focused }

// SetFocused gives or removes focus.
func (a *TextArea) SetFocused(v bool) { a.focused = v; a.ed.stale = true }

// MinSize returns the smallest meaningful area: one cell by one cell, which
// shows one character.
func (a *TextArea) MinSize() buffer.Size { return buffer.Size{W: 1, H: 1} }

// SetText replaces the whole content and resets the caret, the scroll offset and
// the undo history.
func (a *TextArea) SetText(s string) {
	a.ed.setText(s)
	a.topLine = 0
	a.changed()
}

// Text returns the content.
func (a *TextArea) Text() string { return a.ed.String() }

// Len returns the number of runes in the content.
func (a *TextArea) Len() int { return len(a.ed.text) }

// Cursor returns the caret's rune index.
func (a *TextArea) Cursor() int { return a.ed.cursor }

// SetCursor moves the caret to rune index i, clamped into the content, and
// scrolls it into view.
func (a *TextArea) SetCursor(i int) {
	a.ed.setCursor(i, false)
	a.ed.scrollIntoView(a.bounds.W)
	a.ensureVisible()
}

// Selection returns the selected rune range as [lo, hi) and whether there is a
// selection. An area selects with Shift while moving; nothing renders a
// selection, which is stated here so a caller is not surprised by SelectedText
// returning text the screen does not mark.
func (a *TextArea) Selection() (lo, hi int, ok bool) { return a.ed.selection() }

// SelectedText returns the selected content, or "" when nothing is selected.
func (a *TextArea) SelectedText() string {
	lo, hi, ok := a.ed.selection()
	if !ok {
		return ""
	}
	return string(a.ed.text[lo:hi])
}

// SelectAll selects the whole content.
func (a *TextArea) SelectAll() {
	a.ed.selectAll()
	a.ensureVisible()
}

// Undo reverts the most recent edit and reports whether there was one.
func (a *TextArea) Undo() bool {
	if !a.ed.undoLast() {
		return false
	}
	a.ed.scrollIntoView(a.bounds.W)
	a.ensureVisible()
	a.changed()
	return true
}

// UndoDepth returns how many undo steps are available.
func (a *TextArea) UndoDepth() int { return len(a.ed.undo) }

// ScrollOffset returns the first visible wrapped line.
func (a *TextArea) ScrollOffset() int { return a.topLine }

// LineCount returns how many lines the current wrap produced, which is how a
// caller knows whether the content overflows. It is 0 before the first Draw,
// because the wrap depends on the width and there is no width yet.
func (a *TextArea) LineCount() int { return len(a.lines) }

// changed reports that the content changed.
func (a *TextArea) changed() {
	if a.OnChange != nil {
		a.OnChange(a.ed.String())
	}
}

// styles returns the resolved styles the display is built with.
func (a *TextArea) styles() areaStyles {
	text := a.TextStyle.Resolved()
	cur := a.CursorStyle.Resolved()
	if a.CursorStyle.IsUnset() {
		cur = text.WithAttr(buffer.AttrReverse | buffer.AttrUnderline)
	}
	return areaStyles{text: text, cursor: cur}
}

// Draw paints the background and then as many wrapped lines as fit.
//
// It is total for every rect and allocates nothing once the cache is warm.
func (a *TextArea) Draw(buf *buffer.Buffer) {
	r := a.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, a.Background.Resolved().Blank())

	if r.W <= 0 || r.H <= 0 {
		return
	}

	styles := a.styles()
	if !a.cache.matches(r, a.ed.stale) || styles != a.cacheStyles {
		a.rebuild(r, styles)
		a.cache.store(r)
		a.cacheStyles = styles
		a.ed.stale = false
	}

	for k := 0; k < r.H; k++ {
		line := a.topLine + k
		if line >= len(a.lines) {
			break
		}
		if len(a.lines[line]) == 0 {
			continue
		}
		buf.SetSpans(r.X, r.Y+k, a.lines[line])
	}
}

// rebuild re-wraps the content and recomputes the display runs. It is the only
// allocating operation in TextArea.
//
// The whole content is handed to buffer.Wrap in one call and the line→rune
// mapping comes back with the result, as buffer.LineRange. This widget used to
// split the text on newlines itself, wrap each logical line separately and
// re-derive each line's rune bounds from the consumed-space rule; that was a
// second implementation of Wrap's breaking rules living in a widget, which is
// the drift ADR 0008 §2 forbids, and it was wrong in a way only the second kind
// of input exposed: the derivation counted the runes that reached a cell, so one
// combining mark shifted every caret position after it by one. Wrap counts
// zero-width runes in its offsets precisely so this widget does not have to
// think about it.
func (a *TextArea) rebuild(r buffer.Rect, styles areaStyles) {
	a.starts = nil
	a.ends = nil
	a.lines = nil

	wrapped := buffer.Wrap([]buffer.Span{buffer.NewSpan(string(a.ed.text), styles.text)}, r.W)
	ranges := wrapped.Ranges
	if len(ranges) == 0 {
		// An empty area still occupies a row, and that row is where the caret
		// is. Wrap returns no lines for text with nothing in it, which is right
		// for a paragraph and wrong for an editable one.
		ranges = []buffer.LineRange{{}}
	}
	for _, rg := range ranges {
		a.starts = append(a.starts, rg.Start)
		a.ends = append(a.ends, rg.End)
		a.lines = append(a.lines, nil)
	}

	for i := range a.lines {
		start, end := a.starts[i], a.ends[i]
		if start >= end {
			continue
		}
		b := newRunBuilder(a.ed.text)
		for j := start; j < end; j++ {
			st := styles.text
			if a.focused && j == a.ed.cursor {
				st = styles.cursor
			}
			b.at(j, st)
		}
		b.close(end)
		a.lines[i] = b.done()
	}
	// The caret at the very end of the content has no rune to wear the cursor
	// style, so it is drawn as a block on the last line, when there is a cell
	// left for it.
	if a.focused && len(a.lines) > 0 && a.ed.cursor >= len(a.ed.text) {
		last := len(a.lines) - 1
		if last >= a.topLine && last < a.topLine+r.H &&
			buffer.SpansWidth(a.lines[last]) < r.W {
			a.lines[last] = append(a.lines[last], buffer.NewSpan(" ", styles.cursor))
		}
	}
}

// lineEnd returns the rune index just past line i's content, which is where the
// caret goes at the end of the line. It is 0 for an unknown line, so a caller
// that asks before the first Draw gets an answer rather than a panic.
func (a *TextArea) lineEnd(line int) int {
	if line < 0 || line >= len(a.ends) {
		return 0
	}
	return a.ends[line]
}

// lineOf returns the wrapped line holding rune index i, or 0 when the wrap is
// not known yet — before the first Draw, or at a rect too narrow to wrap
// anything. Returning 0 is the safe answer: it puts the caret on the first line,
// which is the only line a caller can see in that state.
func (a *TextArea) lineOf(i int) int {
	if i < 0 {
		return 0
	}
	for n := len(a.starts) - 1; n >= 0; n-- {
		if i >= a.starts[n] {
			return n
		}
	}
	return 0
}

// caretColumn returns the caret's cell column within its wrapped line.
func (a *TextArea) caretColumn() int {
	line := a.lineOf(a.ed.cursor)
	return cellWidth(a.ed.text, a.starts[line], a.ed.cursor)
}

// runeAt returns the rune index at visual line and cell column, which is what a
// mouse click needs.
//
// A click inside a double-width rune lands before it, and a click past a line's
// end lands at the line's end, so a click never splits a glyph and never leaves
// the line.
func (a *TextArea) runeAt(line, col int) int {
	if line < 0 {
		line = 0
	}
	if line >= len(a.starts) {
		return len(a.ed.text)
	}
	start, end := a.starts[line], a.lineEnd(line)
	if col < 0 {
		col = 0
	}
	w := 0
	for i := start; i < end; i++ {
		rw := buffer.RuneWidth(a.ed.text[i])
		if rw == 0 {
			continue
		}
		if w >= col {
			return i
		}
		if w+rw > col {
			return i
		}
		w += rw
	}
	return end
}

// ensureVisible scrolls the minimum amount that brings the caret's line inside
// the viewport: the clamp-not-recentre rule of ADR 0007 §6, applied to a text
// area.
func (a *TextArea) ensureVisible() {
	h := a.bounds.H
	if h < 1 {
		h = 1
	}
	line := a.lineOf(a.ed.cursor)
	if line < a.topLine {
		a.topLine = line
	}
	if line >= a.topLine+h {
		a.topLine = line - h + 1
	}
	if a.topLine < 0 {
		a.topLine = 0
	}
}

// Invalidate satisfies termmosaic.Widget. The area repaints in full every frame.
func (a *TextArea) Invalidate() {}

// Handle offers ev to the area and reports whether it consumed it.
func (a *TextArea) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventPaste:
		if !a.focused {
			return false
		}
		// One operation, one undo step, one OnChange call. A multi-line paste
		// keeps its newlines verbatim, which is the entire difference between
		// this and TextInput's handling of the same event.
		a.ed.insertText(ev.Text)
		a.afterEdit()
		return true

	case termmosaic.EventMouse:
		return a.handleMouse(ev)

	case termmosaic.EventKey:
		return a.handleKey(ev)

	default:
		return false
	}
}

// afterEdit scrolls the caret into view and reports the change.
func (a *TextArea) afterEdit() {
	a.ensureVisible()
	a.changed()
}

// handleMouse places the caret on a click and takes focus. A click outside
// Bounds is not consumed and changes nothing.
func (a *TextArea) handleMouse(ev termmosaic.Event) bool {
	r := a.bounds
	m := ev.Mouse
	if m.Action != termmosaic.MousePress && m.Action != termmosaic.MouseDrag {
		return false
	}
	if m.Button != termmosaic.MouseLeft || !r.Contains(m.X, m.Y) {
		return false
	}
	col := m.X - r.X
	if col > r.W {
		col = r.W
	}
	a.ed.setCursor(a.runeAt(a.topLine+(m.Y-r.Y), col), false)
	a.focused = true
	a.ed.clearSelection()
	return true
}

// handleKey applies one key, returning false for anything the area does not own.
func (a *TextArea) handleKey(ev termmosaic.Event) bool {
	if !a.focused {
		return false
	}
	if ev.Type == termmosaic.KeyRelease {
		return false
	}
	ctrl := ev.Mod.Has(termmosaic.ModCtrl)
	alt := ev.Mod.Has(termmosaic.ModAlt)
	word := ctrl || alt
	shift := ev.Mod.Has(termmosaic.ModShift)

	switch ev.Key {
	case termmosaic.KeyLeft:
		to := a.ed.cursor - 1
		if word {
			to = wordLeft(a.ed.text, a.ed.cursor)
		}
		a.move(to, shift)
		return true
	case termmosaic.KeyRight:
		to := a.ed.cursor + 1
		if word {
			to = wordRight(a.ed.text, a.ed.cursor)
		}
		a.move(to, shift)
		return true
	case termmosaic.KeyUp:
		a.move(a.verticalTarget(-1), shift)
		return true
	case termmosaic.KeyDown:
		a.move(a.verticalTarget(1), shift)
		return true
	case termmosaic.KeyHome:
		if ctrl {
			a.move(0, shift)
		} else {
			a.move(a.starts[a.lineOf(a.ed.cursor)], shift)
		}
		return true
	case termmosaic.KeyEnd:
		if ctrl {
			a.move(len(a.ed.text), shift)
		} else {
			a.move(a.lineEnd(a.lineOf(a.ed.cursor)), shift)
		}
		return true
	case termmosaic.KeyPageUp:
		a.move(a.pageTarget(-1), shift)
		return true
	case termmosaic.KeyPageDown:
		a.move(a.pageTarget(1), shift)
		return true
	case termmosaic.KeyBackspace:
		if !a.ed.backspace(word) {
			return false
		}
		a.afterEdit()
		return true
	case termmosaic.KeyDelete:
		if !a.ed.forwardDelete(word) {
			return false
		}
		a.afterEdit()
		return true
	case termmosaic.KeyEnter:
		a.ed.insertText("\n")
		a.afterEdit()
		return true
	}

	if ev.Key != termmosaic.KeyNone || ev.Rune == 0 {
		return false
	}
	if ctrl || alt {
		switch controlAction(ev.Rune) {
		case ctrlSelectAll:
			a.SelectAll()
			return true
		case ctrlUndo:
			a.Undo()
			return true
		case ctrlWordBackspace:
			if !a.ed.backspace(true) {
				return false
			}
			a.afterEdit()
			return true
		case ctrlWordDelete:
			if !a.ed.forwardDelete(true) {
				return false
			}
			a.afterEdit()
			return true
		case ctrlEnd:
			a.move(len(a.ed.text), shift)
			return true
		}
		return false
	}
	if ev.Rune < 0x20 {
		return false
	}
	a.ed.insertText(string(ev.Rune))
	a.afterEdit()
	return true
}

// move moves the caret and scrolls it into view.
func (a *TextArea) move(i int, extend bool) {
	a.ed.setCursor(i, extend)
	a.ed.scrollIntoView(a.bounds.W)
	a.ensureVisible()
}

// verticalTarget returns the caret position one visual line up or down,
// preserving the cell column. Moving past the first or last line lands at the
// start or the end of the text.
func (a *TextArea) verticalTarget(delta int) int {
	line := a.lineOf(a.ed.cursor)
	col := a.caretColumn()
	switch {
	case delta < 0 && line == 0:
		return 0
	case delta > 0 && line >= len(a.starts)-1:
		return len(a.ed.text)
	}
	return a.runeAt(line+delta, col)
}

// pageTarget returns the caret position one screen up or down. A screen too
// short to move lands at the first or last line rather than dividing by zero.
func (a *TextArea) pageTarget(delta int) int {
	step := a.bounds.H
	if step < 1 {
		step = 1
	}
	line := a.lineOf(a.ed.cursor)
	col := a.caretColumn()
	target := line + delta*step
	last := len(a.starts) - 1
	if target > last {
		target = last
	}
	if target < 0 {
		target = 0
	}
	if target == line {
		// A page key in a one-line viewport has nowhere to go; landing at the
		// text's end is the only movement that means anything.
		if delta > 0 {
			return len(a.ed.text)
		}
		return 0
	}
	return a.runeAt(target, col)
}

// TextArea is a Widget, a Focusable and a Minimizable.
var (
	_ termmosaic.Widget      = (*TextArea)(nil)
	_ termmosaic.Focusable   = (*TextArea)(nil)
	_ termmosaic.Minimizable = (*TextArea)(nil)
)
