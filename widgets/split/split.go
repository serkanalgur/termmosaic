// Package split provides Split, the pane composer: it solves one layout and hands
// each pane a rectangle.
//
// It exists because a terminal application that wants two panes needs three
// things and none of them is interesting: solve a constraint list, hand the
// results out as rectangles, and let the user move the divider. The first is
// layout's job and Split calls it rather than reimplementing it — ADR 0004 chose a
// closed, predictable constraint set precisely so that exactly one solver exists,
// and a second arithmetic pass inside a widget is how two solvers start to
// disagree about overflow.
//
// # The one interface Split adds
//
// A widget receives its rectangle through Bounds, and nothing in the framework
// hands it one: there is no Resize method to push down the tree (ADR 0007 rejects
// it), so a container must be able to SET bounds. Hence Bounded, which a pane
// implements by having a SetBounds method — block.Block, basic.Text and
// basic.Paragraph all do. A pane that does not implement it is still drawn, with
// whatever rectangle the application gave it; Split never invents one.
//
// # Resizing
//
// Two mechanisms, because two are what an application needs:
//
//   - A mouse drag on a divider. A press within a divider cell starts the drag, a
//     drag moves it, a release ends it. The pointer must not be captured: a drag
//     that leaves the widget still tracks, and a release anywhere ends it.
//   - Ctrl with an arrow key, which moves the divider beside the focused pane by
//     one cell. Plain arrows move focus between panes, which is why the modifier
//     is required rather than optional.
//
// A resize does not edit the constraint list the caller supplied. It converts the
// whole list to proportional percentages with a trailing Fill, so that the solver
// keeps handling every later resize — including one the user never touched — and
// the panes keep their proportions through it. A trailing Fill is what absorbs
// the rounding remainder, so the panes still fill the axis exactly rather than
// leaving a one-cell gap.
//
// # Focus
//
// Split is Focusable. It holds the focus INDEX and forwards keys to the focused
// pane, and it gives focus to a pane on a click. Panes that are themselves
// Focusable still report their own state; Split's is about which pane receives
// keys, not about drawing a highlight.
package split

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/layout"
)

// minPane is the smallest a pane may be resized to, in cells. A pane of zero cells
// cannot be focused, scrolled or read, and a drag that reaches it has usually left
// the application in a state the user cannot get out of without a resize.
//
// It is a local named constant rather than framework vocabulary, per ADR 0007 §1
// rule 5: it is this widget's policy about its own children.
const minPane = 1

// Bounded is a pane Split can position: something with a SetBounds method.
//
// It is a local interface rather than a new termmosaic.Widget method, because
// ADR 0007 rejects adding a fourth method to Widget for exactly this: it is a
// silent-failure surface, and a pane that cannot be positioned is a container's
// problem, not the framework's contract.
type Bounded interface {
	// SetBounds gives the pane its rectangle, which must be current before the
	// next Draw (ADR 0007 §3).
	SetBounds(buffer.Rect)
}

// Split arranges panes along one axis.
type Split struct {
	bounds buffer.Rect

	// Direction is the axis to split.
	Direction layout.Direction

	// spacing is the number of cells between adjacent panes. Negative values are
	// treated as zero, as they are in layout.
	//
	// Unexported because the pane solve is cached against the bounds AND this
	// value, so a public field here is a field a caller can assign with no way to
	// mark the solve stale. Use SetSpacing and Spacing.
	spacing int

	// Background is painted across Bounds before the panes, for the same reason
	// every widget paints its own rect: the renderer diffs and never clears.
	Background buffer.Style

	// panes are the children, and constraints holds one sizing rule per pane in
	// the same order. A mismatch between the two is legal and defined: the panes
	// with a constraint are positioned, the rest are drawn with no rectangle of
	// their own, and the constraints with no pane are solved and discarded.
	panes       []termmosaic.Widget
	constraints []layout.Constraint

	// focus is the index of the pane that receives keys.
	focus int
	// focused is whether this Split itself holds focus.
	focused bool

	// dragging is the index of the divider being dragged, or -1. It is the pane
	// whose TRAILING edge moves: dragging divider 1 widens pane 1 and narrows
	// pane 2.
	dragging int

	// sizes is the cached solve result and cachedRect the bounds it was computed
	// for. dirty forces a re-solve after a constraint change that did not come
	// with a new rect.
	sizes      []int
	cachedRect buffer.Rect
	dirty      bool
}

// New returns a Split arranging panes along d, each sharing the space equally.
//
// The constraints are Fill(1) per pane, which is the common case and the one that
// resizes most predictably.
func New(d layout.Direction, panes ...termmosaic.Widget) *Split {
	s := &Split{Direction: d, dragging: -1, dirty: true}
	s.Panes(panes)
	return s
}

// Panes replaces the children and resets the constraints to Fill(1) each, the
// focus to the first pane, and any drag in progress.
//
// Replacing the children resets the constraints too rather than trying to
// reconcile them: a constraint list is a statement about a specific set of panes,
// and matching them up by index after the set has changed is how a container ends
// up with three panes and two constraints and no error.
func (s *Split) Panes(panes []termmosaic.Widget) {
	s.panes = append([]termmosaic.Widget(nil), panes...)
	s.constraints = make([]layout.Constraint, len(panes))
	for i := range s.constraints {
		s.constraints[i] = layout.Fill(1)
	}
	s.focus = 0
	s.dragging = -1
	s.dirty = true
}

// SetConstraints sets one sizing rule per pane, in pane order.
//
// The slice is copied, because a caller that reuses a backing array would
// otherwise change the layout between frames without the Split knowing.
func (s *Split) SetConstraints(cs []layout.Constraint) {
	s.constraints = append([]layout.Constraint(nil), cs...)
	s.dirty = true
}

// Constraints returns a copy of the sizing rules, so a caller can read what the
// Split is actually using — which after a manual resize is the proportional form,
// not what it passed in.
func (s *Split) Constraints() []layout.Constraint {
	return append([]layout.Constraint(nil), s.constraints...)
}

// SetBounds sets the Split's rectangle. It must be current before the next Draw.
func (s *Split) SetBounds(r buffer.Rect) { s.bounds = r }

// Bounds returns the Split's rectangle, safe to call before the first Draw.
func (s *Split) Bounds() buffer.Rect { return s.bounds }

// Spacing returns the gap between panes in cells.
func (s *Split) Spacing() int { return s.spacing }

// SetSpacing sets the gap between panes, clamping a negative value to zero, and
// marks the cached solve stale.
//
// This setter was always correct; what was missing was that Spacing was also a
// public field, so `split.Spacing = 3` reached the same state with no way to mark
// anything dirty and the panes kept their old sizes until the next resize. The
// field is unexported now for the reason ADR 0007 §3 gives: the solve is a
// derivation, and every route to changing an input of a derivation must invalidate.
func (s *Split) SetSpacing(n int) {
	if n < 0 {
		n = 0
	}
	s.spacing = n
	s.dirty = true
}

// SetBackground sets the style painted across Bounds.
func (s *Split) SetBackground(st buffer.Style) { s.Background = st }

// PaneCount returns the number of panes.
func (s *Split) PaneCount() int { return len(s.panes) }

// Pane returns pane i, or nil if i is out of range.
func (s *Split) Pane(i int) termmosaic.Widget {
	if i < 0 || i >= len(s.panes) {
		return nil
	}
	return s.panes[i]
}

// PaneBounds returns the rectangle pane i occupies. It is empty for a pane with no
// constraint and for any index outside the solved range, and it is empty before the
// first solve because the sizes are computed lazily.
//
// It is the accessor a container is meant to use. The solve is cached against the
// rect and the constraint generation, so calling it per frame costs a comparison
// rather than a solve.
func (s *Split) PaneBounds(i int) buffer.Rect {
	sizes := s.currentSizes()
	if i < 0 || i >= len(sizes) {
		return buffer.Rect{}
	}
	return layout.Rect(s.bounds, s.Direction, sizes, s.spacing, i)
}

// PaneAt returns the index of the pane containing (x, y), or -1. The divider cells
// between panes belong to no pane, which is what makes them draggable.
func (s *Split) PaneAt(x, y int) int {
	if s.bounds.Empty() || !s.bounds.Contains(x, y) {
		return -1
	}
	sizes := s.currentSizes()
	for i := range sizes {
		if layout.Rect(s.bounds, s.Direction, sizes, s.spacing, i).Contains(x, y) {
			return i
		}
	}
	return -1
}

// dividerAt returns the index of the divider at (x, y): the run of cells after pane
// i and before pane i+1. ok is false when the point is not on a divider, which is the
// common case and the one that must be cheap.
//
// With a Spacing of one or more the divider owns its gap cells. With no spacing at
// all there is no gap cell, so the divider is the single column where one pane ends
// and the next begins — the same cell as pane i+1's first column, which means a
// press there both focuses that pane and starts a drag. That ambiguity is the price
// of a draggable divider in a layout with no gaps, and it is the cheaper of the two
// failures: the alternative is a layout nobody can resize.
func (s *Split) dividerAt(x, y int) (i int, ok bool) {
	if s.bounds.Empty() || !s.bounds.Contains(x, y) {
		return 0, false
	}
	sizes := s.currentSizes()
	grab := s.spacing
	if grab < 1 {
		grab = 1
	}
	// Walking the solved sizes is what keeps hit testing free: no second solve and
	// no allocation, which a mouse event arriving every few milliseconds does not
	// need.
	off := s.bounds.Y
	at := x
	if s.Direction == layout.Horizontal {
		off = s.bounds.X
	} else {
		at = y
	}
	for j := 1; j < len(sizes); j++ {
		gap := off + sizes[j-1]
		if at >= gap && at < gap+grab {
			return j - 1, true
		}
		off = gap + s.spacing
	}
	return 0, false
}

// Focus returns the index of the pane that receives keys. It is 0 for an empty
// Split rather than an error, because an empty Split is a legal state.
func (s *Split) Focus() int { return s.focus }

// SetFocus makes pane i the key receiver and invalidates the Split. An
// out-of-range index is clamped rather than rejected, since the index is
// layout-derived and a TUI that panics on a rounding error is unusable.
func (s *Split) SetFocus(i int) {
	if len(s.panes) == 0 {
		s.focus = 0
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(s.panes) {
		i = len(s.panes) - 1
	}
	s.focus = i
}

// Focused reports whether the Split itself holds focus.
func (s *Split) Focused() bool { return s.focused }

// SetFocused gives or removes the Split's focus. Gaining focus also focuses the
// first pane, because a Split that holds focus but forwards keys to no pane would
// swallow every key in a screen of them.
func (s *Split) SetFocused(f bool) {
	s.focused = f
	if f && len(s.panes) > 0 {
		s.focus = 0
	}
}

// Resize moves the divider after pane i by delta cells: pane i grows by delta and
// pane i+1 shrinks by it. It reports whether anything changed.
//
// The move is clamped so neither pane falls below minPane, and it is refused
// outright for the last pane, which has no divider after it.
func (s *Split) Resize(i, delta int) bool {
	if delta == 0 || i < 0 || i+1 >= len(s.panes) || s.bounds.Empty() {
		return false
	}
	sizes := s.currentSizes()
	if i+1 >= len(sizes) {
		// A pane count and a constraint count that disagree leave no second pane to
		// take the cells from.
		return false
	}
	// Clamp the move so neither pane falls below minPane. Resizing is refused
	// rather than allowed to swallow a pane when there is nothing left to give.
	d := delta
	if sizes[i]+d < minPane {
		d = minPane - sizes[i]
	}
	if sizes[i+1]-d < minPane {
		d = sizes[i+1] - minPane
	}
	if d == 0 {
		return false
	}
	// The cached slice is edited in place rather than copied: proportional marks
	// the cache stale on the next line, so nothing reads the intermediate values.
	sizes[i] += d
	sizes[i+1] -= d
	s.proportional(sizes)
	// Solve immediately, so the panes are repositioned by the time Resize returns
	// and a caller that resizes and then reads PaneBounds or the screen sees the
	// new sizes rather than the previous frame's.
	s.currentSizes()
	return true
}

// proportional replaces the constraint list with one that still reproduces the
// requested sizes, so a manual resize survives a later resize without the Split
// needing any arithmetic of its own: layout keeps solving, and the solver stays the
// only thing that decides sizes.
//
// The weights ARE the cells. Fill shares what is left in proportion to weight and
// hands out the rounding remainder by largest remainder, so Fill(15)/Fill(5) is 15
// and 5 in 20 cells, 30 and 10 in 40, and never a gap at the end of the axis.
//
// Percentage was the obvious alternative and is wrong here: layout resolves each
// Percentage independently and floors it, so 33/33/33 in 30 cells comes out 9/9/12
// and the last pane silently swallows the rounding. Fill exists to avoid exactly
// that, so a widget that has already computed sizes hands them to Fill rather than
// re-deriving a distribution the solver knows how to make exact.
//
// A pane resolved to zero cells becomes Fill(0), which layout treats as weight 1.
// That is layout's documented reading of a zero weight — share equally rather than
// claim nothing — and it is right here: a pane the screen is too small for should
// come back when there is room rather than staying at zero.
func (s *Split) proportional(sizes []int) {
	if len(sizes) == 0 {
		return
	}
	cs := make([]layout.Constraint, len(sizes))
	for i, n := range sizes {
		cs[i] = layout.Fill(n)
	}
	s.constraints = cs
	s.dirty = true
}

// currentSizes returns the solved sizes, solving first if the cache is stale. It
// exists so the editing operations read one answer rather than each re-deriving.
func (s *Split) currentSizes() []int {
	if s.dirty || s.cachedRect != s.bounds || len(s.sizes) != len(s.constraints) {
		s.solve()
	}
	return s.sizes
}

// solve runs the layout solver and keeps the result for PaneBounds and the
// editing operations.
//
// It is the only allocation in this file and it happens once per rect or per
// constraint change, never per frame: layout.Solve builds a fresh slice every call
// and there is no way to ask it for a view, so the result is copied into a slice
// the Split owns and reused from then on.
func (s *Split) solve() {
	solved := layout.Solve(s.Direction, s.constraints, s.spacing, s.axisLength())
	if cap(s.sizes) < len(solved) {
		s.sizes = make([]int, len(solved))
	} else {
		s.sizes = s.sizes[:len(solved)]
	}
	copy(s.sizes, solved)
	s.cachedRect = s.bounds
	s.dirty = false
	s.position()
}

// position hands every pane its rectangle from the freshly solved sizes.
//
// It runs on every solve rather than only in Draw, so that an operation which
// changes the layout — a resize, a drag, SetConstraints — is observable
// immediately, without waiting for the next frame. A caller that resizes a pane and
// then reads PaneBounds must not get the previous rectangle, and the cheapest way to
// guarantee that is for the pane positions to be a consequence of solving rather
// than of drawing.
func (s *Split) position() {
	for i, pane := range s.panes {
		if pane == nil {
			continue
		}
		if b, ok := pane.(Bounded); ok {
			b.SetBounds(s.paneBounds(i))
		}
	}
}

// paneBounds is PaneBounds without the lazy solve, for use from inside solve.
func (s *Split) paneBounds(i int) buffer.Rect {
	if i < 0 || i >= len(s.sizes) {
		return buffer.Rect{}
	}
	return layout.Rect(s.bounds, s.Direction, s.sizes, s.spacing, i)
}

// axisLength is the space available along the Split's axis, or zero when the rect
// is empty. It is Bounds-derived, never the buffer's size (ADR 0007 §1 rule 1).
func (s *Split) axisLength() int {
	if s.bounds.Empty() {
		return 0
	}
	if s.Direction == layout.Horizontal {
		return s.bounds.W
	}
	return s.bounds.H
}

// MinSize returns the smallest rect in which every pane gets minPane cells and
// the dividers get their spacing, on Split's axis; the cross axis needs one cell,
// since a pane of zero rows or columns has nothing to draw in.
//
// It is the same minPane the drag and the keyboard resize clamp to, so MinSize
// and the smallest the widget will let the user reach are one number rather than
// two. It is pure: it reads the pane count and the configured spacing, never the
// bounds, so it is safe before the first draw and safe to cache.
//
// A spacing wider than the axis makes MinSize exceed any real screen, which is
// true rather than a bug — the caller set a gap the screen cannot pay for, and
// §4's rule applies: draw it clipped, do not panic and do not blank.
func (s *Split) MinSize() buffer.Size {
	n := len(s.panes)
	along := 1
	if n > 0 {
		along = n*minPane + s.spacing*(n-1)
		if along < 1 {
			along = 1
		}
	}
	if s.Direction == layout.Horizontal {
		return buffer.Size{W: along, H: 1}
	}
	return buffer.Size{W: 1, H: along}
}

// Draw paints the background, solves the layout if it changed and draws the panes
// in order. Positioning them is part of solving, so it has already happened.
//
// It is total for every rect including empty, and it allocates nothing in steady
// state: the solve is cached against the rect and the constraint generation.
func (s *Split) Draw(buf *buffer.Buffer) {
	r := s.bounds
	if r.Empty() {
		return
	}
	buf.FillRect(r, s.Background.Resolved().Blank())

	// currentSizes solves if the rect or the constraints changed, and a solve
	// positions every pane, so the panes are already placed here.
	s.currentSizes()
	for _, pane := range s.panes {
		if pane != nil {
			pane.Draw(buf)
		}
	}
}

// Invalidate satisfies termmosaic.Widget. The Split repaints in full every frame;
// a pane that caches invalidation state of its own handles its own.
func (s *Split) Invalidate() {}

// Handle routes an event: mouse events to the drag and focus machinery, keys to
// the focus machinery when the Split has focus, and anything left over to the
// focused pane.
//
// It is safe at every size. With fewer than two panes there is nothing to focus,
// resize or drag, so keys fall through to the single pane and a click on it focuses
// it.
func (s *Split) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventMouse:
		return s.handleMouse(ev.Mouse)
	case termmosaic.EventKey:
		return s.handleKey(ev)
	default:
		return false
	}
}

// handleMouse runs a drag, or focuses the pane under a press.
func (s *Split) handleMouse(m termmosaic.Mouse) bool {
	switch m.Action {
	case termmosaic.MousePress:
		if s.dragging >= 0 && m.Button == termmosaic.MouseLeft {
			// A second press while dragging is not a new drag.
			return true
		}
		if m.Button == termmosaic.MouseLeft {
			if i, ok := s.dividerAt(m.X, m.Y); ok {
				s.dragging = i
				// The pane on the far side of a divider is the one being widened or
				// narrowed, so it is the one whose keys the user wants next.
				s.SetFocus(i + 1)
				return true
			}
		}
		if i := s.PaneAt(m.X, m.Y); i >= 0 {
			s.SetFocus(i)
			return true
		}
		return false
	case termmosaic.MouseDrag:
		if s.dragging < 0 {
			return false
		}
		return s.dragBy(m)
	case termmosaic.MouseRelease:
		if s.dragging < 0 {
			return false
		}
		// A release anywhere ends the drag, including outside the widget: a drag
		// that can only be finished by releasing over the divider strands the user
		// in a mode they cannot see.
		s.dragging = -1
		return true
	default:
		return false
	}
}

// dragBy moves the divider being dragged to the pointer. It is a press-move-press
// conversion rather than a delta: the divider follows the pointer absolutely, so a
// fast drag cannot accumulate rounding error, and a drag that jumps is exact.
func (s *Split) dragBy(m termmosaic.Mouse) bool {
	i := s.dragging
	r := s.bounds
	if r.Empty() {
		return false
	}
	at := m.X
	if s.Direction == layout.Vertical {
		at = m.Y
	}
	start := r.X
	if s.Direction == layout.Vertical {
		start = r.Y
	}
	// The divider sits after pane i, so its wanted position is the start of pane
	// i+1. The offset from the pane's start to the pointer is the new size.
	sizes := s.currentSizes()
	prev := layout.Offset(sizes, s.spacing, i)
	want := at - start - prev
	return s.Resize(i, want-sizes[i])
}

// handleKey routes a key: a modifier-plus-arrow resizes, a bare arrow moves focus,
// and anything unconsumed goes to the focused pane.
func (s *Split) handleKey(ev termmosaic.Event) bool {
	if s.focused && len(s.panes) > 0 {
		switch ev.Key {
		case termmosaic.KeyLeft:
			if ev.Mod&termmosaic.ModCtrl != 0 && s.Direction == layout.Horizontal {
				return s.Resize(s.focus, -1)
			}
			s.moveFocus(-1)
			return true
		case termmosaic.KeyRight:
			if ev.Mod&termmosaic.ModCtrl != 0 && s.Direction == layout.Horizontal {
				return s.Resize(s.focus, +1)
			}
			s.moveFocus(+1)
			return true
		case termmosaic.KeyUp:
			if ev.Mod&termmosaic.ModCtrl != 0 && s.Direction == layout.Vertical {
				return s.Resize(s.focus, -1)
			}
			s.moveFocus(-1)
			return true
		case termmosaic.KeyDown:
			if ev.Mod&termmosaic.ModCtrl != 0 && s.Direction == layout.Vertical {
				return s.Resize(s.focus, +1)
			}
			s.moveFocus(+1)
			return true
		case termmosaic.KeyTab:
			s.moveFocus(+1)
			return true
		case termmosaic.KeyBacktab:
			s.moveFocus(-1)
			return true
		}
	}
	if len(s.panes) > 0 && s.focus >= 0 && s.focus < len(s.panes) {
		return s.panes[s.focus].Handle(ev)
	}
	return false
}

// moveFocus moves the focus index by delta, stopping at the ends rather than
// wrapping: wrapping makes a held arrow key cycle forever, and stopping is what
// every other list in a terminal does.
func (s *Split) moveFocus(delta int) {
	if len(s.panes) == 0 {
		return
	}
	i := s.focus + delta
	if i < 0 {
		i = 0
	}
	if i >= len(s.panes) {
		i = len(s.panes) - 1
	}
	s.focus = i
}

// Compile-time proofs. Split is Focusable because it decides which pane receives
// keys, a Widget because a container is the root of a subtree more often than any
// leaf is, and Minimizable because its minimum is arithmetic over its own pane
// count and spacing rather than a guess.
var (
	_ termmosaic.Widget      = (*Split)(nil)
	_ termmosaic.Focusable   = (*Split)(nil)
	_ termmosaic.Minimizable = (*Split)(nil)
	_ Bounded                = (*Split)(nil)
)
