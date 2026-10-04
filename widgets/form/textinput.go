package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// TextInput is a single-line editable text field: a caret, an optional
// selection, word motions, undo, and bracketed paste.
//
// It is the reference implementation of the whole form set, because ADR 0005 §4
// was written about it. Three properties are load-bearing and each is pinned by
// a test:
//
//   - EventPaste is ONE undoable operation. A 10,000-character paste is one
//     step on the undo stack, so one Ctrl-Z removes the whole of it.
//   - Draw allocates nothing in steady state. The visible runs are rebuilt only
//     when the text, the caret, the styles or the rect change, which is
//     ADR 0007 §3's rect-keyed adaptation applied to a per-keystroke value.
//   - Draw is total for every rect, including 0×0 and 1×1, and below its
//     MinSize it clips rather than blanking.
//
// # Key contract
//
// Keys are consumed only while the field is focused, so an unfocused field in a
// form cannot be typed into. Modifiers are read through KeyMod.Has, which is why
// every binding below has a spelled-out modifier rather than an exact match.
//
//	insert         any printable rune
//	backspace      KeyBackspace; with ModCtrl or ModAlt, the word before
//	               ModCtrl+'h' is accepted too, because a terminal sends
//	               Ctrl-Backspace as 0x08 or as 'h' depending on terminfo
//	delete         KeyDelete; with ModCtrl or ModAlt, the word after
//	left / right   KeyLeft / KeyRight; with ModShift extends the selection, with
//	               ModCtrl or ModAlt moves by word
//	home / end     KeyHome / KeyEnd; with ModCtrl the whole text
//	select all     ModCtrl+'a'
//	undo           ModCtrl+'z'
//	paste          EventPaste, inserted verbatim minus CR and LF
//
// A newline cannot be typed: KeyEnter is NOT consumed, so the enclosing form
// decides what Enter means. That is why multi-line editing is TextArea's job and
// not this widget's.
type TextInput struct {
	ed     editor
	bounds buffer.Rect

	// focused reports whether the field has focus. It is a field rather than
	// derived so that Focused is a cheap read on the event path.
	focused bool

	// TextStyle is the style of unselected text.
	TextStyle buffer.Style

	// SelectionStyle is the style of selected text. An unset value means "the
	// text style with AttrReverse", so selection is visible without the
	// application configuring anything — and remains visible under NO_COLOR,
	// because an attribute survives it.
	SelectionStyle buffer.Style

	// CursorStyle is the style of the rune under the caret, and of the block
	// drawn at the caret when it is past the last character. An unset value
	// means "the text style with AttrReverse and AttrUnderline" — reverse like
	// a selection so the two read as related, underlined so they are still
	// distinguishable from each other without colour.
	CursorStyle buffer.Style

	// Placeholder is shown when the field is empty. It is truncated with the
	// truncation marker rather than clipped, so an over-long placeholder tells
	// the user there is more of it.
	Placeholder string

	// PlaceholderStyle is the style of the placeholder. An unset value resolves
	// to the terminal's own colours with no attributes; MutedStyle is the usual
	// choice.
	PlaceholderStyle buffer.Style

	// Background is painted across Bounds before anything else. An unset value
	// resolves to the terminal's own colours.
	Background buffer.Style

	// Ascii selects buffer's ASCII truncation rung: pass !caps.Unicode. It
	// affects the placeholder's truncation marker only.
	Ascii bool

	// OnChange is called after every accepted edit, with the new content. It is
	// called ONCE per EventPaste, never once per pasted character.
	OnChange func(string)

	// spans is the cached display: the visible runes grouped by style, already
	// cut to the field's width.
	spans []buffer.Span
	// cache tracks the rect spans was built for, and cacheStyles the resolved
	// styles it was built with, so mutating a Style field directly rebuilds.
	cache       visibleCache
	cacheStyles fieldStyles
}

// fieldStyles is the set of resolved styles a field's cached display depends
// on. It is comparable, so one equality test covers all of them.
type fieldStyles struct {
	text, selection, cursor, placeholder buffer.Style
}

// NewTextInput returns an empty TextInput sized r.
func NewTextInput(r buffer.Rect) *TextInput {
	t := &TextInput{bounds: r}
	t.ed.stale = true
	return t
}

// NewTextInputString returns a TextInput holding s, sized r.
func NewTextInputString(r buffer.Rect, s string) *TextInput {
	t := NewTextInput(r)
	t.ed.setText(s)
	return t
}

// Bounds returns the field's rectangle, safe to call before the first Draw.
func (t *TextInput) Bounds() buffer.Rect { return t.bounds }

// SetBounds sets the field's rectangle. It must reflect the new rectangle before
// the next Draw; the cached display is keyed on it, so a new one makes the next
// Draw rebuild.
func (t *TextInput) SetBounds(r buffer.Rect) { t.bounds = r }

// Focused reports whether the field has focus.
func (t *TextInput) Focused() bool { return t.focused }

// SetFocused gives or removes focus.
func (t *TextInput) SetFocused(v bool) { t.focused = v; t.ed.stale = true }

// MinSize returns the smallest meaningful field: one cell, showing one
// character. There is no width below which a text field has nothing to say,
// because the caret alone is information.
func (t *TextInput) MinSize() buffer.Size { return buffer.Size{W: 1, H: 1} }

// SetText replaces the whole content and resets the caret, the selection and the
// undo history.
func (t *TextInput) SetText(s string) {
	t.ed.setText(s)
	t.changed()
}

// Text returns the content.
func (t *TextInput) Text() string { return t.ed.String() }

// Len returns the number of runes in the content.
func (t *TextInput) Len() int { return len(t.ed.text) }

// Cursor returns the caret's rune index.
func (t *TextInput) Cursor() int { return t.ed.cursor }

// SetCursor moves the caret to rune index i, clamped into the content.
func (t *TextInput) SetCursor(i int) {
	t.ed.setCursor(i, false)
	t.ed.scrollIntoView(t.contentWidth())
}

// Selection returns the selected rune range as [lo, hi) and whether there is a
// selection.
func (t *TextInput) Selection() (lo, hi int, ok bool) { return t.ed.selection() }

// SelectedText returns the selected content, or "" when nothing is selected.
func (t *TextInput) SelectedText() string {
	lo, hi, ok := t.ed.selection()
	if !ok {
		return ""
	}
	return string(t.ed.text[lo:hi])
}

// SelectAll selects the whole content.
func (t *TextInput) SelectAll() {
	t.ed.selectAll()
	t.ed.scrollIntoView(t.contentWidth())
}

// Undo reverts the most recent edit and reports whether there was one.
func (t *TextInput) Undo() bool {
	if !t.ed.undoLast() {
		return false
	}
	t.ed.scrollIntoView(t.contentWidth())
	t.changed()
	return true
}

// UndoDepth returns how many undo steps are available, which is what makes "a
// paste is one operation" observable from outside.
func (t *TextInput) UndoDepth() int { return len(t.ed.undo) }

// contentWidth returns the width the field's display is built for: Bounds()
// minus nothing, because a single-line field's text starts at its left edge.
// A rect too small to measure yields 0, which every caller already handles.
func (t *TextInput) contentWidth() int {
	w := t.bounds.W
	if w < 0 {
		return 0
	}
	return w
}

// changed reports that the content changed: it fires the OnChange callback with
// the new value. It is one call per accepted operation, which is what keeps a
// 10,000-character paste from re-laying-out an application 10,000 times.
func (t *TextInput) changed() {
	if t.OnChange != nil {
		t.OnChange(t.ed.String())
	}
}

// styles returns the resolved styles the display is built with, resolving the
// two documented unset sentinels.
func (t *TextInput) styles() fieldStyles {
	text := t.TextStyle.Resolved()
	sel := t.SelectionStyle.Resolved()
	if t.SelectionStyle.IsUnset() {
		sel = text.WithAttr(buffer.AttrReverse)
	}
	cur := t.CursorStyle.Resolved()
	if t.CursorStyle.IsUnset() {
		cur = text.WithAttr(buffer.AttrReverse | buffer.AttrUnderline)
	}
	return fieldStyles{text: text, selection: sel, cursor: cur, placeholder: t.PlaceholderStyle.Resolved()}
}

// Draw paints the background, then the cached display runs.
//
// It is total for every rect and allocates nothing once the cache is warm: the
// rebuild below is the documented cost of ADR 0007 §3's rule, and it happens
// once per change rather than once per frame.
func (t *TextInput) Draw(buf *buffer.Buffer) {
	r := t.bounds
	if r.Empty() {
		// The degenerate-size contract: an empty Bounds returns immediately.
		return
	}

	// Repaint the whole rect before drawing content. The renderer diffs and
	// never clears, so without this a field that just became empty — or shrank —
	// would leave its old characters on screen.
	buf.FillRect(r, t.Background.Resolved().Blank())

	if r.W <= 0 || r.H <= 0 {
		return
	}

	styles := t.styles()
	if !t.cache.matches(r, t.ed.stale) || styles != t.cacheStyles {
		t.rebuild(r, styles)
		t.cache.store(r)
		t.cacheStyles = styles
		t.ed.stale = false
	}
	if len(t.spans) == 0 {
		return
	}
	buf.SetSpans(r.X, r.Y, t.spans)
}

// rebuild recomputes the cached display runs for a rect of r.W cells.
//
// It is the only allocating operation in TextInput. It walks the visible runes
// once, grouping them by style, and cuts at the right edge — which is why the
// result is guaranteed to fit r.W and why Draw can hand it straight to
// SetSpans.
func (t *TextInput) rebuild(r buffer.Rect, styles fieldStyles) {
	t.spans = nil
	width := r.W

	if len(t.ed.text) == 0 {
		t.rebuildEmpty(r, styles, width)
		return
	}

	b := newRunBuilder(t.ed.text)
	lo, hi, sel := t.ed.selection()
	w := 0
	drawn := t.ed.scroll
	showCaret := t.focused
	drewCursor := false

	// How many runes fit at all decides whether the last cell is content or
	// buffer's truncation marker. Reserving the marker unconditionally would make
	// a field showing exactly its own width of text report overflow it does not
	// have, so the budget is chosen before the single build pass.
	overflow := runesFitting(t.ed.text, t.ed.scroll, width) < len(t.ed.text)
	budget := width
	if overflow && width > 0 {
		budget = width - 1
	}
	for i := t.ed.scroll; i < len(t.ed.text); i++ {
		rw := buffer.RuneWidth(t.ed.text[i])
		if rw == 0 {
			continue
		}
		if w+rw > budget {
			break
		}
		w += rw
		drawn = i + 1
		st := styles.text
		if sel && i >= lo && i < hi {
			st = styles.selection
		}
		if showCaret && i == t.ed.cursor {
			st = styles.cursor
			drewCursor = true
		}
		b.at(i, st)
	}
	// The run closes at the last rune actually DRAWN, not at the end of the
	// text: closing at the text's end would splice the scrolled-out remainder
	// into the visible run and write it straight past the field's right edge.
	b.close(drawn)
	t.spans = b.done()

	if overflow {
		// The marker is composed here rather than delegated to buffer.Truncate,
		// because the cell for it has ALREADY been reserved above: Truncate adds a
		// marker only when its input is wider than the width it is given, and what
		// is left here is exactly one cell narrower than the field. The rune itself
		// still comes from buffer's shared constants, so the ASCII rung stays one
		// boolean rather than becoming a second marker spelling.
		marker := buffer.TruncSuffix
		if t.Ascii {
			marker = buffer.AscTruncSuffix
		}
		t.spans = append(t.spans, buffer.NewSpan(marker, styles.text))
		w++
	}

	// The caret at the end of the text has no rune to wear the cursor style, so
	// it is drawn as a block. It is only drawn when there is a spare cell: when
	// the text fills the field, scrollIntoView has already moved the caret
	// inside, and a block past the right edge would be clipped to nothing.
	if showCaret && !drewCursor && w < width && !overflow {
		t.spans = append(t.spans, buffer.NewSpan(" ", styles.cursor))
	}
}

// rebuildEmpty draws the placeholder of an empty field, with the caret block in
// front of it when the field is focused.
//
// The caret is drawn rather than the placeholder being hidden, because a focused
// empty field that shows nothing at all gives the user no evidence that there is
// anything to type into.
func (t *TextInput) rebuildEmpty(r buffer.Rect, styles fieldStyles, width int) {
	if width <= 0 {
		return
	}
	t.spans = nil
	if t.focused {
		t.spans = append(t.spans, buffer.NewSpan(" ", styles.cursor))
		if t.Placeholder == "" {
			return
		}
	}
	if t.Placeholder == "" {
		return
	}
	capped := t.capPlaceholder(width - len(t.spans))
	if len(capped) == 0 {
		return
	}
	t.spans = append(t.spans, capped...)
}

// capPlaceholder truncates the placeholder to width cells, choosing the marker
// from the ASCII rung. It allocates, and is called only from rebuildEmpty.
func (t *TextInput) capPlaceholder(width int) []buffer.Span {
	spans := []buffer.Span{buffer.NewSpan(t.Placeholder, t.PlaceholderStyle)}
	if buffer.StringWidth(t.Placeholder) <= width {
		return spans
	}
	if t.Ascii {
		return buffer.TruncateASCII(spans, width)
	}
	return buffer.Truncate(spans, width)
}

// Invalidate satisfies termmosaic.Widget. The field repaints in full every
// frame, so there is nothing finer-grained to mark.
func (t *TextInput) Invalidate() {}

// Handle offers ev to the field and reports whether it consumed it.
//
// Every path is total: the caret, the anchor and the scroll offset are clamped
// into the content before anything is done with them, so a field left in an
// inconsistent state by its caller degrades to a sane caret rather than
// panicking.
func (t *TextInput) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventPaste:
		if !t.focused {
			return false
		}
		// One operation, one undo step, one OnChange call. See ADR 0005 §4.
		t.ed.insertText(ev.Text)
		t.afterEdit()
		return true

	case termmosaic.EventMouse:
		return t.handleMouse(ev)

	case termmosaic.EventKey:
		return t.handleKey(ev)

	default:
		return false
	}
}

// afterEdit scrolls the caret into view and reports the change.
func (t *TextInput) afterEdit() {
	t.ed.scrollIntoView(t.contentWidth())
	t.changed()
}

// handleMouse places the caret on a click, extends the selection on a drag, and
// takes focus. A click outside Bounds is not consumed and changes nothing.
func (t *TextInput) handleMouse(ev termmosaic.Event) bool {
	r := t.bounds
	m := ev.Mouse
	if m.Action != termmosaic.MousePress && m.Action != termmosaic.MouseDrag {
		return false
	}
	if !r.Contains(m.X, m.Y) {
		return false
	}
	switch m.Button {
	case termmosaic.MouseLeft:
	default:
		return false
	}
	col := m.X - r.X
	if col > r.W {
		col = r.W
	}
	at := t.ed.runeAtColumn(col, r.W)
	extend := m.Action == termmosaic.MouseDrag
	// A drag extends from the anchor a previous press established; a plain
	// click starts a new selection.
	t.ed.setCursor(at, extend)
	if m.Action == termmosaic.MousePress {
		t.focused = true
		t.ed.anchor = t.ed.cursor
	}
	t.ed.scrollIntoView(t.contentWidth())
	return true
}

// handleKey applies one key, returning false for anything the field does not
// own so the key can reach the rest of the form.
func (t *TextInput) handleKey(ev termmosaic.Event) bool {
	if !t.focused {
		return false
	}
	// A key release changes nothing, and consuming it would let a release
	// satisfy a parent looking for "the user pressed something".
	if ev.Type == termmosaic.KeyRelease {
		return false
	}
	ctrl := ev.Mod.Has(termmosaic.ModCtrl)
	alt := ev.Mod.Has(termmosaic.ModAlt)
	word := ctrl || alt
	shift := ev.Mod.Has(termmosaic.ModShift)

	switch ev.Key {
	case termmosaic.KeyLeft:
		to := t.ed.cursor - 1
		if word {
			to = wordLeft(t.ed.text, t.ed.cursor)
		}
		t.move(to, shift)
		return true
	case termmosaic.KeyRight:
		to := t.ed.cursor + 1
		if word {
			to = wordRight(t.ed.text, t.ed.cursor)
		}
		t.move(to, shift)
		return true
	case termmosaic.KeyHome:
		t.move(0, shift)
		return true
	case termmosaic.KeyEnd:
		t.move(len(t.ed.text), shift)
		return true
	case termmosaic.KeyBackspace:
		// A backspace that deleted nothing reports false, so a form can pass the
		// key on rather than swallowing a keystroke the user expected to move
		// between fields.
		if !t.ed.backspace(word) {
			return false
		}
		t.afterEdit()
		return true
	case termmosaic.KeyDelete:
		if !t.ed.forwardDelete(word) {
			return false
		}
		t.afterEdit()
		return true
	case termmosaic.KeyUp, termmosaic.KeyDown:
		// A single-line field has no vertical motion; the key belongs to the
		// form, which moves between fields with it.
		return false
	}

	// Printable runes arrive as KeyNone with Rune set. The space bar is one of
	// them: input.Decode reports it as the rune ' ', not as KeySpace, unless the
	// kitty keyboard protocol reports the key code.
	if ev.Key != termmosaic.KeyNone || ev.Rune == 0 {
		return false
	}
	if ctrl || alt {
		switch controlAction(ev.Rune) {
		case ctrlSelectAll:
			t.SelectAll()
			return true
		case ctrlUndo:
			t.Undo()
			return true
		case ctrlWordBackspace:
			if !t.ed.backspace(true) {
				return false
			}
			t.afterEdit()
			return true
		case ctrlWordDelete:
			if !t.ed.forwardDelete(true) {
				return false
			}
			t.afterEdit()
			return true
		case ctrlEnd:
			t.move(len(t.ed.text), shift)
			return true
		}
		return false
	}
	if ev.Rune < 0x20 {
		// A control character delivered as a rune is not text.
		return false
	}
	t.ed.insertText(string(ev.Rune))
	t.afterEdit()
	return true
}

// move moves the caret to rune index i, extending the selection when asked.
func (t *TextInput) move(i int, extend bool) {
	t.ed.setCursor(i, extend)
	t.ed.scrollIntoView(t.contentWidth())
}

// TextInput is a Widget, a Focusable and a Minimizable: a field is the one
// widget whose whole job is to be typed into, so both optional interfaces apply.
var (
	_ termmosaic.Widget      = (*TextInput)(nil)
	_ termmosaic.Focusable   = (*TextInput)(nil)
	_ termmosaic.Minimizable = (*TextInput)(nil)
)
