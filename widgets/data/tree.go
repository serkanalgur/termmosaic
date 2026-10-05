package data

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/virtual"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds and constants for Tree, per ADR 0007 §1 rule 5.
const (
	// indentW is how many cells one level of depth costs. Two reads as a level
	// without the wide gap a tab would leave at depth 8.
	indentW = 2
	// expanderW is the expand/collapse glyph's column: one cell, plus the one
	// space separating it from the label so a leaf's blank and its parent's '-'
	// do not run into the text.
	expanderW = 2
	// minTreeW is the narrowest interior showing a marker, an expander, a couple
	// of indent cells and a label.
	minTreeW = 12
	// minTreeH is the smallest tree worth showing.
	minTreeH = 2
)

// Expansion glyphs. They are ASCII on purpose: the expansion state of a node has
// to be readable on a terminal with no Unicode, and '+'/'-' are the two glyphs
// that are unambiguous at one cell wide. A caller who prefers arrows assigns
// CollapsedRune and ExpandedRune.
const (
	collapsedRuneDefault = '+'
	expandedRuneDefault  = '-'
	leafRuneDefault      = ' '
)

// Node is one tree node.
//
// A Node holds its Children BY VALUE in a slice, which is what lets a tree be
// built as a literal without a constructor call per node and without a second
// type for "node or branch".
type Node struct {
	// Label is the node's text, used when Spans is empty.
	Label string
	// Spans overrides Label when it has any cell width.
	Spans []buffer.Span
	// Style is the node's rendition. An unset value falls back to ItemStyle.
	Style buffer.Style
	// Children are the node's children. An empty slice is a leaf.
	Children []Node
	// Expanded is the initial expansion state. It is honoured by SetNodes and
	// then owned by the Tree, which is the only thing that can change it
	// consistently.
	Expanded bool
}

// Tree shows a hierarchy with expandable nodes, keyboard navigation and lazy
// rendering of the visible rows.
//
// It is Focusable on the same terms as List and Table.
//
// # How laziness is achieved, and what it costs
//
// SetNodes flattens the hierarchy ONCE into parallel arrays — depth, subtree
// end, parent, expansion — and keeps a third list, visible, of the node indices
// currently on screen as rows. A collapsed node's entire subtree is skipped in
// one step rather than walked, so building the visible list is O(visible rows),
// not O(nodes), and drawing a frame is O(visible rows) with no walking at all:
// row k is visible[k], an array read.
//
// The visible list is rebuilt whenever expansion changes — SetNodes, Toggle,
// ExpandAll, CollapseAll — which is a key press rather than a frame. Its cost is
// O(visible rows), so expanding a node inside a 100,000-node tree is as cheap as
// the tree is tall, and is the minimum any correct implementation can be: you
// cannot know what is on screen without counting it.
//
// Per-frame cost does not depend on the node count at all, which
// TestTreeHundredThousandNodesIsFlat measures rather than asserts.
//
// # Key contract
//
// Consumed only while focused. Selection is a ROW, so it follows expansion: with
// a node collapsed, the rows below it are simply gone.
//
//	up / down       move the selection by one visible row
//	page up/down    move it by a screen, less one row of overlap
//	home / end      first / last visible row
//	right           expand the selected node, or step into it if it is open
//	left            collapse the selected node, or step out to its parent
//	enter, space    toggle the selected node
//	wheel up/down   scroll WITHOUT moving the selection
//	press           select the pressed row and take focus
//
// KeyTab is NOT consumed, as in List and Table.
type Tree struct {
	blk    *block.Block
	bounds buffer.Rect

	// The flattened hierarchy, all indexed by node index.
	labels  [][]buffer.Span
	styles  []buffer.Style
	depths  []int32
	ends    []int32
	parents []int32
	hasKids []bool
	open    []bool
	// visible is the node index of each visible row, in display order.
	visible []int32

	// vm scrolls over visible rows, so the engine's count is the number of rows
	// the user can reach rather than the number of nodes.
	vm *virtual.Model

	selected int
	focused  bool

	// Marker is the selection gutter glyph, as in List and Table.
	Marker      string
	MarkerStyle buffer.Style

	// CollapsedRune and ExpandedRune are the expansion glyphs, and LeafRune what a
	// node with no children shows in their place. An unset rune field is replaced
	// by the default at draw time, so a caller can set only one of them.
	CollapsedRune rune
	ExpandedRune  rune
	LeafRune      rune

	// ItemStyle is an unselected row's background and SelectedStyle the selected
	// row's. The gutter marker is the colour-independent signal.
	ItemStyle     buffer.Style
	SelectedStyle buffer.Style

	// Scrollbar draws the vertical thumb; ScrollbarStyle is its rendition.
	Scrollbar      bool
	ScrollbarStyle buffer.Style

	// OnActivate, when set, is called with the selected NODE index — not the row —
	// so a caller can identify a node without mapping rows back itself.
	OnActivate func(node int)

	regions []geometry.Region
	keep    []bool

	markerW, barW int
	cachedRect    buffer.Rect
	body          buffer.Rect
	gutter        buffer.Rect
	mark          rune
	markerRune    rune
	thumbRune     rune

	// stack is the explicit depth-first stack SetNodes uses, kept so a rebuild
	// does not allocate on a repeated expansion change.
	stack []treeFrame
}

// treeFrame is one entry of the depth-first stack: the children still to visit,
// the index within them, and the identity the visited nodes record.
type treeFrame struct {
	kids   []Node
	i      int
	parent int32
	depth  int32
	first  int32
}

// NewTree returns a Tree sized r holding nodes.
func NewTree(r buffer.Rect, nodes ...Node) *Tree {
	t := &Tree{
		blk:      block.New(r),
		bounds:   r,
		vm:       virtual.New(0),
		selected: 0,
		Marker:   defaultMarker,
		regions: []geometry.Region{
			{Size: buffer.StringWidth(defaultMarker) + markerPad, Prio: geometry.PrioHigh},
			{Size: scrollbarW, Prio: geometry.PrioLow},
		},
	}
	t.blk.SetBounds(r)
	t.SetNodes(nodes)
	return t
}

// SetNodes replaces the hierarchy and rebuilds the flattened form.
//
// It is the only way to change the tree, which is where the O(nodes) work lives:
// a caller that mutates its own slice afterwards is changing the tree behind the
// widget's back, and nothing in this package copies it defensively.
func (t *Tree) SetNodes(nodes []Node) {
	t.labels = t.labels[:0]
	t.styles = t.styles[:0]
	t.depths = t.depths[:0]
	t.ends = t.ends[:0]
	t.parents = t.parents[:0]
	t.hasKids = t.hasKids[:0]
	t.open = t.open[:0]

	t.stack = append(t.stack[:0], treeFrame{kids: nodes, parent: -1, first: 0})
	for len(t.stack) > 0 {
		top := &t.stack[len(t.stack)-1]
		if top.i >= len(top.kids) {
			// Every child of this frame has been emitted, so its subtree ends
			// where the flattened arrays now end. The guard covers the root frame
			// of an empty tree, whose first index has no entry to write.
			if int(top.first) < len(t.ends) {
				t.ends[top.first] = int32(len(t.labels))
			}
			t.stack = t.stack[:len(t.stack)-1]
			continue
		}
		node := &top.kids[top.i]
		top.i++
		n := len(t.labels)
		var spans []buffer.Span
		switch {
		case buffer.SpansWidth(node.Spans) > 0:
			spans = node.Spans
		case node.Label != "":
			spans = []buffer.Span{buffer.NewSpan(node.Label, node.Style)}
		}
		t.labels = append(t.labels, spans)
		t.styles = append(t.styles, node.Style)
		t.depths = append(t.depths, top.depth)
		t.parents = append(t.parents, top.parent)
		t.hasKids = append(t.hasKids, len(node.Children) > 0)
		t.open = append(t.open, node.Expanded)
		// The default end is this node alone; a frame over its children overwrites
		// it when that frame pops.
		t.ends = append(t.ends, int32(n+1))
		if len(node.Children) > 0 {
			t.stack = append(t.stack, treeFrame{
				kids:   node.Children,
				parent: int32(n),
				depth:  top.depth + 1,
				first:  int32(n),
			})
		}
	}
	t.rebuildVisible()
	t.vm.SetCount(len(t.visible))
	if t.selected >= len(t.visible) {
		t.selected = len(t.visible) - 1
	}
	if t.selected < 0 {
		t.selected = 0
	}
	t.vm.ScrollIntoView(t.selected)
	t.cachedRect = buffer.Rect{}
}

// rebuildVisible recomputes which nodes are on screen.
//
// A collapsed node is skipped in one step through its subtree end, so this is
// O(visible rows) however large the tree is.
func (t *Tree) rebuildVisible() {
	t.visible = t.visible[:0]
	for i := 0; i < len(t.labels); i++ {
		t.visible = append(t.visible, int32(i))
		if !t.open[i] {
			// A collapsed node is skipped whole: the loop's own increment then
			// lands on the first node AFTER its subtree.
			i = int(t.ends[i]) - 1
		}
	}
}

// Nodes returns the number of nodes in the hierarchy, collapsed ones included.
func (t *Tree) Nodes() int { return len(t.labels) }

// VisibleRows returns the number of rows the tree currently has, which is what
// the user can scroll through.
func (t *Tree) VisibleRows() int { return len(t.visible) }

// Bounds returns the tree's rectangle, safe to call before the first Draw.
func (t *Tree) Bounds() buffer.Rect { return t.bounds }

// SetBounds sets the tree's rectangle.
func (t *Tree) SetBounds(r buffer.Rect) {
	t.bounds = r
	t.blk.SetBounds(r)
}

// Block returns the block that draws this tree's chrome, so a caller can
// configure the border, title, padding, background and ASCII rung.
func (t *Tree) Block() *block.Block { return t.blk }

// Focused reports whether the tree has focus.
func (t *Tree) Focused() bool { return t.focused }

// SetFocused gives or removes focus. The selection survives either way.
func (t *Tree) SetFocused(v bool) { t.focused = v }

// MinSize returns the smallest tree that shows a marker, an expander, an indent
// and a label: a whole-widget size including chrome.
func (t *Tree) MinSize() buffer.Size { return minWhole(t.blk, minTreeW, minTreeH) }

// Selected returns the selected NODE index, or -1 when the tree is empty.
func (t *Tree) Selected() int {
	node := t.SelectedNode()
	return int(node)
}

// SelectedRow returns the selected visible row, which is what the engine scrolls.
func (t *Tree) SelectedRow() int { return t.selected }

// SelectedNode returns the selected node index, or -1 when the tree is empty.
// It is -1 rather than a stale index because a collapse can remove the selected
// node from the visible list entirely.
func (t *Tree) SelectedNode() int32 {
	if t.selected < 0 || t.selected >= len(t.visible) {
		return -1
	}
	return t.visible[t.selected]
}

// SelectRow moves the selection to visible row i, clamped, and scrolls it into
// view.
func (t *Tree) SelectRow(i int) {
	if len(t.visible) == 0 {
		t.selected = -1
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(t.visible) {
		i = len(t.visible) - 1
	}
	t.selected = i
	t.vm.ScrollIntoView(i)
}

// Offset returns the index of the first visible row.
func (t *Tree) Offset() int { return t.vm.Offset() }

// SetOffset scrolls to row i, clamped.
func (t *Tree) SetOffset(i int) { t.vm.SetOffset(i) }

// Expanded reports whether node i is expanded, and whether it has children at
// all. An out-of-range node is reported as a collapsed leaf rather than
// panicking, because node indices arrive from callers.
func (t *Tree) Expanded(i int) bool {
	if i < 0 || i >= len(t.open) {
		return false
	}
	return t.open[i]
}

// HasChildren reports whether node i has children.
func (t *Tree) HasChildren(i int) bool {
	if i < 0 || i >= len(t.hasKids) {
		return false
	}
	return t.hasKids[i]
}

// Depth returns node i's depth, with the roots at 0.
func (t *Tree) Depth(i int) int32 {
	if i < 0 || i >= len(t.depths) {
		return 0
	}
	return t.depths[i]
}

// Toggle expands a collapsed node or collapses an expanded one, and returns
// whether the node now exists to be toggled. A leaf is not a no-op that lies: it
// reports false so a caller can tell a click on a leaf from a click on a branch.
func (t *Tree) Toggle(i int) bool {
	if i < 0 || i >= len(t.open) || !t.hasKids[i] {
		return false
	}
	t.open[i] = !t.open[i]
	t.afterExpansionChange()
	return true
}

// Expand expands node i, reporting whether it had children to expand.
func (t *Tree) Expand(i int) bool {
	if i < 0 || i >= len(t.open) || !t.hasKids[i] || t.open[i] {
		return false
	}
	t.open[i] = true
	t.afterExpansionChange()
	return true
}

// Collapse collapses node i, reporting whether it was expanded.
func (t *Tree) Collapse(i int) bool {
	if i < 0 || i >= len(t.open) || !t.hasKids[i] || !t.open[i] {
		return false
	}
	t.open[i] = false
	t.afterExpansionChange()
	return true
}

// ExpandAll opens every node in the hierarchy.
func (t *Tree) ExpandAll() {
	for i := range t.open {
		if t.hasKids[i] {
			t.open[i] = true
		}
	}
	t.afterExpansionChange()
}

// CollapseAll closes every node, leaving only the roots visible.
func (t *Tree) CollapseAll() {
	for i := range t.open {
		t.open[i] = false
	}
	t.afterExpansionChange()
}

// afterExpansionChange rebuilds the visible rows and re-clamps the selection,
// which a collapse may have removed from under it.
//
// The selection follows the row it was on rather than the node: with the selected
// node collapsed away, the row it was on now holds whatever node took its place,
// and keeping the row is the least surprising thing a user pressing Enter twice
// in a row will see.
func (t *Tree) afterExpansionChange() {
	t.rebuildVisible()
	t.vm.SetCount(len(t.visible))
	if t.selected >= len(t.visible) {
		t.selected = len(t.visible) - 1
	}
	if t.selected < 0 && len(t.visible) > 0 {
		t.selected = 0
	}
	t.vm.ScrollIntoView(t.selected)
	t.cachedRect = buffer.Rect{}
}

// Invalidate satisfies termmosaic.Widget: it drops the layout cache so the next
// Draw re-derives it.
//
// The widget repaints its whole rectangle every frame, so there is no dirty region
// to mark — but there IS state derived from the rect, and this is the documented
// way to say it is stale. A caller that changes a field the layout depends on
// (Scrollbar, Header, Status, Marker) calls Invalidate; without it the change lands
// whenever the rect next changes, which is the "broken for exactly one frame and
// repaired by the next" shape ADR 0007 §3 describes for a cache keyed on the wrong
// thing.
func (t *Tree) Invalidate() {
	t.cachedRect = buffer.Rect{}
}

// Handle consumes navigation keys while focused and mouse events inside its
// rectangle.
func (t *Tree) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventKey:
		if !t.focused {
			return false
		}
		return t.handleKey(ev)
	case termmosaic.EventMouse:
		return t.handleMouse(ev)
	default:
		return false
	}
}

// handleKey is the key contract, one case per documented binding.
func (t *Tree) handleKey(ev termmosaic.Event) bool {
	t.syncChrome()
	switch ev.Key {
	case termmosaic.KeyUp:
		t.move(-1)
		return true
	case termmosaic.KeyDown:
		t.move(1)
		return true
	case termmosaic.KeyPageUp:
		t.move(-t.vm.Visible() + 1)
		return true
	case termmosaic.KeyPageDown:
		t.move(t.vm.Visible() - 1)
		return true
	case termmosaic.KeyHome:
		t.SelectRow(0)
		return true
	case termmosaic.KeyEnd:
		t.SelectRow(len(t.visible) - 1)
		return true
	case termmosaic.KeyRight:
		t.stepIn()
		return true
	case termmosaic.KeyLeft:
		t.stepOut()
		return true
	case termmosaic.KeyEnter, termmosaic.KeySpace:
		t.Toggle(int(t.SelectedNode()))
		if t.OnActivate != nil {
			if n := t.SelectedNode(); n >= 0 {
				t.OnActivate(int(n))
			}
		}
		return true
	}
	return false
}

// move moves the selection by delta rows, treating zero as one.
func (t *Tree) move(delta int) {
	if delta == 0 {
		delta = 1
	}
	t.SelectRow(t.selected + delta)
}

// stepIn is Right: expand a collapsed node, or step into an open one.
func (t *Tree) stepIn() {
	n := int(t.SelectedNode())
	if n < 0 || !t.hasKids[n] {
		return
	}
	if !t.open[n] {
		t.open[n] = true
		t.afterExpansionChange()
		return
	}
	t.SelectRow(t.selected + 1)
}

// stepOut is Left: collapse an open node, or step out to the parent.
func (t *Tree) stepOut() {
	n := int(t.SelectedNode())
	if n < 0 {
		return
	}
	if t.open[n] {
		t.open[n] = false
		t.afterExpansionChange()
		return
	}
	if p := t.parents[n]; p >= 0 {
		t.SelectRow(t.rowOf(int(p)))
	}
}

// rowOf returns the visible row showing node i, or 0 when the node is not
// visible — which cannot happen for an ancestor of the selection, so the fallback
// is unreachable in practice and exists so the function is total.
func (t *Tree) rowOf(i int) int {
	for k, n := range t.visible {
		if int(n) == i {
			return k
		}
	}
	return 0
}

// handleMouse consumes a press or a wheel notch inside the tree.
func (t *Tree) handleMouse(ev termmosaic.Event) bool {
	if !t.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
		return false
	}
	t.syncChrome()
	switch ev.Mouse.Button {
	case termmosaic.MouseWheelUp:
		t.vm.LineUp(wheelLines)
		return true
	case termmosaic.MouseWheelDown:
		t.vm.LineDown(wheelLines)
		return true
	}
	if ev.Mouse.Action != termmosaic.MousePress || ev.Mouse.Button != termmosaic.MouseLeft {
		return false
	}
	i, ok := t.vm.ItemAt(ev.Mouse.Y - t.body.Y)
	if !ok {
		return false
	}
	t.focused = true
	t.SelectRow(i)
	return true
}

// syncChrome adapts if the interior has changed, so an event handled before the
// first Draw lands on the row the user can see. It allocates, which is why it is
// not on the frame path.
func (t *Tree) syncChrome() {
	in := t.blk.Interior()
	if in.Empty() || in == t.cachedRect {
		t.vm.Resize(t.bodyRect())
		return
	}
	t.adapt(in)
}

// bodyRect returns the rectangle rows are painted into: the interior minus the
// gutter and the scrollbar.
func (t *Tree) bodyRect() buffer.Rect {
	in := t.blk.Interior()
	if in.Empty() {
		return in
	}
	w := in.W - t.markerW - t.barW
	if w < 0 {
		w = 0
	}
	return buffer.Rect{X: in.X + t.markerW, Y: in.Y, W: w, H: in.H}
}

// Draw paints the chrome and the visible rows.
//
// It is total and allocation-free: the layout is derived in adapt, which runs once
// per distinct interior, and a row is an array read plus a clipped write.
func (t *Tree) Draw(buf *buffer.Buffer) {
	r := t.bounds
	if r.Empty() {
		return
	}
	t.blk.Draw(buf)
	in := t.blk.Interior()
	if in.Empty() {
		return
	}
	if in != t.cachedRect {
		t.adapt(in)
	}
	if t.body.Empty() {
		return
	}
	t.vm.Resize(t.body)
	tree := t
	t.vm.ForEach(t.body, buf, tree.drawRow)
	if t.barW > 0 {
		t.drawScrollbar(buf, in)
	}
}

// drawRow paints one row: the background across the row and the gutter, the
// marker for the selected row, the depth indent, the expansion glyph and the
// node's label clipped to what is left.
func (t *Tree) drawRow(dst *buffer.Buffer, row buffer.Rect, i int) {
	node := int(t.visible[i])
	selected := i == t.selected
	bg := t.ItemStyle
	st := t.styles[node]
	if st.IsUnset() {
		st = t.ItemStyle
	}
	if selected {
		bg = t.SelectedStyle
		st = t.SelectedStyle
	}
	dst.FillRect(row, bg.Resolved().Blank())
	if t.markerW > 0 {
		g := buffer.Rect{X: t.gutter.X, Y: row.Y, W: t.gutter.W, H: 1}
		dst.FillRect(g, bg.Resolved().Blank())
		if selected && t.markerRune != 0 {
			mst := t.MarkerStyle
			if mst.IsUnset() {
				mst = t.SelectedStyle
			}
			dst.SetCell(g.X, g.Y, mst.Resolved().Cell(t.markerRune))
		}
	}

	// Indent, bounded so that a deep node still shows a few cells of its label
	// rather than an indent and nothing else. The row arrives with its origin
	// already past the gutter, so the indent is measured from there.
	avail := row.W
	indent := int(t.depths[node]) * indentW
	if room := avail - expanderW - 2; indent > room {
		indent = room
	}
	if indent < 0 {
		indent = 0
	}
	x := row.X + indent
	glyph := defaultRune(t.LeafRune, leafRuneDefault)
	if t.hasKids[node] {
		glyph = defaultRune(t.ExpandedRune, expandedRuneDefault)
		if !t.open[node] {
			glyph = defaultRune(t.CollapsedRune, collapsedRuneDefault)
		}
	}
	dst.SetCell(x, row.Y, st.Resolved().Cell(glyph))
	// The label goes through paintRow so the node's own rendition — ItemStyle as a
	// fallback, SelectedStyle on the selected row — reaches the text and not only
	// the expander glyph above it. The rect is the label region alone, so the
	// fill inside paintRow is a second pass over cells already filled with the
	// same bg and cannot reach the marker or the indent painted above.
	// st reaches the text of a SINGLE-span label; a label carrying several spans
	// keeps its own styles, which is paintRow's documented exception. Stated here
	// because this is where the Tree defect of v0.4.0 lived, and a reader who
	// takes the next line as unconditional is wrong in exactly that way.
	label := buffer.Rect{X: x + expanderW, Y: row.Y, W: row.Right() - x - expanderW, H: 1}
	paintRow(dst, label, t.labels[node], 0, t.mark, bg, st)
}

// drawScrollbar paints the vertical position thumb, as in List and Table.
func (t *Tree) drawScrollbar(buf *buffer.Buffer, in buffer.Rect) {
	if t.vm.MaxOffset() <= 0 || in.H <= 0 {
		return
	}
	h := in.H
	x := in.Right() - 1
	thumbH := h * t.vm.Visible() / t.vm.Count()
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > h {
		thumbH = h
	}
	start := 0
	if slack := h - thumbH; slack > 0 {
		start = slack * t.vm.Offset() / t.vm.MaxOffset()
	}
	c := t.ScrollbarStyle.Resolved().Cell(t.thumbRune)
	for y := 0; y < thumbH; y++ {
		buf.SetCell(x, in.Y+start+y, c)
	}
}

// adapt recomputes everything derived from the interior: the rect-keyed cache of
// ADR 0007 §3.
func (t *Tree) adapt(in buffer.Rect) {
	t.cachedRect = in
	t.mark = truncMark(t.blk.Ascii)
	t.markerRune = firstRune(t.Marker)
	t.thumbRune = thumbRune
	if t.blk.Ascii {
		t.thumbRune = asciiThumb
	}
	if len(t.regions) > 0 {
		t.regions[0].Size = buffer.StringWidth(t.Marker) + markerPad
	}
	t.keep = geometry.Budget(t.regions, in.W)
	t.markerW, t.barW = 0, 0
	if len(t.keep) == len(t.regions) {
		if t.keep[0] && t.Marker != "" {
			t.markerW = t.regions[0].Size
		}
		if t.keep[1] && t.Scrollbar {
			t.barW = scrollbarW
		}
	}
	if t.markerW >= in.W {
		t.markerW = 0
	}
	w := in.W - t.markerW - t.barW
	if w < 0 {
		w = 0
	}
	t.body = buffer.Rect{X: in.X + t.markerW, Y: in.Y, W: w, H: in.H}
	t.gutter = buffer.Rect{X: in.X, Y: in.Y, W: t.markerW, H: in.H}
	// The engine is resized here rather than only in Draw, so an event handled
	// before the first frame maps to the row the user can see.
	t.vm.Resize(t.body)
}

// defaultRune returns r, or fallback when the caller left the field at zero.
func defaultRune(r, fallback rune) rune {
	if r == 0 {
		return fallback
	}
	return r
}

var (
	_ termmosaic.Widget      = (*Tree)(nil)
	_ termmosaic.Focusable   = (*Tree)(nil)
	_ termmosaic.Minimizable = (*Tree)(nil)
)
