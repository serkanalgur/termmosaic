package viz

import (
	"math"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds for Sparkline, per ADR 0007 §1 rule 5.
const (
	// minSparkW is the narrowest interior that shows more than one cell of series.
	minSparkW = 1
	// minSparkH is the smallest height that shows a series at all.
	minSparkH = 1
	// maxLabelW caps a BarChart's category labels, in cells: an axis label wider
	// than this would eat the plot, and the caller can see the value beside it.
	maxLabelW = 20
)

// Sparkline is a series of values drawn in one row — or one column — with
// sub-cell resolution.
//
// # Why the glyphs are what they are
//
// One cell of a terminal cannot show a fraction of a bar, so a sparkline that drew
// whole blocks per sample would quantise a series to its sample count and lose every
// peak between two samples. Two encodings fix that, and this widget offers both
// because they trade along different axes:
//
//	Braille (default)  two samples per cell, four levels each: eight quanta per cell
//	                   along the HORIZONTAL axis, so a 40-cell sparkline resolves 80
//	                   values
//	Block              one sample per cell, eight levels: eight quanta per cell along
//	                   the VERTICAL axis, so a short wide sparkline reads a trend far
//	                   better than a braille one
//
// Braille is a Block Elements neighbour (U+2800–28FF) and is legitimate texture
// rather than a border, which is why it is legal outside buffer/border.go.
//
// # Colour is never the only signal
//
// The data IS geometry here — a sample's height is its value — so the series is
// fully readable with no colour at all. The styles are emphasis layered on top, and
// the ones this widget ships are ATTRIBUTE-based (reverse, bold) rather than
// hue-based for exactly that reason.
//
// # Values
//
// A NaN sample is drawn as zero rather than skipped: dropping it would shift every
// later sample one cell left, which silently misreports the series. Samples are
// normalised against Min and Max, which default to the series' own extremes and
// can be pinned by the caller; a series where max == min is drawn flat rather than
// dividing by zero.
type Sparkline struct {
	blk    *block.Block
	bounds buffer.Rect

	// Values is the series, in order. It is read by reference: SetValues does not
	// copy, so a caller must not mutate the slice while drawing.
	Values []float64

	// Braille selects the two-samples-per-cell encoding. It is a field rather than
	// a second widget because both encodings share the normalisation, the styles and
	// the layout, and a caller switching between them at a width threshold would
	// otherwise have to hold two widgets in step.
	Braille bool

	// Vertical lays the series down a column instead of across a row.
	Vertical bool

	// Min and Max bound the series. Auto derives them from the data, which is the
	// useful default: a sparkline scaled to the axis rather than to its own range
	// shows no trend at all.
	Min, Max float64
	Auto     bool

	// Style is the ordinary sample's rendition; MinStyle the lowest sample's, MaxStyle
	// the highest's and LastStyle the final sample's. All four are emphasis, never
	// the signal.
	Style     buffer.Style
	MinStyle  buffer.Style
	MaxStyle  buffer.Style
	LastStyle buffer.Style

	// Threshold is the level above which a sample takes ThresholdStyle, which
	// defaults to AttrReverse: an attribute, so a breach is marked on a monochrome
	// terminal. A threshold at or above Max marks nothing.
	Threshold      float64
	ThresholdStyle buffer.Style

	// TrackStyle is the background behind the series, so a falling line is visible
	// against it rather than against whatever the block painted.
	TrackStyle buffer.Style

	cachedRect buffer.Rect
	plot       buffer.Rect
	min, max   float64
	mark       rune
}

// NewSparkline returns a Sparkline sized r with no values.
func NewSparkline(r buffer.Rect) *Sparkline {
	s := &Sparkline{
		blk:            block.New(r),
		bounds:         r,
		Braille:        true,
		Auto:           true,
		ThresholdStyle: buffer.ReverseStyle,
	}
	s.blk.SetBounds(r)
	return s
}

// Bounds returns the sparkline's rectangle, safe to call before the first Draw.
func (s *Sparkline) Bounds() buffer.Rect { return s.bounds }

// SetBounds sets the sparkline's rectangle. The normalisation cache is keyed on it,
// because the plot's HEIGHT is what decides which levels a cell can show.
func (s *Sparkline) SetBounds(r buffer.Rect) {
	s.bounds = r
	s.blk.SetBounds(r)
}

// Block returns the block that draws this sparkline's chrome.
func (s *Sparkline) Block() *block.Block { return s.blk }

// MinSize returns the smallest sparkline that shows a cell of series: a whole-widget
// size including chrome.
func (s *Sparkline) MinSize() buffer.Size { return minWhole(s.blk, minSparkW, minSparkH) }

// SetValues replaces the series, and drops the normalisation cache so the next
// Draw re-derives the minimum and maximum it scales the plot against.
//
// Without it a new series is drawn through the OLD series' range, which is the
// worst shape this widget can fail in: the sparkline looks like a plausible
// sparkline, showing the wrong trend, with nothing on screen to say so. The cache
// is keyed on the rect alone, so no resize is coming to repair it.
func (s *Sparkline) SetValues(v []float64) {
	s.Values = v
	s.cachedRect = buffer.Rect{}
}

// Invalidate satisfies termmosaic.Widget: it drops the cache so the next Draw
// re-derives the normalisation.
func (s *Sparkline) Invalidate() { s.cachedRect = buffer.Rect{} }

// Handle satisfies termmosaic.Widget. A sparkline has no focus and no interaction.
func (s *Sparkline) Handle(termmosaic.Event) bool { return false }

// Draw paints the chrome and the series.
//
// It is total and allocation-free. The normalisation is computed in adapt, once per
// distinct rect, and Draw only reads it — recomputing min and max per frame would be
// O(series) per frame, which is exactly the cost this widget must not have.
func (s *Sparkline) Draw(buf *buffer.Buffer) {
	r := s.bounds
	if r.Empty() {
		return
	}
	s.blk.Draw(buf)
	in := s.blk.Interior()
	if in.Empty() {
		return
	}
	if in != s.cachedRect {
		s.adapt(in)
	}
	if s.plot.Empty() {
		return
	}
	fillRow(buf, s.plot, s.TrackStyle)
	if s.Vertical {
		s.drawVertical(buf)
		return
	}
	s.drawHorizontal(buf)
}

// adapt computes the plot rectangle and the series' normalisation.
func (s *Sparkline) adapt(in buffer.Rect) {
	s.cachedRect = in
	s.mark = truncMark(s.blk.Ascii)
	s.plot = in
	if s.Auto {
		s.normalise()
	} else {
		s.min, s.max = s.Min, s.Max
	}
}

// normalise derives the series' extremes. A NaN is skipped rather than becoming a
// bound: one bad sample must not define the whole scale.
func (s *Sparkline) normalise() {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range s.Values {
		if v != v {
			continue
		}
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	if math.IsInf(lo, 1) {
		lo, hi = 0, 0
	}
	s.min, s.max = lo, hi
}

// span returns the range the series is drawn against, never zero.
func (s *Sparkline) span() float64 {
	if d := s.max - s.min; d > 0 {
		return d
	}
	// A flat series is drawn at the bottom rather than dividing by zero, which is
	// the honest answer: "this value does not move" is not "this value is huge".
	return 1
}

// level returns v's position within the series' range, in [0, 1]. A NaN is zero.
func (s *Sparkline) level(v float64) float64 {
	if v != v {
		return 0
	}
	return ratioOf(v-s.min, s.span())
}

// styleFor returns the rendition of sample i: the threshold style for a breach,
// then the min, max and last emphases, then the ordinary style.
func (s *Sparkline) styleFor(i int, v float64) buffer.Style {
	if s.ThresholdStyle != (buffer.Style{}) && !s.ThresholdStyle.IsUnset() && s.thresholdArmed() && v >= s.Threshold {
		return s.ThresholdStyle
	}
	if len(s.Values) > 0 {
		if i == len(s.Values)-1 && !s.LastStyle.IsUnset() {
			return s.LastStyle
		}
		if v <= s.min && !s.MinStyle.IsUnset() {
			return s.MinStyle
		}
		if v >= s.max && !s.MaxStyle.IsUnset() {
			return s.MaxStyle
		}
	}
	return s.Style
}

// thresholdArmed reports whether a threshold is actually set: one at or above the
// series' top would mark everything, and one below its bottom nothing, and in both
// cases the caller has not said anything worth showing.
func (s *Sparkline) thresholdArmed() bool {
	return s.Threshold > s.min && s.Threshold <= s.max
}

// drawHorizontal draws the series across the plot's width.
//
// In Braille mode each cell carries two samples with four levels each; otherwise
// each cell carries one sample with eight levels, which is the eighth-block ramp.
func (s *Sparkline) drawHorizontal(buf *buffer.Buffer) {
	p := s.plot
	if p.W < 1 {
		return
	}
	if !s.Braille {
		for i, v := range s.Values {
			x := p.X + i
			if x >= p.Right() {
				break
			}
			buf.SetCell(x, p.Y, s.styleFor(i, v).Resolved().Cell(eighthsRune(s.level(v))))
		}
		return
	}
	for x := 0; x < p.W; x++ {
		bits := 0
		used := false
		for half := 0; half < 2; half++ {
			i := x*2 + half
			if i >= len(s.Values) {
				continue
			}
			v := s.Values[i]
			used = true
			// Four dot rows per column, filled from the bottom: the resolution is
			// four levels per sample and two samples per cell.
			rows := int(s.level(v)*4 + 0.5)
			for r := 0; r < rows; r++ {
				bits |= 1 << brailleBits[half][3-r]
			}
			if rows == 0 && s.level(v) > 0 {
				// A value above zero but below a quarter still has to show: without
				// this the bottom eighth of every range would be indistinguishable
				// from zero, which is the difference between a flat line and a
				// falling one.
				bits |= 1 << brailleBits[half][3]
			}
		}
		if !used || bits == 0 {
			continue
		}
		// The cell takes the style of its LAST sample, so a cell spanning a breach
		// and a normal reading is marked rather than silently dropped.
		idx := x*2 + 1
		if idx >= len(s.Values) {
			idx = len(s.Values) - 1
		}
		buf.SetCell(p.X+x, p.Y, s.styleFor(idx, s.Values[idx]).Resolved().Cell(rune(brailleBase+bits)))
	}
}

// drawVertical draws the series down the plot's height, one sample per row.
//
// There is no sub-cell resolution on this axis — a column of cells is one sample
// each — so the glyph is the eighth-block ramp, which at least makes a partial final
// row visible rather than rounding it away.
func (s *Sparkline) drawVertical(buf *buffer.Buffer) {
	p := s.plot
	for i, v := range s.Values {
		y := p.Y + i
		if y >= p.Bottom() {
			break
		}
		st := s.styleFor(i, v).Resolved()
		for x := 0; x < p.W; x++ {
			buf.SetCell(p.X+x, y, st.Cell(eighthsRune(s.level(v))))
		}
	}
}

var (
	_ termmosaic.Widget      = (*Sparkline)(nil)
	_ termmosaic.Minimizable = (*Sparkline)(nil)
)
