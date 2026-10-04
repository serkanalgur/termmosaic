package viz

import (
	"math"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds for Gauge, per ADR 0007 §1 rule 5.
const (
	// minGaugeW and minGaugeH are the interior sizes at which a dial is drawn rather
	// than a bar. A Braille cell is 2 dots across and 4 down, so a round dial needs
	// about twice the cells across as it has down.
	minGaugeW = 9
	minGaugeH = 5
	// minGaugeBarW is the narrowest interior that draws the fallback bar.
	minGaugeBarW = 6
	// minGaugeBarH is the smallest height that draws the fallback bar.
	minGaugeBarH = 1
	// needleTol is how far from the reading, as a fraction of a turn, a dot may be
	// and still count as part of the needle. At four dots to a cell it is about one
	// dot wide, which is as thin as the needle can be drawn and still be continuous.
	needleTol = 0.012
)

// dialParts selects what a Gauge draws inside its ring.
type dialParts uint8

const (
	// dialArc draws the filled arc from twelve o'clock clockwise.
	dialArc dialParts = 1 << iota
	// dialNeedle draws the needle from the hub to the reading.
	dialNeedle
)

// Gauge is a single bounded value on a dial, with a bar fallback.
//
// # Why a dial, and what one costs
//
// A Braille cell is a 2-by-4 dot grid, so a dial is drawn by testing each of a
// rectangle's dots against the ring's inner and outer radii and against the swept
// angle. That is real geometry rather than a bar drawn twice, and it is why a Gauge
// is worth having where a ProgressBar is not: the needle's POSITION carries the
// value, which no colour and no amount of bar length can improve on.
//
// # The fallback, and why it exists
//
// Below dialMinW by dialMinH the interior cannot hold a circle — at three cells tall
// a dial would be a smear — so Gauge draws a horizontal bar with the value printed
// beside it. That is not a lesser widget: it is the same reading, and ADR 0007 §4
// asks for a widget to draw its minimum layout clipped rather than to blank itself.
//
// # Values
//
// Set takes a value on the gauge's own scale and clamps it: below ScaleMin is
// ScaleMin, above ScaleMax is ScaleMax, and a NaN is ScaleMin. ScaleMax <=
// ScaleMin reads as empty rather than dividing by zero.
type Gauge struct {
	blk    *block.Block
	bounds buffer.Rect

	// Value is the current reading, clamped by Set.
	Value float64
	// ScaleMin and ScaleMax bound the reading.
	ScaleMin, ScaleMax float64

	// Label is the text drawn beside the dial or bar.
	Label      []buffer.Span
	LabelStyle buffer.Style
	// ShowLabel and ShowValue toggle the two text regions.
	ShowLabel bool
	ShowValue bool
	// ValueStyle is the number's rendition.
	ValueStyle buffer.Style

	// ArcStyle is the filled arc's and needle's rendition; TrackStyle the dial's
	// unfilled ring's and the fallback bar's track.
	ArcStyle   buffer.Style
	TrackStyle buffer.Style
	// Background is painted across Bounds by the block.

	// Dial selects which parts of the dial are drawn. Zero means both.
	Dial dialParts

	cachedRect buffer.Rect
	dial       buffer.Rect
	bar        buffer.Rect
	textRows   buffer.Rect
	mark       rune
}

// NewGauge returns a Gauge sized r reading zero on a 0..100 scale.
func NewGauge(r buffer.Rect) *Gauge {
	g := &Gauge{
		blk:       block.New(r),
		bounds:    r,
		ScaleMax:  100,
		ShowLabel: true,
		ShowValue: true,
		Dial:      dialArc | dialNeedle,
	}
	g.blk.SetBounds(r)
	return g
}

// Bounds returns the gauge's rectangle, safe to call before the first Draw.
func (g *Gauge) Bounds() buffer.Rect { return g.bounds }

// SetBounds sets the gauge's rectangle.
func (g *Gauge) SetBounds(r buffer.Rect) {
	g.bounds = r
	g.blk.SetBounds(r)
}

// Block returns the block that draws this gauge's chrome, so a caller can configure
// the border, title, padding, background and ASCII rung. A dial needs the ASCII
// rung more than any other widget here: Braille outside U+2800 renders as nothing,
// so an ASCII terminal would show an empty circle unless the fallback is chosen.
func (g *Gauge) Block() *block.Block { return g.blk }

// MinSize returns the smallest gauge that shows a dial with a value beside it: a
// whole-widget size including chrome.
func (g *Gauge) MinSize() buffer.Size {
	// The dial needs its own minimum, not the bar's: a gauge that reported the bar's
	// minimum would be handed a 5-by-2 rect and told it could draw a dial.
	return minWhole(g.blk, dialMinW, dialMinH)
}

// Reading returns the current value, clamped into the scale.
func (g *Gauge) Reading() float64 { return g.clamp(g.Value) }

// Set sets the reading, clamped into the scale: below ScaleMin is ScaleMin, above
// ScaleMax is ScaleMax, and a NaN is ScaleMin.
func (g *Gauge) Set(v float64) { g.Value = g.clamp(v) }

// SetLabel sets the label from a single styled run.
func (g *Gauge) SetLabel(s string, st buffer.Style) {
	g.Label = []buffer.Span{buffer.NewSpan(s, st)}
	g.cachedRect = buffer.Rect{}
}

// Ratio returns the reading as a fraction of the scale in [0, 1], which is what
// the dial's sweep and the bar's fill both consume.
func (g *Gauge) Ratio() float64 {
	return ratioOf(g.clamp(g.Value)-g.ScaleMin, g.ScaleMax-g.ScaleMin)
}

// clamp maps v onto the gauge's scale.
func (g *Gauge) clamp(v float64) float64 {
	lo, hi := g.ScaleMin, g.ScaleMax
	if hi < lo {
		hi = lo
	}
	if v != v {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Invalidate satisfies termmosaic.Widget: it drops the layout cache so the next
// Draw re-derives it.
func (g *Gauge) Invalidate() { g.cachedRect = buffer.Rect{} }

// Handle satisfies termmosaic.Widget. A gauge has no focus and no interaction.
func (g *Gauge) Handle(termmosaic.Event) bool { return false }

// Draw paints the chrome and then either the dial or the fallback bar.
//
// It is total and allocation-free. Which of the two it draws is decided in adapt,
// from the rect, so the decision costs one comparison per frame rather than a
// branch tree in the painter.
func (g *Gauge) Draw(buf *buffer.Buffer) {
	r := g.bounds
	if r.Empty() {
		return
	}
	g.blk.Draw(buf)
	in := g.blk.Interior()
	if in.Empty() {
		return
	}
	if in != g.cachedRect {
		g.adapt(in)
	}
	if !g.bar.Empty() {
		g.drawBar(buf)
	} else {
		g.drawDial(buf)
	}
	g.drawText(buf)
}

// adapt recomputes everything derived from the interior: whether there is room for
// a dial, where it goes, and where the text goes beside it.
func (g *Gauge) adapt(in buffer.Rect) {
	g.cachedRect = in
	g.mark = truncMark(g.blk.Ascii)

	// The text sits on its own row when there is a row to spare, and to the right of
	// the dial when there is not. That is the whole responsive story for this
	// widget: a gauge is a dial plus a number, and at each size one of the two has
	// to give way.
	textH := 0
	if in.H > minGaugeH {
		textH = in.H - minGaugeH
		if textH > 1 {
			textH = 1
		}
	}

	dialW := in.W
	dialH := in.H - textH
	if textH == 0 {
		// The text goes beside the dial, so the dial gets the rest of the width.
		tw := g.textWidth()
		if tw > 0 && in.W > minGaugeW+tw {
			dialW = in.W - tw
		}
	}
	if g.blk.Ascii || dialW < minGaugeW || dialH < minGaugeH {
		// Too small for a dial, or a terminal that cannot draw one: a bar, with the
		// text beside it rather than below, because a one-row bar with a row of text
		// under it wastes the height a caller gave the widget.
		tw := g.textWidth()
		bw := in.W
		if tw > 0 && in.W > minGaugeBarW+tw {
			bw = in.W - tw
		}
		if bw < 1 {
			bw = in.W
		}
		g.bar = buffer.Rect{X: in.X, Y: in.Y, W: bw, H: 1}
		g.textRows = buffer.Rect{X: in.X + bw, Y: in.Y, W: in.W - bw, H: in.H}
		g.dial = buffer.Rect{}
		return
	}
	g.bar = buffer.Rect{}
	g.dial = buffer.Rect{X: in.X, Y: in.Y, W: dialW, H: dialH}
	if textH > 0 {
		g.textRows = buffer.Rect{X: in.X, Y: in.Y + dialH, W: in.W, H: textH}
	} else {
		g.textRows = buffer.Rect{X: in.X + dialW, Y: in.Y, W: in.W - dialW, H: in.H}
	}
}

// textWidth returns the cell width the label and the value need together.
func (g *Gauge) textWidth() int {
	w := 0
	if g.ShowLabel {
		w += buffer.SpansWidth(g.Label) + 1
	}
	if g.ShowValue {
		w += gaugeValueWidth
	}
	return w
}

// gaugeValueWidth is the width of a printed reading from -1000 to 1000, so the
// dial does not resize itself as the number grows.
const gaugeValueWidth = len("-1000")

// drawDial draws the ring, the filled arc and the needle in Braille.
//
// Each cell is assembled dot by dot and written as one rune, which is what gives a
// 20-cell-wide dial a resolution of 40 dots across: no amount of block-element work
// gets there, and that is the entire reason this widget uses Braille.
func (g *Gauge) drawDial(buf *buffer.Buffer) {
	d := g.dial
	if d.Empty() {
		return
	}
	// Centre in DOT space, because that is the space the ring lives in: a cell is
	// two dots across and four down, which is why a round dial needs about twice the
	// cells across as it has down.
	dcx := float64(d.W*2) / 2
	dcy := float64(d.H*4) / 2
	rOut := dcx
	if lim := dcy * 0.5; rOut > lim {
		rOut = lim
	}
	if rOut < 1 {
		return
	}
	rIn := rOut * dialInnerRatio
	rGap := rOut * dialGapRatio

	ratio := g.Ratio()
	arc, needle := g.Dial&dialArc != 0, g.Dial&dialNeedle != 0
	if g.Dial == 0 {
		// The zero Dial means "both", so a caller that never touches the field gets
		// a dial with a needle rather than an empty ring.
		arc, needle = true, true
	}
	fill := g.ArcStyle.Resolved()
	track := g.TrackStyle.Resolved()

	for cyi := 0; cyi < d.H; cyi++ {
		for cxi := 0; cxi < d.W; cxi++ {
			arcBits, trackBits := 0, 0
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					px := float64(cxi*2+dx) + 0.5 - dcx
					py := float64(cyi*4+dy) + 0.5 - dcy
					d2 := px*px + py*py
					if d2 > rOut*rOut || d2 < rIn*rIn {
						continue
					}
					bit := 1 << brailleBits[dx][dy]
					turn := dotTurn(px, py)
					// The sweep runs clockwise from twelve o'clock, so a dot is
					// "before" the reading when its turn is at or below the sweep.
					if arc && turn <= ratio {
						arcBits |= bit
						continue
					}
					if needle && d2 >= rGap*rGap && nearAngle(turn, ratio, needleTol) {
						arcBits |= bit
						continue
					}
					trackBits |= bit
				}
			}
			if trackBits != 0 {
				buf.SetCell(d.X+cxi, d.Y+cyi, track.Cell(rune(brailleBase+trackBits)))
			}
			if arcBits != 0 {
				// Written second so the arc wins where both land in the same cell:
				// the filled part is what the reading is, and a cell showing the track
				// over it would under-report the value.
				buf.SetCell(d.X+cxi, d.Y+cyi, fill.Cell(rune(brailleBase+arcBits)))
			}
		}
	}
}

// dotTurn returns a dot's position as a fraction of a turn clockwise from twelve
// o'clock, in [0, 1).
//
// The obvious hand-rolled version — two divisions and a sign test — is WRONG, and
// wrong in the way that matters here: it approximates the angle linearly between the
// axes, so a dot at one o'clock would read as half past three, and the ARC LENGTH of
// the dial would stop being proportional to the reading. A gauge whose arc does not
// mean what it says is worse than a bar. So this calls math.Atan2, once per ring
// dot: a 21-by-9 dial is about a hundred ring dots, which is a microsecond.
//
// The axis convention is the screen's: twelve o'clock is -y, and clockwise runs
// toward +x.
func dotTurn(px, py float64) float64 {
	turn := math.Atan2(px, -py) / twoPi
	if turn < 0 {
		turn++
	}
	if turn >= 1 {
		turn = 0
	}
	return turn
}

// twoPi is the denominator of a turn in radians, named so the division above reads
// as an angle rather than as a magic number.
const twoPi = 2 * math.Pi

// nearAngle reports whether turn is within tol of target, wrapping at 1.
func nearAngle(turn, target, tol float64) bool {
	d := turn - target
	if d < 0 {
		d = -d
	}
	if d > 0.5 {
		d = 1 - d
	}
	return d <= tol
}

// drawBar draws the fallback: a horizontal bar with the reading's position marked
// by a glyph, for an interior too small for a dial.
func (g *Gauge) drawBar(buf *buffer.Buffer) {
	b := g.bar
	if b.Empty() {
		return
	}
	fillRow(buf, b, g.TrackStyle)
	r := g.Ratio()
	fill := int(r*float64(b.W) + 0.5)
	if fill > b.W {
		fill = b.W
	}
	if fill > 0 {
		// The fill is written as GLYPHS rather than as a filled rectangle: a style's
		// background would paint spaces, and a bar made of spaces is not a bar.
		st := g.ArcStyle.Resolved().Cell(fillGlyph(g.blk.Ascii))
		for i := 0; i < fill; i++ {
			buf.SetCell(b.X+i, b.Y, st)
		}
		// The needle becomes the fill's last cell, drawn as a distinct glyph so the
		// reading's exact position is marked even where the fill and track styles are
		// close, or where there is no colour at all.
		buf.SetCell(b.X+fill-1, b.Y, g.ArcStyle.Resolved().Cell(g.needleRune()))
	}
}

// needleRune returns the reading's marker on the fallback bar.
func (g *Gauge) needleRune() rune {
	if g.blk.Ascii {
		return 'o'
	}
	return '▸'
}

// drawText paints the label and the value beside or below the dial.
//
// The number is the accessibility story: a dial whose only difference between 30
// and 90 is an arc length is unreadable to anyone who cannot resolve the arc, and a
// dial is exactly the widget where that is likely.
func (g *Gauge) drawText(buf *buffer.Buffer) {
	t := g.textRows
	if t.Empty() {
		return
	}
	fillRow(buf, t, g.TrackStyle)
	x := t.X
	if g.ShowLabel {
		x = buf.SetSpansCappedIn(x, t.Right(), t.Y, g.Label, g.mark)
		x++
	}
	if g.ShowValue {
		putNum(buf, x, t.Right(), t.Y, g.Reading(), 0, g.ValueStyle.Resolved())
	}
}

var (
	_ termmosaic.Widget      = (*Gauge)(nil)
	_ termmosaic.Minimizable = (*Gauge)(nil)
)
