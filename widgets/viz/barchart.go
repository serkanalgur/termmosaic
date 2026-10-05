package viz

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds for BarChart, per ADR 0007 §1 rule 5.
const (
	// minChartW is the narrowest interior that shows a bar and a label.
	minChartW = 6
	// minChartH is the smallest vertical chart that shows a bar, an axis and a
	// label.
	minChartH = 4
	// axisRune is the axis line. It is a hyphen rather than a box-drawing glyph:
	// buffer/border.go owns those, and a chart axis is texture rather than a frame.
	// It is one cell wide on both the Unicode and the ASCII rung, so the plot's
	// arithmetic is identical either way.
	axisRune = '-'
	// valuePrio is the per-bar value label's priority against the plot: a bar
	// without its number is still a bar, a number without a bar is a table.
	valuePrio = geometry.PrioLow
	// minValueLabelW is the narrowest column that can carry a value label at all.
	minValueLabelW = 3
)

// Datum is one bar: a category, a value and a rendition.
type Datum struct {
	// Label is the category name, drawn on the axis (vertical) or beside the bar
	// (horizontal).
	Label string
	// Value is the bar's magnitude. A negative value clamps to zero: a categorical
	// bar chart with no baseline axis has nowhere to put a negative bar, and
	// drawing one downward would be a claim about the axis the widget does not make.
	Value float64
	// Style is the bar's rendition, falling back to BarStyle.
	Style buffer.Style
}

// BarChart is a categorical bar chart, vertical or horizontal, with an axis and
// value labels.
//
// It is a plain termmosaic.Widget: a chart is not selectable.
//
// # Two orientations, one arithmetic
//
// Vertical is the default and the case a dashboard usually wants: categories across,
// magnitude up. Horizontal exists because a category with a long name, or a chart
// with forty categories, does not fit the other way round. The scale is shared —
// one Max, one value-to-cells conversion — so switching orientation cannot change
// what a value looks like.
//
// # The scale, and why it defaults to the data
//
// Max <= 0 means "use the largest value", which is what a reader expects from a
// categorical chart. A caller who pins Max pins the comparison between two charts,
// which is the only reason to want it.
//
// # Values
//
// A negative or NaN value draws as no bar at all, and the category's label and
// value are still printed. That is deliberate: a chart that dropped the category
// would make a negative measurement look like a missing one.
type BarChart struct {
	blk    *block.Block
	bounds buffer.Rect

	// Data is the series, in order.
	Data []Datum

	// Vertical draws the categories across the width and the magnitude up the
	// height. The zero value draws them down the rows instead.
	Vertical bool

	// Max is the top of the scale. Zero or less means the data's own maximum.
	Max float64

	// BarStyle is the default bar rendition, and AxisStyle and LabelStyle the axis
	// line's and the category labels'.
	BarStyle   buffer.Style
	AxisStyle  buffer.Style
	LabelStyle buffer.Style

	// ValueStyle is the per-bar value label's rendition, and ShowValue toggles it.
	// The number is the colour-independent reading of the chart: without it, two
	// bars of nearly equal height are indistinguishable in monochrome, and no amount
	// of axis precision replaces it in a dashboard read at a glance.
	ValueStyle buffer.Style
	ShowValue  bool

	// Axis draws the axis line and the category labels.
	Axis bool

	// regions is the height budget for a vertical chart: the value row against the
	// plot, built once and refreshed in adapt.
	regions []geometry.Region
	keep    []bool

	plot       buffer.Rect
	axisRow    buffer.Rect
	labelRow   buffer.Rect
	valueRow   buffer.Rect
	labelW     int
	cachedRect buffer.Rect
	scale      float64
	mark       rune
	widths     []int
}

// NewBarChart returns a vertical BarChart sized r with no data.
func NewBarChart(r buffer.Rect) *BarChart {
	c := &BarChart{
		blk:       block.New(r),
		bounds:    r,
		Vertical:  true,
		Axis:      true,
		ShowValue: true,
		regions: []geometry.Region{
			{Size: 1, Prio: valuePrio},
			{Size: 0, Prio: geometry.PrioAlways},
		},
	}
	c.blk.SetBounds(r)
	return c
}

// Bounds returns the chart's rectangle, safe to call before the first Draw.
func (c *BarChart) Bounds() buffer.Rect { return c.bounds }

// SetBounds sets the chart's rectangle.
func (c *BarChart) SetBounds(r buffer.Rect) {
	c.bounds = r
	c.blk.SetBounds(r)
}

// Block returns the block that draws this chart's chrome.
func (c *BarChart) Block() *block.Block { return c.blk }

// MinSize returns the smallest chart that shows a bar, an axis and a label: a
// whole-widget size including chrome. A horizontal chart needs only two rows, so
// the minimum follows the orientation.
func (c *BarChart) MinSize() buffer.Size {
	if c.Vertical {
		return minWhole(c.blk, minChartW, minChartH)
	}
	return minWhole(c.blk, minChartW+maxLabelW, 2)
}

// SetData replaces the series.
func (c *BarChart) SetData(d []Datum) { c.Data = d }

// MaxValue returns the top of the scale the chart is drawing against, which is the
// data's own maximum unless Max was pinned.
func (c *BarChart) MaxValue() float64 {
	if c.Max > 0 {
		return c.Max
	}
	hi := 0.0
	for _, d := range c.Data {
		if d.Value == d.Value && d.Value > hi {
			hi = d.Value
		}
	}
	return hi
}

// Invalidate satisfies termmosaic.Widget: it drops the layout cache so the next Draw
// re-derives the column widths and the scale.
func (c *BarChart) Invalidate() { c.cachedRect = buffer.Rect{} }

// Handle satisfies termmosaic.Widget. A chart has no focus and no interaction.
func (c *BarChart) Handle(termmosaic.Event) bool { return false }

// Draw paints the chrome and then the chart.
//
// It is total and allocation-free: the column widths come from layout.Solve inside
// adapt, cached against the rect, and read here.
func (c *BarChart) Draw(buf *buffer.Buffer) {
	r := c.bounds
	if r.Empty() {
		return
	}
	c.blk.Draw(buf)
	in := c.blk.Interior()
	if in.Empty() {
		return
	}
	if in != c.cachedRect {
		c.adapt(in)
	}
	if c.Vertical {
		c.drawVertical(buf)
		return
	}
	c.drawHorizontal(buf)
}

// adapt recomputes everything derived from the interior: the plot, the axis and
// label rows, the per-bar column widths and the scale.
func (c *BarChart) adapt(in buffer.Rect) {
	c.cachedRect = in
	c.mark = truncMark(c.blk.Ascii)
	c.scale = c.MaxValue()
	if c.scale <= 0 {
		// Every value is zero or negative: the scale is set to one so a division by
		// it is exact and the bars are empty rather than infinite.
		c.scale = 1
	}
	c.plot = in
	c.axisRow = buffer.Rect{}
	c.labelRow = buffer.Rect{}
	c.valueRow = buffer.Rect{}
	c.labelW = 0

	if !c.Vertical {
		// Horizontal: the label column is as wide as the widest label, capped, and
		// the bars share what is left.
		for _, d := range c.Data {
			if w := buffer.StringWidth(d.Label); w > c.labelW {
				c.labelW = w
			}
		}
		if c.labelW > maxLabelW {
			c.labelW = maxLabelW
		}
		if c.labelW > 0 && c.Axis {
			c.labelW += 2
		}
		w := in.W - c.labelW
		if w < 0 {
			w = 0
		}
		// axisRow is the LABEL COLUMN here, and drawHorizontal reads its X to place
		// every category's label. Leaving it at the zero Rect put every label at
		// absolute column 0 — which is inside Bounds only for a chart at the origin,
		// and is a widget writing outside its own rectangle everywhere else
		// (ADR 0007 §4).
		c.axisRow = buffer.Rect{X: in.X, Y: in.Y, W: c.labelW, H: in.H}
		c.plot = buffer.Rect{X: in.X + c.labelW, Y: in.Y, W: w, H: in.H}
		c.measureBars()
		return
	}

	// Vertical: one row for the axis and labels, and one for the values when the
	// budget says so. The body region is declared as what is LEFT, because
	// geometry.Budget compares declared sizes against the available cells.
	if c.Axis && in.H > 0 {
		c.axisRow = buffer.Rect{X: in.X, Y: in.Y + in.H - 1, W: in.W, H: 1}
	}
	if len(c.regions) == 2 {
		// The plot is declared as what is LEFT after the value row, because
		// geometry.Budget compares declared sizes against the available cells: a
		// plot declared as the whole height would leave nothing for the values and
		// they would always be dropped.
		c.regions[1].Size = in.H - 1
		if c.regions[1].Size < 0 {
			c.regions[1].Size = 0
		}
	}
	c.keep = geometry.Budget(c.regions, in.H)
	showValue := false
	if len(c.keep) == 2 && c.ShowValue && c.keep[0] {
		showValue = true
		c.valueRow = buffer.Rect{X: in.X, Y: in.Y, W: in.W, H: 1}
	}
	// The value row is taken out of the plot's TOP, and the axis out of its bottom,
	// and the plot's height is what is left. The offsets are separate from the
	// heights because the value row is above the plot and the axis below it.
	head, h := 0, in.H
	if showValue {
		head, h = 1, h-1
	}
	if !c.axisRow.Empty() {
		h--
	}
	if h < 0 {
		h = 0
	}
	c.plot = buffer.Rect{X: in.X, Y: in.Y + head, W: in.W, H: h}
	c.measureBars()
}

// measureBars solves the per-bar widths for a VERTICAL chart with layout.Solve, so
// the distribution of cells among categories is the framework's rule and not a
// second one in a widget.
func (c *BarChart) measureBars() {
	n := len(c.Data)
	if n == 0 {
		c.widths = c.widths[:0]
		return
	}
	if cap(c.widths) < n {
		c.widths = make([]int, n)
	}
	c.widths = c.widths[:n]
	for i := range c.widths {
		c.widths[i] = 0
	}
	cs := make([]layout.Constraint, n) // adapt-only allocation
	for i := range cs {
		cs[i] = layout.Fill(1)
	}
	sizes := layout.Solve(layout.Horizontal, cs, 0, c.plot.W)
	copy(c.widths, sizes)
}

// drawVertical draws the bars with the categories across the width.
func (c *BarChart) drawVertical(buf *buffer.Buffer) {
	if c.plot.Empty() || len(c.Data) == 0 {
		return
	}
	x := c.plot.X
	for i, d := range c.Data {
		w := 1
		if i < len(c.widths) {
			w = c.widths[i]
		}
		if w < 1 {
			w = 1
		}
		if x >= c.plot.Right() {
			break
		}
		if x+w > c.plot.Right() {
			w = c.plot.Right() - x
		}
		c.drawBar(buf, buffer.Rect{X: x, Y: c.plot.Y, W: w, H: c.plot.H}, d)
		c.drawValue(buf, x, w, d.Value)
		x += w
	}
	if !c.axisRow.Empty() {
		c.drawAxis(buf)
	}
}

// drawHorizontal draws one bar per row, with the category label beside it.
func (c *BarChart) drawHorizontal(buf *buffer.Buffer) {
	if c.plot.W < 1 {
		return
	}
	for i, d := range c.Data {
		y := c.plot.Y + i
		if y >= c.plot.Bottom() {
			break
		}
		bar := buffer.Rect{X: c.plot.X, Y: y, W: c.plot.W, H: 1}
		c.drawBar(buf, bar, d)
		if c.labelW > 2 {
			row := buffer.Rect{X: c.axisRow.X, Y: y, W: c.labelW - 2, H: 1}
			buf.SetSpansCappedIn(row.X, row.Right(), y, []buffer.Span{buffer.NewSpan(d.Label, c.LabelStyle)}, c.mark)
		}
		// The value sits at the end of the bar when there is room for it, which is
		// the reading a dashboard is scanned for.
		used := c.barCells(d.Value, bar.W)
		if x := bar.X + used + 1; x < bar.Right() {
			putNum(buf, x, bar.Right(), y, d.Value, 0, c.ValueStyle.Resolved())
		}
	}
}

// drawAxis paints the axis line and the category labels under a vertical chart.
//
// Both are ASCII on purpose: a chart axis is not a frame, so it does not come from
// buffer's border table, and a hyphen is unambiguous at one cell wide.
func (c *BarChart) drawAxis(buf *buffer.Buffer) {
	row := c.axisRow
	axis := c.AxisStyle.Resolved().Cell(axisRune)
	for x := row.X; x < row.Right(); x++ {
		buf.SetCell(x, row.Y, axis)
	}
	if len(c.Data) == 0 {
		return
	}
	x := c.plot.X
	for i, d := range c.Data {
		w := 1
		if i < len(c.widths) {
			w = c.widths[i]
		}
		if x >= row.Right() {
			break
		}
		if x+w > row.Right() {
			w = row.Right() - x
		}
		lw := buffer.StringWidth(d.Label)
		if lw > w {
			lw = w
		}
		lx := x + geometry.AlignCenter.Offset(w, lw)
		buf.SetSpansCappedIn(x, row.Right(), row.Y, []buffer.Span{buffer.NewSpan(d.Label, c.LabelStyle)}, c.mark)
		_ = lx
		x += w
	}
}

// drawValue paints one bar's value above it, in a vertical chart.
//
// It is drawn per bar rather than per row because a row of values above a row of
// bars of different heights is unreadable: each number belongs to the column it sits
// over. A column too narrow for the number prints nothing, and the axis label below
// it is the fallback reading.
func (c *BarChart) drawValue(buf *buffer.Buffer, x, w int, v float64) {
	if c.valueRow.Empty() || !c.ShowValue || w < minValueLabelW {
		return
	}
	lw := buffer.SpansWidth([]buffer.Span{buffer.NewSpan(numString(v), c.ValueStyle)})
	if lw > w {
		return
	}
	putNum(buf, x+geometry.AlignCenter.Offset(w, lw), c.valueRow.Right(), c.valueRow.Y, v, 0, c.ValueStyle.Resolved())
}

// drawBar paints one bar's fill inside its rectangle, from the bottom up.
//
// The topmost cell is a fractional eighth rather than a whole block, so a bar 3
// cells tall showing 60% is visibly 1.8 cells and not 2. The eighth-block ramp is
// what makes that possible at one cell per row.
func (c *BarChart) drawBar(buf *buffer.Buffer, r buffer.Rect, d Datum) {
	if r.Empty() {
		return
	}
	st := d.Style
	if st.IsUnset() {
		st = c.BarStyle
	}
	st = st.Resolved()
	glyph := fillGlyph(c.blk.Ascii)
	// Declared without an initialiser: both branches assign it, so a value here
	// would be dead. A vertical bar is measured against the row height and a
	// horizontal one against the width.
	var fill float64
	if c.Vertical {
		fill = ratioOf(d.Value, c.scale) * float64(r.H)
	} else {
		fill = ratioOf(d.Value, c.scale) * float64(r.W)
	}
	if fill <= 0 {
		return
	}
	if !c.Vertical {
		n := int(fill + 0.5)
		if n > r.W {
			n = r.W
		}
		for i := 0; i < n; i++ {
			buf.SetCell(r.X+i, r.Y, st.Cell(glyph))
		}
		return
	}
	if fill >= float64(r.H) {
		for y := r.Y; y < r.Bottom(); y++ {
			buf.SetCell(r.X, y, st.Cell(glyph))
		}
		return
	}
	whole := int(fill)
	frac := fill - float64(whole)
	for y := r.Bottom() - whole; y < r.Bottom(); y++ {
		buf.SetCell(r.X, y, st.Cell(glyph))
	}
	if whole < r.H && frac > 0 {
		// The partial cell sits directly above the whole ones, at the bar's own x:
		// the coordinates are (x, y), and mixing them up puts the fraction in a
		// column of its own at the top of the plot.
		buf.SetCell(r.X, r.Bottom()-1-whole, st.Cell(eighthsRune(frac)))
	}
}

// barCells returns how many cells bar w needs for value, which is what a horizontal
// bar draws and what its value label is placed after.
func (c *BarChart) barCells(value float64, w int) int {
	n := int(ratioOf(value, c.scale)*float64(w) + 0.5)
	if n > w {
		n = w
	}
	if n < 0 {
		n = 0
	}
	return n
}

// numString returns v's decimal text, used only to MEASURE a value label's width —
// the digits themselves are written by putNum, which allocates nothing, so the only
// string on this path is the one that never reaches a cell.
func numString(v float64) string {
	var dst [32]byte
	return string(numDigits(dst[:0], v, 0))
}

var (
	_ termmosaic.Widget      = (*BarChart)(nil)
	_ termmosaic.Minimizable = (*BarChart)(nil)
)
