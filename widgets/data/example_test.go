package data_test

import (
	"fmt"
	"strings"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/data"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// show renders root on a w-by-h screen and returns the rows with trailing
// spaces trimmed.
//
// Every Output comment in this package is the CELL GRID the renderer produced,
// read back out of a headless.MemorySink — not a description of it and not the
// escape sequences a real terminal would have received. widgettest walks the
// whole widget -> buffer -> renderer -> diff -> encoder -> MemorySink path, so
// what a reader sees here is what the widget's own tests assert on.
//
// widgettest.Capture rather than Render: an Example has no testing.TB to hand
// over, and Capture reports the same failure as an error rather than t.Fatal.
func show(w, h int, root termmosaic.Widget) string {
	sink, err := widgettest.Capture(w, h, 1, root)
	if err != nil {
		panic(err)
	}
	return widgettest.Screen(sink)
}

// chrome gives a collection widget the border, title and gutter every one of
// them shares, because a capture without it is a rectangle of floating text.
func chrome(b *block.Block, title string) *block.Block {
	b.SetBorder(buffer.BorderRounded)
	b.SetTitleString(title, buffer.Style{})
	b.SetTitleAlign(geometry.AlignLeft)
	return b
}

// press offers an event to a focused widget, which is how an example drives a
// widget with the same calls an event loop would make.
func press(w termmosaic.Widget, k termmosaic.Key, mod termmosaic.KeyMod) bool {
	return w.Handle(termmosaic.SpecialKeyEvent(k, mod))
}

// Example is the shape the collection widgets are almost always used in: a
// bordered list with a title, a scrollbar and a selected row.
//
// The chrome comes from Block(), which every collection widget exposes, so a
// program configures one border rather than drawing one per widget. The selection
// marker is a separate GUTTER column: a marker inside the label would shift every
// row's text one cell when the selection moved.
func Example() {
	list := data.NewList(buffer.Rect{W: 28, H: 8},
		data.ListItem{Label: "buffer"},
		data.ListItem{Label: "geometry"},
		data.ListItem{Label: "headless"},
		data.ListItem{Label: "layout"},
		data.ListItem{Label: "render"},
		data.ListItem{Label: "widgets"},
	)
	list.Scrollbar = true
	list.Select(2)
	chrome(list.Block(), "packages")
	list.SetFocused(true)

	fmt.Println(show(28, 8, list))
	// Output:
	// ╭ packages ────────────────╮
	// │  buffer                  │
	// │  geometry                │
	// │› headless                │
	// │  layout                  │
	// │  render                  │
	// │  widgets                 │
	// ╰──────────────────────────╯
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// ExampleList_selection drives the selection with keys rather than by calling
// Select, because the thing worth showing is that EVERY navigation key scrolls
// the selection into view — clamping the offset rather than re-centring it, which
// is virtual's ScrollIntoView and ADR 0007 §6 rule 2.
//
// KeyTab is NOT consumed, so a form can move focus out of a list with the
// keyboard exactly as it moves out of a text input.
func ExampleList_selection() {
	items := make([]data.ListItem, 0, 7)
	for _, label := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		items = append(items, data.ListItem{Label: label})
	}
	list := data.NewList(buffer.Rect{W: 20, H: 5})
	list.SetItems(items)
	list.Scrollbar = true
	list.SetFocused(true)

	press(list, termmosaic.KeyDown, 0)
	press(list, termmosaic.KeyEnd, 0)
	fmt.Printf("selected %d, offset %d of %d items\n",
		list.Selected(), list.Offset(), list.Items())
	fmt.Println(show(20, 5, list))
	// Output:
	// selected 6, offset 2 of 7 items
	//   three
	//   four
	//   five             █
	//   six              █
	// › seven            █
}

// ExampleList_styledItems is the escape hatch in the item type. A plain string
// is the common case and Spans is what lets ONE row carry several styles without
// giving up the convenient literal for every other row.
//
// SetItems is the only way to change the content, and it is where the row styles
// are normalised — which is why an item's Style cannot be set after the fact.
func ExampleList_styledItems() {
	bold := buffer.NewStyle(buffer.DefaultColour, buffer.DefaultColour, buffer.AttrBold)
	list := data.NewList(buffer.Rect{W: 26, H: 4}, data.ListItem{
		Label: "a plain row",
	}, data.ListItem{
		Spans: []buffer.Span{
			buffer.NewSpan("mixed: ", buffer.Style{}),
			buffer.NewSpan("bold", bold),
			buffer.NewSpan(" and plain", buffer.Style{}),
		},
	}, data.ListItem{
		Label: "another plain row",
	})
	fmt.Println(show(26, 4, list))
	// Output:
	// › a plain row
	//   mixed: bold and plain
	//   another plain row
}

// ---------------------------------------------------------------------------
// Table
// ---------------------------------------------------------------------------

// ExampleTable is a table with a header and a right-aligned numeric column.
//
// AlignRight is worth saying out loud because the default is AlignLeft, and it
// is the difference between a column of numbers that reads as numbers and one
// that reads as text.
func ExampleTable() {
	t := data.NewTable(buffer.Rect{W: 34, H: 6},
		data.Column{Title: title("module"), Grow: 1},
		data.Column{Title: title("files"), Width: 7, Align: geometry.AlignRight},
		data.Column{Title: title("lines"), Width: 8, Align: geometry.AlignRight},
	)
	t.Header = true
	t.Scrollbar = true
	t.SetRows(rows(
		"buffer", "14", "3,180",
		"render", "9", "1,940",
		"widgets", "24", "9,600",
	))
	chrome(t.Block(), "modules")
	t.Select(1)
	t.SetFocused(true)

	fmt.Println(show(34, 6, t))
	// Output:
	// ╭ modules ───────────────────────╮
	// │  module         files    lines │
	// │  buffer            14    3,180 │
	// │› render             9    1,940 │
	// │  widgets           24    9,600 │
	// ╰────────────────────────────────╯
}

// ExampleTable_columnWidths is the part of a table that surprises people, so it
// is worth a widget of its own. Three modes are resolved per rect, in order:
// a positive Width wins, then a positive Grow, and otherwise the column is
// MEASURED FROM THE VISIBLE ROWS ONLY.
//
// Measuring from the visible rows is not an approximation but the only affordable
// answer: a 100,000-row table that measured its content would be O(item count)
// on every resize, which ADR 0007 §6 rules out. The consequence is stated
// plainly — a content-derived column can change width as different content
// scrolls through — so a caller who needs stable widths gives explicit Width or
// Grow values.
func ExampleTable_columnWidths() {
	// The first and last columns are pinned. The middle one differs between the
	// two tables below, and the difference is the whole point: Width, then Grow,
	// then measurement of the visible rows.
	cols := func() []data.Column {
		return []data.Column{
			{Title: title("name"), Width: 9},
			{Title: title("middle"), Grow: 1},
			{Title: title("n"), Width: 4, Align: geometry.AlignRight},
		}
	}

	// Three rows rather than four, so the frame has no trailing blank row: Go's
	// example comparison collapses a run of blank lines, and a frame padded to
	// two empties cannot be written down exactly.
	pinned := data.NewTable(buffer.Rect{W: 30, H: 3}, cols()...)
	pinned.Header = true
	pinned.SetRows([]data.Row{
		{Cells: []data.Cell{
			{Text: "pinned"}, {Text: "Grow takes the rest"}, {Text: "42"},
		}},
	})

	measured := data.NewTable(buffer.Rect{W: 40, H: 4},
		data.Column{Title: title("name"), Width: 9},
		// No Width and no Grow: measured from the visible rows, and only from
		// them. A 100,000-row table that measured its whole collection would be
		// O(item count) on every resize, which ADR 0007 §6 rules out.
		data.Column{Title: title("middle")},
		data.Column{Title: title("n"), Width: 4, Align: geometry.AlignRight},
	)
	measured.Header = true
	measured.SetRows([]data.Row{
		{Cells: []data.Cell{
			{Text: "measured"}, {Text: "as wide as this cell"}, {Text: "7"},
		}},
		{Cells: []data.Cell{
			{Text: "second"}, {Text: "short"}, {Text: "8"},
		}},
	})

	fmt.Println(show(30, 3, pinned))
	fmt.Println(show(40, 4, measured))
	// Output:
	// name      middle          n
	// › pinned    Grow takes …   42
	//
	//
	//   name      middle                  n
	// › measured  as wide as this cell    7
	//   second    short                   8
}

// ExampleTable_shortRows records a contract rather than a feature: a row with
// fewer cells than there are columns is legal and the missing columns draw empty,
// and so is a row with more, the surplus being unreachable rather than a panic.
// Data arrives from disk, and a malformed row must not take the frame down.
func ExampleTable_shortRows() {
	t := data.NewTable(buffer.Rect{W: 26, H: 5},
		data.Column{Title: title("a"), Width: 4},
		data.Column{Title: title("b"), Width: 4},
		data.Column{Title: title("c"), Width: 4},
	)
	t.Header = true
	t.SetRows([]data.Row{
		{Cells: []data.Cell{{Text: "1"}, {Text: "2"}, {Text: "3"}}},
		// Short: the third column draws empty.
		{Cells: []data.Cell{{Text: "4"}, {Text: "5"}}},
		// Long: the surplus is unreachable, not a panic.
		{Cells: []data.Cell{{Text: "6"}, {Text: "7"}, {Text: "8"}, {Text: "9"}}},
	})
	fmt.Printf("%d rows, %d columns\n", t.Rows(), t.Columns())
	fmt.Println(show(26, 5, t))
	// Output:
	// 3 rows, 3 columns
	//   a    b    c
	// › 1    2    3
	//   4    5
	//   6    7    8
}

// ExampleTable_cellStyles is the third way a cell can say something: a string, or
// styled spans, or a style on the cell itself, in that order of precedence.
//
// CellStyle on a Column is applied when the cells are NORMALISED, which is
// SetRows. Changing it afterwards has no effect until SetRows is called again,
// because re-styling per frame would mean rebuilding a span slice per cell per
// frame — ADR 0008 §4's allocation rule applied to a table.
func ExampleTable_cellStyles() {
	warn := buffer.NewStyle(buffer.DefaultColour, buffer.DefaultColour, buffer.AttrBold)
	t := data.NewTable(buffer.Rect{W: 28, H: 4},
		data.Column{Title: title("check"), Width: 8},
		data.Column{Title: title("status"), Grow: 1},
	)
	t.Header = true
	t.SetRows([]data.Row{
		{Cells: []data.Cell{{Text: "build"}, {Spans: []buffer.Span{
			buffer.NewSpan("passing", buffer.Style{}),
		}}}},
		{Cells: []data.Cell{{Text: "lint"}, {Text: "warnings", Style: warn}}},
	})
	fmt.Println(show(28, 4, t))
	// Output:
	// check    status
	// › build    passing
	//   lint     warnings
}

// ---------------------------------------------------------------------------
// Tree
// ---------------------------------------------------------------------------

// ExampleTree is a hierarchy written as a Go literal. A Node holds its children
// BY VALUE in a slice, which is what lets a tree be built without a constructor
// call per node and without a second type for "node or branch".
//
// SetNodes flattens the hierarchy once into parallel arrays, so a frame costs
// O(visible rows) whatever the node count — the benchmark in the package's own
// tests measures that rather than asserting it.
func ExampleTree() {
	// Eight rows for six visible ones, so the frame has no trailing blank.
	tree := data.NewTree(buffer.Rect{W: 30, H: 8}, data.Node{
		Label: "termmosaic", Expanded: true, Children: []data.Node{
			{Label: "buffer", Children: []data.Node{
				{Label: "buffer.go"},
				{Label: "style.go"},
			}},
			{Label: "render", Children: []data.Node{
				{Label: "render.go"},
			}},
			{Label: "widgets", Expanded: true, Children: []data.Node{
				{Label: "basic"},
				{Label: "data"},
			}},
		},
	})
	tree.Scrollbar = true
	chrome(tree.Block(), "modules")
	tree.SetFocused(true)

	fmt.Printf("%d nodes, %d visible rows\n", tree.Nodes(), tree.VisibleRows())
	fmt.Println(show(30, 8, tree))
	// Output:
	// 9 nodes, 6 visible rows
	// ╭ modules ───────────────────╮
	// │› - termmosaic              │
	// │    + buffer                │
	// │    + render                │
	// │    - widgets               │
	// │        basic               │
	// │        data                │
	// ╰────────────────────────────╯
}

// ExampleTree_expansion is the state a Tree spends its life in. Expanded is
// honoured once by SetNodes and then OWNED by the Tree, which is the only thing
// that can change it consistently — Expand, Collapse and Toggle keep the
// flattened arrays in step.
//
// Selection is a ROW, not a node, so it follows expansion: collapsing a node
// moves the selection if the rows under it went away. That is why SelectedNode
// and SelectedRow are separate accessors.
func ExampleTree_expansion() {
	tree := data.NewTree(buffer.Rect{W: 26, H: 6}, data.Node{
		Label: "root", Expanded: true, Children: []data.Node{
			{Label: "branch", Expanded: true, Children: []data.Node{
				{Label: "leaf a"},
				{Label: "leaf b"},
			}},
			{Label: "sibling"},
		},
	})
	tree.SetFocused(true)
	tree.SelectRow(3)
	// Selection is a ROW and SelectedNode maps it back to a node, which is the
	// pair an application needs: rows move when a branch is collapsed, nodes do
	// not.
	fmt.Printf("row %d is node %d\n", tree.SelectedRow(), tree.SelectedNode())

	tree.Toggle(1)
	fmt.Printf("after collapse: %d visible rows, node %d expanded=%v\n",
		tree.VisibleRows(), 1, tree.Expanded(1))
	fmt.Println(show(26, 6, tree))
	// Output:
	// row 3 is node 3
	// after collapse: 3 visible rows, node 1 expanded=false
	//   - root
	//     + branch
	// ›     sibling
}

// ---------------------------------------------------------------------------
// Pager
// ---------------------------------------------------------------------------

// ExamplePager is a long document shown through a small window. The pager wraps
// to its own width rather than scrolling horizontally, and it does not scroll one
// LINE at a time: a visual row index would need the wrapped height of every line
// before it, which is O(document) for a keystroke.
//
// Scrolling a screen is therefore O(one screen) — it walks BACKWARDS from the
// target until it has covered a screenful, which is why a match in the last line
// of a 10 MB document is found without having measured every line before it.
func ExamplePager() {
	pager := data.NewPager(buffer.Rect{W: 34, H: 6})
	// Lines short enough not to wrap, so a scroll moves whole lines and the
	// example is about the pager rather than about where the wrap happened to
	// fall. A line longer than the interior would wrap, and the wrapped remainder
	// is what makes a scroll count visual rows rather than lines.
	pager.SetText(strings.Repeat("TermMosaic draws the cell grid.\n", 8))
	chrome(pager.Block(), "notes")

	// ScrollToEnd is O(one screen) whatever the document length: it walks
	// BACKWARDS from the last line until it has covered a screenful, which is why
	// this pager has no cumulative row index and still reaches the end of a
	// 10 MB file instantly. Scrolling to an arbitrary line would need the wrapped
	// height of every line before it, and that is O(document) for a keystroke.
	pager.ScrollToEnd()
	fmt.Printf("%d lines, showing from line %d\n", pager.Lines(), pager.TopLine())
	fmt.Println(show(34, 6, pager))
	// Output:
	// 9 lines, showing from line 6
	// ╭ notes ─────────────────────────╮
	// │Ln 7/9                          │
	// │TermMosaic draws the cell grid. │
	// │TermMosaic draws the cell grid. │
	// │                                │
	// ╰────────────────────────────────╯
}

// ExamplePager_search is the pager's only query interface, and the QUERY ITSELF IS
// THE APPLICATION'S: SetQuery takes it, because a matcher is not a scrolling
// concern and a second widget doing highlighting would be a second model.
//
// SetQuery returns the pager to the top, an empty query clears the highlight, and
// Matches is O(document) and a CALLER's question rather than something Draw needs.
// That is why NextMatch exists as a separate call rather than as work done every
// frame.
func ExamplePager_search() {
	pager := data.NewPager(buffer.Rect{W: 34, H: 5})
	pager.SetText("alpha\nbeta\ngamma\nbeta again\ndelta\nbeta once more")
	pager.SetQuery("beta")
	fmt.Printf("%d lines, %d matches\n", pager.Lines(), pager.Matches())

	// NextMatch scans FORWARD LINE BY LINE, which is why the third match is found
	// without having measured the lines between. It reports whether it moved.
	fmt.Printf("next match moved: %v\n", pager.NextMatch())
	pager.NextMatch()
	fmt.Printf("at line %d: %q\n", pager.TopLine(), pager.Line(pager.TopLine()))
	fmt.Println(show(34, 5, pager))
	// Output:
	// 6 lines, 3 matches
	// next match moved: true
	// at line 3: "beta again"
	// Ln 4/6 >beta
	// beta again
	// delta
	// beta once more
}

// ExamplePager_status turns off the position readout, which is the one row a
// caller gives up when a pager is very short. It is a setter rather than a field
// because it changes the LAYOUT — adapt hands that row to the status instead of
// the body — and a field changed without Invalidate would leave a stale layout
// nothing ever repairs.
func ExamplePager_status() {
	pager := data.NewPager(buffer.Rect{W: 28, H: 4})
	pager.SetText(strings.Repeat("a line of notes\n", 8))

	fmt.Printf("with status: top line %d\n", pager.TopLine())
	fmt.Println(show(28, 4, pager))

	pager.SetStatus(false)
	fmt.Printf("without:      top line %d\n", pager.TopLine())
	fmt.Println(show(28, 4, pager))
	// Output:
	// with status: top line 0
	// Ln 1/9
	// a line of notes
	// a line of notes
	// a line of notes
	// without:      top line 0
	// a line of notes
	// a line of notes
	// a line of notes
	// a line of notes
}

// ---------------------------------------------------------------------------
// helpers for the examples above
// ---------------------------------------------------------------------------

// title builds a header cell. The Table styles the header itself, so the caller
// writes the text and nothing else — which is why a Column's Title is spans and
// not a Style-bearing field of its own.
func title(s string) []buffer.Span { return []buffer.Span{buffer.NewSpan(s, buffer.Style{})} }

// rows builds a Table's body from one string per column, which is the shape the
// examples below want and which keeps them from drowning in data.Cell literals.
func rows(cells ...string) []data.Row {
	r := make([]data.Row, 0, len(cells)/3)
	for i := 0; i+2 < len(cells); i += 3 {
		r = append(r, data.Row{Cells: []data.Cell{
			{Text: cells[i]}, {Text: cells[i+1]}, {Text: cells[i+2]},
		}})
	}
	return r
}
