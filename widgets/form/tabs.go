package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// Tabs markers. They are exported for the same reason the other widgets'
// markers are: the non-colour state indicator is part of the widget's contract.
const (
	// TabMarkSelected is the marker drawn before the selected tab.
	TabMarkSelected = "["
	// TabMarkSelectedEnd is the marker drawn after the selected tab, so a selected
	// tab reads as bracketed rather than as merely opened.
	TabMarkSelectedEnd = "]"
	// TabMarkUnselected is the marker drawn before every other tab. It is a
	// space, so every tab is the same width as every other tab and the row does
	// not reflow when the selection moves.
	TabMarkUnselected = " "
	// TabMarkUnselectedEnd is the marker drawn after every other tab.
	TabMarkUnselectedEnd = " "
)

// Tabs is a single row of tab labels with one selected, scrollable when the tabs
// do not all fit.
//
// # Accessibility
//
// The selected tab is BRACKETED — "[Name]" — and every other tab is space-padded
// — " Name ". The bracket is a shape, so the selection is readable in a
// monochrome terminal, under NO_COLOR, and by a reader who sees no difference
// between two colours an application chose. This is the requirement from ADR
// 0008's accessibility rule: a selected tab must be distinguishable without
// colour.
//
// Keeping the unselected mark a space rather than nothing is deliberate. It means
// every tab is exactly its label plus two cells, so selecting a different tab
// cannot reflow the row, and the brackets are unambiguously the selection
// indicator rather than decoration.
//
// # Scrolling
//
// A tab bar is a HORIZONTAL strip, so it does not use virtual.Model: that engine
// counts rows in a viewport of a given HEIGHT, and one row of tabs would mean a
// capacity of one tab no matter how wide the pane is. The offset here is clamped
// — never re-derived — in the same spirit as ADR 0007 §6: moving the selection
// past the right edge scrolls the minimum amount that shows it.
//
// # Key contract
//
// Keys are consumed only while focused. Moving with an arrow SELECTS, as in every
// tab bar: there is no separate commit step, because a tab bar has no unselected
// state to commit from.
//
//	left / right   KeyLeft / KeyRight select the previous / next tab
//	up / down      KeyUp / KeyDown do the same, so a tab bar works in a vertical
//	               form layout too
//	home / end     KeyHome / KeyEnd select the first / last tab
//	page up/down   KeyPageUp / KeyPageDown select as many tabs as fit
//	activate       KeyEnter or KeySpace fires OnSelect for the selected tab
//	wheel          MouseWheelUp / MouseWheelDown scroll without selecting, and
//	               only over the pointer being inside Bounds (ADR 0010)
type Tabs struct {
	bounds buffer.Rect

	// labels are the tab texts, copied by SetTabs so a later mutation of the
	// caller's slice cannot change what a drawn frame means.
	labels []string
	// selected is the selected index, or -1 when there are no tabs.
	selected int
	// offset is the index of the first visible tab.
	offset int
	// visible is how many tabs fit in the current rect, from the last rebuild.
	visible int
	// focused reports whether the tab row has focus.
	focused bool

	// TabStyle is the style of an unselected tab.
	TabStyle buffer.Style

	// SelectedStyle is the style of the selected tab and its brackets. An unset
	// value means "the tab style with AttrReverse".
	SelectedStyle buffer.Style

	// Background is painted across Bounds before anything else.
	Background buffer.Style

	// Ascii selects buffer's ASCII truncation rung: pass !caps.Unicode.
	Ascii bool

	// OnSelect is called with the selected index whenever the user changes or
	// activates the selection. It is not called by SetTabs or SetSelected.
	OnSelect func(int)

	// rows is the cached per-tab display. It is keyed on the rect and on the
	// resolved styles; the labels and the selection invalidate it through SetTabs
	// and SetSelected, so they are flags rather than keys.
	rows        rowCache
	cacheStyles tabStyles
}

// tabStyles is the resolved style set a Tabs' cached rows depend on.
type tabStyles struct {
	tab, selected, background buffer.Style
}

// NewTabs returns a Tabs over labels, sized r, with the first label selected if
// there is one.
func NewTabs(r buffer.Rect, labels []string) *Tabs {
	t := &Tabs{bounds: r}
	t.SetTabs(labels)
	return t
}

// Bounds returns the tab row's rectangle, safe to call before the first Draw.
func (t *Tabs) Bounds() buffer.Rect { return t.bounds }

// SetBounds sets the tab row's rectangle. The cached rows are keyed on it.
func (t *Tabs) SetBounds(r buffer.Rect) { t.bounds = r }

// Focused reports whether the tab row has focus.
func (t *Tabs) Focused() bool { return t.focused }

// SetFocused gives or removes focus.
func (t *Tabs) SetFocused(v bool) { t.focused = v }

// MinSize returns the smallest meaningful tab row: one cell, showing one cell of
// one tab.
func (t *Tabs) MinSize() buffer.Size { return buffer.Size{W: 1, H: 1} }

// Labels returns the tab labels.
func (t *Tabs) Labels() []string { return t.labels }

// SetTabs replaces the tab labels.
func (t *Tabs) SetTabs(labels []string) {
	if len(labels) == 0 {
		t.labels = nil
		t.selected = -1
	} else {
		t.labels = append([]string(nil), labels...)
		if t.selected < 0 || t.selected >= len(t.labels) {
			t.selected = 0
		}
	}
	t.rows.ok = false
}

// Count returns the number of tabs.
func (t *Tabs) Count() int { return len(t.labels) }

// Selected returns the selected index, or -1 when there are no tabs.
//
// -1 rather than 0 for an empty row, so a caller storing the index can tell "the
// first tab" from "there are none".
func (t *Tabs) Selected() int { return t.selected }

// SetSelected selects index i, clamped into range, and scrolls it into view.
func (t *Tabs) SetSelected(i int) {
	if len(t.labels) == 0 {
		t.selected = -1
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(t.labels) {
		i = len(t.labels) - 1
	}
	t.selected = i
	t.scrollIntoView()
	t.rows.ok = false
}

// SelectedLabel returns the selected tab's label, or "" when there is none.
func (t *Tabs) SelectedLabel() string {
	if t.selected < 0 || t.selected >= len(t.labels) {
		return ""
	}
	return t.labels[t.selected]
}

// Offset returns the index of the first visible tab.
func (t *Tabs) Offset() int { return t.offset }

// styles returns the resolved style set.
func (t *Tabs) styles() tabStyles {
	tab := t.TabStyle.Resolved()
	sel := t.SelectedStyle.Resolved()
	if t.SelectedStyle.IsUnset() {
		sel = tab.WithAttr(buffer.AttrReverse)
	}
	return tabStyles{tab: tab, selected: sel, background: t.Background.Resolved()}
}

// maxOffset returns the largest offset that still shows a tab, given how many
// fit. It is the "clamp, do not re-derive" rule of ADR 0007 §6 in one comparison.
func (t *Tabs) maxOffset() int {
	n := len(t.labels) - t.visible
	if n < 0 {
		return 0
	}
	return n
}

// scrollIntoView moves the offset the minimum amount that shows the selected tab.
func (t *Tabs) scrollIntoView() {
	if t.selected < 0 {
		t.offset = 0
		return
	}
	if t.selected < t.offset {
		t.offset = t.selected
	}
	if t.visible > 0 && t.selected >= t.offset+t.visible {
		t.offset = t.selected - t.visible + 1
	}
	t.clampOffset()
}

// clampOffset pins the offset into [0, maxOffset].
func (t *Tabs) clampOffset() {
	if t.offset > t.maxOffset() {
		t.offset = t.maxOffset()
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

// rebuild recomputes the cached rows for a rect r.W cells wide, and how many of
// them fit.
//
// Each tab's row includes both of its own marker cells, so every tab is its
// label's width plus two and the row does not reflow as the selection moves. The
// markers are never truncated away: losing the bracket would leave colour as the
// only signal, which is precisely what this widget must not do.
func (t *Tabs) rebuild(r buffer.Rect) {
	styles := t.styles()
	t.rows.rows = make([][]buffer.Span, len(t.labels))
	if r.W < 2 {
		// A one-cell tab row cannot hold both markers, and a tab whose selection
		// bracket is missing would be signalled by colour alone — the one thing
		// this widget must not do. So it shows nothing rather than half a tab.
		t.rows.store(r)
		t.cacheStyles = styles
		t.recomputeVisible(r)
		return
	}
	for i, label := range t.labels {
		st, mark, end := styles.tab, TabMarkUnselected, TabMarkUnselectedEnd
		if i == t.selected {
			st, mark, end = styles.selected, TabMarkSelected, TabMarkSelectedEnd
		}
		lead := []buffer.Span{buffer.NewSpan(mark, st)}
		body := capRow(t.Ascii, label, st, r.W-2)
		row := joinRow(lead, body)
		row = append(row, buffer.NewSpan(end, st))
		t.rows.rows[i] = row
	}
	t.rows.store(r)
	t.cacheStyles = styles
	t.recomputeVisible(r)
}

// recomputeVisible counts how many tabs fit in r.W cells, at least one.
func (t *Tabs) recomputeVisible(r buffer.Rect) {
	used, n := 0, 0
	for _, row := range t.rows.rows {
		w := buffer.SpansWidth(row)
		if n > 0 && used+w > r.W {
			break
		}
		used += w
		n++
	}
	if n == 0 && len(t.rows.rows) > 0 {
		n = 1
	}
	t.visible = n
	t.clampOffset()
	t.scrollIntoView()
}

// cacheCurrent reports whether the cached rows describe what a rebuild would
// produce.
func (t *Tabs) cacheCurrent(r buffer.Rect, styles tabStyles) bool {
	return t.rows.matches(r) && styles == t.cacheStyles
}

// Draw paints the background and then the visible tabs, on the first row.
//
// The row is one line tall by contract — a tab bar that wrapped onto a second row
// would stop being a tab bar — so the extra rows of a taller Bounds get the
// background and nothing else. That is "clip, never blank".
func (t *Tabs) Draw(buf *buffer.Buffer) {
	r := t.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, t.Background.Resolved().Blank())
	if r.W <= 0 || r.H <= 0 {
		return
	}

	styles := t.styles()
	if !t.cacheCurrent(r, styles) {
		t.rebuild(r)
	}

	x := r.X
	last := t.offset + t.visible
	if last > len(t.rows.rows) {
		last = len(t.rows.rows)
	}
	for i := t.offset; i < last; i++ {
		if x >= r.Right() {
			break
		}
		if len(t.rows.rows[i]) == 0 {
			continue
		}
		x = buf.SetSpans(x, r.Y, t.rows.rows[i])
	}
}

// Invalidate satisfies termmosaic.Widget. The tab row repaints in full every
// frame.
func (t *Tabs) Invalidate() {}

// Handle offers ev to the tab row and reports whether it consumed it.
func (t *Tabs) Handle(ev termmosaic.Event) bool {
	if d, ok := wheelDelta(ev, t.bounds); ok {
		t.offset += d
		t.clampOffset()
		return true
	}
	switch ev.Kind {
	case termmosaic.EventMouse:
		return t.handleMouse(ev)
	case termmosaic.EventKey:
		return t.handleKey(ev)
	default:
		return false
	}
}

// ensureRows rebuilds the cached rows when the rect, the labels, the selection or
// the styles have moved on since the last rebuild.
//
// It exists because a click can arrive BEFORE the first Draw — a layout hands out
// rectangles and then input arrives — and hit-testing needs the widths of the rows
// it is testing against. Handle is not the frame path, so an allocation here costs
// nothing; the same rebuild on every frame would cost a frame.
func (t *Tabs) ensureRows() {
	if t.bounds.Empty() {
		return
	}
	if !t.cacheCurrent(t.bounds, t.styles()) {
		t.rebuild(t.bounds)
	}
}

// tabAt returns the index of the tab occupying cell column col of the row, and
// whether any tab is there. It walks the visible tabs, which is O(visible).
func (t *Tabs) tabAt(col int) (int, bool) {
	x := 0
	last := t.offset + t.visible
	if last > len(t.rows.rows) {
		last = len(t.rows.rows)
	}
	for i := t.offset; i < last; i++ {
		w := buffer.SpansWidth(t.rows.rows[i])
		if col < x+w {
			return i, true
		}
		x += w
	}
	return 0, false
}

// handleMouse selects the clicked tab and takes focus. A click outside Bounds, or
// in the space past the last tab, is not consumed and changes nothing.
func (t *Tabs) handleMouse(ev termmosaic.Event) bool {
	if ev.Mouse.Button != termmosaic.MouseLeft || ev.Mouse.Action != termmosaic.MousePress {
		return false
	}
	if !t.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
		return false
	}
	t.ensureRows()
	i, ok := t.tabAt(ev.Mouse.X - t.bounds.X)
	if !ok {
		return false
	}
	t.focused = true
	if i != t.selected {
		t.SetSelected(i)
		if t.OnSelect != nil {
			t.OnSelect(i)
		}
	}
	return true
}

// handleKey applies one key, selecting as it goes.
func (t *Tabs) handleKey(ev termmosaic.Event) bool {
	if !t.focused || ev.Type == termmosaic.KeyRelease {
		return false
	}
	moved := false
	switch ev.Key {
	case termmosaic.KeyLeft, termmosaic.KeyUp:
		moved = t.move(-1)
	case termmosaic.KeyRight, termmosaic.KeyDown:
		moved = t.move(1)
	case termmosaic.KeyHome:
		moved = t.moveTo(0)
	case termmosaic.KeyEnd:
		moved = t.moveTo(len(t.labels) - 1)
	case termmosaic.KeyPageUp:
		moved = t.moveTo(t.selected - t.pageStep())
	case termmosaic.KeyPageDown:
		moved = t.moveTo(t.selected + t.pageStep())
	default:
		if !activateKey(ev) {
			return false
		}
		if t.OnSelect != nil && t.selected >= 0 {
			t.OnSelect(t.selected)
		}
		return true
	}
	if moved && t.OnSelect != nil && t.selected >= 0 {
		t.OnSelect(t.selected)
	}
	return true
}

// pageStep returns how many tabs a page key moves: as many as fit, or one when
// none have been measured yet. A step of zero would make the key a no-op, which
// is never what a page key means.
func (t *Tabs) pageStep() int {
	if t.visible < 1 {
		return 1
	}
	return t.visible
}

// move selects the tab delta positions away, clamped, and reports whether the
// selection changed.
func (t *Tabs) move(delta int) bool {
	if len(t.labels) == 0 {
		return false
	}
	return t.moveTo(t.selected + delta)
}

// moveTo selects index i, clamped, and reports whether the selection changed.
func (t *Tabs) moveTo(i int) bool {
	if len(t.labels) == 0 {
		return false
	}
	if i < 0 {
		i = 0
	}
	if i >= len(t.labels) {
		i = len(t.labels) - 1
	}
	if i == t.selected {
		return false
	}
	t.SetSelected(i)
	return true
}

// Tabs is a Widget, a Focusable and a Minimizable.
var (
	_ termmosaic.Widget      = (*Tabs)(nil)
	_ termmosaic.Focusable   = (*Tabs)(nil)
	_ termmosaic.Minimizable = (*Tabs)(nil)
)
