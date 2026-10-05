package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// SelectDefaultMarker is the marker a Select puts in the marker column of the
// highlighted option.
//
// It is exported because it is part of the widget's contract rather than an
// implementation detail: a test, a golden file and an application that lays out
// beside the widget all need to name the same string.
const SelectDefaultMarker = ">"

// Select is a closed list of options with one highlighted, scrollable when the
// options do not all fit.
//
// # Accessibility
//
// The highlighted option is marked by SelectDefaultMarker in its own marker
// column; every other option carries a space in that column. That is the
// non-colour signal, and it is deliberately a separate column rather than part
// of the label: a marker that moved with the text would shift every row's label
// one cell, which is exactly the misalignment a column exists to prevent.
//
// # Key contract
//
// Keys are consumed only while focused. The wheel is consumed whether or not the
// widget is focused, because pointing at a list and scrolling it does not require
// taking focus away from whatever is being typed in.
//
//	up / down      KeyUp / KeyDown highlight one option
//	left / right   KeyLeft / KeyRight highlight one option, so the widget works
//	               in a horizontal form layout too
//	home / end     KeyHome / KeyEnd highlight the first / last option
//	page up/down   KeyPageUp / KeyPageDown highlight one screenful
//	activate       KeyEnter or KeySpace accepts the highlighted option, which
//	               fires OnSelect without changing the highlight
//	wheel          MouseWheelUp / MouseWheelDown scroll without moving the
//	               highlight
//
// A closed list never changes what it contains, so there is no type-ahead and no
// Remove: a Select whose options change is a different widget.
type Select struct {
	list *optionList

	bounds buffer.Rect

	// Marker is the non-colour indicator drawn in the marker column of the
	// highlighted option. An empty Marker leaves the column as a single space,
	// which is legal but then relies on colour alone, so it is not the default.
	Marker string

	// OptionStyle is the style of an unhighlighted option.
	OptionStyle buffer.Style

	// SelectedStyle is the style of the highlighted option and of its marker.
	// An unset value means "the option style with AttrReverse", so the highlight
	// is visible with no configuration and under NO_COLOR.
	SelectedStyle buffer.Style

	// Background is painted across Bounds before anything else.
	Background buffer.Style

	// Ascii selects buffer's ASCII truncation rung: pass !caps.Unicode.
	Ascii bool

	// OnSelect is called with the highlighted index when the user activates the
	// widget. It is not called by SetLabels or SetSelected, so an application
	// that changes the selection programmatically does not also receive a user
	// activation it did not ask for.
	OnSelect func(int)

	// rows is the cached per-option display. It is keyed on the rect and on the
	// resolved styles, and every mutator invalidates it through rebuildCache, so
	// the key holds only what Draw cannot learn any other way.
	rows        rowCache
	cacheStyles selectStyles
}

// selectStyles is the resolved style set a Select's cached rows depend on.
type selectStyles struct {
	option, selected, background buffer.Style
}

// NewSelect returns a Select over labels, sized r, with the first label
// highlighted if there is one.
func NewSelect(r buffer.Rect, labels []string) *Select {
	s := &Select{bounds: r, Marker: SelectDefaultMarker}
	s.list = newOptionList(labels)
	s.list.resize(r)
	return s
}

// Bounds returns the select's rectangle, safe to call before the first Draw.
func (s *Select) Bounds() buffer.Rect { return s.bounds }

// SetBounds sets the select's rectangle. The cached rows are keyed on it, so the
// next Draw rebuilds them at the new width.
//
// The scroll engine is told the new size here as well as in Draw, so that the
// viewport is correct for a key or a click that arrives before the first frame —
// which is the normal case for a layout that hands out rectangles and then
// delivers input.
func (s *Select) SetBounds(r buffer.Rect) {
	s.bounds = r
	s.list.resize(r)
}

// Focused reports whether the select has focus.
func (s *Select) Focused() bool { return s.list.focused }

// SetFocused gives or removes focus.
func (s *Select) SetFocused(v bool) { s.list.focused = v }

// MinSize returns the smallest meaningful select: one cell, showing one option.
func (s *Select) MinSize() buffer.Size { return buffer.Size{W: 1, H: 1} }

// Labels returns the option labels.
func (s *Select) Labels() []string { return s.list.Labels() }

// SetLabels replaces the options.
func (s *Select) SetLabels(labels []string) {
	s.list.SetLabels(labels)
	s.rebuildCache()
}

// Count returns the number of options.
func (s *Select) Count() int { return s.list.Count() }

// Selected returns the highlighted index, or -1 when there are no options.
func (s *Select) Selected() int { return s.list.Selected() }

// SetSelected highlights index i, clamped into range.
func (s *Select) SetSelected(i int) {
	s.list.SetSelected(i)
	s.rebuildCache()
}

// SelectedLabel returns the highlighted option's label, or "" when there is none.
func (s *Select) SelectedLabel() string {
	i := s.list.Selected()
	if i < 0 || i >= len(s.list.Labels()) {
		return ""
	}
	return s.list.Labels()[i]
}

// Offset returns the index of the first visible option.
func (s *Select) Offset() int { return s.list.offset() }

// SetMarker sets the non-colour marker for the highlighted option.
//
// It rebuilds the row cache like every other mutator. The marker is a column of
// its own, so a change to it moves every label in the list; without the rebuild
// the cached rows kept the old widths and the select drew its labels under a
// marker they had never been laid out against.
func (s *Select) SetMarker(m string) {
	s.Marker = m
	s.rebuildCache()
}

// styles returns the resolved style set, resolving the documented unset
// sentinel on SelectedStyle.
func (s *Select) styles() selectStyles {
	opt := s.OptionStyle.Resolved()
	sel := s.SelectedStyle.Resolved()
	if s.SelectedStyle.IsUnset() {
		sel = opt.WithAttr(buffer.AttrReverse)
	}
	return selectStyles{option: opt, selected: sel, background: s.Background.Resolved()}
}

// rebuildCache discards the cached rows, forcing the next Draw to rebuild them.
// It is called from every mutator; Draw itself keys on the rect, the styles, the
// number of labels and the highlighted index.
func (s *Select) rebuildCache() { s.rows.ok = false }

// markerWidth returns the width of the marker column, which is the marker plus
// the one space separating it from the label.
func (s *Select) markerWidth() int {
	w := buffer.StringWidth(s.Marker)
	if w < 1 {
		// A zero-width marker still needs its separating space, and the column
		// must exist so every label starts on the same cell.
		w = 1
	}
	return w + 1
}

// rebuild recomputes the cached rows for a rect r.W cells wide.
func (s *Select) rebuild(r buffer.Rect) {
	styles := s.styles()
	head := s.markerWidth()
	labels := s.list.Labels()
	s.rows.rows = make([][]buffer.Span, len(labels))
	if head >= r.W {
		// The marker column alone is as wide as the widget. There is nowhere to put
		// it and nowhere to put a label, so the row is left empty rather than
		// spilling the marker over the widget beside it.
		s.rows.store(r)
		s.cacheStyles = styles
		return
	}
	for i, label := range labels {
		st, mark := styles.option, " "
		if i == s.list.Selected() {
			st, mark = styles.selected, s.Marker
		}
		lead := []buffer.Span{buffer.NewSpan(mark+" ", st)}
		s.rows.rows[i] = joinRow(lead, capRow(s.Ascii, label, st, r.W-head))
	}
	s.rows.store(r)
	s.cacheStyles = styles
}

// cacheCurrent reports whether the cached rows still describe what would be
// rebuilt: the same rect and the same resolved styles. Everything else that can
// change the rows — the labels, the highlight, the marker — goes through
// rebuildCache, so it is a flag rather than a key.
func (s *Select) cacheCurrent(r buffer.Rect, styles selectStyles) bool {
	return s.rows.matches(r) && styles == s.cacheStyles
}

// Draw paints the background and then the visible options.
//
// It is total for every rect and allocates nothing once the rows are cached: the
// virtualized iteration is over the visible range only, so a thousand options
// cost what ten do (ADR 0007 §6).
func (s *Select) Draw(buf *buffer.Buffer) {
	r := s.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, s.Background.Resolved().Blank())
	if r.W <= 0 || r.H <= 0 {
		return
	}

	s.list.resize(r)
	styles := s.styles()
	if !s.cacheCurrent(r, styles) {
		s.rebuild(r)
	}

	first, last := s.list.visibleRange()
	off := s.list.offset()
	for i := first; i < last; i++ {
		if i >= len(s.rows.rows) {
			break
		}
		y := r.Y + (i - off)
		if y < r.Y || y >= r.Bottom() {
			continue
		}
		if len(s.rows.rows[i]) == 0 {
			continue
		}
		buf.SetSpans(r.X, y, s.rows.rows[i])
	}
}

// Invalidate satisfies termmosaic.Widget. The select repaints in full every frame.
func (s *Select) Invalidate() {}

// Handle offers ev to the select and reports whether it consumed it.
func (s *Select) Handle(ev termmosaic.Event) bool {
	if d, ok := wheelDelta(ev); ok {
		s.list.scrollBy(d)
		return true
	}
	switch ev.Kind {
	case termmosaic.EventMouse:
		return s.handleMouse(ev)
	case termmosaic.EventKey:
		return s.handleKey(ev)
	default:
		return false
	}
}

// handleMouse highlights the clicked option and takes focus. A click outside
// Bounds, or in the empty space under a short list, is not consumed and changes
// nothing.
func (s *Select) handleMouse(ev termmosaic.Event) bool {
	if ev.Mouse.Button != termmosaic.MouseLeft || ev.Mouse.Action != termmosaic.MousePress {
		return false
	}
	i, ok := s.list.optionAt(s.bounds, ev.Mouse.X, ev.Mouse.Y)
	if !ok {
		return false
	}
	s.list.focused = true
	s.list.SetSelected(i)
	s.rebuildCache()
	return true
}

// handleKey applies one key. Every branch reports true even when the highlight
// did not move, because the key was still the select's: a form must not fall
// through to the next field because the user pressed Down at the end of a list.
func (s *Select) handleKey(ev termmosaic.Event) bool {
	if !s.list.focused || ev.Type == termmosaic.KeyRelease {
		return false
	}
	moved := false
	switch ev.Key {
	case termmosaic.KeyUp:
		moved = s.list.move(-1)
	case termmosaic.KeyDown:
		moved = s.list.move(1)
	case termmosaic.KeyLeft:
		moved = s.list.move(-1)
	case termmosaic.KeyRight:
		moved = s.list.move(1)
	case termmosaic.KeyHome:
		moved = s.list.toFirst()
	case termmosaic.KeyEnd:
		moved = s.list.toLast()
	case termmosaic.KeyPageUp:
		moved = s.list.page(-1)
	case termmosaic.KeyPageDown:
		moved = s.list.page(1)
	default:
		if !activateKey(ev) {
			return false
		}
		if s.OnSelect != nil && s.list.Selected() >= 0 {
			s.OnSelect(s.list.Selected())
		}
		return true
	}
	if moved {
		s.rebuildCache()
	}
	return true
}

// Select is a Widget, a Focusable and a Minimizable.
var (
	_ termmosaic.Widget      = (*Select)(nil)
	_ termmosaic.Focusable   = (*Select)(nil)
	_ termmosaic.Minimizable = (*Select)(nil)
)
