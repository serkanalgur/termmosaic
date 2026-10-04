package viz

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// Local thresholds for Meter, per ADR 0007 §1 rule 5.
const (
	// minMeterW is the narrowest interior showing a zone name, a bar and a value.
	minMeterW = 16
	// minMeterH is the smallest height that shows a meter.
	minMeterH = 1
	// namePrio is the active zone NAME's priority. It outranks the value: "disk
	// critical" is the sentence a user needs, and the number is in the title.
	namePrio = geometry.PrioHigh
	// meterValuePrio is the numeric value's priority against the zone name.
	meterValuePrio = geometry.PrioLow
	// markerRune is the threshold marker: '|' rather than a box-drawing glyph,
	// because it must be one cell wide, unambiguous, and legible on a terminal
	// whose border fell back to ASCII.
	markerRune = '|'
	// zoneEdgeRune separates the zones on the track. It is a difference in GLYPH as
	// well as in style, which is what makes the zone boundaries readable without
	// colour.
	zoneEdgeRune = '+'
)

// Zone is one named band of a Meter's range.
//
// Zones are the whole reason Meter exists and are what separates it from
// ProgressBar: a progress bar says how far along it is, a meter says how much of a
// BUDGET is used and in which band that lands.
type Zone struct {
	// Name is the band's label, drawn as text. It is the colour-independent
	// signal: a meter in the critical zone says the word.
	Name string
	// From and To bound the band on the meter's scale. To <= From means the band
	// runs to the end of the scale, which is how the top zone is written.
	From, To float64
	// Style is the band's rendition in the track, and FillStyle the same band's
	// rendition in the filled part.
	//
	// Two styles rather than one because a zone has to be visible BEFORE the value
	// reaches it — the track shows the shape of the budget, and the fill shows what
	// has been spent of it.
	Style     buffer.Style
	FillStyle buffer.Style
}

// Meter is a bounded value with named zones and a threshold marker.
//
// # The two non-colour signals
//
//  1. The active zone's NAME is drawn as text. "ok", "warn", "critical" is a
//     sentence, and a sentence survives a monochrome terminal and a colour-blind
//     reader, which a hue shift does not.
//  2. The threshold is marked with a '|' glyph, and the zone boundaries with a '+',
//     so the SHAPE of the budget is visible independently of its colour.
//
// # Values
//
// Set takes a value on the meter's own scale and clamps it into [From, To) of the
// first and last zones; a NaN is zero. The default scale is 0..100, which is what
// a percentage-shaped measurement wants and what every default zone below assumes.
type Meter struct {
	blk    *block.Block
	bounds buffer.Rect

	// Zones are the bands, in order. They are laid out over [ScaleMin, ScaleMax];
	// a gap between two zones is drawn as track with no name.
	Zones []Zone

	// ScaleMin and ScaleMax bound the meter's range. ScaleMax <= ScaleMin gives a
	// meter that reads zero rather than dividing by zero.
	ScaleMin, ScaleMax float64

	// Threshold is where the warning marker sits on the scale, and ThresholdStyle
	// its rendition. A threshold outside the scale is ignored rather than clamped
	// onto the end, because a marker pinned to the edge says nothing.
	Threshold      float64
	ThresholdStyle buffer.Style

	// Value is the current reading, clamped into the scale by Set.
	Value float64

	// NameStyle is the active zone name's rendition, ValueStyle the number's, and
	// TrackStyle the unfilled part of the bar's.
	NameStyle  buffer.Style
	ValueStyle buffer.Style
	TrackStyle buffer.Style

	// ShowName and ShowValue toggle the two text regions.
	ShowName  bool
	ShowValue bool

	// ThresholdVisible draws the threshold marker; without it the meter is a
	// three-band bar with no indication of where the line is.
	ThresholdVisible bool

	regions          []geometry.Region
	keep             []bool
	nameW, valueW    int
	bar              buffer.Rect
	cachedRect       buffer.Rect
	mark             rune
	zoneFrom, zoneTo []int // per-zone cell boundaries within the bar, from adapt
}

// NewMeter returns a Meter sized r with the default zones: ok below 70, warn below
// 90, critical above, on a 0..100 scale.
func NewMeter(r buffer.Rect) *Meter {
	m := &Meter{
		blk:              block.New(r),
		bounds:           r,
		ScaleMax:         100,
		Threshold:        90,
		ThresholdVisible: true,
		ShowName:         true,
		ShowValue:        true,
		regions: []geometry.Region{
			{Size: 0, Prio: namePrio},
			{Size: 0, Prio: geometry.PrioAlways},
			{Size: 0, Prio: meterValuePrio},
		},
	}
	m.blk.SetBounds(r)
	m.SetZones(DefaultZones())
	return m
}

// DefaultZones returns the three-band default: ok, warn and critical, on a 0..100
// scale with the warning line at 90.
//
// It is a function rather than a package variable so a caller cannot mutate the
// default out from under every other meter in the process, which is the same
// reasoning behind not shipping a shared Style table.
func DefaultZones() []Zone {
	return []Zone{
		{Name: "ok", From: 0, To: 70},
		{Name: "warn", From: 70, To: 90},
		{Name: "critical", From: 90},
	}
}

// SetZones replaces the bands. Zones are laid out in the order given, and each is
// given the room its share of the scale deserves, so the bands on screen are
// proportional to the ranges they stand for.
func (m *Meter) SetZones(zones []Zone) {
	m.Zones = zones
	m.cachedRect = buffer.Rect{}
}

// Bounds returns the meter's rectangle, safe to call before the first Draw.
func (m *Meter) Bounds() buffer.Rect { return m.bounds }

// SetBounds sets the meter's rectangle.
func (m *Meter) SetBounds(r buffer.Rect) {
	m.bounds = r
	m.blk.SetBounds(r)
}

// Block returns the block that draws this meter's chrome.
func (m *Meter) Block() *block.Block { return m.blk }

// MinSize returns the smallest meter that shows a zone name, a bar and a value: a
// whole-widget size including chrome.
func (m *Meter) MinSize() buffer.Size { return minWhole(m.blk, minMeterW, minMeterH) }

// Set sets the reading, clamped into the scale: below ScaleMin is ScaleMin, above
// ScaleMax is ScaleMax, and a NaN is ScaleMin.
//
// Clamping rather than panicking is the contract. The alternative — drawing a bar
// from a NaN ratio — produces a bar of arbitrary length, which is a lie rather
// than an error.
func (m *Meter) Set(v float64) {
	m.Value = m.clamp(v)
}

// clamp maps v onto the meter's scale.
func (m *Meter) clamp(v float64) float64 {
	lo, hi := m.ScaleMin, m.ScaleMax
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

// ActiveZone returns the index of the zone v falls in, and whether there is one.
// An out-of-range v answers with the nearest zone rather than with -1, because a
// caller asking "which band is this reading in" wants a band, and -1 would push
// the clamping decision back onto every caller.
func (m *Meter) ActiveZone(v float64) (int, bool) {
	if len(m.Zones) == 0 {
		return 0, false
	}
	for i := range m.Zones {
		z := m.Zones[i]
		if v >= z.From && (z.To <= z.From || v < z.To) {
			return i, true
		}
	}
	if v < m.Zones[0].From {
		return 0, true
	}
	return len(m.Zones) - 1, true
}

// Invalidate satisfies termmosaic.Widget: it drops the layout cache so the next
// Draw re-derives it.
func (m *Meter) Invalidate() { m.cachedRect = buffer.Rect{} }

// Handle satisfies termmosaic.Widget. A meter has no focus and no interaction.
func (m *Meter) Handle(termmosaic.Event) bool { return false }

// Draw paints the chrome, the zone name, the bar and the value.
//
// It is total and allocation-free.
func (m *Meter) Draw(buf *buffer.Buffer) {
	r := m.bounds
	if r.Empty() {
		return
	}
	m.blk.Draw(buf)
	in := m.blk.Interior()
	if in.Empty() {
		return
	}
	if in != m.cachedRect {
		m.adapt(in)
	}
	m.drawName(buf, in)
	m.drawValue(buf, in)
	m.drawBar(buf)
}

// adapt recomputes everything derived from the interior, including each zone's
// cell boundary — integer arithmetic only, so no float ever decides which cell a
// band starts in.
func (m *Meter) adapt(in buffer.Rect) {
	m.cachedRect = in
	m.mark = truncMark(m.blk.Ascii)

	// The name region is as wide as the WIDEST zone name, not the current one, so
	// that the bar does not shift horizontally as the reading crosses a boundary.
	// A meter that resized itself every time the zone changed would be unreadable in
	// a dashboard precisely when it matters most.
	nameW := 0
	if m.ShowName {
		for _, z := range m.Zones {
			if w := buffer.StringWidth(z.Name); w > nameW {
				nameW = w
			}
		}
	}
	if len(m.regions) == 3 {
		m.regions[0].Size = nameW
		if m.ShowValue {
			m.regions[2].Size = valueWidth()
		} else {
			m.regions[2].Size = 0
		}
	}
	m.keep = geometry.Budget(m.regions, in.W)
	m.nameW, m.valueW = 0, 0
	if len(m.keep) == 3 {
		if m.keep[0] {
			m.nameW = m.regions[0].Size
		}
		if m.keep[2] {
			m.valueW = m.regions[2].Size
		}
	}
	lead, tail := 0, 0
	if m.nameW > 0 {
		lead = m.nameW + 2
	}
	if m.valueW > 0 {
		tail = m.valueW + 2
	}
	w := in.W - lead - tail
	if w < 0 {
		w = 0
	}
	if w < minBarW && (m.nameW > 0 || m.valueW > 0) {
		w = 0
	}
	m.bar = buffer.Rect{X: in.X + lead, Y: in.Y, W: w, H: in.H}

	// Zone boundaries as cell indices. A band gets the cells its share of the scale
	// deserves, and the rounding remainder goes to the last band, so the zones
	// always cover the bar exactly with no gap and no overlap.
	span := m.ScaleMax - m.ScaleMin
	if span <= 0 {
		span = 1
	}
	if cap(m.zoneFrom) < len(m.Zones)+1 {
		m.zoneFrom = make([]int, len(m.Zones)+1)
		m.zoneTo = make([]int, len(m.Zones)+1)
	}
	m.zoneFrom = m.zoneFrom[:len(m.Zones)+1]
	m.zoneTo = m.zoneTo[:len(m.Zones)+1]
	prev := 0
	for i, z := range m.Zones {
		f := (z.From - m.ScaleMin) / span
		to := m.bar.W
		if z.To > z.From {
			t := (z.To - m.ScaleMin) / span
			to = int(t*float64(m.bar.W) + 0.5)
		}
		if i == len(m.Zones)-1 {
			to = m.bar.W
		}
		if to > m.bar.W {
			to = m.bar.W
		}
		if to < prev {
			to = prev
		}
		m.zoneFrom[i] = prev
		m.zoneTo[i] = to
		prev = to
		_ = f
	}
	m.zoneFrom[len(m.Zones)] = prev
	m.zoneTo[len(m.Zones)] = m.bar.W
}

// drawName paints the active zone's name — the sentence, not the shade.
func (m *Meter) drawName(buf *buffer.Buffer, in buffer.Rect) {
	if m.nameW == 0 {
		return
	}
	i, ok := m.ActiveZone(m.Value)
	if !ok {
		return
	}
	row := buffer.Rect{X: in.X, Y: in.Y, W: m.nameW, H: in.H}
	paintRow(buf, row, []buffer.Span{buffer.NewSpan(m.Zones[i].Name, m.NameStyle)}, 0, m.mark, m.TrackStyle)
}

// drawValue paints the reading right-aligned, as a number rather than a percentage
// sign: a meter is on its own scale, and 85 of 100 is not 85%.
func (m *Meter) drawValue(buf *buffer.Buffer, in buffer.Rect) {
	if m.valueW == 0 {
		return
	}
	row := buffer.Rect{X: in.Right() - m.valueW, Y: in.Y, W: m.valueW, H: in.H}
	fillRow(buf, row, m.TrackStyle)
	putNum(buf, row.X, row.Right(), row.Y, m.Value, 0, m.ValueStyle.Resolved())
}

// drawBar paints the zoned track, the fill up to the reading, the zone boundaries
// and the threshold marker.
func (m *Meter) drawBar(buf *buffer.Buffer) {
	bar := m.bar
	if bar.Empty() {
		return
	}
	fillRow(buf, bar, m.TrackStyle)

	// The track, zone by zone, in each zone's own style: this is what shows the
	// shape of the budget before any of it is spent.
	for i, z := range m.Zones {
		x0, x1 := m.zoneFrom[i], m.zoneTo[i]
		if x1 <= x0 || i >= len(m.zoneTo) {
			continue
		}
		seg := buffer.Rect{X: bar.X + x0, Y: bar.Y, W: x1 - x0, H: bar.H}
		fillRow(buf, seg, z.Style)
	}

	// The fill, in the ACTIVE zone's fill style, so the reading and the band it
	// falls in are the same object rather than two things to correlate.
	ratio := ratioOf(m.clamp(m.Value)-m.ScaleMin, m.ScaleMax-m.ScaleMin)
	fill := int(ratio*float64(bar.W) + 0.5)
	if fill > bar.W {
		fill = bar.W
	}
	if fill > 0 {
		if i, ok := m.ActiveZone(m.Value); ok {
			st := m.Zones[i].FillStyle
			if st.IsUnset() {
				st = m.Zones[i].Style
			}
			seg := buffer.Rect{X: bar.X, Y: bar.Y, W: fill, H: bar.H}
			fillRow(buf, seg, st)
			// The leading cell is the boundary of the reading, drawn as a glyph so
			// the fill's extent is visible where the fill style and the track style
			// are close.
			buf.SetCell(seg.X+fill-1, bar.Y, st.Resolved().Cell(m.readingRune()))
		}
	}

	// Zone boundaries and the threshold marker: both are glyphs, so both are
	// readable with no colour at all.
	edge := m.readingStyle()
	for i := 1; i < len(m.Zones); i++ {
		if x := m.zoneTo[i-1]; x > 0 && x < bar.W {
			buf.SetCell(bar.X+x, bar.Y, edge.Cell(m.edgeRune()))
		}
	}
	if m.ThresholdVisible {
		if x, ok := m.thresholdCell(bar.W); ok {
			buf.SetCell(bar.X+x, bar.Y, m.ThresholdStyle.Resolved().Cell(m.thresholdRune()))
		}
	}
}

// thresholdCell returns the cell the threshold marker sits in, and whether the
// threshold is on the scale at all. A threshold outside the scale draws nothing:
// a marker pinned to an edge would look like a boundary rather than like a
// threshold.
func (m *Meter) thresholdCell(w int) (int, bool) {
	span := m.ScaleMax - m.ScaleMin
	if span <= 0 || w <= 0 {
		return 0, false
	}
	if m.Threshold <= m.ScaleMin || m.Threshold >= m.ScaleMax {
		return 0, false
	}
	x := int((m.Threshold-m.ScaleMin)/span*float64(w) + 0.5)
	if x < 0 {
		x = 0
	}
	if x >= w {
		x = w - 1
	}
	return x, true
}

// thresholdRune returns the threshold marker: '|' on a Unicode terminal and '!'
// on an ASCII one, which is one cell wide on both and says "look here" rather than
// than "boundary" the way '+' does.
func (m *Meter) thresholdRune() rune {
	if m.blk.Ascii {
		return '!'
	}
	return markerRune
}

// edgeRune returns the zone-boundary glyph, '+' on both rungs: it is a cross, and
// a cross has no Unicode counterpart that is one cell wide and unambiguous.
func (m *Meter) edgeRune() rune { return zoneEdgeRune }

// readingRune returns the glyph marking the fill's leading edge: a full block, or
// '#' on an ASCII terminal.
func (m *Meter) readingRune() rune { return fillGlyph(m.blk.Ascii) }

// readingStyle returns the rendition of the fill's edge and of the zone
// boundaries: the active zone's fill style when the reading is inside a zone, and
// the track style otherwise. Both markers then belong to the bar they are drawn on
// rather than to a style the caller forgot to set.
func (m *Meter) readingStyle() buffer.Style {
	if i, ok := m.ActiveZone(m.Value); ok {
		st := m.Zones[i].FillStyle
		if !st.IsUnset() {
			return st.Resolved()
		}
		return m.Zones[i].Style.Resolved()
	}
	return m.TrackStyle.Resolved()
}

// valueWidth returns the cell width of a reading from -1000 to 1000, so the number
// does not shift the bar as it grows.
func valueWidth() int { return len("-1000") }

var (
	_ termmosaic.Widget      = (*Meter)(nil)
	_ termmosaic.Minimizable = (*Meter)(nil)
)
