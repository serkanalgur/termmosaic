package main

// The frame: a market turned into everything the draw path will need, already
// formatted.
//
// # Why formatting happens here and not in Draw
//
// A dashboard's numbers are floats, and drawing a float means either fmt.Sprintf
// or a hand-rolled digit loop. Both allocate, and Draw is the frame path — ADR
// 0002's diff is held to zero allocations and there is no reason for the widget
// tree above it to be worse. So every string on this screen is built HERE, on the
// render goroutine but outside Draw, and stored as a plain string or a prebuilt
// []buffer.Span. Draw then does nothing but read them and write cells.
//
// That is what makes TestDashboardFrameIsAllocationFree a real assertion rather
// than a hope: the only thing between the frame and the screen is buffer.SetSpans
// and the widgets' own cached truncation.
//
// # Accessibility is decided here too
//
// Every direction this screen reports — a change, a move, a breach — carries a
// sign AND an arrow glyph, and the arrow has an ASCII rung. Green and red are
// layered on top as emphasis. A dashboard whose only up/down signal is a hue is
// unreadable to a colour-blind reader and to every monochrome terminal, and this
// is the catalog's showcase, so it is the one thing here that must not be
// negotiable.

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/data"
	"github.com/serkanalgur/termmosaic/widgets/viz"
)

// Direction of a move, as a value rather than a colour.
//
// It is a signed integer because a sign is the signal that survives a monochrome
// terminal, and it is stored rather than resolved to a style because the STYLE is
// the application's decision (ADR 0008 ships no default colours) while the
// direction is a fact about the data.
type direction int8

const (
	dirNone direction = iota
	dirUp
	dirDown
	dirFlat
)

// arrowUp and arrowDown mark a move without relying on hue.
//
// These are U+25B2 and U+25BC — Geometric Shapes, explicitly outside the
// box-drawing block that buffer/border.go owns, and one cell wide under
// buffer.RuneWidth. They are not emoji and do not vary in width.
//
// Each has an ASCII counterpart because a terminal whose caps report no Unicode
// gets the same information in a different shape rather than a blank cell, which
// is the whole point of the degradation ladder.
const (
	arrowUp        = '▲' // BLACK UP-POINTING TRIANGLE
	arrowDown      = '▼' // BLACK DOWN-POINTING TRIANGLE
	arrowFlat      = '='
	arrowUpASCII   = '^'
	arrowDownASCII = 'v'
)

// mark is the arrow for a direction on the given rung.
func mark(d direction, ascii bool) rune {
	switch d {
	case dirUp:
		if ascii {
			return arrowUpASCII
		}
		return arrowUp
	case dirDown:
		if ascii {
			return arrowDownASCII
		}
		return arrowDown
	case dirFlat:
		return arrowFlat
	default:
		return ' '
	}
}

// numTiles is how many headline figures the KPI row carries.
const numTiles = 4

// Tile indices, named so that a reordering of the row cannot silently move the
// spot rate into the "window low" slot.
const (
	tileSpot = iota
	tileChange
	tileHigh
	tileLow
)

// chartPairs is how many movers the bar chart shows. Six is what a glance can
// compare; forty is a table, and the table is already on the screen.
const chartPairs = 6

// absentNote is the note a tile carries when its value is absent: WHAT is absent,
// rather than a bare placeholder that could equally mean "too narrow to show" or
// "failed to fetch".
const absentNote = "not fetched"

// noDataPlaceholder is what every figure reads when there is nothing to report.
//
// It is a word rather than a zero, and a word rather than a dash, because "0.00"
// on a currency is a rate and "--" is obviously the absence of one. A dashboard
// that renders an absent number as zero has told a lie that looks like data.
const noDataPlaceholder = "n/a"

// meterLow and meterHigh bound the meter, in basis points of day-on-day change.
// They are the scale the ZONES are expressed in too, so a zone boundary and the
// end of the scale are the same arithmetic.
const (
	meterLow  = -200.0
	meterHigh = 200.0
)

// meterFlat is the half-width of the "flat" zone in basis points: a move inside
// it is not worth a colour change and not worth an arrow either, and saying so
// with a named band is more honest than rounding it to zero.
const meterFlat = 20.0

// snapshot is one fetch cycle's outcome: the data, the error, and when it
// arrived. It is immutable once stored, which is what lets the store hand the
// same pointer to a reader that is building a frame while the fetcher is already
// building the next one.
type snapshot struct {
	m      *market
	err    error
	at     time.Time
	source string
	ok     bool // at least one source answered
	gen    uint64
}

// tileText is one KPI tile's three strings, pre-formatted.
type tileText struct {
	label string
	value string
	note  string
	dir   direction
}

// frame is everything the draw path reads. Built once per data change; only the
// status line is rebuilt on the heartbeat.
type frame struct {
	// gen is the store generation this frame was built from, carried through so
	// the heartbeat can tell a republish of the frame already applied from a new
	// one without the board having to hold the store.
	gen uint64

	// status is the single-row footer.
	status string
	// stale marks a footer that carries a failure, which is the only thing that
	// changes its colour.
	stale bool
	// noData means there is nothing to plot at all, and the board draws a
	// diagnostic panel instead of the bands.
	noData bool
	// noDataBody is the reason, already flattened to one line by oneLine.
	noDataBody string

	tiles  [numTiles]tileText
	series []float64

	rows  []data.Row
	chart []viz.Datum

	gaugeLo, gaugeHi, gaugeV float64
	meterV                   float64
}

// buildFrame turns a snapshot into a frame.
//
// It is the whole of the presentation logic, and it is deliberately a pure
// function of its arguments plus the formatting helpers: no clock, no globals
// that change, no widget state. That is what lets the offline and live paths
// produce byte-identical output from identical inputs, which is what makes the
// goldens worth having.
func buildFrame(s *snapshot) *frame {
	f := &frame{gen: s.gen}
	f.noData = !s.ok
	f.stale = s.err != nil
	f.status = statusLine(s)

	if !s.ok {
		// Every figure reads as absent rather than as zero, and the reason is on
		// screen. A dashboard that shows a grid of zeros because a fetch failed has
		// produced a screen that is confidently wrong.
		//
		// The tiles are still BUILT even though the dashboard replaces the bands with
		// the failure panel: the panel is not the only thing that can read them, and
		// a widget whose fields are unset in one of its two modes is a widget whose
		// behaviour differs between them for reasons nobody wrote down.
		f.tiles[tileSpot] = tileText{label: tileLabel(tileSpot), value: noDataPlaceholder, note: absentNote}
		f.tiles[tileChange] = tileText{label: tileLabel(tileChange), value: noDataPlaceholder, note: absentNote}
		f.tiles[tileHigh] = tileText{label: tileLabel(tileHigh), value: noDataPlaceholder, note: absentNote}
		f.tiles[tileLow] = tileText{label: tileLabel(tileLow), value: noDataPlaceholder, note: absentNote}
		f.noDataBody = failureText(s)
		return f
	}

	m := s.m
	buildTiles(f, m)
	f.series = m.history
	f.rows = buildRows(m)
	f.chart = buildChart(m)
	f.meterV = changeBps(m, m.primary)

	spot, haveSpot := m.spot()
	lo, hi, spread := m.extremes()
	switch {
	case !haveSpot:
		f.gaugeV = 0
	case !spread:
		// A flat window has no position within it. The gauge reads zero rather
		// than dividing by a zero span, and the note on the tile says the window
		// did not move, so a zero here is explained rather than merely shown.
		f.gaugeLo, f.gaugeHi = spot, spot+1
	default:
		f.gaugeLo, f.gaugeHi = lo, hi
		f.gaugeV = spot
	}
	return f
}

// tileLabel is a tile's title, as a function of its index so that the labels and
// the constants cannot drift apart.
func tileLabel(i int) string {
	switch i {
	case tileSpot:
		return primaryPair + "/USD spot"
	case tileChange:
		return "1d change"
	case tileHigh:
		return "window high"
	case tileLow:
		return "window low"
	default:
		return "?"
	}
}

// buildTiles fills the KPI row.
//
// The four figures are the spot rate, the day-on-day change, and the window's two
// extremes — which between them answer "where is it now, which way is it going,
// and how far has it come" without the reader cross-referencing another panel.
func buildTiles(f *frame, m *market) {
	spot, haveSpot := m.spot()
	f.tiles[tileSpot] = tileText{
		label: tileLabel(tileSpot),
		value: orNA(haveSpot, rateText(spot)),
		note:  orDefault(m.asOf != "", m.asOf, "no publication date"),
	}

	chg, haveChg := m.changePct(m.primary)
	dir := changeDir(chg, haveChg)
	f.tiles[tileChange] = tileText{
		label: tileLabel(tileChange),
		value: signPct(chg, haveChg),
		note:  arrowNote(dir, "since previous publication"),
	}

	// A window with no spread has no extremes to report, and reporting the spot
	// rate as both the high and the low would be a true statement that reads as a
	// measurement. The note says "no history" in that case, which is why the value
	// and the note are decided together rather than independently.
	lo, hi, spread := m.extremes()
	f.tiles[tileHigh] = tileText{
		label: tileLabel(tileHigh),
		value: orNA(spread, rateText(hi)),
		note:  dateOf(m, hi, true),
	}
	f.tiles[tileLow] = tileText{
		label: tileLabel(tileLow),
		value: orNA(spread, rateText(lo)),
		note:  dateOf(m, lo, false),
	}
}

// dateOf returns the publication date on which the primary series was at v, and
// whether it ever was. A window extreme that the series never reached is
// reported as such rather than being pinned to an arbitrary end date.
func dateOf(m *market, v float64, wantMax bool) string {
	if len(m.history) != len(m.dates) || len(m.dates) == 0 {
		return "no history"
	}
	at := -1
	for i, s := range m.history {
		if s == v && (at < 0 || (wantMax && i > at) || (!wantMax && i < at)) {
			at = i
		}
	}
	if at < 0 {
		return "no history"
	}
	return m.dates[at]
}

// row is one line of the detail table before it becomes a data.Row.
type row struct {
	symbol string
	rate   float64
	have   bool
	chg    float64
	hasChg bool
}

// buildRows assembles the detail table: every tracked currency plus every tracked
// coin, ordered by the size of the move so the rows that matter are at the top
// rather than in whatever order the API happened to use.
//
// One mixed table is a deliberate choice over two tables. FX and crypto are
// different instruments on different clocks — the FX figure is one ECB
// publication apart, the crypto figure is a rolling 24 hours — and a reader who
// cannot tell which is which will compare them wrongly. The panel title says so,
// and the direction column carries the same arrow for both, because the
// accessibility rule does not have an exception for instruments.
func buildRows(m *market) []data.Row {
	rows := make([]row, 0, len(trackedFX)+len(trackedCrypto))
	for _, code := range trackedFX {
		r, ok := m.rates[code]
		if !ok {
			continue
		}
		chg, has := m.changePct(code)
		rows = append(rows, row{symbol: code + "/USD", rate: r, have: true, chg: chg, hasChg: has})
	}
	for _, id := range trackedCrypto {
		q, ok := m.crypto[id]
		if !ok || q.USD <= 0 {
			continue
		}
		sym := cryptoSymbols[id]
		if sym == "" {
			sym = strings.ToUpper(id)
		}
		rows = append(rows, row{symbol: sym + "/USD", rate: q.USD, have: true, chg: q.Change, hasChg: true})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return math.Abs(rows[i].chg) > math.Abs(rows[j].chg)
	})

	out := make([]data.Row, 0, len(rows))
	for _, r := range rows {
		d := changeDir(r.chg, r.hasChg)
		out = append(out, data.Row{Cells: []data.Cell{
			{Text: r.symbol, Style: stSymbol},
			{Text: rateText(r.rate), Style: stFigure},
			// The arrow AND the sign, in one cell. Two signals rather than one is
			// deliberate: the sign is what a reader compares down the column, and the
			// arrow is what survives when the number is truncated at a narrow width —
			// which is exactly when a reader most needs to know the direction.
			{Text: arrowSign(r.chg, r.hasChg), Style: changeStyle(d)},
		}})
	}
	return out
}

// arrowSign formats a change as an arrow and an explicitly signed percentage.
//
// The space between them is what makes the cell readable down a column: the arrows
// align, and the numbers align, and the reader is comparing one against the other
// rather than parsing a run of mixed glyphs and digits.
func arrowSign(v float64, ok bool) string {
	if !ok {
		return noDataPlaceholder
	}
	return string(mark(changeDir(v, ok), asciiRung)) + " " + signPct(v, ok)
}

// buildChart assembles the bar chart of moves.
//
// The VALUES ARE ABSOLUTE and the panel says so, because BarChart documents that
// a negative value draws as no bar at all. Feeding it signed changes would
// therefore render every losing pair as an empty row — which reads as "did not
// move" rather than "moved down", and is exactly the kind of plausible lie a
// chart should not tell. The direction lives in each bar's LABEL, where the arrow
// puts it next to the bar it belongs to.
//
// That costs the chart the one thing a diverging chart would have given, which is
// a left/right axis. See the report: a diverging bar chart is the primitive this
// example wanted and the catalog does not have.
func buildChart(m *market) []viz.Datum {
	movers := make([]row, 0, len(trackedFX)+len(trackedCrypto))
	for _, code := range trackedFX {
		if _, ok := m.rates[code]; !ok {
			continue
		}
		chg, has := m.changePct(code)
		if !has {
			continue
		}
		movers = append(movers, row{symbol: code, chg: chg})
	}
	for _, id := range trackedCrypto {
		q, ok := m.crypto[id]
		if !ok {
			continue
		}
		sym := cryptoSymbols[id]
		if sym == "" {
			sym = strings.ToUpper(id)
		}
		movers = append(movers, row{symbol: sym, chg: q.Change})
	}
	sort.SliceStable(movers, func(i, j int) bool {
		return math.Abs(movers[i].chg) > math.Abs(movers[j].chg)
	})
	if len(movers) > chartPairs {
		movers = movers[:chartPairs]
	}

	out := make([]viz.Datum, 0, len(movers))
	for _, mv := range movers {
		d := changeDir(mv.chg, true)
		// The arrow is INSIDE the label rather than beside the bar, so the sign
		// and the magnitude cannot be read as belonging to different rows.
		out = append(out, viz.Datum{
			Label: mv.symbol + " " + string(mark(d, asciiRung)),
			Value: math.Abs(mv.chg) * 100, // basis points
			Style: changeStyle(d),
		})
	}
	return out
}

// statusLine is the one-row footer: what the numbers are, how old they are, and
// what failed if anything did.
//
// The three parts are separated by a middot rather than a bar so the row does not
// read as a table column, and each part is short enough to survive truncation at
// the narrowest width this dashboard is drawn at.
func statusLine(s *snapshot) string {
	var b strings.Builder
	b.WriteString("markets")
	b.WriteString("  ")
	b.WriteString(s.source)
	if s.ok {
		b.WriteString("  ")
		b.WriteString("as of ")
		b.WriteString(s.m.asOf)
		b.WriteString("  ")
		b.WriteString("fetched ")
		b.WriteString(s.at.UTC().Format("15:04:05") + "Z")
	} else {
		b.WriteString("  ")
		b.WriteString("NO DATA")
	}
	if s.err != nil {
		b.WriteString("  ")
		if s.ok {
			b.WriteString("STALE: ")
		} else {
			b.WriteString("FAILED: ")
		}
		b.WriteString(failureText(s))
		b.WriteString("  retrying every ")
		b.WriteString(strconv.Itoa(int(refreshInterval/time.Second)) + "s")
	}
	return b.String()
}

// failureText names WHICH source failed, preferring the source layer's own record.
//
// A market's failures field already carries "fx …" and "crypto …" with each
// source's reason, and the cycle's error is only their concatenation. So the footer
// reads the field when it has one: "STALE: crypto HTTP 429" tells a reader which
// half of their screen is out of date, and "STALE: HTTP 429" does not.
func failureText(s *snapshot) string {
	if s.m != nil && len(s.m.failures) > 0 {
		return oneLine(errors.New(strings.Join(s.m.failures, "; ")))
	}
	return oneLine(s.err)
}

// dirOf classifies a change already expressed in BASIS POINTS.
//
// Basis points rather than percent is the whole design of this function: the meter
// is driven in basis points and its own zone test is `v >= From && v < To`, so
// classifying in the same unit against the same threshold is what guarantees the
// arrow and the band cannot disagree. Converting here instead would introduce a
// rounding step — 0.2*100 is 20.000000000000004 — and a value exactly on the
// boundary would land in a different band in each panel.
//
// A move inside meterFlat is FLAT rather than rounded to a sign, which is also what
// the meter says: the two are one function of one number.
func dirOf(bps float64, ok bool) direction {
	if !ok {
		return dirNone
	}
	//
	// The comparisons are ASYMMETRIC on purpose, because the Meter's own zone test
	// is: `v >= From && v < To`. So the down band is To:-meterFlat and the up band
	// is From:meterFlat, which makes -20bps flat and +20bps up. Writing `>` on both
	// sides here would put +20bps in the flat band and the meter would draw it in the
	// up one — an up arrow beside an up bar would be right by accident, and the same
	// mistake at the other edge would be visibly wrong.
	switch {
	case bps >= meterFlat:
		return dirUp
	case bps < -meterFlat:
		return dirDown
	default:
		return dirFlat
	}
}

// changeDir is dirOf for a change expressed in PERCENT, converting once.
func changeDir(chg float64, ok bool) direction {
	if !ok {
		return dirNone
	}
	return dirOf(chg*100, true)
}

// changeBps returns a currency's day-on-day move in basis points, zero when there
// is nothing to measure.
func changeBps(m *market, code string) float64 {
	chg, ok := m.changePct(code)
	if !ok {
		return 0
	}
	return chg * 100
}

// changeStyle is the one place a direction becomes a colour, so that the palette
// and the direction vocabulary cannot drift apart.
func changeStyle(d direction) buffer.Style {
	switch d {
	case dirUp:
		return stUp
	case dirDown:
		return stDown
	default:
		return stFlat
	}
}

// arrowNote is the second line of a tile whose value already carries the number:
// the arrow, then the explanation.
//
// The arrow repeats on purpose. It is the signal that survives when the number is
// too wide to draw in the tile, and it is what a reader with no colour perception
// looks at first.
func arrowNote(d direction, tail string) string {
	return string(mark(d, asciiRung)) + " " + tail
}

// rateText formats a rate at the precision a reader needs.
//
// Five decimals because that is the ECB's own precision for the majors and one
// fewer would print two currencies as the same number on a quiet day. Crypto is
// quoted in units and hundreds, and gets no decimals at all — "85492" rather than
// "85492.00000".
func rateText(v float64) string {
	switch {
	case v <= 0:
		return noDataPlaceholder
	case v >= 100:
		return strconv.FormatFloat(v, 'f', 2, 64)
	case v >= 1:
		return strconv.FormatFloat(v, 'f', 4, 64)
	default:
		return strconv.FormatFloat(v, 'f', 5, 64)
	}
}

// signPct formats a percentage with an EXPLICIT sign.
//
// The sign is mandatory rather than conditional on negativity: a column where
// "+0.65" and "0.65" mean the same thing is a column where a reader has to know
// which convention is in force, and "always signed" is the convention that needs
// no explaining.
func signPct(v float64, ok bool) string {
	if !ok {
		return noDataPlaceholder
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	if !strings.HasPrefix(s, "-") {
		s = "+" + s
	}
	return s + "%"
}

// orNA returns yes's formatted value or the absent placeholder.
func orNA(yes bool, s string) string {
	if !yes {
		return noDataPlaceholder
	}
	return s
}

// orDefault returns s when cond holds and otherwise alt, which is the readable
// form of "only mention this if it exists".
func orDefault(cond bool, s, alt string) string {
	if cond {
		return s
	}
	return alt
}

// asciiRung is the one boolean every widget in this example passes to buffer's
// shared ASCII switch, set from the terminal's negotiated caps at construction.
// Nothing in the widget path branches on caps directly (ADR 0008 Decision 4).
var asciiRung bool

// setRung records the terminal's Unicode capability for the whole example.
func setRung(unicode bool) { asciiRung = !unicode }
