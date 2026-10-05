package viz

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds for ProgressBar, per ADR 0007 §1 rule 5.
const (
	// minProgressW is the narrowest interior that shows a bar with a label and a
	// percentage.
	minProgressW = 12
	// minProgressH is the smallest height that shows a bar at all.
	minProgressH = 1
	// labelPrio is the label's priority: a percentage is more information than a
	// label, and the bar itself is more than either.
	labelPrio = geometry.PrioHigh
	// percentPrio is the percentage's priority.
	percentPrio = geometry.PrioLow
)

// ProgressBar is a determinate bar with a label, a percentage and a fill
// character.
//
// It is a plain termmosaic.Widget: nothing about a bar is selectable, and a widget
// that claimed focus would swallow the keys its neighbours needed.
//
// # What survives a narrow rect
//
// The bar is PrioAlways: it is the widget. The label and the percentage compete for
// what is left, and geometry.Budget decides, with the label first because "downloading"
// says more than "62%". A rect too narrow for even the bar still draws the bar's
// track, so the widget degrades to a coloured rule rather than to nothing.
//
// # Values
//
// Set takes a ratio in [0, 1] and clamps it: below zero is zero, above one is one,
// and a NaN is zero. See ratioOf, which is the one place that policy lives.
type ProgressBar struct {
	blk    *block.Block
	bounds buffer.Rect

	// value is the progress ratio, always in [0, 1] after Set.
	value float64

	// Label is the text beside the bar and Percentage toggles the number. Both are
	// rendered from spans, so a label can carry several styles.
	Label        []buffer.Span
	LabelStyle   buffer.Style
	Percentage   bool
	PercentStyle buffer.Style

	// FillStyle is the rendition of the filled part and TrackStyle of the rest.
	// The DIFFERENCE between the two is what makes the fill readable without
	// colour, which is why TrackStyle should carry an attribute or a background
	// rather than merely a dimmer hue.
	FillStyle  buffer.Style
	TrackStyle buffer.Style

	// FillRune is the glyph the filled part is drawn with, defaulting to U+2588
	// FULL BLOCK. An ASCII terminal gets '#' from Block.Ascii.
	FillRune rune

	// labelW and percentW are the measured widths of the two flanking regions, and
	// barX/barW the bar's own rectangle inside the interior — all from adapt.
	regions          []geometry.Region
	keep             []bool
	bar              buffer.Rect
	labelW, percentW int
	cachedRect       buffer.Rect
	mark             rune
	//nolint:unused // Reserved for the label span cache: kept rather than
	// deleted because ProgressBar is an exported library widget and this field is
	// part of its private state that the caching pass is written against.
	labelSpans []buffer.Span
}

// NewProgressBar returns a ProgressBar sized r with no value set, which draws an
// empty track.
func NewProgressBar(r buffer.Rect) *ProgressBar {
	p := &ProgressBar{
		blk:    block.New(r),
		bounds: r,
		regions: []geometry.Region{
			{Size: 0, Prio: labelPrio},
			{Size: 0, Prio: geometry.PrioAlways},
			{Size: 0, Prio: percentPrio},
		},
	}
	p.blk.SetBounds(r)
	return p
}

// Bounds returns the bar's rectangle, safe to call before the first Draw.
func (p *ProgressBar) Bounds() buffer.Rect { return p.bounds }

// SetBounds sets the bar's rectangle. The layout cache is keyed on it, so the next
// Draw re-runs the budget.
func (p *ProgressBar) SetBounds(r buffer.Rect) {
	p.bounds = r
	p.blk.SetBounds(r)
}

// Block returns the block that draws this bar's chrome, so a caller can configure
// the border, title, padding, background and ASCII rung.
func (p *ProgressBar) Block() *block.Block { return p.blk }

// MinSize returns the smallest bar that shows a label, a bar and a percentage: a
// whole-widget size including chrome.
func (p *ProgressBar) MinSize() buffer.Size { return minWhole(p.blk, minProgressW, minProgressH) }

// Value returns the progress ratio, always in [0, 1].
func (p *ProgressBar) Value() float64 { return p.value }

// Set sets the progress ratio, clamped into [0, 1]: below zero is zero, above one
// is one, and a NaN is zero. Clamping rather than panicking is the contract —
// this value comes from a division somewhere, and a division can produce NaN.
func (p *ProgressBar) Set(ratio float64) { p.value = ratioOf(ratio, 1) }

// SetLabel sets the label from a single styled run, which is the common case.
func (p *ProgressBar) SetLabel(s string, st buffer.Style) {
	p.Label = []buffer.Span{buffer.NewSpan(s, st)}
	p.cachedRect = buffer.Rect{}
}

// Invalidate satisfies termmosaic.Widget: it drops the layout cache so the next
// Draw re-derives it.
func (p *ProgressBar) Invalidate() { p.cachedRect = buffer.Rect{} }

// Handle satisfies termmosaic.Widget. A bar has no focus and no interaction, and
// consuming nothing is what lets an application's key handler see every key.
func (p *ProgressBar) Handle(termmosaic.Event) bool { return false }

// Draw paints the chrome, then the label, the bar and the percentage.
//
// It is total and allocation-free: the layout comes from adapt, which runs once
// per distinct interior, and the percentage is written digit by digit from a stack
// array.
func (p *ProgressBar) Draw(buf *buffer.Buffer) {
	r := p.bounds
	if r.Empty() {
		return
	}
	p.blk.Draw(buf)
	in := p.blk.Interior()
	if in.Empty() {
		return
	}
	if in != p.cachedRect {
		p.adapt(in)
	}
	p.drawLabel(buf, in)
	p.drawBar(buf)
	p.drawPercent(buf, in)
}

// adapt recomputes everything derived from the interior: the rect-keyed cache of
// ADR 0007 §3.
func (p *ProgressBar) adapt(in buffer.Rect) {
	p.cachedRect = in
	p.mark = truncMark(p.blk.Ascii)
	if len(p.regions) == 3 {
		p.regions[0].Size = buffer.SpansWidth(p.Label)
		if p.Percentage {
			p.regions[2].Size = percentWidth()
		} else {
			p.regions[2].Size = 0
		}
	}
	p.keep = geometry.Budget(p.regions, in.W)
	p.labelW, p.percentW = 0, 0
	lead := 0
	if len(p.keep) == 3 {
		if p.keep[0] && p.labelW < p.regions[0].Size {
			p.labelW = p.regions[0].Size
		}
		if p.keep[2] && p.Percentage {
			p.percentW = p.regions[2].Size
		}
	}
	// One space of padding on each side of the flanking regions, and one between
	// them and the bar, so text never touches the fill.
	if p.labelW > 0 {
		lead += p.labelW + 2
	}
	tail := 0
	if p.percentW > 0 {
		tail = p.percentW + 2
	}
	w := in.W - lead - tail
	if w < 0 {
		w = 0
	}
	// A bar narrower than the minimum is not worth drawing as a bar: the text
	// regions keep the cells and the widget degrades to a label with a number.
	if w < minBarW && (p.labelW > 0 || p.percentW > 0) {
		w = 0
	}
	p.bar = buffer.Rect{X: in.X + lead, Y: in.Y, W: w, H: in.H}
}

// drawLabel paints the label in the leftmost cells of the interior.
func (p *ProgressBar) drawLabel(buf *buffer.Buffer, in buffer.Rect) {
	if p.labelW == 0 {
		return
	}
	row := buffer.Rect{X: in.X, Y: in.Y, W: p.labelW, H: in.H}
	paintRow(buf, row, p.Label, 0, p.mark, p.TrackStyle)
}

// drawPercent paints the percentage right-aligned in the interior.
//
// The digits ARE the accessibility story: a bar whose only difference between 40%
// and 90% is a hue is unreadable in monochrome, and the number costs three cells.
func (p *ProgressBar) drawPercent(buf *buffer.Buffer, in buffer.Rect) {
	if p.percentW == 0 {
		return
	}
	row := buffer.Rect{X: in.Right() - p.percentW, Y: in.Y, W: p.percentW, H: in.H}
	fillRow(buf, row, p.TrackStyle)
	st := p.PercentStyle.Resolved()
	x := putNum(buf, row.X, row.Right(), row.Y, p.value*100, 0, st)
	if x < row.Right() {
		// The '%' is written as its own cell rather than being part of the digits, so
		// the reserved width and the printed width cannot disagree.
		buf.SetCell(x, row.Y, st.Cell('%'))
	}
}

// drawBar paints the track and the fill.
//
// The fill is computed in whole cells plus a fractional final glyph, so a bar 3
// cells wide showing 50% is genuinely half full rather than either empty or full.
// A zero-width bar draws nothing at all rather than panicking, which is the
// degenerate-size contract applied to a measurement widget.
func (p *ProgressBar) drawBar(buf *buffer.Buffer) {
	bar := p.bar
	if bar.Empty() {
		return
	}
	fillRow(buf, bar, p.TrackStyle)
	fill := p.value * float64(bar.W)
	whole := int(fill)
	if whole > bar.W {
		whole = bar.W
	}
	glyph := p.FillRune
	if glyph == 0 {
		glyph = fillGlyph(p.blk.Ascii)
	}
	c := p.FillStyle.Resolved().Cell(glyph)
	for i := 0; i < whole; i++ {
		buf.SetCell(bar.X+i, bar.Y, c)
	}
	if whole < bar.W {
		if frac := fill - float64(whole); frac > 0 {
			buf.SetCell(bar.X+whole, bar.Y, p.FillStyle.Resolved().Cell(barRune(frac)))
		}
	}
}

// percentWidth returns the cell width of the largest percentage this widget can
// print. Set clamps to [0, 1], so the value is 0..100 and three digits plus the
// sign is four cells; reserving that fixed width is what stops the number from
// shifting the bar as it grows from 9% to 100%.
func percentWidth() int { return len("100%") }

// fillGlyph returns the fill character for the current ASCII rung.
func fillGlyph(ascii bool) rune {
	if ascii {
		return '#'
	}
	return fullBlock
}

// truncMark returns the one-cell truncation marker for the given ASCII rung. Both
// rungs are one cell wide, so the choice cannot shift a layout.
func truncMark(ascii bool) rune {
	if ascii {
		return firstRune(buffer.AscTruncSuffix)
	}
	return firstRune(buffer.TruncSuffix)
}

// firstRune returns the first rune of s, or 0 when s is empty. It is for a
// multi-byte glyph in a marker field, where indexing bytes would split the
// sequence.
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

var (
	_ termmosaic.Widget      = (*ProgressBar)(nil)
	_ termmosaic.Minimizable = (*ProgressBar)(nil)
)
