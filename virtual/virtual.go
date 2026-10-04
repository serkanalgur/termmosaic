// Package virtual is the scroll engine for data widgets: it renders a window of
// rows out of a much larger collection, in constant time per frame regardless of
// how large the collection is.
//
// # Why it exists as its own package
//
// A List, a Table, a Tree and a Pager all need the same four things — a count, a
// scroll offset, a viewport height, and a way to draw row i into row i's rect —
// and none of them is about lists, tables, trees or pagers. ADR 0007 §6 decides
// that a virtualized widget "must be O(visible rows), not O(item count)", and
// decides three rules alongside it, all of which live here so four widgets cannot
// each invent their own:
//
//  1. A resize costs O(visible rows).
//  2. Clamp the scroll offset; never re-derive it. Shrinking the screen clamps
//     the offset so offset+visible <= count, which is O(1).
//  3. Adaptation is cached per rect, so a drag producing eighteen different
//     heights recomputes eighteen times, not once per row per frame.
//
// # The shape of it
//
// A Model owns the arithmetic and knows nothing about items. The widget owns the
// items and paints rows. That split is deliberate: the engine cannot know what a
// row is, and a widget that owns the arithmetic cannot be shared by three others.
//
//	// once, at construction or on a data change
//	v := virtual.New(len(rows))
//	// once, in the widget's rect-change check
//	if v.Resize(interior) {
//	    // anything derived from the viewport size recomputes here
//	}
//	// every frame
//	v.ForEach(interior, buf, func(dst *buffer.Buffer, row buffer.Rect, i int) {
//	    dst.SetSpans(row.X, row.Y, spansFor(rows[i]))
//	})
//
// # Cost
//
// ForEach calls paint once per visible row and does nothing else: no iteration
// over items, no slice building, no allocation. SetCount, Resize, ScrollBy and
// friends are all O(1). A 1,000,000-item collection therefore costs exactly what
// a 10-item one does, and BenchmarkRender10K/100K/1M in this package's tests is
// the proof rather than the claim.
//
// # Selection is not here
//
// The engine scrolls. Deciding what is selected, what happens on a click and how
// far a wheel notch moves are widget policy, and ADR 0007 §2 is explicit that
// policy is the widget's and only the arithmetic is shared. ScrollIntoView is
// included because it is arithmetic and is the same one in every widget.
package virtual

import (
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
)

// Paint draws one row of the collection.
//
// dst and row are the destination buffer and the row's own rectangle, already
// clipped to the viewport. i is the item index, and it is the widget's job to
// turn it into content — the engine has no idea what item i is.
//
// A Paint must write only inside row. The rect is clipped so that a partially
// visible row at the bottom of the viewport is passed with its real height, which
// means a painter that fills row with a background stays inside the viewport
// without any arithmetic of its own.
type Paint func(dst *buffer.Buffer, row buffer.Rect, i int)

// Model is the scroll state of one virtualized collection: how many items there
// are, where the viewport starts, and how tall it is.
//
// It is a plain value with no pointers, so it is safe to embed in a widget by
// value. It is NOT safe for concurrent use; the renderer owns it between frames,
// exactly as it owns a widget's Bounds.
//
// The zero Model is a valid empty collection with no viewport.
type Model struct {
	count  int
	offset int
	height int
	rowH   int
}

// New returns a Model for a collection of count items with a one-cell row height.
//
// count is clamped at zero: a count arriving from a filter or a failing query is
// untrusted input, and a Model reporting a negative count would make every
// visible-row calculation nonsense.
func New(count int) *Model {
	return &Model{count: nonNegative(count), rowH: 1}
}

// nonNegative clamps at zero.
func nonNegative(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

// Count returns the number of items in the collection. It is the only thing the
// engine knows about the data, and it is what keeps per-frame cost independent of
// it.
func (m *Model) Count() int { return m.count }

// SetCount sets the number of items and re-clamps the offset.
//
// A shrink that leaves the viewport past the end of the collection pulls the
// offset back rather than re-deriving it: rule 2 above, clamp do not recentre.
// A caller that wants a specific row visible after a data change calls
// ScrollIntoView, which says what it wants instead of the engine guessing.
func (m *Model) SetCount(n int) {
	m.count = nonNegative(n)
	m.clampOffset()
}

// RowHeight returns the height of one row in cells. It is never below 1.
func (m *Model) RowHeight() int { return m.rowH }

// SetRowHeight sets the height of one row in cells and re-clamps the offset.
//
// A height below 1 is treated as 1 rather than rejected: a row must occupy at
// least one cell to exist, and a caller computing a height from a font metric
// that rounds to zero has a degenerate row, not an invalid model.
func (m *Model) SetRowHeight(h int) {
	if h < 1 {
		h = 1
	}
	m.rowH = h
	m.clampOffset()
}

// Viewport returns the viewport height in cells, which is what SetViewport last
// received. It is 0 before the first Resize.
func (m *Model) Viewport() int { return m.height }

// Capacity returns how many whole rows fit in the viewport, ignoring how many
// items there are: it is the viewport's height in rows, so a partially visible
// row at the bottom does not count.
func (m *Model) Capacity() int {
	if m.rowH < 1 || m.height < 1 {
		return 0
	}
	return m.height / m.rowH
}

// Visible returns how many rows are painted right now: what is left of the
// collection from the offset, capped by the viewport's capacity.
//
// It is the one call that replaces "how many rows do I show", and it is
// geometry.ClampCount rather than a local comparison because thirty widgets would
// otherwise each write one. The distinction from Capacity is the difference
// between a full viewport and the last, short window of a scrolled collection —
// and Visible, not Capacity, is what ForEach paints and what a scrollbar should
// report.
func (m *Model) Visible() int { return geometry.ClampCount(m.count-m.offset, m.Capacity()) }

// Resize tells the model the viewport rectangle and reports whether the visible
// row count changed.
//
// It is the rect-keyed hook of ADR 0007 §3 in the form a data widget needs: the
// widget calls it inside its own size-change check and recomputes anything
// derived from the viewport only when it says true. Passing the rect rather than
// a height is deliberate — the engine does not need the x and y, but a caller
// that has a rect should not have to remember which field of it is the height.
func (m *Model) Resize(r geometry.Rect) bool {
	before := m.Visible()
	m.height = nonNegative(r.H)
	m.clampOffset()
	return m.Visible() != before
}

// SetViewport sets the viewport height in cells directly, for a caller that has a
// height rather than a rect. It is equivalent to Resize on a rect of that height.
func (m *Model) SetViewport(height int) {
	m.height = nonNegative(height)
	m.clampOffset()
}

// Offset returns the index of the first visible row. It is always in
// [0, MaxOffset].
func (m *Model) Offset() int { return m.offset }

// MaxOffset returns the largest offset that still fills the viewport, or 0 when
// the collection is no taller than the viewport.
func (m *Model) MaxOffset() int {
	last := m.count - m.Capacity()
	if last < 0 {
		return 0
	}
	return last
}

// AtEnd reports whether the last row is visible, i.e. whether the collection
// cannot scroll any further.
func (m *Model) AtEnd() bool { return m.offset >= m.MaxOffset() }

// SetOffset scrolls to i, clamped to [0, MaxOffset]. It is O(1).
func (m *Model) SetOffset(i int) {
	m.offset = i
	m.clampOffset()
}

// clampOffset pins the offset into range. It is the whole of rule 2 above, and
// it runs on every path that can invalidate the offset: a count change, a height
// change and a direct scroll.
func (m *Model) clampOffset() {
	if m.offset > m.MaxOffset() {
		m.offset = m.MaxOffset()
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// ScrollBy moves the offset by delta rows, which may be negative. It is clamped
// like SetOffset, so a wheel notch at the end of the collection is a no-op rather
// than an error.
func (m *Model) ScrollBy(delta int) {
	m.SetOffset(m.offset + delta)
}

// ScrollToEnd scrolls so the last row is at the bottom of the viewport.
func (m *Model) ScrollToEnd() { m.SetOffset(m.MaxOffset()) }

// ScrollToStart scrolls back to the first row.
func (m *Model) ScrollToStart() { m.SetOffset(0) }

// LineUp scrolls up n rows, defaulting to one.
func (m *Model) LineUp(n int) { m.ScrollBy(-max1(n)) }

// LineDown scrolls down n rows, defaulting to one.
func (m *Model) LineDown(n int) { m.ScrollBy(max1(n)) }

// PageUp scrolls up by roughly a viewport, keeping one row of context so the
// reader can tell where they are.
func (m *Model) PageUp() { m.ScrollBy(-m.page()) }

// PageDown scrolls down by roughly a viewport.
func (m *Model) PageDown() { m.ScrollBy(m.page()) }

// page is the scroll distance for a page key: a whole viewport, minus the one
// row of overlap that makes consecutive pages readable.
func (m *Model) page() int {
	n := m.Visible() - 1
	if n < 1 {
		n = 1
	}
	return n
}

// ScrollIntoView scrolls the minimum amount that makes item i visible, and does
// nothing if it already is.
//
// It is O(1) and is the only placement rule here, because "clamp, do not
// recentre" is what keeps four widgets from each deciding that a selection should
// sit in the middle of the viewport.
func (m *Model) ScrollIntoView(i int) {
	if i < 0 || i >= m.count {
		return
	}
	vis := m.Visible()
	if vis == 0 {
		// Nothing fits, so there is no "visible" to make i. Leave the offset
		// where it is rather than jumping to a row that cannot be shown.
		return
	}
	switch {
	case i < m.offset:
		m.offset = i
	case i >= m.offset+vis:
		m.offset = i - vis + 1
	}
	m.clampOffset()
}

// Range returns the item indices visible in the viewport as [first, last),
// clipped to the collection.
//
// It is for a caller that needs to do its own per-row work — measuring column
// widths for a visible subset, say — and wants the bounds without running a
// closure. An empty viewport or an empty collection returns [0, 0).
func (m *Model) Range() (first, last int) {
	vis := m.Visible()
	if vis == 0 || m.count == 0 {
		return 0, 0
	}
	first = m.offset
	last = first + vis
	if last > m.count {
		last = m.count
	}
	return first, last
}

// ItemAt returns the item index shown on viewport row y, and whether y is inside
// the viewport at all.
//
// It is what a mouse click needs, and it is the inverse of the drawing order: a
// click on a partially visible row returns that row's index, never an index one
// past the end.
func (m *Model) ItemAt(y int) (int, bool) {
	if m.rowH < 1 || m.height < 1 {
		return 0, false
	}
	if y < 0 || y >= m.height {
		return 0, false
	}
	i := m.offset + y/m.rowH
	if i < 0 || i >= m.count {
		return 0, false
	}
	return i, true
}

// ForEach paints the visible rows into viewport.
//
// It calls paint once per visible row with that row's own rectangle, clipped to
// the viewport so a partial row at the bottom edge is passed its real height.
// Rows past the end of the collection are not painted at all.
//
// It allocates nothing, which is the package's reason for existing: a 100,000-row
// list costs the same per frame as a 10-row one because nothing here is O(count).
func (m *Model) ForEach(viewport geometry.Rect, dst *buffer.Buffer, paint Paint) {
	if paint == nil || viewport.Empty() || m.count == 0 {
		return
	}
	h := m.rowH
	first, last := m.Range()
	for i := first; i < last; i++ {
		y := viewport.Y + (i-m.offset)*h
		if y >= viewport.Bottom() {
			// Defensive: Range already bounds this, but a row height change
			// between Range and here must not write below the viewport.
			break
		}
		row := clipRect(geometry.Rect{X: viewport.X, Y: y, W: viewport.W, H: h}, viewport)
		if row.Empty() {
			continue
		}
		paint(dst, row, i)
	}
}

// clipRect intersects r with bounds without the negative-coordinate handling
// Rect.Clip does for a screen: a viewport is always a real region, so this only
// has to handle the far edge.
func clipRect(r, bounds geometry.Rect) geometry.Rect {
	if r.Y < bounds.Y {
		r.H -= bounds.Y - r.Y
		r.Y = bounds.Y
	}
	if r.Bottom() > bounds.Bottom() {
		r.H = bounds.Bottom() - r.Y
	}
	return r
}

// max1 clamps v at one cell, for the scroll distances where zero would mean "the
// key did nothing", which is never what a line or page key means.
func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
