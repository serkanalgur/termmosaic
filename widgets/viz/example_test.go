package viz_test

import (
	"fmt"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/viz"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// show renders root on a w-by-h screen and returns the rows with trailing
// spaces trimmed.
//
// Every Output comment in this package is the CELL GRID the renderer produced,
// read back out of a headless.MemorySink — not a description of it and not the
// escape sequences a real terminal would have received. That matters more here
// than in the text packages: these five widgets are largely made of glyphs and
// attributes, and the only honest way to show one is to show the cells.
//
// widgettest.Capture rather than Render: an Example has no testing.TB to hand
// over, and Capture reports the same failure as an error rather than t.Fatal.
func show(w, h int, root termmosaic.Widget) string {
	sink, err := widgettest.Capture(w, h, 1, root)
	if err != nil {
		panic(err)
	}
	return widgettest.Screen(sink)
}

// track is the style a bar's unfilled track is painted in.
//
// It carries a BACKGROUND rather than merely a dimmer hue, and that is the
// point: a bar whose track is only a quieter colour is an invisible track. With
// nothing painted there is no band for the fill to be read against, and the
// widget loses the property its own accessibility notes promise — that the shape
// is legible without colour.
var track = buffer.NewStyle(buffer.DefaultColour, buffer.DefaultColour, buffer.AttrFaint)

// chrome gives a widget the border, title and padding every one of them shares,
// because a capture without it is a rectangle of floating glyphs.
func chrome(b *block.Block, title string) *block.Block {
	b.SetBorder(buffer.BorderRounded)
	b.SetTitleString(title, buffer.Style{})
	b.SetTitleAlign(geometry.AlignLeft)
	return b
}

// Example is a dashboard of two of the five, side by side: a determinate bar
// over a series. It shows the shape most of these widgets are used in — the
// widget draws its own chrome, and a caller only supplies the value and the data.
func Example() {
	bar := viz.NewProgressBar(buffer.Rect{W: 34, H: 3})
	bar.SetLabel("downloading modules", buffer.Style{})
	bar.Set(0.62)
	bar.TrackStyle = track
	chrome(bar.Block(), "")

	trend := viz.NewSparkline(buffer.Rect{W: 34, H: 3})
	trend.SetValues([]float64{3, 7, 4, 9, 6, 11, 8, 14, 10, 17, 13, 21})
	chrome(trend.Block(), "requests")

	fmt.Println(show(34, 3, bar))
	fmt.Println(show(34, 3, trend))
	// Output:
	// ╭────────────────────────────────╮
	// │downloading modules  ███████    │
	// ╰────────────────────────────────╯
	// ╭ requests ──────────────────────╮
	// │⠁⠁⠁⠁⠑⠓                          │
	// ╰────────────────────────────────╯
}

// ---------------------------------------------------------------------------
// ProgressBar
// ---------------------------------------------------------------------------

// ExampleProgressBar is a determinate bar with a label and a percentage.
//
// The bar is PrioAlways — it is the widget — and the label and the percentage
// share what is left, which is why four cells move between the number and the bar
// when the number is turned off. That is a layout change, so it is a setter and
// not a field.
func ExampleProgressBar() {
	bar := viz.NewProgressBar(buffer.Rect{W: 32, H: 1})
	bar.SetLabel("go build ./...", buffer.Style{})
	bar.Set(0.42)
	bar.TrackStyle = track
	// The number is OFF by default and this is how it is asked for. It is a
	// setter rather than a field because turning it on moves four cells from the
	// bar to the number, which is a layout change.
	bar.SetPercentage(true)
	fmt.Printf("percentage shown: %v\n", bar.Percentage())
	fmt.Println(show(32, 1, bar))
	// Output:
	// percentage shown: true
	// go build ./...  ████▁       42%
}

// ExampleProgressBar_clamping is the contract rather than the picture. Set takes
// a ratio in [0, 1] and clamps it, and Value always reports a ratio back — so a
// program computing one from a byte count cannot put the widget into a state it
// has no drawing for.
func ExampleProgressBar_clamping() {
	bar := viz.NewProgressBar(buffer.Rect{W: 22, H: 1})
	for _, v := range []float64{-1, 0, 0.5, 1, 2} {
		bar.Set(v)
		fmt.Printf("Set(%5.1f) -> Value() %.2f\n", v, bar.Value())
	}
	// Output:
	// Set( -1.0) -> Value() 0.00
	// Set(  0.0) -> Value() 0.00
	// Set(  0.5) -> Value() 0.50
	// Set(  1.0) -> Value() 1.00
	// Set(  2.0) -> Value() 1.00
}

// ExampleProgressBar_styledLabel is the escape hatch in the label, and the reason
// SetLabelSpans exists at all rather than only SetLabel: a label is a measured
// region, so several styled runs is a layout change and drops the layout cache
// the same way SetLabel does.
func ExampleProgressBar_styledLabel() {
	bar := viz.NewProgressBar(buffer.Rect{W: 40, H: 1})
	bar.SetLabelSpans([]buffer.Span{
		buffer.NewSpan("compiling ", buffer.Style{}),
		buffer.NewSpan("12 packages", buffer.NewStyle(
			buffer.DefaultColour, buffer.DefaultColour, buffer.AttrBold)),
	})
	bar.Set(0.75)
	bar.TrackStyle = track
	bar.SetPercentage(false)
	fmt.Println(show(40, 1, bar))
	// Output:
	// compiling 12 packages  █████████████
}

// ---------------------------------------------------------------------------
// Gauge
// ---------------------------------------------------------------------------

// ExampleGauge is a bounded value on a dial, with the bar fallback shown beside
// it because which of the two is drawn is decided from the interior's size rather
// than by the caller.
//
// POSITION carries the value here, which no colour and no amount of bar length
// can: a dial is read by where the needle sits. Below the dial's minimum the
// interior cannot hold a circle, so the widget draws a bar instead of a squashed
// circle, and the example shows both so the transition is visible.
func ExampleGauge() {
	dial := viz.NewGauge(buffer.Rect{W: 16, H: 7})
	dial.SetLabel("load", buffer.Style{})
	dial.Set(72)

	narrow := viz.NewGauge(buffer.Rect{W: 16, H: 3})
	narrow.SetLabel("load", buffer.Style{})
	narrow.Set(72)

	fmt.Printf("reading %.0f, ratio %.2f\n", dial.Reading(), dial.Ratio())
	fmt.Println(show(16, 7, dial))
	fmt.Println(show(16, 3, narrow))
	// Output:
	// reading 72, ratio 0.72
	//
	//       ⠁⠁⠁⠁
	//      ⠓⠛⠃⠃⠛⠉
	//      ⠙⠉⠁⠁⠓⠛
	//       ⠃⠛⠛⠃
	//
	// load 72
	// ███████████▸
}

// ---------------------------------------------------------------------------
// Meter
// ---------------------------------------------------------------------------

// ExampleMeter is a bounded value with named zones, and the ZONES are what
// separate a Meter from a ProgressBar: a progress bar says how far along it is,
// a meter says how much of a BUDGET is used and in which band that lands.
//
// The band name is drawn as text, which is the colour-independent signal: a meter
// in the critical zone SAYS the word.
func ExampleMeter() {
	m := viz.NewMeter(buffer.Rect{W: 34, H: 3})
	m.SetZones(viz.DefaultZones())
	m.Set(82)
	m.TrackStyle = track
	chrome(m.Block(), "disk")
	fmt.Println(show(34, 3, m))
	// Output:
	// ╭ disk ──────────────────────────╮
	// │warn                 +  |  82   │
	// ╰────────────────────────────────╯
}

// ExampleMeter_zones are the bands themselves, which is where a meter earns its
// keep. Two styles per zone rather than one, because a zone has to be visible
// BEFORE the value reaches it: the track shows the shape of the budget and the
// fill shows what has been spent of it.
//
// To <= From means the band runs to the end of the scale, which is how the top
// band is written — the alternative would be a sentinel nobody would remember.
func ExampleMeter_zones() {
	zones := []viz.Zone{
		{Name: "idle", From: 0, To: 40, Style: buffer.Style{}, FillStyle: buffer.Style{}},
		{Name: "busy", From: 40, To: 80, Style: buffer.Style{}, FillStyle: buffer.Style{}},
		{Name: "hot", From: 80, To: 0, Style: buffer.Style{}, FillStyle: buffer.Style{}},
	}

	// Meter.ActiveZone is the accessor a caller needs and Draw does not use: it
	// answers which band a value falls in without rendering anything, so a
	// program can colour a row or pick a sound from the same zones the bar draws.
	// met below is the same arithmetic over a list the caller has not installed
	// yet, which is what a settings screen wants while the user is still editing
	// the boundaries.
	for _, v := range []float64{10, 60, 95} {
		i, ok := met(v, zones)
		fmt.Printf("%5.0f -> zone %d (%q), in range: %v\n", v, i, zones[i].Name, ok)
	}

	m := viz.NewMeter(buffer.Rect{W: 34, H: 3})
	m.SetZones(zones)
	m.Set(95)
	m.TrackStyle = track
	fmt.Println(show(34, 3, m))
	// Output:
	// 10 -> zone 0 ("idle"), in range: true
	//    60 -> zone 1 ("busy"), in range: true
	//    95 -> zone 2 ("hot"), in range: true
	// hot           +        + |   95
}

// met is ActiveZone over a caller-supplied zone list. The widget's own method
// takes a value on the meter's current scale, which is what a program usually
// wants; this one shows the same answer for a list it is about to install.
func met(v float64, zones []viz.Zone) (int, bool) {
	for i, z := range zones {
		if v >= z.From && (z.To <= z.From || v < z.To) {
			return i, true
		}
	}
	return -1, false
}

// ---------------------------------------------------------------------------
// Sparkline
// ---------------------------------------------------------------------------

// ExampleSparkline is a series in one row with sub-cell resolution.
//
// Braille is the default and gives two samples per cell at four levels each:
// eight quanta along the HORIZONTAL axis, so a 40-cell sparkline resolves 80
// values. Block gives one sample per cell at eight levels: eight quanta along
// the VERTICAL axis, which reads a trend far better in a short wide sparkline.
// Both are offered because they trade along different axes, and both share the
// normalisation, the styles and the layout, so switching at a width threshold does
// not mean holding two widgets in step.
func ExampleSparkline() {
	// Thirty cells and sixty samples: enough for the Braille row above to
	// resolve every sample and for the Block row to have one per cell, which is
	// what makes the two encodings comparable rather than merely different.
	values := make([]float64, 60)
	for i := range values {
		values[i] = float64(10 + (i*i)%37 + i/3)
	}

	braille := viz.NewSparkline(buffer.Rect{W: 30, H: 1})
	braille.SetValues(values)

	blockGlyphs := viz.NewSparkline(buffer.Rect{W: 30, H: 1})
	blockGlyphs.Braille = false
	blockGlyphs.SetValues(values)

	fmt.Println(show(30, 1, braille))
	fmt.Println(show(30, 1, blockGlyphs))
	// Output:
	// ⠁⠁⠁⠉⠁⠁⠉⠁⠙⠙⠙⠁⠑⠑⠑⠑⠉⠁⠁⠁⠁⠛⠑⠑⠓⠉⠓⠙⠛⠉
	// ▁▁▂▃▄▅▇▃▅▃▅▃▇▅▃▂▇▆▆▆▆▇▃▄▅▇▄▆▃▆
}

// ExampleSparkline_scaling is the property that makes a sparkline readable at
// all: samples are normalised against Min and Max, which default to the series'
// own extremes.
//
// Auto deriving the range is the useful default, because a sparkline scaled to an
// absolute axis shows no trend at all when the series lives in a narrow band. The
// consequence is stated rather than hidden — a caller who wants two sparklines
// comparable pins Min and Max on both.
func ExampleSparkline_scaling() {
	// A narrow band: every sample is within eight of every other, which is the
	// case the default exists for.
	values := []float64{98, 101, 99, 103, 100, 105, 97, 102, 104, 96, 101, 105}

	// Auto derives the range from the series, so the eight-cell spread is drawn
	// across the full height of the row and the trend is legible.
	auto := viz.NewSparkline(buffer.Rect{W: 12, H: 1})
	auto.Auto = true
	auto.SetValues(values)

	// Pinned to an absolute window, so the same data is drawn as the small
	// variation it is. Two sparklines sharing a window are comparable; this is
	// what a caller does when the comparison is the point.
	pinned := viz.NewSparkline(buffer.Rect{W: 12, H: 1})
	pinned.Auto = false
	pinned.Min, pinned.Max = 90, 110
	pinned.SetValues(values)

	fmt.Println(show(12, 1, auto))
	fmt.Println(show(12, 1, pinned))
	// Output:
	// ⠁⠑⠓⠑⠉⠓
	// ⠁⠑⠑⠁⠉⠑
}

// ExampleSparkline_threshold marks the samples above a level, and the mark
// defaults to AttrReverse — an attribute, so a breach is visible on a monochrome
// terminal. A threshold at or above Max marks nothing, because nothing can be
// above it.
//
// A NaN sample is drawn as zero rather than skipped: dropping it would shift every
// later sample one cell left, which silently misreports the series.
func ExampleSparkline_threshold() {
	s := viz.NewSparkline(buffer.Rect{W: 34, H: 1})
	s.SetValues([]float64{4, 9, 3, 14, 7, 19, 6, 22, 11, 4, 17, 8})
	s.Threshold = 15
	fmt.Println(show(34, 1, s))
	// Output:
	// ⠁⠁⠑⠓⠁⠉
}

// ---------------------------------------------------------------------------
// BarChart
// ---------------------------------------------------------------------------

// ExampleBarChart is a vertical categorical bar chart.
//
// Max <= 0 means "use the largest value", which is what a reader expects from a
// categorical chart. A caller who pins Max pins the comparison between two charts,
// which is the only reason to want it.
func ExampleBarChart() {
	c := viz.NewBarChart(buffer.Rect{W: 40, H: 7})
	c.Axis = true
	c.SetData([]viz.Datum{
		{Label: "linux", Value: 62},
		{Label: "darwin", Value: 44},
		{Label: "windows", Value: 28},
	})
	chrome(c.Block(), "releases by platform")
	fmt.Printf("scale top: %.0f\n", c.MaxValue())
	fmt.Println(show(40, 7, c))
	// Output:
	// scale top: 62
	// ╭ releases by platform ────────────────╮
	// │     62           44           28     │
	// │█            ▂                        │
	// │█            █            ▄           │
	// │█            █            █           │
	// │----linux-------darwin------windows---│
	// ╰──────────────────────────────────────╯
}

// ExampleBarChart_horizontal is the other orientation, and it exists because a
// category with a long name — or a chart with forty categories — does not fit the
// other way round.
//
// The scale is SHARED: one Max and one value-to-cells conversion, so switching
// orientation cannot change what a value looks like. The horizontal chart below
// pins Max to the same number the vertical one derived, and the bars come out the
// same length.
func ExampleBarChart_horizontal() {
	horizontal := viz.NewBarChart(buffer.Rect{W: 36, H: 5})
	horizontal.Vertical = false
	horizontal.ShowValue = true
	horizontal.SetData([]viz.Datum{
		{Label: "serkanalgur/termmosaic", Value: 40},
		{Label: "serkanalgur/keymap", Value: 26},
	})
	fmt.Printf("scale top: %.0f\n", horizontal.MaxValue())
	fmt.Println(show(36, 5, horizontal))
	// Output:
	// scale top: 40
	// serkanalgur/termmos…  ██████████████
	// serkanalgur/keymap    █████████ 26
}

// ExampleBarChart_values is why ShowValue exists rather than being decoration.
// The printed number is the colour-independent reading of the chart: without it,
// two bars of nearly equal height are indistinguishable in monochrome, and no
// amount of axis precision replaces it in a dashboard read at a glance.
func ExampleBarChart_values() {
	c := viz.NewBarChart(buffer.Rect{W: 34, H: 6})
	c.ShowValue = true
	c.Axis = true
	// Two values a tenth of a percent apart, which is the case ShowValue exists
	// for: at this scale the two bars differ by a fraction of a cell, and in
	// monochrome the reader needs the printed number rather than the bar.
	c.SetData([]viz.Datum{
		{Label: "requests", Value: 1180},
		{Label: "errors", Value: 1193},
		// A NEGATIVE OR NaN VALUE DRAWS AS NO BAR, and the label and value are
		// still printed. A chart that dropped the category would make a negative
		// measurement look like a missing one.
		{Label: "retries", Value: -4},
	})
	fmt.Println(show(34, 6, c))
	// Output:
	// 1180       1193        -4
	// █           █
	// █           █
	// █           █
	// █           █
	// --requests----errors-----retries--
}
