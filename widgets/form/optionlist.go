package form

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/virtual"
)

// optionList is the shared engine behind Select, Radio and Tabs: a list of
// labels, one highlighted index, and a scroll offset maintained by virtual.Model.
//
// It exists because those three widgets differ in exactly two ways — the shape
// of one row, and what a key does to the highlighted index — and in neither of
// them should the ARITHMETIC be re-derived. Which option a click at (x, y) means,
// how far a wheel notch moves, when the selected option has scrolled out of
// view and by how much: those are ADR 0007 §6's three rules, and they are decided
// once in virtual and once here rather than three times in three widgets.
//
// It is embedded by value behind a pointer field-free struct, so a widget
// holding one allocates exactly one virtual.Model, at construction.
type optionList struct {
	// labels are the option texts. They are held by the caller and copied by
	// SetLabels, so a later mutation of the caller's slice cannot change what a
	// drawn frame means.
	labels []string
	// selected is the highlighted index, or -1 when the list is empty. It is
	// always inside [0, len(labels)) or -1; SetSelected and every move clamp it.
	selected int
	// focused reports whether the owning widget has focus.
	focused bool
	// view is the scroll engine. It is a pointer so that Model's pointer-receiver
	// methods work on an embedded value without the widget having to keep a
	// separate field; it is created once, in newOptionList.
	view *virtual.Model
}

// newOptionList returns a list over labels with nothing selected.
func newOptionList(labels []string) *optionList {
	l := &optionList{selected: -1, view: virtual.New(0)}
	l.SetLabels(labels)
	return l
}

// Labels returns the option labels. The result aliases the list's own storage
// and must not be modified.
func (l *optionList) Labels() []string { return l.labels }

// SetLabels replaces the labels, clamps the selected index and re-clamps the
// scroll offset. An empty list selects nothing, which is Selected() reporting -1.
func (l *optionList) SetLabels(labels []string) {
	l.labels = append([]string(nil), labels...)
	l.view.SetCount(len(l.labels))
	if len(l.labels) == 0 {
		l.selected = -1
		return
	}
	if l.selected < 0 || l.selected >= len(l.labels) {
		l.selected = 0
	}
	l.view.ScrollIntoView(l.selected)
}

// Count returns the number of options.
func (l *optionList) Count() int { return len(l.labels) }

// Selected returns the highlighted index, or -1 when the list is empty.
//
// -1 rather than 0 for the empty list is deliberate: a caller that stores the
// index and later reads it back must be able to tell "the first option" from
// "there are none".
func (l *optionList) Selected() int { return l.selected }

// SetSelected highlights index i, clamped into range, and scrolls it into view.
// An empty list ignores it.
func (l *optionList) SetSelected(i int) {
	if len(l.labels) == 0 {
		l.selected = -1
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(l.labels) {
		i = len(l.labels) - 1
	}
	l.selected = i
	l.view.ScrollIntoView(i)
}

// move highlights the option delta positions away, clamped. It reports whether
// the highlighted index changed, so a widget can skip a re-render and a callback
// when a key was pressed at the end of the list.
func (l *optionList) move(delta int) bool {
	if len(l.labels) == 0 {
		return false
	}
	next := l.selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(l.labels) {
		next = len(l.labels) - 1
	}
	if next == l.selected {
		return false
	}
	l.selected = next
	l.view.ScrollIntoView(next)
	return true
}

// toFirst and toLast jump to the ends of the list.
func (l *optionList) toFirst() bool {
	if l.selected == 0 {
		return false
	}
	l.SetSelected(0)
	return true
}

func (l *optionList) toLast() bool {
	if len(l.labels) == 0 || l.selected == len(l.labels)-1 {
		return false
	}
	l.SetSelected(len(l.labels) - 1)
	return true
}

// page moves the highlight by roughly one screenful and returns whether it
// moved. It is the same arithmetic as a wheel notch but changes the selection,
// which is why it lives beside move rather than in virtual.Model.
func (l *optionList) page(delta int) bool {
	if len(l.labels) == 0 {
		return false
	}
	step := l.view.Visible() - 1
	if step < 1 {
		step = 1
	}
	return l.move(delta * step)
}

// resize gives the scroll engine the viewport rect. It is O(1), allocates
// nothing, and re-clamps the offset — which is rule 2 of ADR 0007 §6, "clamp
// the scroll offset; never re-derive it", applied on every frame rather than
// only on a resize, so a wheel scroll and a shrink cannot disagree.
func (l *optionList) resize(r buffer.Rect) {
	l.view.SetCount(len(l.labels))
	l.view.Resize(r)
}

// scrollBy moves the scroll offset by delta rows without changing the
// highlight, which is what a wheel notch does in a list with no selection.
func (l *optionList) scrollBy(delta int) { l.view.ScrollBy(delta) }

// offset returns the index of the first visible option.
func (l *optionList) offset() int { return l.view.Offset() }

// visibleRange returns the indices visible in the viewport as [first, last).
func (l *optionList) visibleRange() (int, int) { return l.view.Range() }

// optionAt returns the option index under the cell (x, y) inside r, and whether
// (x, y) is a cell showing an option.
//
// A cell below the last option returns false rather than an index past the end:
// a click in the empty space under a short list must not select the option after
// the last one.
func (l *optionList) optionAt(r buffer.Rect, x, y int) (int, bool) {
	if !r.Contains(x, y) {
		return 0, false
	}
	return l.view.ItemAt(y - r.Y)
}

// rowCache holds per-option display runs for a choice widget.
//
// Every choice widget builds its rows once per (rect, labels, selection, styles)
// change rather than once per frame, because truncation allocates and because
// grouping each row into styled runs is the same arithmetic three times over.
// Draw then writes one row per visible option with a single SetSpans each.
type rowCache struct {
	// rows holds one entry per option, in option order, each already cut to the
	// widget's width.
	rows [][]buffer.Span
	// rect is the rect the rows were built for.
	rect buffer.Rect
	// ok reports whether rows has ever been built.
	ok bool
}

// matches reports whether the cached rows are current for r.
func (rc *rowCache) matches(r buffer.Rect) bool { return rc.ok && rc.rect == r }

// store records r as the rect the rows were built for.
func (rc *rowCache) store(r buffer.Rect) { rc.rect, rc.ok = r, true }

// capRow truncates a label to width cells, choosing the truncation marker from
// the ASCII rung so a non-UTF-8 terminal gets "~" and the layout is identical
// either way — both markers are one cell wide.
//
// It allocates when it truncates, and is therefore called from a widget's
// rebuild rather than from Draw. A width of zero or less yields no spans at all
// rather than a lone marker: there is no cell to put even the marker in.
func capRow(ascii bool, text string, st buffer.Style, width int) []buffer.Span {
	if width <= 0 {
		return nil
	}
	spans := []buffer.Span{buffer.NewSpan(text, st)}
	if buffer.StringWidth(text) <= width {
		return spans
	}
	if ascii {
		return buffer.TruncateASCII(spans, width)
	}
	return buffer.Truncate(spans, width)
}

// joinRow concatenates two span slices into one row. It allocates, and is
// called from a rebuild.
func joinRow(head, tail []buffer.Span) []buffer.Span {
	out := make([]buffer.Span, 0, len(head)+len(tail))
	out = append(out, head...)
	return append(out, tail...)
}

// wheelDelta maps a wheel event to a scroll distance, and reports whether the
// event was a scrollable wheel notch — that is, one inside r.
//
// Three rows per notch is the number that makes a long list feel right at 60
// frames a second: one row per notch is so slow that a user scrolls a 200-row
// list by dragging the wheel for a second, and one page per notch overshoots a
// short list entirely. It is a widget-local threshold (ADR 0007 §1 rule 5),
// chosen once here because three widgets sharing it would otherwise each pick
// their own.
//
// The bounds check is the load-bearing part, and it lives HERE rather than in
// each caller because every caller got it wrong the same way: a wheel notch was
// tested BEFORE the widget's own Contains check, so a notch anywhere on the
// screen scrolled a list the pointer was nowhere near — which is ADR 0010's
// defect. A widget handles a pointer event only if the pointer is inside its
// Bounds, and the wheel is a pointer event like any other.
func wheelDelta(ev termmosaic.Event, r buffer.Rect) (int, bool) {
	if ev.Kind != termmosaic.EventMouse {
		return 0, false
	}
	if ev.Mouse.Button != termmosaic.MouseWheelUp && ev.Mouse.Button != termmosaic.MouseWheelDown {
		return 0, false
	}
	if ev.Mouse.Action != termmosaic.MousePress {
		return 0, false
	}
	if !r.Contains(ev.Mouse.X, ev.Mouse.Y) {
		return 0, false
	}
	if ev.Mouse.Button == termmosaic.MouseWheelUp {
		return -wheelRows, true
	}
	return wheelRows, true
}

// wheelRows is how many options one wheel notch scrolls.
const wheelRows = 3
