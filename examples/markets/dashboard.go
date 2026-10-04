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
	"strings"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/data"
	"github.com/serkanalgur/termmosaic/widgets/form"
	"github.com/serkanalgur/termmosaic/widgets/split"
	"github.com/serkanalgur/termmosaic/widgets/viz"
)

// The rows below the bands that are not bands: one row of controls and one of
// status.
//
// Two rather than one, because a screen whose only chrome is a status line
// cannot say what the keys do, and a dashboard that has to be discovered by
// pressing keys is not interactive, it is a puzzle. The control row is where the
// key bindings and the currency pair live; the status row keeps the last place a
// reader looks for "is this current", which is the bottom of the screen.
//
// Both are subtracted from the bands' height in adapt and counted in MinSize, so
// the budget and the minimum cannot disagree about how many rows exist.
const chromeRows = 2

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
	// rows below the bands that are not bands.
	minH = kpiH + bandGap + 5 + bandGap + 4 + chromeRows

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

	// pair is the currency chooser: a Tabs row of the tracked majors, which is a
	// focusable widget with its own key contract rather than three application-
	// invented keys. Selecting a tab is what switches the primary pair, so the
	// choice is visible, is hit-testable, and needs no separate commit key.
	pair *form.Tabs

	// hints is the key-binding affordance on the control row. It shows the
	// FOCUSED widget's bindings, so it changes when the focus does rather than
	// listing every binding at once and being unreadable at every width.
	hints *form.KeyHint

	// focusables is the tab ring, in reading order. Cycling it is the whole of the
	// keyboard's focus routing, because neither widget consumes KeyTab — a fact a
	// test asserts rather than assumes, since one of them growing a Tab binding
	// would silently break navigation.
	focusables []termmosaic.Focusable
	// focusLabels names each entry of focusables, for the key hint.
	focusLabels []string
	// focus is the ring index. It starts at 0, which is the pair chooser, because
	// that is the control a reader is most likely to want to change.
	focus int

	// paused suspends the automatic refresh, and helpOpen is whether the help
	// overlay is showing. Both are read by Draw.
	paused   bool
	helpOpen bool

	// OnRefresh is called when the user asks for a fetch. It is a callback rather
	// than a call into the fetch loop because the dashboard must not know how data
	// arrives — and because the callback is the seam a test drives instead of a
	// network.
	OnRefresh func()

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

	// helpBlk, helpTitle and helpBody are the overlay '?' reveals. They are built
	// once, at construction, with their text already set: a help panel whose text
	// is assembled when it is opened would allocate on the frame path the first
	// time it was drawn, and an overlay that costs an allocation to appear is an
	// overlay that stutters.
	helpBlk   *block.Block
	helpTitle *basic.Text
	helpBody  *basic.Paragraph

	// sparkTitle is the sparkline panel's current title, held so retitle can tell a
	// real change from a republish of the same one — SetTitleString copies, so a
	// title rewritten on every frame would allocate on the frame path.
	sparkTitle string

	// cur is the frame the widgets were last fed, and curGen the store generation
	// it came from. Held so that the heartbeat can tell a republish of the frame
	// already applied from a genuinely new one.
	cur    *frame
	curGen uint64

	// market is the snapshot's market as last applied, held so that switching the
	// primary pair can rebuild the frame without going back to the source. A
	// market is immutable once fetched, so holding the pointer is holding a fact,
	// not a copy of one — and rebuilding from it is what keeps a pair switch from
	// costing a network round trip.
	market *market
	// snapAt is the instant cur was built from, carried so a pair switch can
	// rebuild the status line without reading the clock a second time and showing
	// two different fetch times in one frame.
	snapAt time.Time
	// snapErr and snapSrc are the failure and the source label for the same
	// reason: the status line is a function of them, and rebuilding the frame must
	// reproduce it.
	snapErr error
	snapSrc string

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

	// The currency chooser. Three majors, chosen because they are the three the
	// window response has carried reliably and because three is enough to show the
	// tab row scrolling when it does not fit.
	//
	// EUR comes first because it is the pair every other row is quoted against, so
	// the default selection is the one that makes the rest of the screen
	// self-consistent: the sparkline, the gauge and the KPI tile are all about the
	// same pair the table's first FX row is.
	d.pair = form.NewTabs(buffer.Rect{}, pairLabels())
	d.pair.TabStyle = stMuted
	d.pair.SelectedStyle = stAccent
	d.pair.Background = stBody
	// The default tab is the default pair. The lookup's boolean is deliberately
	// ignored: the default pair is selectablePairs' first entry by construction,
	// and a pair that fell out of that list would be a data bug this line should
	// not paper over — the tab row would then highlight whatever is at index 0.
	d.pair.SetSelected(indexOfOrZero(primaryPair))
	// OnSelect rather than a key handler: the tab row already maps left and right
	// to the previous and next pair, and routing the pair change through its own
	// callback means the widget's key contract is the only description of which
	// keys change the pair.
	d.pair.OnSelect = func(i int) {
		if code, ok := pairCodeAt(i); ok {
			d.setPair(code)
		}
	}

	d.hints = form.NewKeyHint(buffer.Rect{}, nil)
	d.hints.KeyStyle = stAccent
	d.hints.HelpStyle = stMuted
	d.hints.SeparatorStyle = stMuted
	d.hints.Background = stBody

	d.helpBlk = block.New(buffer.Rect{})
	d.helpBlk.SetBorder(buffer.BorderPlain)
	d.helpBlk.SetBorderStyle(stEdge)
	d.helpBlk.SetBackground(stPanel)
	d.helpBlk.SetTitleString("keys", stTitle)
	d.helpTitle = basic.NewTextString(buffer.Rect{}, helpTitleText, stMuted)
	d.helpTitle.SetBackground(stPanel)
	d.helpBody = basic.NewParagraphString(buffer.Rect{}, helpBodyText, stMuted)
	d.helpBody.SetBackground(stPanel)

	// The ring, in reading order: the chooser is above the bands and the table is
	// below them, so the order is the order a reader's eye travels.
	d.focusables = []termmosaic.Focusable{d.pair, d.table}
	d.focusLabels = []string{"pair", "pairs table"}
	d.setFocus(0)

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
		d.table.Block(), d.chart.Block(), d.noDataBlk, d.helpBlk,
	} {
		b.Ascii = asciiRung
	}
	// The two chrome widgets carry GLYPHS of their own — the tab row's brackets and
	// the hint's arrows — so the ASCII rung has to reach them as well. Missing this
	// would leave a monochrome terminal showing box-drawing brackets from a widget
	// that was told to use ASCII, which is the kind of half-degraded screen nobody
	// can report usefully.
	d.pair.Ascii = asciiRung
	d.hints.Ascii = asciiRung
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
	// included, plus the rows below the bands. Deriving both from one place is what
	// keeps them from drifting: a MinSize claiming a height the budget would not
	// honour would put the screen above its own minimum and still drawing the
	// diagnostic, which is the one state an application cannot reason about.
	h := (kpiH + bandGap) + (d.seriesHeightFor(arrSingle) + bandGap) + d.detailHeight() + chromeRows
	return buffer.Size{W: narrowW, H: h}
}

// Invalidate satisfies termmosaic.Widget. The dashboard repaints its whole
// rectangle every frame, so there is no finer-grained state to mark.
func (d *dashboard) Invalidate() {}

// Handle routes an event: a resize to SetBounds, and everything else through the
// focus ring to whichever widget owns it.
//
// # The composition contract
//
// This is the whole of the interaction design, and it is deliberately one line
// long per case. Widgets own their keys — the table consumes the arrows, the tab
// row consumes left and right — and the application owns the routing and the keys
// no widget wants: Tab, space, r, ?, and the quit keys. Neither half knows about
// the other, which is why adding a panel to this screen means adding it to the
// focus ring and nothing else.
//
// A key the application claims is consumed even when it did nothing visible, so a
// form never falls through to the next field because the user pressed space at the
// end of a list. That is the same rule form.Select applies internally, and it is
// what stops '?' from reaching the table as a stray rune.
//
// The caller must invoke this on the render goroutine: Handle mutates the focus
// index, the pause flag and the pair, all of which Draw reads.
func (d *dashboard) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventResize:
		d.SetBounds(buffer.Rect{W: ev.Size.W, H: ev.Size.H})
		return true
	case termmosaic.EventMouse:
		return d.handleMouse(ev)
	case termmosaic.EventKey:
		return d.handleKey(ev)
	default:
		return false
	}
}

// handleKey is the application's own routing. Everything it does not claim goes
// to the focused widget, and the widget's own key contract decides whether it was
// consumed.
func (d *dashboard) handleKey(ev termmosaic.Event) bool {
	// The help overlay is MODAL over the keyboard but not over the mouse: a reader
	// who has opened it and then clicks a table row expects the row to be selected,
	// and a modal that swallowed the mouse too would be a worse overlay than no
	// overlay. Any key other than the ones below dismisses it, which is why this
	// runs before the focus ring.
	if d.helpOpen {
		switch ev.Key {
		case termmosaic.KeyEscape, termmosaic.KeyEnter:
			d.helpOpen = false
			return true
		}
		if ev.Mod == 0 && (ev.Rune == '?' || ev.Rune == 'q' || ev.Rune == 'Q') {
			d.helpOpen = false
			return true
		}
	}

	switch {
	case isCtrlC(ev):
		return true // the caller quits; see run
	case ev.Mod == 0 && (ev.Rune == 'q' || ev.Rune == 'Q'):
		return true
	case ev.Key == termmosaic.KeyEscape:
		return true
	case ev.Mod == 0 && ev.Rune == '?':
		d.helpOpen = !d.helpOpen
		return true
	}

	switch ev.Key {
	case termmosaic.KeyTab:
		d.cycleFocus(false)
		return true
	case termmosaic.KeyBacktab:
		d.cycleFocus(true)
		return true
	}

	switch ev.Mod {
	case 0:
		switch ev.Rune {
		case ' ':
			// Space pauses the auto-refresh. It is the application's key rather
			// than a widget's because the thing it pauses is the FETCH, which no
			// widget knows about.
			d.SetPaused(!d.Paused())
			return true
		case 'r':
			if d.OnRefresh != nil {
				d.OnRefresh()
			}
			return true
		}
	}

	if i := d.focusIndex(); i >= 0 && i < len(d.focusables) {
		return d.focusables[i].Handle(ev)
	}
	return false
}

// handleMouse offers the event to every focusable widget and takes focus from the
// one that consumed it: a click on a table row both selects the row and makes the
// table the keyboard's target, which is what a user expects and what no widget can
// do on its own.
//
// The focusable order is the reading order, so a click that two widgets could
// claim — none currently overlap — goes to the one a reader would have aimed at.
func (d *dashboard) handleMouse(ev termmosaic.Event) bool {
	for i, w := range d.focusables {
		if w.Handle(ev) {
			d.setFocus(i)
			return true
		}
	}
	return false
}

// isCtrlC reports whether ev is the interrupt chord, in the decoder's vocabulary.
//
// Ctrl-C arrives as Ctrl+'c' rather than as 0x03, so the raw-byte spelling of this
// check would be comparing against a byte the decoder has already consumed.
func isCtrlC(ev termmosaic.Event) bool {
	return ev.Mod == termmosaic.ModCtrl && (ev.Rune == 'c' || ev.Rune == 'C')
}

// focusIndex returns which widget has the keyboard, or -1 when the ring is empty.
func (d *dashboard) focusIndex() int { return d.focus }

// FocusIndex reports which panel the keyboard is aimed at, which is what the tests
// assert on after a simulated Tab and what an application would read to build its
// own chrome.
func (d *dashboard) FocusIndex() int { return d.focus }

// FocusLabel names the focused panel, for the key hint's first binding.
//
// It is a parallel list rather than something derived from the widget, because a
// widget's own bounds say where it is and nothing about what it is called. Keeping
// the two lists the same length is asserted by a test, because a ring with a label
// list shorter than it is a panic waiting for the reader who tabs far enough.
func (d *dashboard) FocusLabel() string {
	if d.focus < 0 || d.focus >= len(d.focusLabels) {
		return "none"
	}
	return d.focusLabels[d.focus]
}

// cycleFocus moves the keyboard's target by one widget, wrapping. back is
// shift-tab.
func (d *dashboard) cycleFocus(back bool) {
	n := len(d.focusables)
	if n == 0 {
		return
	}
	step := 1
	if back {
		step = -1
	}
	d.setFocus(((d.focus+step)%n + n) % n)
}

// setFocus focuses the i-th focusable and unfocuses the rest. A widget that keeps
// its selection while unfocused is what makes tabbing back to it useful.
func (d *dashboard) setFocus(i int) {
	if i < 0 || i >= len(d.focusables) {
		return
	}
	d.focus = i
	for j, w := range d.focusables {
		w.SetFocused(j == i)
	}
	// The bindings on the control row are the FOCUSED widget's, so they change
	// here rather than in Draw. SetBindings copies, which would allocate on the
	// frame path, so it is called on the interaction path where an allocation costs
	// nothing.
	d.syncHints()
}

// Paused reports whether the automatic refresh is suspended.
func (d *dashboard) Paused() bool { return d.paused }

// SetPaused suspends or resumes the automatic refresh.
//
// It is the dashboard's state rather than main's because the control row has to
// SHOW it — a pause the reader cannot see is indistinguishable from a fetch that
// is slow — and a second source of truth for the same flag is how the row and the
// fetch loop start to disagree.
func (d *dashboard) SetPaused(v bool) {
	if d.paused == v {
		return
	}
	d.paused = v
	d.syncHints()
	d.syncStatus()
}

// HelpOpen reports whether the help overlay is showing.
func (d *dashboard) HelpOpen() bool { return d.helpOpen }

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
	d.market = f.market
	d.snapAt = f.at
	d.snapErr = f.err
	d.snapSrc = f.source

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

	d.syncStatus()
	d.setNoData(f)
	// The sparkline's panel title names the pair, so a switch has to relabel it.
	// It is done here rather than at the point of the switch because SetFrame is
	// the only path that knows what is on screen now, and two callers relabelling
	// is how the panel and the data drift apart.
	d.retitle(f.pair)
	d.lay.valid = false
}

// syncStatus republishes the footer, which is a function of the frame AND of the
// pause flag — a paused dashboard must say so, because a screen that stops
// updating without saying why is indistinguishable from one that has hung.
//
// It is separate from SetFrame for exactly that reason: pausing changes no data,
// so there is no new frame to apply, and a footer that only updated with data
// would leave a paused screen lying about being live.
func (d *dashboard) syncStatus() {
	if d.cur == nil {
		return
	}
	want := d.cur.status
	if d.paused {
		want += "  PAUSED"
	}
	if want == d.statusText {
		return
	}
	d.statusText = want
	d.status.SetText(want, statusStyle(d.cur))
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

// The key hint's bindings, per focused widget.
//
// They are the focused widget's OWN key contract, restated rather than
// discovered. That is the point of showing them: a reader who does not know that
// the table takes PageDown learns it from the row rather than by pressing it and
// watching nothing happen. The bindings the application itself claims are on
// every panel's list, because they work wherever the focus is.
//
// The arrows are ARROW GLYPHS and not the words, and the help text is one word.
// Both are because this row is a fixed width that has to survive truncation with
// an ellipsis: a hint written in full words is cut off before it reaches "quit",
// and a hint whose last visible binding is not the one that gets you out is a
// worse affordance than no hint. The full spelling is on the help overlay, which
// is what '?' is for.
//
// Returned as a value rather than stored, and only when the focus or a toggle
// changes, because SetBindings COPIES its argument: building this on the frame path
// would allocate every frame, while building it in Handle is the interaction path,
// where an allocation costs nothing.
func (d *dashboard) bindings() []form.Binding {
	b := []form.Binding{
		{Key: "tab", Help: "panel"},
		{Key: "space", Help: pauseWord(d.paused)},
		{Key: "r", Help: "fetch"},
		{Key: "?", Help: helpWord(d.helpOpen)},
		{Key: "q", Help: "quit"},
	}
	switch d.FocusLabel() {
	case "pairs table":
		b = append([]form.Binding{
			{Key: "↑↓", Help: "row"},
			{Key: "pgup", Help: "page"},
			{Key: "home", Help: "first"},
		}, b...)
	case "pair":
		b = append([]form.Binding{
			{Key: "←→", Help: "pair"},
			{Key: "home", Help: "first"},
		}, b...)
	}
	return b
}

// pauseWord and helpWord are the two bindings whose help text depends on the state
// they toggle. Naming the ACTION rather than the key is what makes the hint usable
// without prior knowledge: "[space] pause" tells a reader what the key does, and a
// static "[space]" does not.
func pauseWord(paused bool) string {
	if paused {
		return "resume"
	}
	return "pause"
}

func helpWord(open bool) string {
	if open {
		return "hide keys"
	}
	return "keys"
}

// syncHints pushes the focused widget's bindings onto the control row.
//
// It is called on the interaction path only, and it is idempotent in effect: the
// hint caches its line against its rect and its binding count, so re-setting the
// same bindings would only rebuild when they differ.
func (d *dashboard) syncHints() {
	d.hints.SetBindings(d.bindings())
}

// The help overlay's text.
//
// Two lines rather than one paragraph, and each line is a GROUP rather than a list,
// because a reader looking for a key scans for a shape they recognise and a wall of
// bindings is a wall. The mouse line is on the panel because mouse capture is on
// for this example and a reader would otherwise assume the wheel does nothing.
//
// The text is a constant rather than built from the binding table above, for the
// same reason the hint's is not: a help panel assembled at open time allocates on
// the frame path, and the two would then be able to disagree about which keys
// exist.
const (
	helpTitleText = "markets — keys and mouse"
	helpBodyText  = "" +
		"keyboard\n" +
		"  tab / shift-tab   next / previous panel\n" +
		"  arrows            move within the focused panel\n" +
		"  pgup / pgdn       page · home / end  first / last\n" +
		"  space             pause or resume the auto refresh\n" +
		"  r                 fetch now\n" +
		"  ?                 show or hide this panel\n" +
		"  q, esc, ctrl-c    quit\n" +
		"\n" +
		"mouse\n" +
		"  wheel             scroll the pairs table\n" +
		"  click a row       select it and focus the table\n" +
		"  click a tab       change the primary pair\n" +
		"\n" +
		"mouse capture is ON while this screen runs, and is handed back to the\n" +
		"shell on every exit path — including an error and ctrl-c."
)

// The help text's own measurements, computed once at construction.
//
// They are measured rather than written as literals because a literal width is a
// number a future edit to the text would silently invalidate — the panel would
// size itself for text that no longer exists and clip the line that does.
var (
	helpTextW     = longestLine(helpBodyText)
	helpTextLines = strings.Count(helpBodyText, "\n") + 1
	// helpBodyPad is the rows the body asks for beyond its own lines: the panel's
	// top and bottom border. It is the same two cells helpPanelSize adds, named on
	// both sides so the two cannot disagree about what the border costs.
	helpBodyPad = 2
)

// longestLine returns the width in cells of the widest line in s.
//
// buffer.RuneWidth is used rather than a byte count because the text contains
// em-dashes and middots, which are three bytes each and one or two cells wide: a
// byte count would size the panel for text twice as wide as it renders and clip
// the right-hand column for no reason.
func longestLine(s string) int {
	widest := 0
	for _, line := range strings.Split(s, "\n") {
		if n := buffer.StringWidth(line); n > widest {
			widest = n
		}
	}
	return widest
}

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
	d.drawChrome(buf, r)
	if d.cur.noData {
		d.drawNoData(buf, r)
	} else {
		d.bands.Draw(buf)
	}
	// The overlay goes LAST, over everything. It is the only thing drawn after the
	// bands rather than before them, because an overlay under the panels would be an
	// overlay nobody could read.
	//
	// It is drawn even in the failure case, because a reader whose screen has gone
	// empty is exactly the reader who needs to know which key quits.
	if d.helpOpen {
		d.drawHelp(buf, r)
	}
}

// drawChrome paints the two rows below the bands: the controls and the status.
//
// Both are pinned to the bottom rather than stacked below the bands, so they are
// visible at every height instead of being the first thing a grow would reveal. The
// control row is ABOVE the status row because the bindings are what a reader looks
// for first and the fetch time is what they look for when something is wrong.
func (d *dashboard) drawChrome(buf *buffer.Buffer, r buffer.Rect) {
	controls := buffer.Rect{X: r.X, Y: r.Bottom() - chromeRows, W: r.W, H: 1}
	status := buffer.Rect{X: r.X, Y: r.Bottom() - 1, W: r.W, H: 1}

	// The tab row takes the width its labels need and the hint takes the rest. The
	// split is arithmetic rather than a solve because there is one constraint and
	// one remainder — a Solve call here would be the second solver in an example
	// whose whole point is that there is one, and it would allocate.
	pairW := pairRowW(controls.W)
	hintW := controls.W - pairW
	if hintW < 0 {
		hintW = 0
	}
	d.pair.SetBounds(buffer.Rect{X: controls.X, Y: controls.Y, W: pairW, H: 1})
	d.pair.Draw(buf)
	d.hints.SetBounds(buffer.Rect{X: controls.X + pairW, Y: controls.Y, W: hintW, H: 1})
	d.hints.Draw(buf)

	d.status.SetBounds(status)
	d.status.Draw(buf)
}

// pairRowNeed is the width the chooser's labels want: each label plus the two
// marker cells the widget puts around it, and the gap that separates the row from
// the hint.
//
// It is computed ONCE rather than per frame, because pairLabels allocates: a width
// derived from a freshly built label slice would put an allocation on the frame
// path, and the labels are a constant list.
var pairRowNeed = measurePairRow()

// measurePairRow is pairRowNeed's derivation, kept as a function so the expression
// is written once and the variable is plainly a constant.
func measurePairRow() int {
	need := bandGap
	for _, l := range pairLabels() {
		// Two marker cells per tab: the widget brackets the selected tab and
		// space-pads every other one, so every tab is its label plus two.
		need += len(l) + 2
	}
	return need
}

// pairRowW is pairRowNeed capped to a row w cells wide, so the rect is never
// negative — and never larger than the row it is drawn on, which at the narrowest
// width the dashboard draws at is the only thing that stops the tab row painting
// over the hint.
func pairRowW(w int) int {
	if pairRowNeed > w {
		return w
	}
	return pairRowNeed
}

// drawHelp paints the help overlay, centred over the screen.
//
// Centred rather than pinned, because a help panel in a corner is read as part of
// the chrome and this is an interruption. It is sized to its own text with a margin,
// and CLIPPED to the screen, so at a terminal too small for it the panel is a
// smaller panel rather than an off-screen one — ADR 0007 §1 rule 2.
//
// The rectangle is total for every screen size, including one too small for the
// text: helpPanelSize caps it, so the panel shrinks rather than running off the
// edge (ADR 0007 §1 rule 2).
func (d *dashboard) drawHelp(buf *buffer.Buffer, r buffer.Rect) {
	// The panel is sized from the help text's own measurements rather than by
	// wrapping the paragraph here: wrapping allocates, and this runs on the frame
	// path. Both figures are computed ONCE, at construction, from the same constant
	// the panel shows.
	w, h := helpPanelSize(r.W, r.H, helpTextW, helpTextLines+helpBodyPad)

	// Centring is integer arithmetic rather than a layout.Solve, and the reason is
	// worth stating because this file uses the solver everywhere else: Solve
	// ALLOCATES a fresh slice per call, and this runs on the frame path. A centred
	// overlay is one subtraction on each axis and has no breakpoints, so there is
	// no policy for a solver to express — the bands' layout has breakpoints and a
	// height budget, and this has neither.
	panel := buffer.Rect{
		X: r.X + (r.W-w)/2,
		Y: r.Y + (r.H-h)/2,
		W: w,
		H: h,
	}
	d.helpBlk.SetBounds(panel)
	d.helpBlk.Draw(buf)
	in := d.helpBlk.Interior()
	if in.Empty() {
		return
	}
	d.helpTitle.SetBounds(buffer.Rect{X: in.X, Y: in.Y, W: in.W, H: 1})
	d.helpTitle.Draw(buf)
	d.helpBody.SetBounds(buffer.Rect{X: in.X, Y: in.Y + 1, W: in.W, H: in.H - 1})
	d.helpBody.Draw(buf)
}

// helpMargin is the cells between the help panel and the screen's edge, so it never
// touches the border and the reader can see it is an overlay rather than a new
// layout.
const helpMargin = 2

// helpPanelSize is the panel's size for a screen of w by h around content of cw by
// ch: the content plus its border, capped at the screen less the margin.
//
// It is capped rather than allowed to overflow because an overlay that runs off the
// edge of the screen is worse than a truncated one: the reader loses the QUIT key,
// which is the one line the panel exists to guarantee.
func helpPanelSize(w, h, cw, ch int) (int, int) {
	return min(cw+2, max(1, w-2*helpMargin)), min(ch+3, max(1, h-2*helpMargin))
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
	body := buffer.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H - chromeRows}
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

// setPair switches the primary pair and rebuilds the frame for it.
//
// The rebuild is from the MARKET ALREADY HELD rather than from the source, which
// is what makes switching instant: a network round trip to redraw a label is a
// spinner the reader did not ask for. The generation is bumped rather than
// reused, because SetFrame skips a republish of the generation it already applied
// and a pair switch with the store's generation would therefore be silently
// dropped.
//
// A pair the held market has no window for produces a frame whose sparkline and
// gauge report the absence — see market.withPair — rather than one that keeps
// drawing the previous pair's shape.
func (d *dashboard) setPair(code string) {
	if d.market == nil || d.market.primary == code {
		return
	}
	m := d.market.withPair(code)
	if m == nil {
		return
	}
	// The tab row is the control that caused this, so its own selection is already
	// right; setting it again would be harmless but would make the widget's state a
	// function of two writers.
	d.applyFrame(buildFrame(&snapshot{
		m: m, err: d.snapErr, at: d.snapAt, source: d.snapSrc, ok: m.anyData(),
		gen: d.curGen + 1,
	}))
	// The hint names the focused panel's bindings and the overlay's own state, and
	// a pair switch changes neither — so nothing here re-pushes them. It is
	// mentioned because the obvious "re-sync everything" instinct would add an
	// allocation per switch for no visible change.
}

// applyFrame feeds the widgets a frame the application rebuilt itself, which is
// what a pair switch does.
//
// It is SetFrame without the generation check, because the frame it is given is by
// definition not one the store has published: the generation is bumped by the
// caller precisely so that SetFrame's "already applied" test cannot swallow it.
func (d *dashboard) applyFrame(f *frame) {
	d.SetFrame(f)
}

// retitle points the sparkline's panel at pair.
//
// It is a no-op when the title already says that, because SetTitleString copies
// and a title rewritten on every frame would allocate on the frame path for a
// string that did not change.
func (d *dashboard) retitle(pair string) {
	if pair == "" {
		return
	}
	title := pair + "/USD, last " + strconv.Itoa(windowDays) + "d"
	if d.sparkTitle == title {
		return
	}
	d.sparkTitle = title
	d.spark.Block().SetTitleString(title, stTitle)
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
	// The chrome below the bands is not a band — it is outside the bands' split
	// entirely, pinned to the bottom rows — but it is PrioAlways in spirit: a screen
	// with no status line is a screen that cannot say it is stale, and a screen with
	// no key bindings is a screen whose interactivity nobody can discover. It is
	// SUBTRACTED rather than budgeted because it cannot be dropped.
	//
	// chromeRows is used here rather than a literal because this is the third place
	// that has to agree about how many rows exist — with MinSize and with
	// drawChrome — and a chrome row that is budgeted for but not reserved would be a
	// band drawn over the control row, which is the exact stale-cell failure ADR
	// 0007 §1 rule 3 is about, reached from the other direction.
	show := geometry.Budget(d.regions[:], r.H-chromeRows)
	a.showSeries = show[bandSeries] && d.hasSeries()
	a.showDetail = show[bandDetail] && len(d.cur.rows) > 0

	d.configureChildren(a)

	d.bands.SetBounds(buffer.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H - chromeRows})
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
