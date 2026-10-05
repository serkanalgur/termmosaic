// Command dashboard is TermMosaic's showcase: one screen that composes nine widgets
// from the catalog and moves data through all of them.
//
// It exists because a widget catalog is evaluated by what an application can BUILD
// with it, not by what each widget does alone. So this is a real screen — a service
// dashboard — rather than a row of isolated demonstrations:
//
//   - a List of 100,000 synthetic requests, which is the case a virtualized widget
//     exists for and the reason its per-frame cost does not depend on the count
//   - a Table of columns wider than the viewport, which is what makes the horizontal
//     scrolling and the per-column width modes visible
//   - a Tree of services with expandable children, showing the keyboard navigation
//   - a Pager over a large log, wrapped to width and searched
//   - and the five viz widgets: a determinate ProgressBar, a Braille Gauge, a
//     zoned Meter, a Braille Sparkline and a BarChart
//
// Every widget here is Focusable or a plain display, and the key handling below is
// deliberately thin: Tab moves focus between the focusable widgets because none of
// them consumes it, and / begins a search the Pager's read-only API accepts rather
// than typing into it. That is the composition contract in miniature — widgets own
// their keys, the application owns the routing.
//
// The chrome belongs to widgets/block: this file spells no border rune and invents no
// title threshold (ADR 0008 §2).
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/term"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/data"
	"github.com/serkanalgur/termmosaic/widgets/viz"
)

// The application's palette. Colour is an APPLICATION decision — ADR 0008 ships no
// default colours — and this example keeps it to five so that what remains on screen
// in monochrome is still readable: every widget here carries a non-colour signal
// too, which is the accessibility requirement, not a bonus.
var (
	bg      = buffer.NewColour(0x10, 0x14, 0x1c)
	fg      = buffer.NewColour(0xd8, 0xdc, 0xe4)
	dim     = buffer.NewColour(0x60, 0x6a, 0x7a)
	edge    = buffer.NewColour(0x30, 0x36, 0x40)
	accent  = buffer.NewColour(0x30, 0xc0, 0x80)
	warn    = buffer.NewColour(0xd8, 0xa0, 0x30)
	crit    = buffer.NewColour(0xd0, 0x50, 0x50)
	panelBG = buffer.NewColour(0x16, 0x1b, 0x24)
)

// The styles the screen draws with, each built through NewStyle: a partial literal
// would leave a channel at opaque black, which is Style's documented footgun.
var (
	stBody   = buffer.NewStyle(fg, bg, 0)
	stPanel  = buffer.NewStyle(fg, panelBG, 0)
	stMuted  = buffer.NewStyle(dim, panelBG, 0)
	stEdge   = buffer.NewStyle(edge, panelBG, 0)
	stAccent = buffer.NewStyle(accent, panelBG, buffer.AttrBold)
	stWarn   = buffer.NewStyle(warn, panelBG, 0)
	stCrit   = buffer.NewStyle(crit, panelBG, buffer.AttrBold)
	stTrack  = buffer.NewStyle(edge, panelBG, 0)
	stSel    = buffer.NewStyle(fg, panelBG, buffer.AttrReverse)
)

// Layout weights. The screen is three columns over two rows: a tall left column, a
// middle column, and a wide right column, with a header strip above and a metrics
// strip below.
var (
	colWeights = []int{3, 4, 5}
	rowWeights = []int{1, 4, 2}
)

// requestCount is the size of the synthetic request list. It is large on purpose:
// the point of the List is that a frame costs what a ten-item list costs.
const requestCount = 100000

// tickInterval is how often the live widgets move. A dashboard that animates is what
// a renderer is for, and a terminal redraw is cheap enough that this is not a
// performance question.
const tickInterval = 120 * time.Millisecond

// dashboard is the whole screen: nine widgets and the rectangles they were given.
type dashboard struct {
	// bounds is the screen. Every widget's rectangle is derived from it in layout,
	// which is the documented path rather than the clamp-and-hope anti-pattern ADR
	// 0007 replaced.
	bounds buffer.Rect

	list  *data.List
	table *data.Table
	tree  *data.Tree
	pager *data.Pager

	bar   *viz.ProgressBar
	gauge *viz.Gauge
	meter *viz.Meter
	spark *viz.Sparkline
	chart *viz.BarChart

	// focusable is the tab ring, in reading order. Cycling it is the whole of the
	// application's focus routing, because no widget here consumes KeyTab.
	focusable []termmosaic.Focusable
	focus     int

	// tick is the frame counter every live widget reads, so one number drives the
	// whole screen and the widgets stay independent of each other.
	tick int

	// searching is whether keystrokes are going to the pager's query rather than to
	// the focused widget. A read-only widget cannot receive typed text, so the
	// application owns the mode — which is exactly the boundary SetQuery implies.
	searching bool
	query     strings.Builder
	// series is the sparkline's data, held by the application and rewritten in place.
	series []float64
}

// newDashboard builds the screen at size (w, h) with its data loaded.
func newDashboard(w, h int) *dashboard {
	d := &dashboard{bounds: buffer.Rect{W: w, H: h}}
	d.buildWidgets()
	d.layoutWidgets()
	return d
}

// buildWidgets constructs the nine widgets and loads their data. Everything here is
// DATA, which is where an application spends its memory: 100,000 list items, a
// hundred rows of table, a five-thousand-line log. Constructing them costs nothing
// per frame, which is the point.
func (d *dashboard) buildWidgets() {
	d.list = data.NewList(buffer.Rect{}, requestItems(requestCount)...)
	d.list.SelectedStyle = stSel
	d.list.ScrollbarStyle = stAccent
	d.list.Scrollbar = true
	frame(d.list.Block(), "requests")

	d.table = data.NewTable(buffer.Rect{},
		col("id", 8),
		col("method", 10),
		col("path", 24),
		colGrow("route", 12),
		col("status", 8),
		col("ms", 6),
	)
	d.table.Header = true
	d.table.ItemStyle = stPanel
	d.table.SelectedStyle = stSel
	d.table.ScrollbarStyle = stAccent
	d.table.SetRows(requestRows(200))
	frame(d.table.Block(), "recent requests")

	d.tree = data.NewTree(buffer.Rect{}, serviceTree()...)
	d.tree.ItemStyle = stPanel
	d.tree.SelectedStyle = stSel
	d.tree.ScrollbarStyle = stAccent
	frame(d.tree.Block(), "services")

	d.pager = data.NewPager(buffer.Rect{})
	d.pager.TextStyle = stPanel
	d.pager.StatusStyle = stMuted
	d.pager.SetText(logText(4000))
	frame(d.pager.Block(), "log")

	d.bar = viz.NewProgressBar(buffer.Rect{})
	d.bar.FillStyle = stAccent
	d.bar.TrackStyle = stTrack
	d.bar.PercentStyle = stMuted
	d.bar.SetLabel("deploy", stMuted)
	d.bar.Percentage = true

	d.gauge = viz.NewGauge(buffer.Rect{})
	d.gauge.ArcStyle = stAccent
	d.gauge.TrackStyle = stTrack
	d.gauge.ValueStyle = stAccent
	d.gauge.LabelStyle = stMuted
	d.gauge.SetLabel("load", stMuted)

	d.meter = viz.NewMeter(buffer.Rect{})
	d.meter.SetZones([]viz.Zone{
		{Name: "ok", From: 0, To: 60, Style: stTrack, FillStyle: stAccent},
		{Name: "warn", From: 60, To: 85, Style: stTrack, FillStyle: stWarn},
		{Name: "crit", From: 85, Style: stTrack, FillStyle: stCrit},
	})
	d.meter.Threshold = 85
	d.meter.ThresholdStyle = stCrit
	d.meter.NameStyle = stMuted
	d.meter.ValueStyle = stPanel
	d.meter.TrackStyle = stTrack

	d.spark = viz.NewSparkline(buffer.Rect{})
	d.spark.TrackStyle = stTrack
	d.spark.Style = stAccent
	d.spark.LastStyle = stAccent
	d.series = historyInto(d.series, 0)
	d.spark.SetValues(d.series)

	d.chart = viz.NewBarChart(buffer.Rect{})
	d.chart.BarStyle = stAccent
	d.chart.AxisStyle = stEdge
	d.chart.LabelStyle = stMuted
	d.chart.ValueStyle = stMuted
	d.chart.SetData([]viz.Datum{
		{Label: "read", Value: 42},
		{Label: "write", Value: 78},
		{Label: "cache", Value: 25},
		{Label: "net", Value: 61},
	})

	d.focusable = []termmosaic.Focusable{d.list, d.table, d.tree, d.pager}
}

// layoutWidgets solves the grid and hands every widget its rectangle.
//
// It uses layout.Solve rather than dividing by hand, because ADR 0004 chose a closed
// constraint set precisely so that exactly one solver exists, and a second
// arithmetic pass inside an application is how two solvers start to disagree about
// overflow. The remainder goes to the last cell of each axis, so the widgets fill the
// screen exactly.
func (d *dashboard) layoutWidgets() {
	r := d.bounds
	if r.Empty() {
		return
	}
	rows := split(layout.Vertical, rowWeights, 0, r.H)
	head := layout.Rect(r, layout.Vertical, rows, 0, 0)
	body := layout.Rect(r, layout.Vertical, rows, 0, 1)
	foot := layout.Rect(r, layout.Vertical, rows, 0, 2)

	cols := split(layout.Horizontal, colWeights, 1, body.W)
	left := layout.Rect(body, layout.Horizontal, cols, 1, 0)
	mid := layout.Rect(body, layout.Horizontal, cols, 1, 1)
	right := layout.Rect(body, layout.Horizontal, cols, 1, 2)

	// Left is the request list; the middle column is a table over a tree; the right
	// column is the log.
	d.list.SetBounds(left)
	d.table.SetBounds(areaOf(mid, 0.6))
	d.tree.SetBounds(below(mid, 0.6))
	d.pager.SetBounds(right)

	// The header strip is the progress bar, and the foot row is the four live
	// measurements side by side.
	d.bar.SetBounds(head)
	footSizes := split(layout.Horizontal, []int{1, 1, 2, 1}, 1, foot.W)
	d.gauge.SetBounds(layout.Rect(foot, layout.Horizontal, footSizes, 1, 0))
	d.meter.SetBounds(layout.Rect(foot, layout.Horizontal, footSizes, 1, 1))
	d.spark.SetBounds(layout.Rect(foot, layout.Horizontal, footSizes, 1, 2))
	d.chart.SetBounds(layout.Rect(foot, layout.Horizontal, footSizes, 1, 3))
}

// below returns the remainder of r under the first frac of its height, so a pair of
// stacked panes tiles exactly with no gap and no overlap — which is what a caller
// would otherwise get wrong by subtracting the wrong thing.
func below(r buffer.Rect, frac float64) buffer.Rect {
	h := int(float64(r.H) * frac)
	if h < 0 {
		h = 0
	}
	if h > r.H {
		h = r.H
	}
	return buffer.Rect{X: r.X, Y: r.Y + h, W: r.W, H: r.H - h}
}

// areaOf returns the first n hundredths of r vertically, which is how a pane gets
// "the top half" without a second solver.
func areaOf(r buffer.Rect, frac float64) buffer.Rect {
	h := int(float64(r.H) * frac)
	if h < 0 {
		h = 0
	}
	if h > r.H {
		h = r.H
	}
	return buffer.Rect{X: r.X, Y: r.Y, W: r.W, H: h}
}

// split solves weights along d and returns the cell sizes.
func split(d layout.Direction, weights []int, spacing, available int) []int {
	cs := make([]layout.Constraint, len(weights))
	for i, w := range weights {
		if w > 0 {
			cs[i] = layout.Fill(w)
		} else {
			cs[i] = layout.Length(0)
		}
	}
	return layout.Solve(d, cs, spacing, available)
}

// Bounds returns the screen's rectangle.
func (d *dashboard) Bounds() buffer.Rect { return d.bounds }

// SetBounds resizes the screen and re-solves the grid. It is the whole resize
// contract: the application recomputes bounds, and the widgets adapt on their next
// Draw from the rect they are given (ADR 0007 §3).
func (d *dashboard) SetBounds(r buffer.Rect) {
	d.bounds = r
	d.layoutWidgets()
	// A field the layout depends on has changed outside the widgets, so each of them
	// is told its cache is stale. Without this a change lands whenever the rect next
	// changes, which is the failure mode Invalidate exists to prevent.
	for _, w := range d.focusable {
		w.Invalidate()
	}
	d.bar.Invalidate()
	d.gauge.Invalidate()
	d.meter.Invalidate()
	d.spark.Invalidate()
	d.chart.Invalidate()
}

// Invalidate satisfies termmosaic.Widget. Every widget here repaints its whole
// rectangle every frame, so there is no finer-grained state to mark.
func (d *dashboard) Invalidate() {}

// Handle routes an event: the search mode first, then the focused widget, then the
// application's own keys.
func (d *dashboard) Handle(ev termmosaic.Event) bool {
	if d.searching {
		if d.handleSearch(ev) {
			return true
		}
	}
	switch ev.Kind {
	case termmosaic.EventKey:
		return d.handleKey(ev)
	case termmosaic.EventResize:
		d.SetBounds(buffer.Rect{W: ev.Size.W, H: ev.Size.H})
		return true
	case termmosaic.EventMouse:
		return d.handleMouse(ev)
	default:
		return false
	}
}

// handleKey is the application's own routing, and it is three keys long on purpose:
// everything else belongs to the focused widget.
func (d *dashboard) handleKey(ev termmosaic.Event) bool {
	switch ev.Key {
	case termmosaic.KeyTab, termmosaic.KeyBacktab:
		d.cycleFocus(ev.Key == termmosaic.KeyBacktab)
		return true
	case termmosaic.KeyEscape:
		// Leaving the search clears the pager's query too: a highlighted log with no
		// visible reason is worse than an unhighlighted one.
		d.searching = false
		d.query.Reset()
		d.pager.SetQuery("")
		return true
	}
	switch ev.Rune {
	case 'q':
		return true // the caller quits; see run
	case '/':
		d.searching = true
		d.query.Reset()
		return true
	}
	if d.focus < len(d.focusable) {
		return d.focusable[d.focus].Handle(ev)
	}
	return false
}

// handleSearch feeds the pager's query while the search mode is active.
//
// This is the shape a read-only widget forces on an application: the pager takes a
// string and nothing else, and finding where the query is a Pager decision while
// receiving it is the application's.
func (d *dashboard) handleSearch(ev termmosaic.Event) bool {
	if ev.Kind != termmosaic.EventKey {
		return false
	}
	switch ev.Key {
	case termmosaic.KeyEnter:
		d.pager.SetQuery(d.query.String())
		d.searching = false
		return true
	case termmosaic.KeyBackspace:
		s := d.query.String()
		if s != "" {
			d.query.Reset()
			d.query.WriteString(s[:len(s)-1])
		}
		return true
	}
	if ev.Rune >= ' ' && ev.Rune != 0x7f {
		d.query.WriteRune(ev.Rune)
		return true
	}
	return false
}

// handleMouse offers the event to every focusable widget, in order, and takes focus
// from the one that consumed it: a click on a table row both selects it and makes
// the table the keyboard's target, which is what a user expects and what no widget
// can do on its own.
func (d *dashboard) handleMouse(ev termmosaic.Event) bool {
	for i, w := range d.focusable {
		if w.Handle(ev) {
			d.setFocus(i)
			return true
		}
	}
	return false
}

// cycleFocus moves the keyboard's target by one widget.
func (d *dashboard) cycleFocus(back bool) {
	if len(d.focusable) == 0 {
		return
	}
	step := 1
	if back {
		step = -1
	}
	d.setFocus((d.focus + step + len(d.focusable)) % len(d.focusable))
}

// setFocus focuses the i-th focusable widget and unfocuses the rest. A widget that
// keeps its selection while unfocused is what makes tabbing back to it useful.
func (d *dashboard) setFocus(i int) {
	if i < 0 || i >= len(d.focusable) {
		return
	}
	d.focus = i
	for j, w := range d.focusable {
		w.SetFocused(j == i)
	}
}

// FocusIndex returns which widget has the keyboard, which is what the example's
// golden test asserts on after a simulated Tab.
func (d *dashboard) FocusIndex() int { return d.focus }

// Tick advances every live widget by one step.
//
// One number drives the whole screen: the widgets do not know about each other, which
// is the composition property worth demonstrating. The values are deliberately
// different functions of the tick — a ramp, a wave, a load that crosses the meter's
// zones — so a frame shows several shapes moving at once.
func (d *dashboard) Tick() {
	d.tick++
	phase := float64(d.tick)

	d.bar.Set(frac(phase / 40))
	d.gauge.Set(50 + 45*wave(phase/9))
	d.meter.Set(55 + 35*wave(phase/17))
	// The series is rewritten IN PLACE rather than rebuilt: a dashboard updates its
	// data, it does not allocate a new array every frame, and SetValues takes the
	// slice by reference.
	d.series = historyInto(d.series, d.tick)
	d.spark.SetValues(d.series)
	d.list.SetOffset((d.tick * 7) % (requestCount / 2))
}

// wave returns a value in [-1, 1] from a cheap triangle, so the gauge and the meter
// move through their whole range rather than sitting near the middle.
func wave(phase float64) float64 {
	x := phase - float64(int64(phase))
	if x < 0 {
		x += 1
	}
	if x > 0.5 {
		return 4 - 4*x
	}
	return 4*x - 1
}

// frac returns v reduced into [0, 1].
func frac(v float64) float64 {
	x := v - float64(int64(v))
	if x < 0 {
		x += 1
	}
	return x
}

// Draw paints the nine widgets in order.
//
// There is no clearing and no composition buffer: each widget repaints its own
// rectangle (ADR 0007 §1 rule 3), so the diff has nothing stale to skip and this
// method allocates nothing.
func (d *dashboard) Draw(buf *buffer.Buffer) {
	if d.bounds.Empty() {
		return
	}
	buf.FillRect(d.bounds, stBody.Resolved().Blank())
	d.bar.Draw(buf)
	d.gauge.Draw(buf)
	d.meter.Draw(buf)
	d.spark.Draw(buf)
	d.chart.Draw(buf)
	d.list.Draw(buf)
	d.table.Draw(buf)
	d.tree.Draw(buf)
	d.pager.Draw(buf)
}

// frame configures a widget's block with this application's chrome. It is the only
// place the screen touches a border, and it does so through widgets/block rather
// than by drawing one.
func frame(b *block.Block, title string) {
	b.SetBorder(buffer.BorderPlain)
	b.SetBorderStyle(stEdge)
	b.SetBackground(stPanel)
	b.SetTitleString(title, stAccent)
}

// col returns a column with a fixed width and a header title.
func col(title string, width int) data.Column {
	return data.Column{
		Title:       []buffer.Span{buffer.NewSpan(title, stMuted)},
		Width:       width,
		HeaderStyle: stMuted,
	}
}

// colGrow returns a column that absorbs whatever width is left over, which is what
// the path column does: a fixed path column would leave a gap on a wide terminal.
func colGrow(title string, grow int) data.Column {
	return data.Column{
		Title:       []buffer.Span{buffer.NewSpan(title, stMuted)},
		Grow:        grow,
		HeaderStyle: stMuted,
	}
}

// ---------------------------------------------------------------------------
// data
// ---------------------------------------------------------------------------

// requestItems builds n list items. A slice of a hundred thousand is a few megabytes
// and is built once, here, which is the only place this example spends memory.
func requestItems(n int) []data.ListItem {
	verbs := []string{"GET", "POST", "PUT", "DELETE"}
	paths := []string{"/api/users", "/api/orders", "/api/health", "/api/search", "/api/session"}
	out := make([]data.ListItem, n)
	for i := range out {
		out[i] = data.ListItem{Label: fmt.Sprintf("%6d %-6s %s", i, verbs[i%len(verbs)], paths[i%len(paths)])}
	}
	return out
}

// requestRows builds n table rows, including values longer than their column so the
// truncation and the ellipsis are visible.
func requestRows(n int) []data.Row {
	verbs := []string{"GET", "POST", "DELETE"}
	routes := []string{"/v1/users", "/v1/users/:id", "/v1/orders", "/v1/orders/:id/lineItems", "/v1/health"}
	statuses := []string{"200", "201", "404", "500"}
	out := make([]data.Row, n)
	for i := range out {
		out[i] = data.Row{Cells: []data.Cell{
			{Text: fmt.Sprintf("%08x", i*2654435761)},
			{Text: verbs[i%len(verbs)]},
			{Text: routes[i%len(routes)]},
			{Text: fmt.Sprintf("handler%d.handle", i%7)},
			{Text: statuses[i%len(statuses)]},
			{Text: fmt.Sprintf("%dms", 3+(i*17)%900)},
		}}
	}
	return out
}

// serviceTree builds a three-level service hierarchy.
func serviceTree() []data.Node {
	svc := func(name string, kids ...data.Node) data.Node {
		return data.Node{Label: name, Expanded: len(kids) > 0, Children: kids}
	}
	return []data.Node{
		svc("gateway",
			svc("auth", svc("login"), svc("logout"), svc("refresh")),
			svc("router", svc("static"), svc("proxy")),
		),
		svc("inventory",
			svc("catalog", svc("images"), svc("pricing")),
			svc("stock"),
		),
		svc("billing",
			svc("invoices"),
			svc("payments", svc("stripe"), svc("ledger")),
		),
	}
}

// logText builds n lines of log output, long enough that the pager wraps them at any
// width this example runs at.
func logText(n int) string {
	var b strings.Builder
	levels := []string{"INFO", "INFO", "INFO", "WARN", "ERROR"}
	msgs := []string{
		"request completed",
		"cache miss, falling back to the origin",
		"connection pool at 80% capacity",
		"retrying upstream call after a timeout",
		"upstream returned 503, shedding load",
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "12:%02d:%02d %-5s %-46s req=%08x\n",
			(i/60)%60, i%60, levels[i%len(levels)], msgs[i%len(msgs)], i*2654435761)
	}
	return b.String()
}

// seriesLen is how many samples the sparkline holds.
const seriesLen = 60

// historyInto fills dst with a bounded pseudo-random walk advanced by n steps and
// returns it, growing dst only when it is too short.
//
// A sparkline over a monotonic ramp shows nothing, hence the walk; a walk from a
// random source would make the example's own test unreproducible, hence the
// determinism. Writing into dst rather than returning a fresh slice is what keeps a
// ticking frame allocation-free, and it is how a real dashboard feeds a widget that
// holds its data by reference.
func historyInto(dst []float64, n int) []float64 {
	if cap(dst) < seriesLen {
		dst = make([]float64, seriesLen)
	}
	dst = dst[:seriesLen]
	v := 40.0
	for i := range dst {
		v += 6*wave(float64(i+n)/7) - 1.5
		if v < 5 {
			v = 5
		}
		if v > 95 {
			v = 95
		}
		dst[i] = v
	}
	return dst
}

// ---------------------------------------------------------------------------
// the terminal
// ---------------------------------------------------------------------------

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "dashboard:", err)
		os.Exit(1)
	}
}

// run opens the terminal, wires the renderer and the input source, and runs until
// the user quits.
func run() (err error) {
	t, err := term.Open(os.Stdin, os.Stdout, os.Getenv)
	if err != nil {
		return err
	}
	// Restoring the terminal is not optional on any exit path: a program that leaves
	// a terminal in raw mode has broken the user's shell. So a failure to do it is
	// itself worth reporting, which is why the deferred close folds its error into
	// the named return rather than dropping it.
	defer func() { err = errors.Join(err, t.Close()) }()

	w, h := t.Size()
	caps := t.Capabilities()

	r := render.New(term.NewSink(os.Stdout), render.Config{
		Width:   w,
		Height:  h,
		Caps:    caps,
		NoColor: render.NoColorFromEnv(os.Getenv),
	})
	r.SetCursor(render.Cursor{Valid: true, Visible: false})

	board := newDashboard(w, h)
	r.SetRoot(board)

	if err := t.EnterRawMode(); err != nil {
		return err
	}
	if err := t.EnterAltScreen(); err != nil {
		_ = t.LeaveRawMode()
		return err
	}
	if err := r.Reset(); err != nil {
		return err
	}

	quit := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(quit) }) }

	src := input.NewSource(t, input.DefaultConfig())

	// Input. One goroutine over the ordered event stream, exactly as examples/hello
	// does it: resizes and keys are the same channel, so neither can be lost.
	go func() {
		for ev := range src.Events() {
			switch ev.Kind {
			case termmosaic.EventKey:
				board.Handle(ev)
				if ev.Rune == 'q' && !board.searching {
					stop()
					return
				}
			case termmosaic.EventResize:
				// Resize for EVERY event and let the pacer decide when to paint:
				// that is the anti-jank rule of ADR 0007 §6, and it needs no code
				// beyond calling Resize here.
				r.Resize(ev.Size.W, ev.Size.H)
				board.SetBounds(buffer.Rect{W: ev.Size.W, H: ev.Size.H})
			}
		}
		stop()
	}()

	// The heartbeat. The live widgets need something to invalidate them, and this is
	// it: a real application would invalidate on a data change instead.
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ticker.C:
				board.Tick()
			case <-quit:
				return
			}
		}
	}()

	for {
		select {
		case <-quit:
			return nil
		default:
		}
		if _, err := r.Render(); err != nil {
			return err
		}
		if r.NeedsFrame() {
			time.Sleep(tickInterval / 4)
		}
	}
}
