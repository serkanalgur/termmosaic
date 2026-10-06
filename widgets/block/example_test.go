package block_test

import (
	"fmt"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/split"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// show renders root on a w-by-h screen and returns the rows with trailing
// spaces trimmed.
//
// Every example in the catalog prints through this helper. The point is that the
// text in an Output comment is the CELL GRID the renderer produced, not a
// description of it and not the escape sequences a real terminal would have
// received — widgettest walks the whole
// widget -> buffer -> renderer -> diff -> encoder -> MemorySink path, so a
// capture here cannot disagree with what the widget's own tests assert on.
//
// widgettest.Capture rather than Render, because an Example has no testing.TB
// to hand and Capture reports the same failure as an error.
func show(w, h int, root termmosaic.Widget) string {
	sink, err := widgettest.Capture(w, h, 1, root)
	if err != nil {
		panic(err)
	}
	return widgettest.Screen(sink)
}

// Example is the shape a Block is almost always used in: a border, a title
// aligned inside the top edge, and a padding inset.
//
// A Block draws chrome and NOTHING else — it has no children and no idea what
// belongs inside it. Interior is the accessor that makes that composition
// possible: it returns the rectangle left after the border and the padding, and
// it is where the content widget goes.
func Example() {
	panel := block.New(buffer.Rect{W: 30, H: 5})
	panel.SetBorder(buffer.BorderRounded)
	panel.SetTitleString("Package layout", buffer.Style{})
	panel.SetTitleAlign(geometry.AlignLeft)
	panel.SetPadding(1)

	// Interior rather than a hand-computed rect: it is derived from Bounds and
	// the current configuration, so changing the padding above changed this too.
	// Three cells of arithmetic is not worth repeating at every call site.
	//
	// A Block has no children, so this is the whole contract: the program that
	// wants content inside one computes where that content goes. Nothing here
	// hides that the caller does the composing — a Block that drew its own
	// children would be a second layout system, and ADR 0007 rejected that.
	body := basic.NewTextString(panel.Interior(), "24 widgets", buffer.Style{})
	fmt.Printf("interior: %v\n", body.Bounds())

	fmt.Println(show(30, 5, panel))
	// Output:
	// interior: {2 2 26 1}
	// ╭ Package layout ────────────╮
	// │                            │
	// │                            │
	// │                            │
	// ╰────────────────────────────╯
}

// ExampleBlock_borders shows the four border rungs the catalog shares, which is
// the vocabulary every widget's chrome is drawn from.
func ExampleBlock_borders() {
	// One frame rather than four captures, because a reader comparing border
	// rungs needs them adjacent: the point of showing all four is that they are
	// interchangeable and differ only in glyphs.
	var panes []termmosaic.Widget
	for _, b := range []buffer.BorderStyle{
		buffer.BorderPlain, buffer.BorderRounded, buffer.BorderDouble, buffer.BorderThick,
	} {
		w := block.New(buffer.Rect{W: 22, H: 3})
		w.SetBorder(b)
		w.SetTitleString(b.String(), buffer.Style{})
		w.SetTitleAlign(geometry.AlignLeft)
		panes = append(panes, w)
	}
	s := split.New(layout.Vertical, panes...)
	s.SetBounds(buffer.Rect{W: 22, H: 12})
	fmt.Println(show(22, 12, s))
	// Output:
	// ┌ plain ─────────────┐
	// │                    │
	// └────────────────────┘
	// ╭ rounded ───────────╮
	// │                    │
	// ╰────────────────────╯
	// ╔ double ════════════╗
	// ║                    ║
	// ╚════════════════════╝
	// ┏ thick ━━━━━━━━━━━━━┓
	// ┃                    ┃
	// ┗━━━━━━━━━━━━━━━━━━━━┛
}

// ExampleBlock_titleAlignment is the one piece of Block configuration a caller
// reaches for constantly: a title is placed within the span between the two top
// corners, and all three alignments are ordinary.
func ExampleBlock_titleAlignment() {
	// The three are shown in one frame rather than three, because a reader
	// comparing an alignment needs the alignments side by side, and because a Split
	// is how a program stacks widgets anyway.
	var panes []termmosaic.Widget
	for _, a := range []geometry.Align{geometry.AlignLeft, geometry.AlignCenter, geometry.AlignRight} {
		b := block.New(buffer.Rect{W: 26, H: 3})
		b.SetBorder(buffer.BorderPlain)
		b.SetTitleString(a.String(), buffer.Style{})
		b.SetTitleAlign(a)
		panes = append(panes, b)
	}
	s := split.New(layout.Vertical, panes...)
	s.SetBounds(buffer.Rect{W: 26, H: 9})
	fmt.Println(show(26, 9, s))
	// Output:
	// ┌ left ──────────────────┐
	// │                        │
	// └────────────────────────┘
	// ┌──────── center ────────┐
	// │                        │
	// └────────────────────────┘
	// ┌───────────────── right ┐
	// │                        │
	// └────────────────────────┘
}

// ExampleBlock_interior is what a container asks a Block. The result is clipped
// rather than blank: a Block too small for its own border and padding yields an
// empty rect, and an empty rect is a legitimate answer rather than a panic.
func ExampleBlock_interior() {
	b := block.New(buffer.Rect{W: 20, H: 6})
	b.SetBorder(buffer.BorderRounded)
	b.SetPadding(1)
	fmt.Printf("bordered: %v\n", b.Interior())

	// BorderNone consumes nothing, so the interior is the whole rect — which is
	// why a Block with no border is a plain background rather than a frame.
	plain := block.New(buffer.Rect{W: 20, H: 6})
	plain.SetBorder(buffer.BorderNone)
	fmt.Printf("bare:     %v\n", plain.Interior())

	// Too small to hold its own chrome. Clipped, not blank, and not a panic.
	tiny := block.New(buffer.Rect{W: 2, H: 2})
	tiny.SetBorder(buffer.BorderRounded)
	fmt.Printf("too small: %v\n", tiny.Interior())

	// Output:
	// bordered: {2 2 16 2}
	// bare:     {0 0 20 6}
	// too small: {0 0 0 0}
}

// ExampleBlock_minSize is the size a Block asks for when it is inside something
// that needs to reserve room. It is derived from the same constants Draw uses,
// so it cannot drift from what gets painted.
func ExampleBlock_minSize() {
	bare := block.New(buffer.Rect{})
	fmt.Printf("bare:  %dx%d\n", bare.MinSize().W, bare.MinSize().H)

	framed := block.New(buffer.Rect{})
	framed.SetBorder(buffer.BorderRounded)
	framed.SetTitleString("t", buffer.Style{})
	framed.SetPadding(1)
	fmt.Printf("framed: %dx%d\n", framed.MinSize().W, framed.MinSize().H)
	// Output:
	// bare:  1x1
	// framed: 5x5
}
