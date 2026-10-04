package split

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// label returns a pane that paints a single row of text at the top-left of its own
// rectangle, so a rendered screen says which pane each cell came from.
func label(w *basic.Text, s string) *basic.Text {
	*w = *basic.NewTextString(buffer.Rect{}, s, buffer.PlainStyle)
	return w
}

// twoPanes returns a Split of two labelled text panes, sized r.
func twoPanes(d layout.Direction, r buffer.Rect) (*Split, *basic.Text, *basic.Text) {
	a := label(new(basic.Text), "aaaa")
	b := label(new(basic.Text), "bbbb")
	s := New(d, a, b)
	s.SetBounds(r)
	return s, a, b
}

// rows renders the Split and returns the trimmed rows.
func rows(t *testing.T, w, h int, s *Split) []string {
	t.Helper()
	sink := widgettest.Render(t, w, h, 1, s)
	out := make([]string, h)
	for y := 0; y < h; y++ {
		out[y] = widgettest.Row(sink, y)
	}
	return out
}

// ---------------------------------------------------------------------------
// composition
// ---------------------------------------------------------------------------

// TestSplitGivesEachPaneItsRectangle is the whole of the widget's job.
func TestSplitGivesEachPaneItsRectangle(t *testing.T) {
	s, a, b := twoPanes(layout.Horizontal, buffer.Rect{W: 11, H: 2})
	rows(t, 11, 2, s)

	if got, want := a.Bounds(), (buffer.Rect{W: 6, H: 2}); got != want {
		t.Errorf("pane 0 bounds %+v, want %+v", got, want)
	}
	if got, want := b.Bounds(), (buffer.Rect{X: 6, W: 5, H: 2}); got != want {
		t.Errorf("pane 1 bounds %+v, want %+v", got, want)
	}
	// Solve hands the leftover cell to the first pane by largest remainder, so pane
	// 0 is 6 cells and the text is four wide inside it.
	if got := rows(t, 11, 2, s); got[0] != "aaaa  bbbb" {
		t.Errorf("row 0 = %q, want %q", got[0], "aaaa  bbbb")
	}
}

// TestSplitVerticalSplitsTheHeight is the other axis, including that the panes are
// the full width.
func TestSplitVerticalSplitsTheHeight(t *testing.T) {
	s, a, b := twoPanes(layout.Vertical, buffer.Rect{W: 6, H: 5})
	got := rows(t, 6, 5, s)

	if want := (buffer.Rect{W: 6, H: 3}); a.Bounds() != want {
		t.Errorf("pane 0 bounds %+v, want %+v", a.Bounds(), want)
	}
	if want := (buffer.Rect{Y: 3, W: 6, H: 2}); b.Bounds() != want {
		t.Errorf("pane 1 bounds %+v, want %+v", b.Bounds(), want)
	}
	want := []string{"aaaa", "", "", "bbbb", ""}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSplitAppliesSpacing checks that the gap is reserved before the panes are
// sized, which is layout's documented resolution order and the reason spacing
// costs each pane rather than being carved off the last one.
func TestSplitAppliesSpacing(t *testing.T) {
	s, a, b := twoPanes(layout.Horizontal, buffer.Rect{W: 12, H: 1})
	s.SetSpacing(2)
	rows(t, 12, 1, s)

	if got, want := a.Bounds().W, 5; got != want {
		t.Errorf("pane 0 width %d, want %d (12 cells less 2 of spacing, shared)", got, want)
	}
	if got, want := b.Bounds().X, 7; got != want {
		t.Errorf("pane 1 x %d, want %d", got, want)
	}
}

// TestSplitHonoursCallerConstraints checks that a Split is a container and not a
// second solver: Length and Percentage constraints arrive from layout untouched.
func TestSplitHonoursCallerConstraints(t *testing.T) {
	s, a, b := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	s.SetConstraints([]layout.Constraint{layout.Length(6), layout.Fill(1)})
	rows(t, 20, 1, s)

	if got, want := a.Bounds(), (buffer.Rect{W: 6, H: 1}); got != want {
		t.Errorf("pane 0 bounds %+v, want %+v", got, want)
	}
	if got, want := b.Bounds(), (buffer.Rect{X: 6, W: 14, H: 1}); got != want {
		t.Errorf("pane 1 bounds %+v, want %+v", got, want)
	}
}

// TestSplitDrawsAPaneWithoutSetBounds: a pane the Split cannot position is still
// drawn, with whatever rectangle the application gave it. Split never invents one.
func TestSplitDrawsAPaneWithoutSetBounds(t *testing.T) {
	fixed := label(new(basic.Text), "fixed")
	fixed.SetBounds(buffer.Rect{X: 2, Y: 0, W: 5, H: 1})
	s := New(layout.Horizontal, fixed, label(new(basic.Text), "other"))
	s.SetBounds(buffer.Rect{W: 20, H: 1})

	got := rows(t, 20, 1, s)
	if !strings.Contains(got[0], "fixed") {
		t.Errorf("row 0 = %q: an unpositioned pane was not drawn", got[0])
	}
}

// TestSplitToleratesAPaneCountAndConstraintCountMismatch: the mismatched entries
// are defined behaviour, not a crash.
func TestSplitToleratesAPaneCountAndConstraintCountMismatch(t *testing.T) {
	a := label(new(basic.Text), "aaaa")
	s := New(layout.Horizontal, a)
	s.SetConstraints([]layout.Constraint{layout.Fill(1), layout.Fill(1)})
	s.SetBounds(buffer.Rect{W: 10, H: 1})

	if got := s.PaneBounds(0); got.Empty() {
		t.Errorf("pane 0 got an empty rectangle: the first constraint should still apply")
	}
	rows(t, 10, 1, s) // must not panic

	other := New(layout.Horizontal, label(new(basic.Text), "a"), label(new(basic.Text), "b"))
	other.SetConstraints(nil)
	other.SetBounds(buffer.Rect{W: 10, H: 1})
	if got := other.PaneBounds(0); !got.Empty() {
		t.Errorf("pane with no constraint got %+v, want empty", got)
	}
	rows(t, 10, 1, other) // must not panic
}

// TestSplitToleratesANilPane, because a container building its children from a
// configuration can produce one.
func TestSplitToleratesANilPane(t *testing.T) {
	s := New(layout.Horizontal, label(new(basic.Text), "a"), nil)
	s.SetBounds(buffer.Rect{W: 8, H: 1})
	rows(t, 8, 1, s) // must not panic
	if got := s.Pane(1); got != nil {
		t.Errorf("Pane(1) = %v, want nil", got)
	}
	if got := s.Pane(5); got != nil {
		t.Errorf("Pane(5) = %v, want nil for an out-of-range index", got)
	}
}

// TestSplitPaneBoundsUsesBoundsNotTheBuffer is ADR 0007 §1 rule 1 for a container:
// the panes are sized out of the Split's rectangle, never the screen's, so a Split
// occupying part of a screen hands out part-screen panes.
func TestSplitPaneBoundsUsesBoundsNotTheBuffer(t *testing.T) {
	s, a, b := twoPanes(layout.Horizontal, buffer.Rect{X: 4, W: 8, H: 1})
	rows(t, 40, 1, s)

	if got, want := a.Bounds(), (buffer.Rect{X: 4, W: 4, H: 1}); got != want {
		t.Errorf("pane 0 bounds %+v, want %+v: the buffer's 40 columns were used", got, want)
	}
	if got, want := b.Bounds(), (buffer.Rect{X: 8, W: 4, H: 1}); got != want {
		t.Errorf("pane 1 bounds %+v, want %+v", got, want)
	}
}

// ---------------------------------------------------------------------------
// repainting, in both directions
// ---------------------------------------------------------------------------

// TestSplitShrinkRepaintsWholeRect is ADR 0007 §1 rule 3 at the container level: a
// Split that showed two panes and is then shown at half the size must repaint
// every row it still owns, or the right-hand pane's old text stays on screen.
//
// It is the SHRINK that matters. A grow-only test passes against a widget that
// never blanks anything, because growing only adds cells the widget paints.
func TestSplitShrinkRepaintsWholeRect(t *testing.T) {
	s, _, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	wide := rows(t, 20, 1, s)
	if !strings.Contains(wide[0], "bbbb") {
		t.Fatalf("setup: row 0 = %q, want both panes", wide[0])
	}

	// Shrink within the same buffer, so nothing outside the widget is cleared for
	// us and a missing repaint shows up as stale text.
	buf := buffer.NewBuffer(20, 1)
	s.Draw(buf)
	s.SetBounds(buffer.Rect{W: 6, H: 1})
	s.Draw(buf)
	if got := rowOf(buf, 0, 6); strings.Contains(got, "bbbb") {
		t.Errorf("row 0 = %q: the pane that no longer fits left stale cells", got)
	}
}

// TestSplitGrowReflowsPanes: growing must re-solve, or the panes keep the old
// rectangles and the right-hand one stops short of the edge.
func TestSplitGrowReflowsPanes(t *testing.T) {
	s, a, b := twoPanes(layout.Horizontal, buffer.Rect{W: 10, H: 1})
	s.Draw(buffer.NewBuffer(40, 1))
	if got, want := b.Bounds().Right(), 10; got != want {
		t.Fatalf("setup: pane 1 right edge %d, want %d", got, want)
	}

	s.SetBounds(buffer.Rect{W: 20, H: 1})
	s.Draw(buffer.NewBuffer(40, 1))
	if got, want := a.Bounds().W, 10; got != want {
		t.Errorf("pane 0 width %d, want %d: the layout was not re-solved", got, want)
	}
	if got, want := b.Bounds(), (buffer.Rect{X: 10, W: 10, H: 1}); got != want {
		t.Errorf("pane 1 bounds %+v, want %+v", got, want)
	}
}

// TestSplitRepaintsTheGapBetweenPanes checks the cells no pane owns: with spacing
// or a shrinking pane, they must be the Split's background rather than leftovers.
func TestSplitRepaintsTheGapBetweenPanes(t *testing.T) {
	bg := buffer.NewStyle(buffer.DefaultColour, buffer.NewColour(0, 0, 0x40), 0)
	s, _, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	s.SetSpacing(4)
	s.SetBackground(bg)
	buf := buffer.NewBuffer(20, 1)
	s.Draw(buf)

	for x := s.PaneBounds(0).Right(); x < s.PaneBounds(1).X; x++ {
		if got := buf.CellAt(x, 0).BG; got != bg.BG {
			t.Errorf("cell %d in the gap has bg %s, want %s", x, got, bg.BG)
		}
	}
}

// ---------------------------------------------------------------------------
// degenerate sizes
// ---------------------------------------------------------------------------

// TestSplitDrawIsTotalAtEverySize is ADR 0007 §4 for a container, and a container
// has the most ways to be degenerate: empty rects, one cell, fewer cells than
// panes, more panes than cells.
func TestSplitDrawIsTotalAtEverySize(t *testing.T) {
	for w := 0; w <= 8; w++ {
		for h := 0; h <= 4; h++ {
			for _, n := range []int{1, 2, 5} {
				panes := make([]termmosaic.Widget, n)
				for i := range panes {
					panes[i] = label(new(basic.Text), "x")
				}
				for _, d := range []layout.Direction{layout.Horizontal, layout.Vertical} {
					s := New(d, panes...)
					s.SetSpacing(1)
					s.SetBounds(buffer.Rect{W: w, H: h})
					widgettest.Render(t, atLeast1(w), atLeast1(h), 1, s)
					// And the accessors a caller would reach for at that size.
					_ = s.PaneBounds(n)
					_, _ = s.dividerAt(0, 0)
					s.Handle(termmosaic.Event{
						Kind:  termmosaic.EventMouse,
						Mouse: termmosaic.Mouse{X: 0, Y: 0, Button: termmosaic.MouseLeft, Action: termmosaic.MouseDrag},
					})
					s.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Key: termmosaic.KeyRight, Mod: termmosaic.ModCtrl})
				}
			}
		}
	}
}

// TestSplitHandlesEverythingOnAnEmptySplit: zero panes is a legal state, and every
// method on it must be defined.
func TestSplitHandlesEverythingOnAnEmptySplit(t *testing.T) {
	s := New(layout.Horizontal)
	s.SetBounds(buffer.Rect{W: 10, H: 2})

	if got := s.Focus(); got != 0 {
		t.Errorf("Focus on an empty Split = %d, want 0", got)
	}
	s.SetFocus(3)
	s.SetFocused(true)
	s.Resize(0, 1)
	s.moveFocus(1)
	if s.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Rune: 'x'}) {
		t.Error("an empty Split consumed a key")
	}
	if s.Handle(termmosaic.Event{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{X: 1, Y: 1, Action: termmosaic.MousePress}}) {
		t.Error("an empty Split consumed a mouse press")
	}
	rows(t, 10, 2, s) // must not panic
}

// ---------------------------------------------------------------------------
// focus
// ---------------------------------------------------------------------------

// TestSplitFocusMovesWithArrowsAndClampsAtTheEnds: wrapping would make a held key
// cycle forever.
func TestSplitFocusMovesWithArrowsAndClampsAtTheEnds(t *testing.T) {
	panes := make([]termmosaic.Widget, 3)
	for i := range panes {
		panes[i] = label(new(basic.Text), "x")
	}
	s := New(layout.Horizontal, panes...)
	s.SetBounds(buffer.Rect{W: 30, H: 1})
	s.SetFocused(true)
	buf := buffer.NewBuffer(30, 1)

	if got := s.Focus(); got != 0 {
		t.Fatalf("initial focus %d, want 0", got)
	}
	key := func(k termmosaic.Key) bool {
		return s.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Key: k})
	}
	if !key(termmosaic.KeyRight) || s.Focus() != 1 {
		t.Errorf("focus %d after Right, want 1", s.Focus())
	}
	if !key(termmosaic.KeyTab) || s.Focus() != 2 {
		t.Errorf("focus %d after Tab, want 2", s.Focus())
	}
	if !key(termmosaic.KeyRight) || s.Focus() != 2 {
		t.Errorf("focus %d past the end, want it clamped at 2", s.Focus())
	}
	if !key(termmosaic.KeyLeft) || s.Focus() != 1 {
		t.Errorf("focus %d after Left, want 1", s.Focus())
	}
	if !key(termmosaic.KeyBacktab) || s.Focus() != 0 {
		t.Errorf("focus %d after Backtab, want 0", s.Focus())
	}
	if !key(termmosaic.KeyLeft) || s.Focus() != 0 {
		t.Errorf("focus %d before the start, want it clamped at 0", s.Focus())
	}
	s.Draw(buf)
}

// TestSplitFocusOnlyMovesWhenFocused: a Split that has not been given focus must
// let arrows through to the pane, or a screen of unfocused containers would eat
// every arrow key before the focused widget saw it.
func TestSplitFocusOnlyMovesWhenFocused(t *testing.T) {
	s, _, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 11, H: 1})
	if s.Focused() {
		t.Fatal("setup: a fresh Split should not hold focus")
	}
	s.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Key: termmosaic.KeyRight})
	if got := s.Focus(); got != 0 {
		t.Errorf("focus moved to %d while unfocused, want 0", got)
	}
}

// TestSplitClickFocusesAPane: a click inside a pane gives it the keys.
func TestSplitClickFocusesAPane(t *testing.T) {
	s, _, b := twoPanes(layout.Horizontal, buffer.Rect{W: 11, H: 2})
	rows(t, 11, 2, s)

	if !s.Handle(termmosaic.Event{Kind: termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: 8, Y: 0, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress}}) {
		t.Fatal("a click inside a pane was not consumed")
	}
	if got := s.Focus(); got != 1 {
		t.Errorf("focus %d after clicking pane 1, want 1", got)
	}
	if got, want := b.Bounds(), (buffer.Rect{X: 6, W: 5, H: 2}); got != want {
		t.Errorf("pane 1 bounds %+v, want %+v", got, want)
	}
}

// TestSplitClickOutsideDoesNothing: a click beyond the last pane is the container's
// dead space, and swallowing it would make a composed screen feel broken.
func TestSplitClickOutsideDoesNothing(t *testing.T) {
	s, _, _ := twoPanes(layout.Horizontal, buffer.Rect{X: 5, W: 6, H: 1})
	if s.Handle(termmosaic.Event{Kind: termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: 0, Y: 0, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress}}) {
		t.Error("a click outside the Split was consumed")
	}
}

// TestSplitForwardsUnconsumedKeysToTheFocusedPane: the pane is the thing that
// knows what a key means.
func TestSplitForwardsUnconsumedKeysToTheFocusedPane(t *testing.T) {
	rec := &recorder{}
	s := New(layout.Horizontal, rec)
	s.SetBounds(buffer.Rect{W: 10, H: 1})
	s.SetFocus(0)
	s.SetFocused(true)

	if s.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Rune: 'j'}) {
		t.Error("Split consumed a key it has no meaning for")
	}
	if got := rec.got; got != 'j' {
		t.Errorf("the focused pane saw %q, want 'j'", string(got))
	}
}

// recorder is a pane that notes the last rune it was offered.
type recorder struct {
	got    rune
	bounds buffer.Rect
}

func (r *recorder) Bounds() buffer.Rect { return r.bounds }
func (r *recorder) SetBounds(b buffer.Rect) {
	r.bounds = b
}
func (r *recorder) Invalidate() {}
func (r *recorder) Handle(ev termmosaic.Event) bool {
	if ev.Kind == termmosaic.EventKey {
		r.got = ev.Rune
	}
	return false
}
func (r *recorder) Draw(*buffer.Buffer) {}

// ---------------------------------------------------------------------------
// resizing
// ---------------------------------------------------------------------------

// TestSplitResizeMovesOneDivider is the key-driven mechanism, and it must keep the
// other pane's size constant.
func TestSplitResizeMovesOneDivider(t *testing.T) {
	s, a, b := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	rows(t, 20, 1, s)

	ctrl := func(k termmosaic.Key) bool {
		return s.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Key: k, Mod: termmosaic.ModCtrl})
	}
	s.SetFocus(0)
	s.SetFocused(true)
	before0, before1 := a.Bounds().W, b.Bounds().W

	if !ctrl(termmosaic.KeyRight) {
		t.Fatal("Ctrl+Right was not consumed")
	}
	if got := a.Bounds().W; got != before0+1 {
		t.Errorf("pane 0 width %d, want %d", got, before0+1)
	}
	if got := b.Bounds().W; got != before1-1 {
		t.Errorf("pane 1 width %d, want %d: the other pane must give up the cells", got, before1-1)
	}
	if got := a.Bounds().W + b.Bounds().W; got != 20 {
		t.Errorf("the panes total %d cells, want the axis's 20", got)
	}

	if !ctrl(termmosaic.KeyLeft) || a.Bounds().W != before0 {
		t.Errorf("after Ctrl+Left pane 0 width %d, want %d", a.Bounds().W, before0)
	}
}

// TestSplitResizeIsRefusedOnTheLastDivider: the last pane has nothing after it to
// take the cells from.
func TestSplitResizeIsRefusedOnTheLastDivider(t *testing.T) {
	s, _, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	rows(t, 20, 1, s)
	if s.Resize(1, 1) {
		t.Error("resizing the last pane's trailing edge reported a change")
	}
	if s.Resize(-1, 1) || s.Resize(5, 1) {
		t.Error("an out-of-range divider reported a change")
	}
}

// TestSplitResizeRefusesToSwallowAPane: a pane of zero cells cannot be focused or
// read, and a drag that reaches it leaves the user with no way back.
func TestSplitResizeRefusesToSwallowAPane(t *testing.T) {
	s, a, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 10, H: 1})
	rows(t, 10, 1, s)

	for i := 0; i < 40; i++ {
		s.Resize(0, -1)
	}
	if got := a.Bounds().W; got != minPane {
		t.Errorf("pane 0 shrank to %d cells, want the floor of %d", got, minPane)
	}
}

// TestSplitDragMovesTheDivider is the pointer mechanism, end to end: press on the
// divider, drag, release.
func TestSplitDragMovesTheDivider(t *testing.T) {
	s, a, b := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	rows(t, 20, 1, s)

	mouse := func(action termmosaic.MouseAction, x int) bool {
		return s.Handle(termmosaic.Event{Kind: termmosaic.EventMouse,
			Mouse: termmosaic.Mouse{X: x, Y: 0, Button: termmosaic.MouseLeft, Action: action}})
	}
	// The divider after pane 0 sits at x=10.
	if !mouse(termmosaic.MousePress, 10) {
		t.Fatal("a press on the divider was not consumed")
	}
	if got := s.dragging; got != 0 {
		t.Fatalf("dragging divider %d, want 0", got)
	}
	if !mouse(termmosaic.MouseDrag, 14) {
		t.Fatal("a drag was not consumed")
	}
	if got, want := a.Bounds().W, 14; got != want {
		t.Errorf("pane 0 width %d, want %d", got, want)
	}
	if got, want := b.Bounds().W, 6; got != want {
		t.Errorf("pane 1 width %d, want %d", got, want)
	}
	if !mouse(termmosaic.MouseRelease, 14) {
		t.Fatal("a release was not consumed")
	}
	if s.dragging != -1 {
		t.Error("the drag was not ended by the release")
	}
	// A drag after the release does nothing.
	if mouse(termmosaic.MouseDrag, 2) {
		t.Error("a drag with no press in progress was consumed")
	}
}

// TestSplitDragFollowsThePointerAbsolutely: the divider goes where the pointer is,
// so a fast drag cannot accumulate error.
func TestSplitDragFollowsThePointerAbsolutely(t *testing.T) {
	s, a, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	rows(t, 20, 1, s)

	for _, x := range []int{3, 17, 8} {
		s.Handle(termmosaic.Event{Kind: termmosaic.EventMouse,
			Mouse: termmosaic.Mouse{X: 10, Y: 0, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress}})
		s.Handle(termmosaic.Event{Kind: termmosaic.EventMouse,
			Mouse: termmosaic.Mouse{X: x, Y: 0, Button: termmosaic.MouseLeft, Action: termmosaic.MouseDrag}})
		if got, want := a.Bounds().W, x; got != want {
			t.Errorf("dragging to %d put the divider at %d, want %d", x, got, want)
		}
	}
}

// TestSplitDragEndsAnywhereOutsideTheWidget: a release over the terminal, not over
// the divider, has to end the drag or the user is stuck in an invisible mode.
func TestSplitDragEndsAnywhereOutsideTheWidget(t *testing.T) {
	s, _, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	rows(t, 20, 1, s)

	s.Handle(termmosaic.Event{Kind: termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: 10, Y: 0, Button: termmosaic.MouseLeft, Action: termmosaic.MousePress}})
	if !s.Handle(termmosaic.Event{Kind: termmosaic.EventMouse,
		Mouse: termmosaic.Mouse{X: 99, Y: 99, Button: termmosaic.MouseLeft, Action: termmosaic.MouseRelease}}) {
		t.Error("a release outside the widget did not end the drag")
	}
	if s.dragging != -1 {
		t.Error("the drag survived a release outside the widget")
	}
}

// TestSplitDividerHitAreaWithAndWithoutSpacing pins where a divider is grabbable,
// including the awkward case: with no spacing there is no gap cell, so the divider is
// the single column where one pane ends and the next begins.
func TestSplitDividerHitAreaWithAndWithoutSpacing(t *testing.T) {
	s, _, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	rows(t, 20, 1, s)

	// No spacing: column 10 is both pane 1's first column and the divider.
	if got, ok := s.dividerAt(10, 0); !ok || got != 0 {
		t.Errorf("dividerAt(10,0) = %d,%v, want 0,true", got, ok)
	}
	if got, ok := s.dividerAt(9, 0); ok {
		t.Errorf("dividerAt(9,0) = %d,true: the middle of a pane is not a divider", got)
	}

	s.SetSpacing(2)
	rows(t, 20, 1, s)
	// Two cells of gap: both are grabbable, and the pane starts after them.
	for _, x := range []int{9, 10} {
		if got, ok := s.dividerAt(x, 0); !ok || got != 0 {
			t.Errorf("dividerAt(%d,0) = %d,%v, want 0,true", x, got, ok)
		}
	}
	if got, ok := s.dividerAt(11, 0); ok {
		t.Errorf("dividerAt(11,0) = %d,true: that is inside the pane after the gap", got)
	}
}

// TestSplitDraggedProportionsSurviveAResize is the reason a manual resize switches
// to percentages rather than editing fixed sizes: the user's chosen proportions must
// still hold when the terminal is resized, and layout must still be the thing that
// works them out.
func TestSplitDraggedProportionsSurviveAResize(t *testing.T) {
	s, a, b := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	rows(t, 20, 1, s)
	s.Resize(0, 5) // 15 / 5

	if got, want := a.Bounds().W, 15; got != want {
		t.Fatalf("before resize: pane 0 width %d, want %d", got, want)
	}

	s.SetBounds(buffer.Rect{W: 40, H: 1})
	s.Draw(buffer.NewBuffer(40, 1))
	if got, want := a.Bounds().W, 30; got != want {
		t.Errorf("after doubling the width: pane 0 width %d, want %d", got, want)
	}
	if got, want := b.Bounds().W, 10; got != want {
		t.Errorf("after doubling the width: pane 1 width %d, want %d", got, want)
	}
	if got := a.Bounds().W + b.Bounds().W; got != 40 {
		t.Errorf("the panes total %d cells, want 40: the rounding remainder must not open a gap", got)
	}
}

// TestSplitResizeIgnoresTheWrongAxis: Ctrl+Left on a vertical Split is a focus move,
// not a resize, because there is no horizontal divider to move.
func TestSplitResizeIgnoresTheWrongAxis(t *testing.T) {
	s, a, _ := twoPanes(layout.Vertical, buffer.Rect{W: 6, H: 10})
	rows(t, 6, 10, s)
	before := a.Bounds().H

	s.SetFocused(true)
	s.Handle(termmosaic.Event{Kind: termmosaic.EventKey, Key: termmosaic.KeyLeft, Mod: termmosaic.ModCtrl})
	if got := a.Bounds().H; got != before {
		t.Errorf("Ctrl+Left changed a vertical Split's pane heights: %d, want %d", got, before)
	}
}

// TestSplitConstraintsReportsTheProportionalForm: a caller asking what the Split is
// using gets the truth, not what it passed in.
func TestSplitConstraintsReportsTheProportionalForm(t *testing.T) {
	s, _, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	s.SetConstraints([]layout.Constraint{layout.Length(6), layout.Fill(1)})
	rows(t, 20, 1, s)
	if got := s.Constraints()[0].String(); got != "Length(6)" {
		t.Fatalf("before a resize the first constraint is %s, want Length(6)", got)
	}

	// Length(6) with a Fill in 20 cells resolves to 6 and 14, so widening the first
	// pane by 4 makes the two equal — which is what the Fill weights then encode.
	s.Resize(0, 4)
	cs := s.Constraints()
	if got := cs[0].String(); got != "Fill(10)" {
		t.Errorf("after a resize the first constraint is %s, want Fill(10)", got)
	}
	if got := cs[1].String(); got != "Fill(10)" {
		t.Errorf("the trailing constraint is %s, want Fill(10)", got)
	}
	// The copy must not alias the Split's own slice.
	cs[0] = layout.Fill(9)
	if got := s.Constraints()[0].String(); got == "Fill(9)" {
		t.Error("Constraints returned the Split's own slice")
	}
}

// ---------------------------------------------------------------------------
// cost
// ---------------------------------------------------------------------------

// TestSplitDrawIsAllocationFreeInSteadyState: layout.Solve builds a fresh slice on
// every call, so the result is cached against the rect and the constraint
// generation. This is what fails if that cache is removed.
func TestSplitDrawIsAllocationFreeInSteadyState(t *testing.T) {
	panes := make([]termmosaic.Widget, 4)
	for i := range panes {
		panes[i] = label(new(basic.Text), "some text in a pane")
	}
	s := New(layout.Horizontal, panes...)
	s.SetSpacing(1)
	s.SetBounds(buffer.Rect{W: 80, H: 24})
	buf := buffer.NewBuffer(80, 24)
	s.Draw(buf) // solve once

	if got := testing.AllocsPerRun(200, func() { s.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %.1f objects per run, want 0. The layout solve is cached per rect "+
			"(ADR 0007 §3)", got)
	}
}

// TestSplitPanesReportsNoAllocations is the same claim stated where the panes are
// involved, so a pane that allocated would be caught here rather than in an
// application's frame budget.
func TestSplitPanesReportsNoAllocations(t *testing.T) {
	s, a, _ := twoPanes(layout.Horizontal, buffer.Rect{W: 20, H: 1})
	buf := buffer.NewBuffer(20, 1)
	s.Draw(buf)
	_ = a

	if got := testing.AllocsPerRun(200, func() { s.Draw(buf) }); got != 0 {
		t.Errorf("Draw with two text panes allocated %.1f objects per run, want 0", got)
	}
}

// BenchmarkSplitDraw is the container's frame cost: eight panes, one screen.
func BenchmarkSplitDraw(b *testing.B) {
	panes := make([]termmosaic.Widget, 8)
	for i := range panes {
		panes[i] = label(new(basic.Text), "a row of text")
	}
	s := New(layout.Horizontal, panes...)
	s.SetSpacing(1)
	s.SetBounds(buffer.Rect{W: 200, H: 60})
	buf := buffer.NewBuffer(200, 60)
	s.Draw(buf)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Draw(buf)
	}
}

// rowOf renders the first n cells of row y of buf as a string with trailing spaces
// trimmed.
func rowOf(buf *buffer.Buffer, y, n int) string {
	row := buf.Row(y)
	if n > len(row) {
		n = len(row)
	}
	var sb strings.Builder
	for _, c := range row[:n] {
		sb.WriteRune(c.Rune())
	}
	return strings.TrimRight(sb.String(), " ")
}

// atLeast1 clamps a degenerate dimension.
func atLeast1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
