package split_test

import (
	"fmt"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/split"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// show renders root on a w-by-h screen and returns the rows with trailing
// spaces trimmed.
//
// The text in every Output below is the cell grid the renderer produced, not a
// description of it: widgettest walks the whole
// widget -> buffer -> renderer -> diff -> encoder -> MemorySink path, so what a
// reader sees here is what the widget's own tests assert on.
//
// widgettest.Capture rather than Render, because an Example has no testing.TB
// to hand over and Capture reports the same failure as an error.
func show(w, h int, root termmosaic.Widget) string {
	sink, err := widgettest.Capture(w, h, 1, root)
	if err != nil {
		panic(err)
	}
	return widgettest.Screen(sink)
}

// Example is the ordinary use: two panes sharing a width equally.
//
// SetBounds is not optional. split.New takes a direction and panes but no
// rectangle, so without it the solve has no space to divide, Draw returns
// early, and the frame is blank for a reason that has nothing to do with the
// panes. That is the one thing about this constructor worth stating out loud,
// because it is the first thing every caller gets wrong.
func Example() {
	left := basic.NewTextString(buffer.Rect{}, "logs", buffer.Style{})
	right := basic.NewTextString(buffer.Rect{}, "inspector", buffer.Style{})

	s := split.New(layout.Horizontal, left, right)
	s.SetBounds(buffer.Rect{W: 30, H: 1})
	// Fill(1) each is what New already chose, and is the common case: two panes
	// of equal interest. The gap between them is zero unless SetSpacing says
	// otherwise.
	fmt.Println(show(30, 1, s))
	// Output:
	// logs           inspector
}

// ExampleSplit_constraints replaces the equal share with the caller's own
// sizing rules, one per pane in pane order.
//
// Percentage(60) and not Ratio(3, 2): Ratio is THREE HALVES of the available
// space, not three fifths of it, so a Ratio here would hand the first pane the
// whole width and the second pane nothing. The two constraint kinds have almost
// identical names and opposite meanings, and this is the sharp edge ADR 0004
// recorded.
func ExampleSplit_constraints() {
	sidebar := basic.NewTextString(buffer.Rect{}, "sidebar", buffer.Style{})
	editor := basic.NewTextString(buffer.Rect{}, "editor", buffer.Style{})

	s := split.New(layout.Horizontal, sidebar, editor)
	s.SetBounds(buffer.Rect{W: 40, H: 3})
	s.SetConstraints([]layout.Constraint{layout.Percentage(25), layout.Fill(1)})
	s.SetSpacing(1)
	fmt.Println(show(40, 1, s))
	// Output:
	// sidebar   editor
}

// ExampleSplit_focus is the property a still frame cannot show, so it is stated
// through the accessors instead: the Split holds one focus index, the pane at
// that index is the key receiver, and only the focused Split moves it.
//
// Tab and the arrows move the index, and a click inside a pane moves focus to
// it. A program drives that through SetFocus and reads it back through Focus.
func ExampleSplit_focus() {
	a := basic.NewTextString(buffer.Rect{}, "left", buffer.Style{})
	b := basic.NewTextString(buffer.Rect{}, "right", buffer.Style{})

	s := split.New(layout.Horizontal, a, b)
	s.SetBounds(buffer.Rect{W: 20, H: 1})
	s.SetFocus(1)

	// Focus is an index into the panes and is 0 on an empty Split rather than an
	// error, because an empty Split is a legal state rather than a mistake.
	fmt.Printf("pane %d at x=%d\n", s.Focus(), s.PaneBounds(1).X)

	// Focus only moves while the Split itself has it. An unfocused Split takes
	// keys only from the pointer, which is why SetFocused comes first here and
	// why a form can hold two Splits in one focus ring.
	s.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0))
	fmt.Printf("unfocused, after tab: pane %d\n", s.Focus())

	s.SetFocused(true)
	s.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyTab, 0))
	// Clamped at the ends rather than wrapped: pane 1 was already the last, so
	// Tab leaves it there.
	fmt.Printf("focused, after tab: pane %d of %d\n", s.Focus(), s.PaneCount())
	s.Handle(termmosaic.SpecialKeyEvent(termmosaic.KeyBacktab, 0))
	fmt.Printf("after backtab:      pane %d of %d\n", s.Focus(), s.PaneCount())
	// Output:
	// pane 1 at x=10
	// unfocused, after tab: pane 1
	// focused, after tab: pane 1 of 2
	// after backtab:      pane 0 of 2
}

// ExampleSplit_vertical stacks panes down the rows rather than across the
// columns. Everything else is identical: the direction is the only difference,
// which is what makes a Split a layout primitive rather than a widget.
func ExampleSplit_vertical() {
	title := basic.NewTextString(buffer.Rect{}, "title", buffer.Style{})
	body := basic.NewTextString(buffer.Rect{}, "body", buffer.Style{})
	footer := basic.NewTextString(buffer.Rect{}, "footer", buffer.Style{})

	s := split.New(layout.Vertical, title, body, footer)
	s.SetBounds(buffer.Rect{W: 16, H: 3})
	// Fixed rows for the chrome and Fill for the body, which is the whole reason
	// to have a solver here: the middle pane takes whatever is left, so a resize
	// reflows the content without touching the header or the footer.
	s.SetConstraints([]layout.Constraint{
		layout.Length(1), layout.Fill(1), layout.Length(1),
	})
	// The middle pane took the two rows nothing else asked for, which is the
	// reflow: a taller screen gives the body more and the chrome exactly one row
	// each, with no arithmetic in the program. Three rows rather than five
	// because a taller body would leave two adjacent blank rows, and Go's example
	// comparison collapses a run of them.
	fmt.Printf("body pane at %v\n", s.PaneBounds(1))
	fmt.Println(show(16, 3, s))
	// Output:
	// body pane at {0 1 16 1}
	// title
	// body
	// footer
}

// ExampleSplit_resize is the manual half of the divider. Resize moves the
// divider after pane i by delta cells, clamped so neither pane falls below the
// minimum, and reports whether it moved at all — so a caller can tell a refused
// resize from an accepted one that happened to be a no-op.
//
// The matching half is a drag: pressing on the divider and moving it. Both paths
// end up in the same clamp, so a dragged divider cannot be dragged somewhere a
// programmatic resize would have been refused.
func ExampleSplit_resize() {
	s := split.New(layout.Horizontal,
		basic.NewTextString(buffer.Rect{}, "a", buffer.Style{}),
		basic.NewTextString(buffer.Rect{}, "b", buffer.Style{}),
	)
	s.SetBounds(buffer.Rect{W: 30, H: 1})

	fmt.Println(show(30, 1, s))
	fmt.Printf("widen pane 0 by 6: %v\n", s.Resize(0, 6))
	fmt.Println(show(30, 1, s))
	// Past the end of the screen the move is CLAMPED rather than refused, so
	// pane 1 keeps its minimum and the bool still reports that something moved.
	// A refusal is a resize that cannot move at all: nothing to give, or no
	// second pane to take the cells from.
	fmt.Printf("widen pane 0 by 999: %v\n", s.Resize(0, 999))
	fmt.Printf("there is no pane 2 to shrink: %v\n", s.Resize(2, 1))
	// Output:
	// a              b
	// widen pane 0 by 6: true
	// a                    b
	// widen pane 0 by 999: true
	// there is no pane 2 to shrink: false
}

// ExampleSplit_paneBounds reads the solve rather than repeating it. A container
// that needs to place something next to a pane asks the Split where the pane
// ended up instead of recomputing a divide it would have to keep in step.
//
// PaneAt is the inverse, and the pair is what makes the Split useful to a
// program routing a click: which pane is this cell in?
func ExampleSplit_paneBounds() {
	s := split.New(layout.Horizontal,
		basic.NewTextString(buffer.Rect{}, "one", buffer.Style{}),
		basic.NewTextString(buffer.Rect{}, "two", buffer.Style{}),
		basic.NewTextString(buffer.Rect{}, "three", buffer.Style{}),
	)
	s.SetBounds(buffer.Rect{W: 33, H: 3})
	s.SetSpacing(1)

	for i := 0; i < s.PaneCount(); i++ {
		fmt.Printf("pane %d at %v\n", i, s.PaneBounds(i))
	}
	// PaneAt answers which pane a cell belongs to, and -1 for a cell in the gap
	// between two of them — a click there belongs to the divider, not to a pane.
	fmt.Printf("cell (17, 1) is pane %d\n", s.PaneAt(17, 1))
	// x 11 is the one-cell gap SetSpacing left: a click there belongs to the
	// divider rather than to a pane, which is what makes a drag on it a resize
	// rather than a selection.
	fmt.Printf("cell (11, 1) is pane %d\n", s.PaneAt(11, 1))
	// Output:
	// pane 0 at {0 0 11 3}
	// pane 1 at {12 0 10 3}
	// pane 2 at {23 0 10 3}
	// cell (17, 1) is pane 1
	// cell (11, 1) is pane -1
}

// ExampleSplit_alignment is one of the two things a Split does that a plain
// divide does not: it aligns each pane's text. Alignment here is the pane's own
// text placement, so a centred pane reads as a centred column rather than as
// text that happens to be that wide.
func ExampleSplit_alignment() {
	mk := func(s string, a geometry.Align) *basic.Text {
		t := basic.NewTextString(buffer.Rect{}, s, buffer.Style{})
		t.SetAlign(a)
		return t
	}
	s := split.New(layout.Vertical,
		mk("left", geometry.AlignLeft),
		mk("centred", geometry.AlignCenter),
		mk("right", geometry.AlignRight),
	)
	s.SetBounds(buffer.Rect{W: 20, H: 3})
	s.SetConstraints([]layout.Constraint{
		layout.Length(1), layout.Length(1), layout.Length(1),
	})
	fmt.Println(show(20, 3, s))
	// Output:
	// left
	//       centred
	//                right
}
