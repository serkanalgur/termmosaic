package basic_test

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
// It goes through widgettest rather than through a bare buffer because a bare
// buffer proves only that Draw did not panic. What a reader needs to see is the
// cell grid the renderer produced, which is what a terminal would have shown,
// so the text in every Output below is what the widget drew rather than a
// description of it.
//
// widgettest.Capture rather than widgettest.Render: an Example has no
// testing.TB to hand over, and Capture is the same
// widget -> buffer -> renderer -> diff -> encoder -> MemorySink path with the
// failure reported as an error instead of through t.Fatal.
func show(w, h int, root termmosaic.Widget) string {
	sink, err := widgettest.Capture(w, h, 1, root)
	if err != nil {
		panic(err)
	}
	return widgettest.Screen(sink)
}

// Example composes the package's two widgets the way they are usually used: a
// Text as the one-line heading, a Paragraph filling the space under it.
//
// The Split is not decoration. It is how the two rectangles are derived at all,
// and neither widget is told anything about the other.
func Example() {
	heading := basic.NewTextString(
		buffer.Rect{W: 40, H: 1},
		"Release policy",
		buffer.NewStyle(buffer.DefaultColour, buffer.DefaultColour, buffer.AttrBold),
	)
	body := basic.NewParagraphString(
		buffer.Rect{},
		"Every bump since v0.1.0 has been a minor, and no patch release exists "+
			"to have violated SemVer.",
		buffer.Style{},
	)

	// SetBounds is not optional. split.New takes a direction and panes but no
	// rectangle, so without it the solve has no space to divide and Draw returns
	// before drawing anything.
	s := split.New(layout.Vertical, heading, body)
	s.SetBounds(buffer.Rect{W: 40, H: 4})
	// Length for the heading and Fill for the body, rather than two Fills: the
	// heading is exactly one line by definition, and asking the solver to
	// divide two rows between it and a paragraph would give it half a row.
	s.SetConstraints([]layout.Constraint{layout.Length(1), layout.Fill(1)})

	fmt.Println(show(40, 4, s))
	// Output:
	// Release policy
	// Every bump since v0.1.0 has been a
	// minor, and no patch release exists to
	// have violated SemVer.
}

// ExampleText renders one line of styled spans, which is the shape a form uses
// for a label with an emphasised value beside it.
func ExampleText() {
	label := basic.NewText(buffer.Rect{W: 34, H: 1}, []buffer.Span{
		buffer.NewSpan("Name: ", buffer.Style{}),
		buffer.NewSpan("TermMosaic", buffer.NewStyle(buffer.DefaultColour, buffer.DefaultColour, buffer.AttrBold)),
	})
	fmt.Println(show(34, 1, label))
	// Output:
	// Name: TermMosaic
}

// ExampleText_alignment places the text within its own rectangle rather than at
// its left edge, which is what makes a right-hand column of values line up.
func ExampleText_alignment() {
	r := buffer.Rect{W: 30, H: 1}
	label := basic.NewTextString(r, "dependencies", buffer.Style{})
	label.SetAlign(geometry.AlignRight)
	fmt.Println(show(30, 1, label))
	// Output:
	//            dependencies
}

// ExampleText_truncation is the behaviour a narrow column depends on. A Text
// that does not fit is TRUNCATED WITH A MARKER rather than clipped mid-word, so
// a reader can tell there was more of it.
func ExampleText_truncation() {
	long := basic.NewTextString(buffer.Rect{W: 12, H: 1}, "downloading index", buffer.Style{})
	fmt.Println(show(12, 1, long))
	// Output:
	// downloading…
}

// ExampleParagraph wraps its content to the width it is given and shows the top
// of it. There is no scrolling here: a Paragraph that scrolled would be a
// second scroll model, and Pager is already that model.
func ExampleParagraph() {
	p := basic.NewParagraphString(
		buffer.Rect{W: 30, H: 3},
		"A paragraph wraps to the width it is given and shows the top of its content.",
		buffer.Style{},
	)
	fmt.Println(show(30, 3, p))
	// Output:
	// A paragraph wraps to the width
	// it is given and shows the top
	// of its content.
}

// ExampleParagraph_alignment moves the wrapped lines within the rectangle.
// Alignment never changes where a line breaks — Wrap produces left-aligned
// lines and alignment only places them afterwards.
func ExampleParagraph_alignment() {
	p := basic.NewParagraphString(
		buffer.Rect{W: 26, H: 2},
		"centred, but still wrapped to fit",
		buffer.Style{},
	)
	p.SetAlign(geometry.AlignCenter)
	fmt.Println(show(26, 2, p))
	// Output:
	// centred, but still wrapped
	//           to fit
}
