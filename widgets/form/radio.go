package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// Radio markers. They are exported for the same reason SelectDefaultMarker is:
// the widget's non-colour state indicator is part of its contract, and a test or
// a golden file needs to name the same string the widget draws.
const (
	// RadioMarkSelected is the marker on the chosen option.
	RadioMarkSelected = "(o)"
	// RadioMarkUnselected is the marker on every other option.
	RadioMarkUnselected = "( )"
	// RadioFocusMark is drawn in the focus column of the focused option. A space
	// is drawn there for every other option.
	RadioFocusMark = ">"
)

// Radio is a one-of-N group of options: exactly one is chosen, and the choice is
// visible without colour.
//
// # Accessibility
//
// Two independent non-colour signals, which is what a radio group needs and one
// checkbox is not enough of:
//   - the chosen option's marker is "(o)" and every other option's is "( )", so
//     the choice is a SHAPE difference;
//   - the focused option carries a ">" in its own focus column, so focus is not
//     signalled by the same thing that signals selection.
//
// # Key contract
//
// Keys are consumed only while focused. Moving with an arrow CHOOSES, which is
// how every radio group behaves: there is no separate "commit" step, because a
// radio group has no unselected state to commit from.
//
//	up / down      KeyUp / KeyDown choose the previous / next option and fire
//	left / right   KeyLeft / KeyRight, so the group works in a horizontal form
//	home / end     KeyHome / KeyEnd choose the first / last option
//	page up/down   KeyPageUp / KeyPageDown choose one screenful away
//	activate       KeyEnter or KeySpace re-fires OnSelect for the chosen option,
//	               which is what a user pressing it to confirm expects
//	wheel          MouseWheelUp / MouseWheelDown scroll without choosing
type Radio struct {
	list *optionList

	bounds buffer.Rect

	// OptionStyle is the style of an unchosen option.
	OptionStyle buffer.Style

	// SelectedStyle is the style of the chosen option, its marker and its focus
	// mark. An unset value means "the option style with AttrReverse".
	SelectedStyle buffer.Style

	// FocusStyle is the style of the ">" focus mark. An unset value means "the
	// selected style with AttrUnderline", so the focus mark is underlined whether
	// or not the option happens to be the chosen one.
	FocusStyle buffer.Style

	// Background is painted across Bounds before anything else.
	Background buffer.Style

	// Ascii selects buffer's ASCII truncation rung: pass !caps.Unicode.
	Ascii bool

	// OnSelect is called with the newly chosen index whenever the user changes
	// the choice with a key or a click, and with the chosen index when the user
	// activates the group.
	OnSelect func(int)

	// rows is the cached per-option display, keyed on the rect and the resolved
	// styles. SetFocused deliberately does NOT invalidate it: focus is part of
	// the key instead, because the focus column is drawn in every row and
	// rebuilding every row on a focus change would be work the cache exists to
	// avoid.
	rows         rowCache
	cacheStyles  radioStyles
	cacheFocused bool
}

// radioStyles is the resolved style set a Radio's cached rows depend on.
type radioStyles struct {
	option, selected, focus, background buffer.Style
}

// NewRadio returns a Radio over labels, sized r, with the first label chosen if
// there is one.
func NewRadio(r buffer.Rect, labels []string) *Radio {
	g := &Radio{bounds: r}
	g.list = newOptionList(labels)
	g.list.resize(r)
	return g
}

// Bounds returns the group's rectangle, safe to call before the first Draw.
func (g *Radio) Bounds() buffer.Rect { return g.bounds }

// SetBounds sets the group's rectangle. The cached rows are keyed on it, and the
// scroll engine is told the new size here as well as in Draw so that a key or a
// click arriving before the first frame still sees the right viewport.
func (g *Radio) SetBounds(r buffer.Rect) {
	g.bounds = r
	g.list.resize(r)
}

// Focused reports whether the group has focus.
func (g *Radio) Focused() bool { return g.list.focused }

// SetFocused gives or removes focus.
func (g *Radio) SetFocused(v bool) { g.list.focused = v }

// MinSize returns the smallest meaningful group: the focus column, the marker,
// the separating space and one cell of label, on one row.
func (g *Radio) MinSize() buffer.Size {
	return buffer.Size{W: g.rowHead() + 1, H: 1}
}

// rowHead returns the fixed cells every row spends before its label: the focus
// column, the marker and the separating space.
func (g *Radio) rowHead() int {
	return 1 + buffer.StringWidth(RadioMarkUnselected) + 1
}

// Labels returns the option labels.
func (g *Radio) Labels() []string { return g.list.Labels() }

// SetLabels replaces the options.
func (g *Radio) SetLabels(labels []string) {
	g.list.SetLabels(labels)
	g.rows.ok = false
}

// Count returns the number of options.
func (g *Radio) Count() int { return g.list.Count() }

// Selected returns the chosen index, or -1 when there are no options.
func (g *Radio) Selected() int { return g.list.Selected() }

// SetSelected chooses index i, clamped into range.
func (g *Radio) SetSelected(i int) {
	g.list.SetSelected(i)
	g.rows.ok = false
}

// SelectedLabel returns the chosen option's label, or "" when there is none.
func (g *Radio) SelectedLabel() string {
	i := g.list.Selected()
	if i < 0 || i >= len(g.list.Labels()) {
		return ""
	}
	return g.list.Labels()[i]
}

// Offset returns the index of the first visible option.
func (g *Radio) Offset() int { return g.list.offset() }

// styles returns the resolved style set.
func (g *Radio) styles() radioStyles {
	opt := g.OptionStyle.Resolved()
	sel := g.SelectedStyle.Resolved()
	if g.SelectedStyle.IsUnset() {
		sel = opt.WithAttr(buffer.AttrReverse)
	}
	focus := g.FocusStyle.Resolved()
	if g.FocusStyle.IsUnset() {
		focus = sel.WithAttr(buffer.AttrUnderline)
	}
	return radioStyles{option: opt, selected: sel, focus: focus, background: g.Background.Resolved()}
}

// rebuild recomputes the cached rows for a rect r.W cells wide.
func (g *Radio) rebuild(r buffer.Rect) {
	styles := g.styles()
	head := g.rowHead()
	labels := g.list.Labels()
	g.rows.rows = make([][]buffer.Span, len(labels))
	if head >= r.W {
		// The focus column and the marker are as wide as the whole widget: there is
		// no room for a label, and drawing the marker would spill over whatever is
		// composed beside it.
		g.rows.store(r)
		g.cacheStyles = styles
		g.cacheFocused = g.list.focused
		return
	}
	for i, label := range labels {
		st, mark := styles.option, RadioMarkUnselected
		if i == g.list.Selected() {
			st, mark = styles.selected, RadioMarkSelected
		}
		focus := " "
		if g.list.focused && i == g.list.Selected() {
			focus = RadioFocusMark
		}
		lead := []buffer.Span{
			buffer.NewSpan(focus+" ", styles.focus),
			buffer.NewSpan(mark+" ", st),
		}
		g.rows.rows[i] = joinRow(lead, capRow(g.Ascii, label, st, r.W-head))
	}
	g.rows.store(r)
	g.cacheStyles = styles
	g.cacheFocused = g.list.focused
}

// cacheCurrent reports whether the cached rows describe what a rebuild would
// produce.
func (g *Radio) cacheCurrent(r buffer.Rect, styles radioStyles) bool {
	return g.rows.matches(r) && styles == g.cacheStyles && g.cacheFocused == g.list.focused
}

// Draw paints the background and then the visible options.
func (g *Radio) Draw(buf *buffer.Buffer) {
	r := g.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, g.Background.Resolved().Blank())
	if r.W <= 0 || r.H <= 0 {
		return
	}

	g.list.resize(r)
	styles := g.styles()
	if !g.cacheCurrent(r, styles) {
		g.rebuild(r)
	}

	first, last := g.list.visibleRange()
	off := g.list.offset()
	for i := first; i < last; i++ {
		if i >= len(g.rows.rows) {
			break
		}
		y := r.Y + (i - off)
		if y < r.Y || y >= r.Bottom() {
			continue
		}
		if len(g.rows.rows[i]) == 0 {
			continue
		}
		buf.SetSpans(r.X, y, g.rows.rows[i])
	}
}

// Invalidate satisfies termmosaic.Widget. The group repaints in full every frame.
func (g *Radio) Invalidate() {}

// Handle offers ev to the group and reports whether it consumed it.
func (g *Radio) Handle(ev termmosaic.Event) bool {
	if d, ok := wheelDelta(ev); ok {
		g.list.scrollBy(d)
		return true
	}
	switch ev.Kind {
	case termmosaic.EventMouse:
		return g.handleMouse(ev)
	case termmosaic.EventKey:
		return g.handleKey(ev)
	default:
		return false
	}
}

// handleMouse chooses the clicked option and takes focus. A click outside Bounds,
// or in the empty space under a short group, changes nothing.
func (g *Radio) handleMouse(ev termmosaic.Event) bool {
	if ev.Mouse.Button != termmosaic.MouseLeft || ev.Mouse.Action != termmosaic.MousePress {
		return false
	}
	i, ok := g.list.optionAt(g.bounds, ev.Mouse.X, ev.Mouse.Y)
	if !ok {
		return false
	}
	g.list.focused = true
	g.list.SetSelected(i)
	g.rows.ok = false
	if g.OnSelect != nil {
		g.OnSelect(i)
	}
	return true
}

// handleKey applies one key, choosing as it goes.
func (g *Radio) handleKey(ev termmosaic.Event) bool {
	if !g.list.focused || ev.Type == termmosaic.KeyRelease {
		return false
	}
	moved := false
	switch ev.Key {
	case termmosaic.KeyUp:
		moved = g.list.move(-1)
	case termmosaic.KeyDown:
		moved = g.list.move(1)
	case termmosaic.KeyLeft:
		moved = g.list.move(-1)
	case termmosaic.KeyRight:
		moved = g.list.move(1)
	case termmosaic.KeyHome:
		moved = g.list.toFirst()
	case termmosaic.KeyEnd:
		moved = g.list.toLast()
	case termmosaic.KeyPageUp:
		moved = g.list.page(-1)
	case termmosaic.KeyPageDown:
		moved = g.list.page(1)
	default:
		if !activateKey(ev) {
			return false
		}
		if g.OnSelect != nil && g.list.Selected() >= 0 {
			g.OnSelect(g.list.Selected())
		}
		return true
	}
	if moved {
		g.rows.ok = false
		if g.OnSelect != nil {
			g.OnSelect(g.list.Selected())
		}
	}
	return true
}

// Radio is a Widget, a Focusable and a Minimizable.
var (
	_ termmosaic.Widget      = (*Radio)(nil)
	_ termmosaic.Focusable   = (*Radio)(nil)
	_ termmosaic.Minimizable = (*Radio)(nil)
)
