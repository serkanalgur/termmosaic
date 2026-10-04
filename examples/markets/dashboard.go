package main

// The screen: three bands of widgets, one layout decision per width, and a Draw
// that only reads.
//
// # The bands
//
//  1. KPI ROW — four headline figures side by side: the spot rate, the day-on-day
//     change, and the window's two extremes. Between them they answer "where is
//     it, which way is it going, and how far has it come" without the reader
//     cross-referencing anything.
//  2. TIME SERIES — a Sparkline of the primary pair over the fetched window, a
//     Gauge showing where the spot sits inside that window's range, and a Meter
//     of the day's move in basis points against named bands.
//  3. DETAIL — a Table of every tracked pair with its rate and signed change, and
//     a BarChart of the largest moves.
//
// # What changes as the terminal narrows
//
// Three widths, declared as local named constants per ADR 0007 §1 rule 5 — they
// are this application's product decisions and the framework deliberately has no
// vocabulary for them:
//
//	W >= wideW (108)   every panel, side by side
//	W >= midW  (86)    the series band STACKS: the gauge and meter move beneath
//	                   the sparkline, and the detail band keeps the table but gives
//	                   the chart its own column
//	W >= narrowW (58)  one panel per band: the sparkline alone, the table alone,
//	                   and the KPI row cut to two figures by geometry.ClampCount
//	W <  narrowW       a one-line diagnostic, never a clipped dashboard
//
// The middle band is the one that matters and the one the layout test pins: the
// panels MOVE, they do not merely get narrower. Asserting on "the screen changed"
// would pass for a layout that had merely reflowed its text; asserting that the
// gauge's rectangle is now BELOW the sparkline's rather than beside it cannot.
//
// # Why Draw allocates nothing
//
// Three rules, and all three are load-bearing:
//
//   - Every string is formatted in buildFrame, off the draw path (see view.go).
//   - Everything derived from the rectangle — which band arrangement applies, how
//     many KPI tiles fit, each band's rectangle — is computed in adapt, keyed on
//     the rectangle, and recomputed only when it differs. That is ADR 0007 §3's
//     lazy rule, and it is where the layout's one layout.Solve lives.
//   - Draw reads those cached rectangles and calls the widgets' Draw. Nothing
//     here formats, wraps, truncates, appends or allocates.

import (
	"strconv"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/data"
	"github.com/serkanalgur/termmosaic/widgets/split"
	"github.com/serkanalgur/termmosaic/widgets/viz"
)

// The breakpoints, and the band's own geometry. Every one of these is a local
// constant: ADR 0007 §1 rule 5 says a widget's thresholds are its own policy, and
// a shared breakpoint constant would be a second source of truth for space that
// already has one — the rect.
const (
	// narrowW is below which the screen is one panel per band. It is also the
	// width MinSize reports, so "too small for this" and "too small for anything"
	// are one threshold rather than two that could disagree. It is derived from
	// kpiTileW: two tiles plus a gap, which is the narrowest KPI row that still
	// shows the figures a reader came for.
	narrowW = 2*kpiTileW + bandGap

	// midW is where the series band gains the ability to stack rather than
	// fitting everything in a row.
	midW = 86

	// wideW is where the detail band's table and chart both fit side by side with
	// room for their content, which is the arrangement the example is designed
	// around and the one the wide golden captures.
	wideW = 108

	// minH is the shortest screen that shows all three bands at once: the KPI row,
	// a sparkline, the table's own minimum, and the two gaps between them, plus the
	// footer row.
	minH = kpiH + bandGap + 5 + bandGap + 4 + 1

	// bandGap is the cells between two panels. Panels are bordered, so a gap of
	// zero would butt two borders together and read as one panel with a seam.
	bandGap = 1

	// kpiH is the KPI row's height: a top border, the label, the value, the note,
	// and a bottom border. Fixed rather than solved because a KPI row that changed
	// height as the terminal narrowed would reflow every panel below it, and a
	// dashboard whose numbers move when you resize it is one nobody trusts.
	//
	// It is the tile's own requirement, which is why statTile.MinSize reports the
	// same number: a row one cell shorter would clip every tile's NOTE, which is
	// where the arrow and the provenance live.
	kpiH = 5

	// kpiTileW is the narrowest a KPI tile is worth drawing. Below it the tile cannot
	// hold "window position" and a rate under its title, so the row shows FEWER
	// tiles rather than narrower ones. It is 24 rather than 20 because a tile whose
	// note truncates to "2026-09…" has thrown away the one thing the note is for,
	//	// which is saying WHEN.
	kpiTileW = 24
)

// band is one of the three horizontal strips of the screen.
type band uint8

const (
	bandKPI band = iota
	bandSeries
	bandDetail
	numBands
)

// layoutCache is everything derived from the screen's rectangle.
//
// One struct, one key. Every field in it is a function of the rect and of the
// frame's SHAPE (how many series samples, how many rows) and nothing else, which
// is what makes keying the cache on the rect sufficient — ADR 0007 §3 and its
// amendment about caches keyed on the wrong thing. The KPI tile count depends on
// the rect only, because it is a function of the width; the series band depends on
// the series' existence, which adapt re-derives only when the rect changes, and
// which SetFrame invalidates by resetting the key.
type layoutCache struct {
	// key is the rectangle this was derived from. valid distinguishes "derived
	// from the zero rect" from "derived from a zero-size rect", which are the
	// same value and different answers.
	key   buffer.Rect
	valid bool

	// arr is the band arrangement: wide, stacked, or single.
	arr arrangement

	// tiles is how many KPI tiles fit, from geometry.ClampCount.
	tiles int

	// showSeries and showDetail are the height budget's answer. A short terminal
	// drops the series band before the table, because the table is the dashboard
	// and the sparkline is a summary of it.
	showSeries bool
	showDetail bool
}

// kpiTilesFor returns how many kpiTileW-wide tiles fit across w cells, gaps
// included.
//
// A row of n tiles needs n*kpiTileW + (n-1)*bandGap, and the widest n satisfying
// that is the answer. It is computed by growing one tile at a time rather than by
// dividing, because the gap term makes a closed form awkward and the count is at
// most numTiles — a bound this package declares, so the loop is bounded by
// construction rather than by hope.
func kpiTilesFor(w int) int {
	n := 0
	for k := 1; k <= numTiles; k++ {
		if k*kpiTileW+(k-1)*bandGap > w {
			break
		}
		n = k
	}
	return n
}

// arrangement is which of the three layouts is in force.
type arrangement uint8

const (
	// arrSingle is one panel per band: sparkline, table, two KPI figures.
	arrSingle arrangement = iota
	// arrStacked puts the gauge and meter beneath the sparkline, and keeps the
	// table and chart side by side.
	arrStacked
	// arrWide is everything side by side, which is the designed arrangement.
	arrWide
)

// dashboard is the whole screen.
//
// It is a plain termmosaic.Widget rather than a container widget from the catalog:
// the catalog has no dashboard, and inventing one would put an application
// decision in the framework. What it composes IS the catalog — block, basic, data,
// viz, split — and the split is what carries the nesting, so there is exactly one
// composition mechanism in this example rather than two.
type dashboard struct {
	bounds buffer.Rect

	// bands is the vertical stack. It is a Split rather than a hand-rolled solver
	// because widgets/split already solved "arrange along one axis, hand each
	// child a rect", and a second such mechanism inside an example is how two
	// start to disagree about overflow.
	bands *split.Split

	// The KPI row is a Split of statTile panes.
	kpi *split.Split
	// The series band's panes: the sparkline, the gauge and the meter.
	series *split.Split
	// The detail band's panes: the table and the chart.
	detail *split.Split

	// The pane lists the arrangements choose between, held so that a panel the
	// narrow arrangement dropped can be RESTORED. They are fields rather than
	// literals in configureChildren for the reason the comment there gives: a
	// dropped pane left out of a later Panes call would never come back, and the
	// screen would stay narrow forever however wide the terminal became.
	seriesPanes []termmosaic.Widget
	detailPanes []termmosaic.Widget
	sparkOnly   []termmosaic.Widget
	tableOnly   []termmosaic.Widget

	// The widgets. Held as named fields rather than a slice because each one
	// needs its own configuration and its own Draw, and an index into a slice
	// would be a way to draw the gauge where the meter belongs.
	spark *viz.Sparkline
	gauge *viz.Gauge
	meter *viz.Meter
	table *data.Table
	chart *viz.BarChart
	stat  [numTiles]*statTile

	// status is the footer: a single row of pre-formatted text.
	status *basic.Text

	// diag is the below-minimum diagnostic. It is a widget rather than a
	// hand-drawn string so that it truncates through buffer's shared writers,
	// which is what makes it legible at 20x8 instead of an ellipsis soup.
	diag *basic.Text

	// noData is the panel shown when a fetch has never succeeded. It is a Block
	// with the reason inside it, which is what "degrade visibly" has to mean: the
	// screen says what went wrong, in the space where data would be.
	noDataBlk   *block.Block
	noDataTitle *basic.Text
	noDataBody  *basic.Paragraph

	// cur is the frame the widgets were last fed, and curGen the store generation
	// it came from. Held so that the heartbeat can tell a republish of the frame
	// already applied from a genuinely new one.
	cur    *frame
	curGen uint64

	// statusText is the footer's current string, and noDataShown the failure panel's,
	// both held so that a republish of unchanged text costs a string comparison
	// rather than a SetText — which copies, and would allocate on the heartbeat.
	statusText  string
	noDataShown string

	lay layoutCache

	// regions is the vertical budget table, held so that adapt patches it rather
	// than building a fresh one per size change.
	regions [numBands]geometry.Region
}

// newDashboard builds the screen at size (w, h) with an empty frame.
//
// Everything size-independent is built here: the chrome, the columns, the zones,
// the panes. The only values derived from a size are in layoutCache, and those are
// computed by adapt on the first Draw at a new rectangle.
func newDashboard(w, h int) *dashboard {
	d := &dashboard{bounds: buffer.Rect{W: w, H: h}, cur: &frame{}}

	d.spark = viz.NewSparkline(buffer.Rect{})
	framePanel(d.spark.Block(), "EUR/USD, last "+strconv.Itoa(windowDays)+"d")
	d.spark.Braille = true
	d.spark.Auto = true
	d.spark.Style = stSpark
	d.spark.MinStyle = stSparkLow
	d.spark.MaxStyle = stSparkHigh
	d.spark.LastStyle = stAccent
	d.spark.TrackStyle = stTrack

	d.gauge = viz.NewGauge(buffer.Rect{})
	framePanel(d.gauge.Block(), "spot in window")
	d.gauge.ArcStyle = stAccent
	d.gauge.TrackStyle = stTrack
	d.gauge.ValueStyle = stFigure
	d.gauge.SetLabel("window position", stMuted)

	// The meter's zones are its whole point, so they are named rather than
	// coloured: a reader with no colour perception still reads "down", "flat" or
	// "up" off the same cell.
	d.meter = viz.NewMeter(buffer.Rect{})
	framePanel(d.meter.Block(), "1d move, bps")
	d.meter.ScaleMin, d.meter.ScaleMax = meterLow, meterHigh
	d.meter.SetZones([]viz.Zone{
		{Name: "down", From: meterLow, To: -meterFlat, Style: stTrack, FillStyle: stDown},
		{Name: "flat", From: -meterFlat, To: meterFlat, Style: stTrack, FillStyle: stFlatFill},
		{Name: "up", From: meterFlat, To: meterHigh, Style: stTrack, FillStyle: stUp},
	})
	d.meter.Threshold = 0
	d.meter.ThresholdStyle = stThreshold
	d.meter.NameStyle = stMuted
	d.meter.ValueStyle = stFigure
	d.meter.TrackStyle = stTrack

	d.table = data.NewTable(buffer.Rect{}, tableColumns()...)
	framePanel(d.table.Block(), "pairs")
	d.table.Header = true
	d.table.ItemStyle = stPanel
	d.table.SelectedStyle = stSelected
	d.table.ScrollbarStyle = stAccent

	d.chart = viz.NewBarChart(buffer.Rect{})
	framePanel(d.chart.Block(), "largest moves, |bps|")
	// Horizontal, because the labels are currency tickers plus an arrow and a
	// vertical chart would either truncate them to nothing or rotate them.
	d.chart.Vertical = false
	d.chart.BarStyle = stAccent
	d.chart.AxisStyle = stEdge
	d.chart.LabelStyle = stMuted
	d.chart.ValueStyle = stFigure
	d.chart.ShowValue = true

	for i := range d.stat {
		d.stat[i] = newStatTile(tileLabel(i))
	}

	// The footer starts EMPTY rather than empty-looking: SetFrame is what fills it,
	// and a dashboard constructed but not yet fed would otherwise show a stale
	// status from a previous board.
	d.status = basic.NewTextString(buffer.Rect{}, "", stMuted)
	d.status.SetBackground(stBody)

	d.diag = basic.NewTextString(buffer.Rect{}, tooSmallText, stMuted)
	d.diag.SetBackground(stBody)

	d.noDataBlk = block.New(buffer.Rect{})
	d.noDataBlk.SetBorder(buffer.BorderPlain)
	d.noDataBlk.SetBorderStyle(stEdge)
	d.noDataBlk.SetBackground(stPanel)
	// "no market data" rather than "no data": the first word on the panel has to be
	// the one that says WHAT is missing, because the transport error follows it on
	// the next rows and a reader scanning the panel reads the first line.
	d.noDataTitle = basic.NewTextString(buffer.Rect{}, "no market data", stTitle)
	d.noDataTitle.SetBackground(stPanel)
	d.noDataBody = basic.NewParagraphString(buffer.Rect{}, noDataReason, stMuted)
	d.noDataBody.SetBackground(stPanel)

	// The nesting. bands stacks the three rows; each row is itself a Split, so
	// the whole screen is one composition mechanism applied three times rather
	// than one Split plus a bespoke second path.
	kpiPanes := make([]termmosaic.Widget, numTiles)
	for i := range d.stat {
		kpiPanes[i] = d.stat[i]
	}
	d.seriesPanes = []termmosaic.Widget{d.spark, d.gauge, d.meter}
	d.detailPanes = []termmosaic.Widget{d.table, d.chart}
	d.sparkOnly = []termmosaic.Widget{d.spark}
	d.tableOnly = []termmosaic.Widget{d.table}

	d.kpi = split.New(layout.Horizontal, kpiPanes...)
	d.kpi.SetSpacing(bandGap)
	d.kpi.SetBackground(stBody)

	d.series = split.New(layout.Horizontal, d.seriesPanes...)
	d.series.SetSpacing(bandGap)
	d.series.SetBackground(stBody)

	d.detail = split.New(layout.Horizontal, d.detailPanes...)
	d.detail.SetSpacing(bandGap)
	d.detail.SetBackground(stBody)

	d.bands = split.New(layout.Vertical, d.kpi, d.series, d.detail)
	d.bands.SetSpacing(bandGap)
	d.bands.SetBackground(stBody)

	d.applyRung()
	return d
}

// applyRung passes the terminal's Unicode capability to every block in the screen.
//
// It is a separate pass rather than something framePanel does, because framePanel
// runs before setRung has necessarily been called and because the ASCII flag is a
// property of the TERMINAL rather than of any one panel. Doing it in one place also
// means a panel added later cannot forget it — which is the failure mode of a
// convention, and the reason this is a loop over the blocks rather than a line in
// each constructor.
func (d *dashboard) applyRung() {
	for _, b := range []*block.Block{
		d.spark.Block(), d.gauge.Block(), d.meter.Block(),
		d.table.Block(), d.chart.Block(), d.noDataBlk,
	} {
		b.Ascii = asciiRung
	}
	for i := range d.stat {
		d.stat[i].blk.Ascii = asciiRung
	}
}

// Bounds returns the screen's rectangle.
func (d *dashboard) Bounds() buffer.Rect { return d.bounds }

// MinSize returns the smallest rectangle in which every band is worth drawing.
//
// It is a computed value rather than a bare constant because it is the sum of the
// bands' own declared minimums plus the gaps, and hard-coding the sum is how the
// two drift apart — a band that grew would leave MinSize claiming a size the
// screen can no longer honour, and every caller above it would be misled.
func (d *dashboard) MinSize() buffer.Size {
	// Every component, from its owner's own minimum rather than from a number
	// copied out of it, and in the NARROWEST arrangement's terms — because that is
	// the one that has to fit at the narrowest width.
	//
	// The heights are the same three terms the height budget declares, gaps
	// included, plus the footer row. Deriving both from one place is what keeps them
	// from drifting: a MinSize claiming a height the budget would not honour would
	// put the screen above its own minimum and still drawing the diagnostic, which
	// is the one state an application cannot reason about.
	h := (kpiH + bandGap) + (d.seriesHeightFor(arrSingle) + bandGap) + d.detailHeight() + 1
	return buffer.Size{W: narrowW, H: h}
}

// Invalidate satisfies termmosaic.Widget. The dashboard repaints its whole
// rectangle every frame, so there is no finer-grained state to mark.
func (d *dashboard) Invalidate() {}

// Handle satisfies termmosaic.Widget. The dashboard routes a resize to SetBounds
// so that a caller may drive it through the ordinary event path; everything else
// belongs to the widgets, and the only key the application itself claims is the
// one that quits.
func (d *dashboard) Handle(ev termmosaic.Event) bool {
	if ev.Kind == termmosaic.EventResize {
		d.SetBounds(buffer.Rect{W: ev.Size.W, H: ev.Size.H})
		return true
	}
	return false
}

// SetBounds resizes the screen and drops the layout cache, so the next Draw
// re-derives the arrangement from the new rectangle (ADR 0007 §3).
func (d *dashboard) SetBounds(r buffer.Rect) {
	d.bounds = r
	d.lay.valid = false
}

// SetFrame feeds new data to the widgets.
//
// It runs on the render goroutine — the fetch goroutine hands the snapshot over
// through render.Renderer.Post — and it is the only place widget content changes.
// Everything it writes is derived from the frame, which is immutable, so there is
// no window in which two halves of the screen disagree about the numbers.
//
// The status line is set here rather than drawn from d.cur in Draw, and that is
// deliberate: SetSpans COPIES, so a footer built in Draw would allocate a span
// slice on every frame. Building it when the data changes and reading it in Draw
// is what keeps the frame path free, and it is why the footer has to be
// re-published on the heartbeat — see the note on the heartbeat in main.go.
//
// The layout cache is invalidated here rather than left keyed on the rect alone,
// because a frame can change the SHAPE of the content (no series at all, no rows)
// without the rectangle changing at all. ADR 0007's amendment names exactly this
// hazard: a cache keyed only on its rect is permanently wrong for a field change,
// because nothing will ever produce a different rect to invalidate it.
func (d *dashboard) SetFrame(f *frame) {
	// A republish of the generation already applied is skipped. That is what the
	// one-second heartbeat does, and skipping it is what keeps the heartbeat from
	// re-normalising the table fourteen rows a second for nothing.
	//
	// The generation rather than the frame pointer is the key, because the heartbeat
	// rebuilds the frame every tick from the same snapshot: a pointer comparison
	// would miss every one of them.
	if d.cur != nil && d.curGen == f.gen {
		return
	}
	d.cur = f
	d.curGen = f.gen

	d.spark.SetValues(f.series)
	// The sparkline's normalisation is cached against the interior, and a new
	// series with the same length and a different range is a different
	// normalisation. Invalidating is what makes the next Draw recompute it.
	d.spark.Invalidate()

	d.gauge.ScaleMin = f.gaugeLo
	d.gauge.ScaleMax = f.gaugeHi
	d.gauge.Set(f.gaugeV)
	d.gauge.Invalidate()

	d.meter.Set(f.meterV)
	d.meter.Invalidate()

	d.table.SetRows(f.rows)
	d.chart.SetData(f.chart)
	// Both widgets derive their column widths from their content and cache them
	// against the interior, so a new set of rows or a new longest label is a new
	// layout.
	d.table.Invalidate()
	d.chart.Invalidate()

	for i := range d.stat {
		d.stat[i].set(f.tiles[i])
	}

	// The footer, from the same pre-formatted string the frame carries. Only when it
	// changed, because SetSpans copies.
	if f.status != d.statusText {
		d.statusText = f.status
		d.status.SetText(f.status, statusStyle(f))
	}

	d.setNoData(f)
	d.lay.valid = false
}

// statusStyle is the footer's one style decision: a screen carrying a failure is
// styled differently from a healthy one, which is emphasis on a footer whose TEXT
// already says which of the two it is.
func statusStyle(f *frame) buffer.Style {
	if f.stale {
		return stWarn
	}
	return stMuted
}

// setNoData fills the failure panel.
//
// It writes the reason into the panel rather than only into the footer because the
// footer is one row at the bottom of a screen the reader may never look at, and a
// dashboard whose only explanation of itself is in the corner is a dashboard that
// will be read as broken rather than as offline.
func (d *dashboard) setNoData(f *frame) {
	if !f.noData {
		d.noDataBody.SetText(noDataReason, stMuted)
		return
	}
	// The reason is set once, at construction, and only the ERROR changes here. A
	// failure panel whose text is rebuilt on every heartbeat would allocate a
	// wrapped paragraph every second for a string that has not changed.
	if f.noDataBody == d.noDataShown {
		return
	}
	d.noDataShown = f.noDataBody
	d.noDataBody.SetText(noDataReason+"\n\n"+f.noDataBody, stMuted)
}

// noDataReason is the headline of the failure panel, above the transport error.
const noDataReason = "Neither source answered. The screen will fill in as soon as one does."

// tooSmallText is what the screen says below MinSize.
//
// It NAMES the minimum in the message rather than leaving the reader to work it
// out, which is the one thing a too-small diagnostic has to do that a clipped
// dashboard does not.
//
// It is built once, at construction, from the same constants MinSize derives from,
// and the sentence is written by a helper rather than by a literal for the reason
// examples/hello gives about fmt on the frame path — but here the formatting is
// done ONCE and the result is a constant-shaped string, so there is no per-frame
// cost and no way for the message and the minimum to disagree.
var tooSmallText = tooSmallMessage(narrowW, minH)

// tooSmallMessage composes the diagnostic for a minimum of w by h.
//
// The wording is ordered so that "too small" is in the FIRST FOURTEEN CELLS. That
// is the constraint the message is written against: basic.Text truncates from the
// right, so a diagnostic whose key words sat at the end would become unreadable
// exactly at the narrow widths where it is the only thing on screen. "markets"
// would have been the natural first word and is therefore not.
func tooSmallMessage(w, h int) string {
	return "too small: need " + strconv.Itoa(w) + "x" + strconv.Itoa(h)
}

// Draw paints the screen.
//
// It is total for every rectangle including empty, and it allocates nothing in
// steady state. Everything derived from the size is in adapt, which runs at most
// once per distinct rectangle.
func (d *dashboard) Draw(buf *buffer.Buffer) {
	r := d.bounds
	if r.Empty() {
		// ADR 0007 §4: an empty Bounds returns immediately.
		return
	}
	// Repaint the whole rect first. The renderer diffs and never clears, so a
	// screen that drew four KPI tiles at 120x36 and then two at 60x20 would
	// otherwise leave the other two on screen (ADR 0007 §1 rule 3).
	buf.FillRect(r, stBody.Blank())

	if !d.enough(r) {
		d.drawTooSmall(buf, r)
		return
	}

	d.adapt(r)
	d.drawStatus(buf, r)
	if d.cur.noData {
		d.drawNoData(buf, r)
		return
	}
	d.bands.Draw(buf)
}

// enough reports whether r is large enough for the bands at all.
func (d *dashboard) enough(r buffer.Rect) bool {
	m := d.MinSize()
	return r.W >= m.W && r.H >= m.H
}

// drawTooSmall paints the one-line diagnostic, painting the whole rect first
// because the renderer never clears.
func (d *dashboard) drawTooSmall(buf *buffer.Buffer, r buffer.Rect) {
	row := buffer.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}
	d.diag.SetBounds(row)
	d.diag.Draw(buf)
}

// drawStatus paints the footer at the bottom row.
//
// It is pinned to the last row rather than stacked below the bands, so it is
// visible at every height instead of being the first thing a grow would reveal.
func (d *dashboard) drawStatus(buf *buffer.Buffer, r buffer.Rect) {
	row := buffer.Rect{X: r.X, Y: r.Bottom() - 1, W: r.W, H: 1}
	d.status.SetBounds(row)
	d.status.Draw(buf)
}

// drawNoData paints the failure panel in place of the bands.
//
// It occupies the whole area above the footer, because an empty grid of panels
// with nothing in them reads as a bug rather than as an outage.
func (d *dashboard) drawNoData(buf *buffer.Buffer, r buffer.Rect) {
	body := buffer.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H - 1}
	if body.Empty() {
		return
	}
	d.noDataBlk.SetBounds(body)
	d.noDataBlk.Draw(buf)
	in := d.noDataBlk.Interior()
	if in.Empty() {
		return
	}
	title := buffer.Rect{X: in.X, Y: in.Y, W: in.W, H: 1}
	d.noDataTitle.SetBounds(title)
	d.noDataTitle.Draw(buf)
	rest := buffer.Rect{X: in.X, Y: in.Y + 1, W: in.W, H: in.H - 1}
	d.noDataBody.SetBounds(rest)
	d.noDataBody.Draw(buf)
}

// adapt derives the arrangement from the rectangle, or reuses what was derived
// from the same one.
//
// This is the one function in the file that allocates: layout.Solve returns a
// fresh slice and geometry.Budget a fresh []bool. It allocates once per distinct
// rectangle, not once per frame, which is what keeps the frame path free.
//
// It is also where the responsive policy lives, and it is written as a small
// decision table rather than as arithmetic spread through Draw, because the whole
// point of a breakpoint is that a reader can see all of them in one place.
func (d *dashboard) adapt(r buffer.Rect) {
	if d.lay.valid && d.lay.key == r {
		return
	}
	a := &d.lay
	a.key = r
	a.valid = true
	a.arr = arrangementFor(r.W)

	// The KPI row's tile count is ClampCount and nothing else: pass the number of
	// tiles and how many fit, and it answers. That is ADR 0007 §2's level-1 form
	// exactly, and it is why this row needs no bespoke arithmetic.
	//
	// "How many fit" is the WIDTH less the gaps between the tiles that will be
	// shown, so the arithmetic counts gaps rather than assuming there are none. A
	// row of N tiles across W cells needs N*kpiTileW + (N-1)*bandGap, and solving
	// that for N by division is what tileFit below is doing — with the +1 for the
	// absent trailing gap folded in, which is why it rounds up rather than down at
	// the boundary.
	tileFit := kpiTilesFor(r.W)
	a.tiles = geometry.ClampCount(numTiles, tileFit)

	// The height budget. Each band DECLARES the height it wants — kpiH for the KPI
	// row, seriesHeight for the series band, the table's own minimum for the detail
	// band — and Budget reports which of them the screen can pay for, dropping from
	// the lowest priority up. The series band is PrioNormal and the detail band
	// PrioHigh, so a short screen keeps the table and loses the sparkline rather than
	// the reverse.
	//
	// Only the series band's size varies, and it varies with the ARRANGEMENT rather
	// than with the rect, so it is patched into a table built here rather than
	// rebuilding one: a Region is a statement about content and only the answer
	// depends on the size.
	// Each Size INCLUDES the gap that follows the band, because the bands' split
	// reserves a gap between adjacent panes and Budget does not know about it.
	//
	// Declaring heights alone would make the budget and the solve disagree: three
	// bands declaring 5 + 9 + 4 = 18 rows fit in 18, the solve then spends 2 of
	// those 18 on gaps, and the detail band is handed 2 rows where it declared it
	// needed 4 — so Budget reported it kept and the table drew an empty panel. The
	// last band declares no trailing gap because there is none after it.
	d.regions[bandKPI] = geometry.Region{Size: kpiH + bandGap, Prio: geometry.PrioHigh}
	d.regions[bandSeries] = geometry.Region{Size: d.seriesHeight() + bandGap, Prio: geometry.PrioNormal}
	d.regions[bandDetail] = geometry.Region{Size: d.detailHeight(), Prio: geometry.PrioHigh}
	// The footer is not a band — it is outside the bands' split entirely, pinned to
	// the last row — but it is PrioAlways in spirit: a screen with no status line is
	// a screen that cannot say it is stale, which is the one thing this example
	// exists to demonstrate. It is subtracted rather than budgeted because it cannot
	// be dropped.
	show := geometry.Budget(d.regions[:], r.H-1)
	a.showSeries = show[bandSeries] && d.hasSeries()
	a.showDetail = show[bandDetail] && len(d.cur.rows) > 0

	d.configureChildren(a)

	d.bands.SetBounds(buffer.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H - 1})
	d.bands.SetConstraints(d.bandConstraints(a))
}

// bandConstraints is the vertical solve: the KPI row is fixed, and what is left
// goes to the series band as a LENGTH and everything left over to the detail band
// as a Fill.
//
// A length for the series band rather than a weight, because the series band's
// height is not a matter of taste: a dial needs 5 interior rows to be a dial, a
// meter needs 1, and a weight would hand the band whatever the leftover happened to
// be — so on a short terminal the gauge would quietly stop being a gauge and become
// a bar, which is a silent change of widget rather than a visible loss of space.
//
// The dropped bands are given Length(0) rather than being left out of the list,
// because a constraint list and a pane list that disagree in length is defined
// behaviour in layout but a confusing thing to read. Zero-height panes draw
// nothing, which is the same answer.
func (d *dashboard) bandConstraints(a *layoutCache) []layout.Constraint {
	cs := make([]layout.Constraint, numBands)
	cs[bandKPI] = layout.Length(kpiH)
	if a.showSeries {
		cs[bandSeries] = layout.Length(d.seriesHeight())
	} else {
		cs[bandSeries] = layout.Length(0)
	}
	if a.showDetail {
		cs[bandDetail] = layout.Fill(1)
	} else {
		cs[bandDetail] = layout.Length(0)
	}
	return cs
}

// seriesHeight is what the series band asks the height budget for, by
// arrangement. It is a function of the arrangement rather than of the rect because
// the arrangement is the rect's consequence, and expressing it the other way round
// would compute the same threshold twice in two places.
//
// The numbers are the sum of what each panel in the band needs plus its own chrome,
// which is why the stacked band is so much taller: three panels in a COLUMN is
// three panels' worth of height, where one row of three is only the tallest of them.
func (d *dashboard) seriesHeight() int { return d.seriesHeightFor(d.lay.arr) }

// seriesHeightFor is seriesHeight for a named arrangement rather than the current
// one. The split exists so a test can ask what a band WOULD ask for at another
// width without first resizing the screen to that width and back.
func (d *dashboard) seriesHeightFor(arr arrangement) int {
	switch arr {
	case arrWide:
		// One row of three: the tallest panel in it, which is the gauge's dial at 5
		// interior rows, plus its 2 rows of chrome and one for its text.
		return 9
	case arrStacked:
		// A column of three, each with its own height and chrome, plus the two gaps.
		return (sparkHStacked + 2) + gaugeHStacked + meterHStacked + 2*bandGap
	default:
		// One panel: a 3-row sparkline with its chrome.
		return 5
	}
}

// detailHeight is what the detail band DECLARES, which is the table's own minimum
// and nothing more.
//
// Minimum, not a wish: this band is a Fill, so it is handed every row the other
// bands did not take, and a tall terminal therefore gives it a long table without
// this having to say so. Declaring a larger figure would be worse than useless —
// Budget compares declared sizes against what is available, so a band declaring 10
// rows on a 14-row screen would be DROPPED while the sparkline, declaring 6, was
// kept. That inverts the priorities: the table is the dashboard and the sparkline
// is a summary of it.
func (d *dashboard) detailHeight() int {
	if m := d.table.MinSize(); m.H > 0 {
		return m.H
	}
	return 4
}

// hasSeries reports whether there is anything to plot. A window with no history is
// not a sparkline of nothing, it is the absence of one.
func (d *dashboard) hasSeries() bool { return len(d.cur.series) >= 2 }

// arrangementFor is the breakpoint table, and the only place the thresholds are
// compared.
func arrangementFor(w int) arrangement {
	switch {
	case w >= wideW:
		return arrWide
	case w >= midW:
		return arrStacked
	default:
		return arrSingle
	}
}

// ConfigureChildren applies the arrangement to the child splits.
//
// It is called from adapt, so it runs once per distinct rectangle rather than per
// frame. Each arrangement changes a child's DIRECTION and its constraints, which
// is what makes a panel move rather than merely narrow: the series band goes from
// one row of three to a column of three, and the gauge's rectangle moves from
// beside the sparkline's to beneath it.
// It ALWAYS sets the pane list, not only in the arrangement that drops panels.
//
// That is the bug this shape exists to prevent, and it is worth writing down: a
// Split's pane list and its constraints are two separate pieces of state, and
// changing only the constraints leaves the DROPPED panes in place. A screen that
// had been narrow and then grown would keep showing one panel per band forever —
// which is a stale layout that no amount of resizing fixes, and the exact class of
// silent invalidation bug ADR 0003 names as a TUI's worst failure mode.
//
// Split.Panes resets the constraints to Fill(1) each, so SetConstraints follows it
// unconditionally rather than only where the constraints change.
func (d *dashboard) configureChildren(a *layoutCache) {
	tiles := make([]termmosaic.Widget, 0, a.tiles)
	for i := 0; i < a.tiles; i++ {
		tiles = append(tiles, d.stat[i])
	}
	d.kpi.Panes(tiles)

	switch a.arr {
	case arrWide:
		d.series.Panes(d.seriesPanes)
		d.series.Direction = layout.Horizontal
		d.series.SetConstraints([]layout.Constraint{
			layout.Fill(seriesSparkWeight),
			layout.Length(gaugeW),
			layout.Fill(meterWeight),
		})
		d.detail.Panes(d.detailPanes)
		d.detail.Direction = layout.Horizontal
		d.detail.SetConstraints([]layout.Constraint{
			layout.Fill(tableWeight),
			layout.Length(chartW),
		})
	case arrStacked:
		// The move: the series band goes VERTICAL, so the gauge and the meter sit
		// UNDER the sparkline instead of beside it.
		d.series.Panes(d.seriesPanes)
		d.series.Direction = layout.Vertical
		d.series.SetConstraints([]layout.Constraint{
			layout.Length(sparkHStacked),
			// The gauge's height is its dial's minimum, FIXED rather than a share:
			// the meter below it is one row of chrome plus a bar, so it can give up
			// space the dial cannot.
			layout.Length(gaugeHStacked),
			layout.Fill(1),
		})
		d.detail.Panes(d.detailPanes)
		d.detail.Direction = layout.Horizontal
		d.detail.SetConstraints([]layout.Constraint{
			layout.Fill(tableWeight),
			layout.Length(chartWNarrow),
		})
	default:
		// One panel per band. The gauge and the meter are not given a zero-length
		// slot: they are not in the pane list at all, so there is no question of
		// what a zero-height pane draws.
		d.series.Panes(d.sparkOnly)
		d.detail.Panes(d.tableOnly)
		d.series.Direction = layout.Horizontal
		d.series.SetConstraints([]layout.Constraint{layout.Fill(1)})
		d.detail.Direction = layout.Horizontal
		d.detail.SetConstraints([]layout.Constraint{layout.Fill(1)})
	}
}

// Child pane geometry. These are the widths the three arrangements solve for, and
// they are lengths rather than fills on purpose: a gauge that grows with the
// terminal stops being a gauge and becomes a decoration, and the chart needs a
// fixed label column to keep its bars comparable.
const (
	// gaugeW is wide enough for the panel TITLE as well as the dial: a 14-cell
	// title truncated to "spot in wind…" is a title the reader has to guess at, and
	// the wide arrangement is the one this example is designed around.
	gaugeW       = 18
	chartW       = 30
	chartWNarrow = 22

	seriesSparkWeight = 3
	meterWeight       = 2

	tableWeight = 2

	// The stacked band's three rows.
	//
	// The sparkline and the gauge both get FIXED heights and the meter takes the
	// remainder. The gauge's is its dial's own minimum — a Braille dial is tested
	// against five interior rows and degrades to a bar below that, so giving it a
	// share would mean the widget silently changed identity on a short terminal.
	// The meter cannot do that: one interior row is a meter at any size, so it is
	// the one panel that can absorb whatever is left.
	sparkHStacked = 5
	gaugeHStacked = 7
	meterHStacked = 3
)

// Compile-time proof that the dashboard is what the renderer needs it to be,
// including the optional MinSize interface it is the reason for: an application
// facing a too-small terminal can only react if something will tell it what it
// needs.
var (
	_ termmosaic.Widget      = (*dashboard)(nil)
	_ termmosaic.Minimizable = (*dashboard)(nil)
)
