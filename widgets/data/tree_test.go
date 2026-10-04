package data

import (
	"fmt"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// sampleTree returns a three-level hierarchy:
//
//	root
//	  a        (expanded)
//	    a1     (collapsed, has children)
//	    a2
//	  b        (collapsed, has children)
//	    b1
func sampleTree(t *testing.T, w, h int) *Tree {
	t.Helper()
	tr := NewTree(buffer.Rect{X: 0, Y: 0, W: w, H: h},
		Node{Label: "root", Expanded: true, Children: []Node{
			{Label: "a", Expanded: true, Children: []Node{
				{Label: "a1", Children: []Node{{Label: "a1x"}}},
				{Label: "a2"},
			}},
			{Label: "b", Children: []Node{{Label: "b1"}}},
		}},
	)
	tr.Block().SetBorder(buffer.BorderPlain)
	return tr
}

// wideTree returns a tree with n leaf children under one expanded root, for the
// flat-cost and end-of-collection assertions.
func wideTree(t *testing.T, w, h, n int) *Tree {
	t.Helper()
	kids := make([]Node, n)
	for i := range kids {
		kids[i] = Node{Label: fmt.Sprintf("n%d", i)}
	}
	tr := NewTree(buffer.Rect{X: 0, Y: 0, W: w, H: h}, Node{Label: "root", Expanded: true, Children: kids})
	tr.Block().SetBorder(buffer.BorderPlain)
	return tr
}

func TestTreeDrawsOnlyExpandedRows(t *testing.T) {
	// root + a + a1 + a2 are visible; b1 and a1x are not, because their parents
	// are collapsed. The count is the assertion: four rows, not six.
	tr := sampleTree(t, 20, 7)
	if tr.VisibleRows() != 5 {
		t.Errorf("VisibleRows = %d, want 5", tr.VisibleRows())
	}
	if tr.Nodes() != 7 {
		t.Errorf("Nodes = %d, want 7", tr.Nodes())
	}
	got := rows(t, 20, 7, tr)
	// The body starts after the two-cell marker gutter, and each level of depth
	// costs two cells before a one-cell expander and a space.
	want(t, got, 1, "› - root          ")
	want(t, got, 2, "    - a           ")
	want(t, got, 3, "      + a1        ")
	want(t, got, 4, "        a2        ")
	want(t, got, 5, "    + b           ")
}

func TestTreeExpansionGlyphIsNotAColour(t *testing.T) {
	// The glyph is the whole signal that a branch is closed, so the two states
	// must differ in a CELL and not merely in a style.
	tr := sampleTree(t, 20, 7)
	before := rows(t, 20, 7, tr)
	focus(tr)
	tr.SelectRow(2) // the "a1" row
	mustPress(t, tr, "\r")
	after := rows(t, 20, 7, tr)
	if tr.VisibleRows() != 6 {
		t.Errorf("after expanding a1: VisibleRows = %d, want 6", tr.VisibleRows())
	}
	// Row 3 is "a1" before the toggle and still "a1" after it: the SAME row, with
	// a different glyph. Comparing it against itself before the fix is what makes
	// the assertion non-vacuous rather than a comparison of two different rows.
	row := func(rs []string) string {
		if len(rs) <= 3 {
			return ""
		}
		return rs[3]
	}
	assertDifferent(t, "the expanded and collapsed glyph rows", row(before), row(after))
	if !strings.Contains(row(before), "+") || !strings.Contains(row(after), "-") {
		t.Errorf("the glyph did not change: before=%q after=%q", row(before), row(after))
	}
}

func TestTreeLeftCollapsesAndRightExpands(t *testing.T) {
	tr := sampleTree(t, 20, 8)
	focus(tr)
	tr.SelectRow(1) // "a", expanded with two children
	if tr.VisibleRows() != 5 {
		t.Fatalf("fixture: VisibleRows = %d, want 5", tr.VisibleRows())
	}
	mustPress(t, tr, "\x1b[D") // left collapses
	if tr.VisibleRows() != 3 {
		t.Errorf("after collapsing a: VisibleRows = %d, want 3 (root, a, b)", tr.VisibleRows())
	}
	if tr.Expanded(1) {
		t.Error("Expanded(1) reports the collapsed node as open")
	}
	mustPress(t, tr, "\x1b[C") // right expands again
	if tr.VisibleRows() != 5 {
		t.Errorf("after re-expanding a: VisibleRows = %d, want 5", tr.VisibleRows())
	}
	mustPress(t, tr, "\x1b[C") // right again steps into the first child
	if tr.SelectedNode() != 2 {
		t.Errorf("after stepping in: selected node %d, want 2", tr.SelectedNode())
	}
}

func TestTreeLeftFromALeafStepsOutToItsParent(t *testing.T) {
	tr := sampleTree(t, 20, 8)
	focus(tr)
	tr.SelectRow(3) // "a2", a leaf
	if tr.SelectedNode() != 4 {
		t.Fatalf("fixture: selected node %d, want 4", tr.SelectedNode())
	}
	mustPress(t, tr, "\x1b[D")
	if tr.SelectedNode() != 1 {
		t.Errorf("after stepping out: selected node %d, want 1 (its parent)", tr.SelectedNode())
	}
}

func TestTreeNavigationKeysMoveTheSelection(t *testing.T) {
	tr := sampleTree(t, 20, 8)
	focus(tr)
	mustPress(t, tr, "\x1b[B")
	if tr.SelectedNode() != 1 {
		t.Errorf("after down: node %d, want 1", tr.SelectedNode())
	}
	mustPress(t, tr, "\x1b[F") // end
	if tr.SelectedNode() != 5 {
		t.Errorf("after end: node %d, want 5 (the last VISIBLE row, not the last node)", tr.SelectedNode())
	}
	mustPress(t, tr, "\x1b[H") // home
	if tr.SelectedNode() != 0 {
		t.Errorf("after home: node %d, want 0", tr.SelectedNode())
	}
	if !tr.HasChildren(0) || tr.Depth(0) != 0 {
		t.Errorf("the root is not reported as a depth-0 branch: hasKids=%v depth=%d", tr.HasChildren(0), tr.Depth(0))
	}
}

func TestTreeClickSelectsAndTakesFocus(t *testing.T) {
	tr := sampleTree(t, 20, 7)
	if !clickAt(tr, 6, 3) {
		t.Fatal("a press inside the tree was not consumed")
	}
	if !tr.Focused() {
		t.Error("a press did not take focus")
	}
	if tr.SelectedNode() != 2 {
		t.Errorf("after clicking the third row: node %d, want 2", tr.SelectedNode())
	}
	if clickAt(tr, 90, 90) {
		t.Error("a press outside the tree was consumed")
	}
}

func TestTreeWheelScrollsWithoutExpanding(t *testing.T) {
	tr := wideTree(t, 20, 6, 100)
	focus(tr)
	tr.Draw(cellBuf(20, 6))
	if !wheelAt(tr, 5, 3, false) {
		t.Fatal("a wheel notch was not consumed")
	}
	if tr.Offset() != wheelLines {
		t.Errorf("after a notch: offset %d, want %d", tr.Offset(), wheelLines)
	}
	if tr.VisibleRows() != 101 {
		t.Errorf("scrolling changed the row count to %d", tr.VisibleRows())
	}
}

func TestTreeMarkerIsTheSelectionSignal(t *testing.T) {
	tr := sampleTree(t, 20, 7)
	first := rows(t, 20, 7, tr)
	tr.SelectRow(2)
	second := rows(t, 20, 7, tr)
	assertDifferent(t, "the selected row", first[1], first[3])
	if !strings.Contains(first[1], "›") {
		t.Errorf("the selected row carries no marker: %q", first[1])
	}
	if !strings.Contains(second[3], "›") {
		t.Errorf("the newly selected row carries no marker: %q", second[3])
	}
}

func TestTreeDegenerateSizesDoNotPanic(t *testing.T) {
	for _, size := range []buffer.Size{{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 2}, {W: 3, H: 1}, {W: 7, H: 3}, {W: 0, H: 9}} {
		tr := sampleTree(t, size.W, size.H)
		tr.Block().SetBorder(buffer.BorderPlain)
		focus(tr)
		buf := cellBuf(size.W, size.H)
		tr.Draw(buf)
		for _, seq := range []string{"\x1b[B", "\x1b[C", "\x1b[D", "\r", "\x1b[F"} {
			mustPress(t, tr, seq)
		}
		tr.Draw(cellBuf(size.W, size.H))
	}
}

func TestTreeDrawIsAllocationFree(t *testing.T) {
	tr := wideTree(t, 40, 20, 5000)
	buf := cellBuf(40, 20)
	drawAll(tr, buf, 3)
	if got := testing.AllocsPerRun(200, func() { tr.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per frame; the frame path must be free", got)
	}
}

func TestTreeScrollingDoesNotAllocate(t *testing.T) {
	tr := wideTree(t, 40, 20, 100000)
	focus(tr)
	buf := cellBuf(40, 20)
	drawAll(tr, buf, 3)
	if got := testing.AllocsPerRun(200, func() {
		tr.vm.ScrollBy(1)
		tr.Draw(buf)
	}); got != 0 {
		t.Errorf("scrolling then Draw allocated %v times", got)
	}
}

func TestTreeHundredThousandNodesIsFlat(t *testing.T) {
	cost := func(n int) float64 {
		tr := wideTree(t, 40, 20, n)
		buf := cellBuf(40, 20)
		drawAll(tr, buf, 3)
		return testing.AllocsPerRun(50, func() { tr.Draw(buf) })
	}
	if small, huge := cost(5), cost(100000); small != huge {
		t.Errorf("100k nodes cost %v allocs/frame and 5 nodes cost %v", huge, small)
	}
	// And the far end is reachable: End selects the last leaf of a 100k-child tree.
	tr := wideTree(t, 40, 20, 100000)
	focus(tr)
	tr.Draw(cellBuf(40, 20))
	mustPress(t, tr, "\x1b[F")
	if tr.SelectedNode() != 100000 {
		t.Errorf("after end on 100k children: node %d, want 100000", tr.SelectedNode())
	}
	if tr.Offset() <= 0 {
		t.Error("End did not scroll the tail into view")
	}
}

func TestTreeGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	tr := wideTree(t, 34, 5, 20)
	wide := rows(t, 34, 5, tr)
	if !strings.Contains(wide[1], "root") {
		t.Fatalf("the wide layout did not draw the root: %q", wide[1])
	}
	tr.SetBounds(buffer.Rect{X: 0, Y: 0, W: 14, H: 5})
	narrow := rows(t, 14, 5, tr)
	if !strings.Contains(narrow[1], "root") {
		t.Errorf("the root did not survive the shrink: %q", narrow[1])
	}
	for y := range narrow {
		if len([]rune(narrow[y])) > 14 {
			t.Errorf("row %d is wider than the screen after shrinking: %q", y, narrow[y])
		}
	}
}

func TestTreeDeepIndentIsBounded(t *testing.T) {
	// A label at depth 40 in a 12-cell window must still show a few cells of text:
	// the indent is bounded by the available width rather than overflowing it.
	node := Node{Label: "leaf"}
	for i := 0; i < 40; i++ {
		node = Node{Label: fmt.Sprintf("d%d", i), Expanded: true, Children: []Node{node}}
	}
	tr := NewTree(buffer.Rect{X: 0, Y: 0, W: 12, H: 5}, node)
	tr.ExpandAll()
	focus(tr)
	mustPress(t, tr, "\x1b[F")
	got := rows(t, 12, 5, tr)
	// The body is 7 cells wide, so the indent is capped at 3 and the label gets
	// the last two: enough to see that there is a label, which is the point of the
	// bound.
	if !strings.Contains(got[4], "l") {
		t.Errorf("the deepest label is not visible at all: %q", got[4])
	}
}

func TestTreeMinSizeIncludesChrome(t *testing.T) {
	tr := NewTree(buffer.Rect{W: 1, H: 1})
	tr.Block().SetBorder(buffer.BorderPlain)
	got := tr.MinSize()
	if got.W != minTreeW+2 || got.H != minTreeH+2 {
		t.Errorf("MinSize = %+v, want {%d,%d}", got, minTreeW+2, minTreeH+2)
	}
}

func TestTreeEmptyHierarchyIsLegal(t *testing.T) {
	tr := NewTree(buffer.Rect{X: 0, Y: 0, W: 10, H: 4})
	if tr.VisibleRows() != 0 || tr.SelectedNode() != -1 {
		t.Errorf("an empty tree reports %d rows and node %d", tr.VisibleRows(), tr.SelectedNode())
	}
	got := rows(t, 10, 4, tr)
	if strings.Contains(strings.Join(got, ""), "›") {
		t.Errorf("an empty tree drew a marker: %q", got)
	}
	// And the API is total on it.
	focus(tr)
	mustPress(t, tr, "\x1b[B")
	mustPress(t, tr, "\x1b[C")
	tr.Expand(0)
	tr.Collapse(0)
	tr.Toggle(0)
	tr.Draw(cellBuf(10, 4))
}
