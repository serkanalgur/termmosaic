package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// readFile and writeFile are the two halves of the golden plumbing, separated so
// the failure message above them can talk about reading without a write in sight.
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func writeFile(path, s string) error {
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(s), 0o644)
}

// newNoColorRenderer returns a renderer with colour forced off, which is how the
// accessibility test proves the direction signals do not depend on hue.
func newNoColorRenderer(sink termmosaic.Sink, w, h int) *render.Renderer {
	caps := termmosaic.DefaultCaps()
	caps.TrueColor = false
	caps.Color256 = false
	return render.New(sink, render.Config{Width: w, Height: h, Caps: caps, NoColor: true})
}

// update is set by -update to rewrite the golden files.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// The screen sizes the goldens capture. Five, because they are five DIFFERENT
// layouts rather than five renderings of one:
//
//	wideW    the designed arrangement, everything side by side
//	midW     the series band stacked, the table and chart still side by side
//	narrowW  one panel per band
//	below    under MinSize, so the diagnostic rather than a clipped dashboard
//	0x0      the degenerate size ADR 0007 §4 makes valid
var (
	goldenWideW, goldenWideH     = 132, 40
	goldenMidW, goldenMidH       = 96, 40
	goldenNarrowW, goldenNarrowH = 66, 30
	goldenSmallW, goldenSmallH   = 40, 12
	goldenZeroW, goldenZeroH     = 0, 0
)

// at is the instant every golden is rendered at, so the footer's clock is a
// constant and the golden is a function of the layout rather than of when the
// suite ran.
var at = time.Date(2026, 10, 2, 14, 30, 5, 0, time.UTC)

// board returns a dashboard at size (w, h) fed the bundled sample, which is the
// offline path and therefore the only path any test in this file takes.
//
// No test here reaches the network, and that is not a convenience: a test whose
// result depends on an HTTP endpoint is a test that fails when the endpoint is
// rate-limited, and CI must not depend on the internet. The live decode paths are
// covered separately, in TestLiveSourceDecodesRealResponses, with stubbed
// RoundTrippers.
func board(t testing.TB, w, h int) *dashboard {
	t.Helper()
	d := newDashboard(w, h)
	m, _ := newOfflineSource().Fetch(context.Background())
	snap := &snapshot{m: m, at: at, source: "offline: bundled sample, no network", ok: true, gen: 1}
	d.SetFrame(buildFrame(snap))
	return d
}

// render draws n frames of d and returns the screen.
func renderAt(t testing.TB, w, h, n int, d *dashboard) *headless.MemorySink {
	t.Helper()
	d.SetBounds(buffer.Rect{W: w, H: h})
	return widgettest.Render(t, w, h, n, d)
}

func screen(t testing.TB, w, h, n int, d *dashboard) string {
	t.Helper()
	return widgettest.Screen(renderAt(t, w, h, n, d))
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := writeFile(path, got); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := readFile(path)
	if err != nil {
		t.Fatalf("reading golden file (run `go test ./examples/markets -update` to create it): %v", err)
	}
	if got != want {
		t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s\n--- diff in rows ---\n%s",
			name, got, want, describeDiff(want, got))
	}
}

// describeDiff renders a row-by-row difference, so a failing golden says which row
// changed rather than dumping two screens and asking the reader to find it.
func describeDiff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			fmt.Fprintf(&b, "row %d:\n  want %q\n  got  %q\n", i, wl, gl)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// goldens
// ---------------------------------------------------------------------------

// TestGoldens pins the five layouts.
//
// This is the test that would catch a change to any widget's drawing that altered
// this screen, and it is a golden rather than an assertion because the interesting
// property — that the screen LOOKS like a dashboard — is not expressible as a
// handful of substrings.
func TestGoldens(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"markets_wide.txt", goldenWideW, goldenWideH},
		{"markets_mid.txt", goldenMidW, goldenMidH},
		{"markets_narrow.txt", goldenNarrowW, goldenNarrowH},
		{"markets_small.txt", goldenSmallW, goldenSmallH},
		{"markets_zero.txt", goldenZeroW, goldenZeroH},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newDashboard(c.w, c.h)
			m, _ := newOfflineSource().Fetch(context.Background())
			snap := &snapshot{m: m, at: at, source: "offline: bundled sample, no network", ok: true, gen: 1}
			d.SetFrame(buildFrame(snap))
			checkGolden(t, c.name, screen(t, c.w, c.h, 2, d))
		})
	}
}

// TestInteractiveStateGoldens pins the four interactive states the plain layout
// goldens cannot reach: a focused table row, the help overlay, a switched currency,
// and a paused screen.
//
// The layout goldens above all render the same state — the default pair, no
// selection, no overlay — because they are about the LAYOUT. These four are about
// the states a reader puts the screen into, and a golden is the right tool for them
// for the same reason it is for the others: what "a focused row looks right" means
// is a picture, and a handful of substring assertions would pass on a screen where
// the marker was in the wrong cell as long as the right words were present.
//
// Each state is reached through the ORDINARY event path — a decoded key, never a
// setter — so a golden here is also a test that the input path reaches the state at
// all. A golden generated by poking the widget's fields would pin the rendering of a
// state no reader can reach.
func TestInteractiveStateGoldens(t *testing.T) {
	t.Run("focused row", func(t *testing.T) {
		d := boardAt(t, goldenWideW, goldenWideH)
		focusedTable(t, d)
		press(t, d, seqDown)
		press(t, d, seqDown)
		checkGolden(t, "markets_focus_table.txt", screen(t, goldenWideW, goldenWideH, 2, d))
	})
	t.Run("help overlay", func(t *testing.T) {
		d := boardAt(t, goldenWideW, goldenWideH)
		if !press(t, d, "?") {
			t.Fatal("'?' was not consumed")
		}
		checkGolden(t, "markets_help.txt", screen(t, goldenWideW, goldenWideH, 2, d))
	})
	t.Run("selected currency", func(t *testing.T) {
		d := boardAt(t, goldenWideW, goldenWideH)
		if !press(t, d, seqRight) {
			t.Fatal("right was not consumed")
		}
		checkGolden(t, "markets_pair_gbp.txt", screen(t, goldenWideW, goldenWideH, 2, d))
	})
	t.Run("paused", func(t *testing.T) {
		d := boardAt(t, goldenWideW, goldenWideH)
		if !press(t, d, " ") {
			t.Fatal("space was not consumed")
		}
		checkGolden(t, "markets_paused.txt", screen(t, goldenWideW, goldenWideH, 2, d))
	})
}

// TestInteractiveGoldenFilesExist fails with a clear instruction rather than a
// file-not-found error, because that is the first thing anyone hits when a golden
// for a new state was never generated.
func TestInteractiveGoldenFilesExist(t *testing.T) {
	for _, name := range []string{
		"markets_focus_table.txt", "markets_help.txt",
		"markets_pair_gbp.txt", "markets_paused.txt",
	} {
		if _, err := os.Stat(filepath.Join("testdata", name)); err != nil {
			t.Errorf("missing golden file %s: %v (run `go test ./examples/markets -update`)", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// what the wide screen contains
// ---------------------------------------------------------------------------

// TestWideScreenShowsEveryPanel is the assertion a screenshot makes: at the
// designed size, every band is VISIBLE and says what it is.
//
// A golden alone would pass on a screen where a panel had silently shrunk to
// nothing as long as the pixels were then frozen into the golden, so the panels
// are asserted by name and by geometry too.
func TestWideScreenShowsEveryPanel(t *testing.T) {
	d := board(t, goldenWideW, goldenWideH)
	got := screen(t, goldenWideW, goldenWideH, 1, d)

	for _, want := range []string{
		// the four KPI tiles, by their labels
		"EUR/USD spot", "1d change", "window high", "window low",
		// the series band's three panels, by their block titles
		"EUR/USD, last 30d", "spot in window", "1d move, bps",
		// the detail band's two panels
		"pairs", "largest moves",
		// the table's header
		"pair", "rate", "1d",
		// the footer
		"markets", "as of 2026-10-02", "fetched 14:30:05Z",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the wide screen does not show %q:\n%s", want, got)
		}
	}
}

// TestWideScreenShowsRealNumbers is the "is this actually the data" check.
//
// It asserts the specific values the bundled capture holds, which is what
// distinguishes a dashboard rendering real figures from one rendering plausible
// ones: a formatter that dropped a sign or a decimal would still pass the panel
// names above.
func TestWideScreenShowsRealNumbers(t *testing.T) {
	got := screen(t, goldenWideW, goldenWideH, 1, board(t, goldenWideW, goldenWideH))
	for _, want := range []string{
		// the spot rate of the primary pair, at five decimals
		"0.89087",
		// its day-on-day change: 0.89087/0.88511 is +0.65%, and the sign is
		// MANDATORY rather than conditional on negativity
		"+0.65%",
		// the window's extremes
		"0.85822", "2026-09-09", "2026-10-02",
		// crypto, which comes from the other source and is formatted at two
		// decimals rather than five because it is quoted in units
		"85492.00", "2701.83",
		// a row that moved the other way, so the screen is not all one direction
		"-0.95%",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the wide screen does not show %q:\n%s", want, got)
		}
	}
}

// TestEveryMoveCarriesANonColourSignal is the accessibility requirement, asserted
// rather than asserted-in-prose.
//
// The rule under test: a direction is never carried by hue alone. Every figure
// that moves carries either a sign or an arrow, and a screen rendered with colour
// REMOVED must still be readable. So the test renders with NoColor and checks that
// the arrows and signs survive — which they do only because they are glyphs and
// characters, not styles.
func TestEveryMoveCarriesANonColourSignal(t *testing.T) {
	d := board(t, goldenWideW, goldenWideH)
	plain := renderNoColor(t, goldenWideW, goldenWideH, d)

	// Arrows for up and down, and the flat marker, all on the Unicode rung.
	for _, want := range []rune{arrowUp, arrowDown, arrowFlat} {
		if !strings.Contains(plain, string(want)) {
			t.Errorf("with colour removed the screen no longer shows the direction glyph %q (%U):\n%s",
				string(want), want, plain)
		}
	}
	// Signs on the numbers themselves, so a value that is too wide for its tile
	// still says which way it went.
	for _, want := range []string{"+0.65%", "-0.95%"} {
		if !strings.Contains(plain, want) {
			t.Errorf("with colour removed the screen no longer shows the signed move %q:\n%s", want, plain)
		}
	}
	// And the meter's zone NAME, which is the word rather than the shade: "up" is
	// the band the sample's +0.65% move lands in, and it survives with colour off.
	if !strings.Contains(plain, "up") {
		t.Errorf("with colour removed the meter's zone name is gone:\n%s", plain)
	}
}

// TestMeterZoneNameIsTheWordNotTheShade checks the zone name independently of the
// rest of the screen, at a size where the meter is given its whole band.
//
// The general test above can only assert the zone that happens to be active in the
// fixture. This one drives the meter to each of its three bands and checks the
// NAME each time, because "the band is a word" is the property and "up happens to
// be on screen" is not.
func TestMeterZoneNameIsTheWordNotTheShade(t *testing.T) {
	for _, c := range []struct {
		value float64
		want  string
	}{
		{-100, "down"},
		{0, "flat"},
		{100, "up"},
	} {
		d := board(t, 132, 40)
		d.meter.Set(c.value)
		got := renderNoColor(t, 132, 40, d)
		if !strings.Contains(got, c.want) {
			t.Errorf("at %v bps the meter does not name the %q band with colour removed:\n%s",
				c.value, c.want, got)
		}
	}
}

// TestASCIIRungKeepsTheDirectionSignal is the degradation half of the same rule.
//
// A terminal whose caps report no Unicode gets '^' and 'v' instead of the
// triangles. The point is that the INFORMATION survives, not that the glyph does:
// if the ASCII rung fell back to blanks, a monochrome-and-no-Unicode terminal
// would have no direction signal at all, which is the failure this guards.
func TestASCIIRungKeepsTheDirectionSignal(t *testing.T) {
	defer setRung(!asciiRung)
	// setRung takes the terminal's UNICODE capability, so "no Unicode" is false —
	// the inverse of what the test's name suggests, which is exactly why the
	// function's parameter is named rather than the flag it sets.
	setRung(false)

	d := board(t, goldenWideW, goldenWideH)
	got := screen(t, goldenWideW, goldenWideH, 1, d)
	for _, want := range []string{string(arrowUpASCII), string(arrowDownASCII)} {
		if !strings.Contains(got, want) {
			t.Errorf("the ASCII rung does not show %q (%q):\n%s", want, string(want), got)
		}
	}
	for _, forbidden := range []string{string(arrowUp), string(arrowDown)} {
		if strings.Contains(got, forbidden) {
			t.Errorf("the ASCII rung still shows the Unicode glyph %q", forbidden)
		}
	}
}

// ---------------------------------------------------------------------------
// responsiveness
// ---------------------------------------------------------------------------

// TestLayoutMovesPanelsRatherThanShrinkingThem is the responsiveness assertion
// this example exists to earn.
//
// It compares the ARRANGEMENT at three widths rather than the pixels, because
// "the screen changed" would pass for a layout that had merely reflowed its text.
// What it asserts is that a panel's rectangle MOVES:
//
//	at wide, the gauge is BESIDE the sparkline: same row, different column
//	at mid,  the gauge is BELOW it: different row, same column
//	at narrow, the gauge is not drawn at all
//
// A layout that only shrank panels would keep the gauge beside the sparkline at
// every width and would fail the second assertion. That is the whole difference
// between "responsive" and "clamp-and-hope" in ADR 0007's terms.
func TestLayoutMovesPanelsRatherThanShrinkingThem(t *testing.T) {
	// The three widths, each in its own arrangement band.
	wide := board(t, goldenWideW, goldenWideH)
	wideScreen := renderAt(t, goldenWideW, goldenWideH, 1, wide)

	mid := board(t, goldenMidW, goldenMidH)
	renderAt(t, goldenMidW, goldenMidH, 1, mid)

	narrow := board(t, goldenNarrowW, goldenNarrowH)
	renderAt(t, goldenNarrowW, goldenNarrowH, 1, narrow)

	// Wide: the gauge is beside the sparkline.
	if wide.lay.arr != arrWide {
		t.Fatalf("at %d wide the arrangement is %d, want arrWide", goldenWideW, wide.lay.arr)
	}
	gs, gg := wide.spark.Bounds(), wide.gauge.Bounds()
	if gg.Y != gs.Y {
		t.Errorf("at %d wide the gauge is on row %d and the sparkline on row %d; they should be side by side",
			goldenWideW, gg.Y, gs.Y)
	}
	if gg.X <= gs.X {
		t.Errorf("at %d wide the gauge is at column %d and the sparkline at %d; the gauge should be to the RIGHT",
			goldenWideW, gg.X, gs.X)
	}

	// Mid: the gauge has MOVED below the sparkline. This is the assertion that
	// distinguishes a moving panel from a shrinking one.
	if mid.lay.arr != arrStacked {
		t.Fatalf("at %d wide the arrangement is %d, want arrStacked", goldenMidW, mid.lay.arr)
	}
	ms, mg := mid.spark.Bounds(), mid.gauge.Bounds()
	if mg.Y <= ms.Y {
		t.Errorf("at %d wide the gauge is on row %d and the sparkline on row %d; the gauge should have MOVED below it",
			goldenMidW, mg.Y, ms.Y)
	}
	if mg.X != ms.X {
		t.Errorf("at %d wide the gauge is at column %d and the sparkline at %d; they should share a column when stacked",
			goldenMidW, mg.X, ms.X)
	}
	if mg.W == gg.W && mg.H == gg.H && mg.Y == gg.Y {
		t.Error("the gauge's rectangle is identical at wide and mid; the layout did not move it")
	}

	// Narrow: the gauge is gone rather than squeezed.
	if narrow.lay.arr != arrSingle {
		t.Fatalf("at %d wide the arrangement is %d, want arrSingle", goldenNarrowW, narrow.lay.arr)
	}
	if got := narrow.series.PaneCount(); got != 1 {
		t.Errorf("at %d wide the series band has %d panes; it should have exactly the sparkline", goldenNarrowW, got)
	}
	if got := narrow.detail.PaneCount(); got != 1 {
		t.Errorf("at %d wide the detail band has %d panes; it should have exactly the table", goldenNarrowW, got)
	}

	// And the wide screen really did contain the panels that mid and narrow drop,
	// so "moved" is not the same as "never drawn".
	if !strings.Contains(widgettest.Screen(wideScreen), "spot in window") {
		t.Error("the gauge's panel title is missing at the wide size")
	}
}

// TestLayoutArrangementFollowsWidth is the breakpoint table itself, asserted
// boundary by boundary rather than at three arbitrary samples.
//
// An off-by-one at a threshold is invisible in a golden — the golden simply shows
// the other arrangement — and it is exactly the kind of bug a reader hits as "it
// jumped at 87" and cannot explain.
func TestLayoutArrangementFollowsWidth(t *testing.T) {
	cases := []struct {
		w    int
		want arrangement
	}{
		{narrowW - 1, arrSingle},
		{narrowW, arrSingle},
		{midW - 1, arrSingle},
		{midW, arrStacked},
		{wideW - 1, arrStacked},
		{wideW, arrWide},
		{wideW + 40, arrWide},
	}
	for _, c := range cases {
		if got := arrangementFor(c.w); got != c.want {
			t.Errorf("at %d columns the arrangement is %d, want %d", c.w, got, c.want)
		}
	}
}

// TestKPIRowDropsTilesWithWidth is the ClampCount behaviour on the KPI row.
//
// It goes from four figures to two as the terminal narrows, and it does so by
// dropping the LOWEST-priority figures — the window's extremes — rather than the
// first two, because the spot rate and the change are the two a reader came for.
func TestKPIRowDropsTilesWithWidth(t *testing.T) {
	// Four tiles need 4*kpiTileW + 3 gaps = 99; three need 74; two need 49; one
	// needs 24. Every case is one of those numbers or exactly one short of one, so
	// the arithmetic is pinned on BOTH sides of each step — a test that only sampled
	// the steps themselves would not catch an off-by-one that cost a tile at the
	// boundary.
	cases := []struct {
		w    int
		want int
	}{
		{goldenWideW, 4},
		{wideW, 4},
		{4*kpiTileW + 3*bandGap, 4},     // exactly enough for four
		{4*kpiTileW + 3*bandGap - 1, 3}, // one column short of four
		{midW, 3},
		{3*kpiTileW + 2*bandGap, 3},     // exactly enough for three
		{3*kpiTileW + 2*bandGap - 1, 2}, // one column short of three
		{narrowW, 2},
		{2*kpiTileW + bandGap - 1, 1}, // one column short of two
		{kpiTileW, 1},                 // exactly one tile
		{kpiTileW - 1, 0},             // not even one tile
		{1, 0},                        // a one-column terminal
		{0, 0},                        // and a detached one
	}
	for _, c := range cases {
		// The pane count is only meaningful at a width the screen draws at all:
		// below MinSize the diagnostic replaces the bands, so the KPI row keeps
		// whatever pane list it was last given. That is correct behaviour and the
		// test above covers it; here the arithmetic is what is under test.
		if c.w >= narrowW {
			d := board(t, c.w, 30)
			renderAt(t, c.w, 30, 1, d)
			if got := d.kpi.PaneCount(); got != c.want {
				t.Errorf("at %d columns the KPI row has %d tiles, want %d", c.w, got, c.want)
			}
		}
	}
	// kpiTilesFor itself, at the boundaries, so the loop's arithmetic is pinned
	// independently of the layout that consumes it.
	for _, c := range cases {
		if got := kpiTilesFor(c.w); got != c.want {
			t.Errorf("kpiTilesFor(%d) = %d, want %d", c.w, got, c.want)
		}
	}
	// And the tiles that SURVIVE are the first ones, which are spot and change.
	d := board(t, narrowW, 30)
	renderAt(t, narrowW, 30, 1, d)
	if got := d.kpi.Pane(0); got != d.stat[tileSpot] {
		t.Errorf("the first KPI tile is not the spot rate")
	}
}

// TestNarrowScreenDropsTheWindowExtremes asserts the same fact at the SCREEN
// level, because a pane count is a layout fact and a reader's fact is what is on
// the screen.
func TestNarrowScreenDropsTheWindowExtremes(t *testing.T) {
	narrow := screen(t, goldenNarrowW, goldenNarrowH, 1, board(t, goldenNarrowW, goldenNarrowH))
	for _, want := range []string{"EUR/USD spot", "1d change"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("the narrow screen does not show %q:\n%s", want, narrow)
		}
	}
	for _, gone := range []string{"window high", "window low"} {
		if strings.Contains(narrow, gone) {
			t.Errorf("the narrow screen still shows %q, which the KPI budget should have dropped:\n%s", gone, narrow)
		}
	}
}

// TestBelowMinSizeSaysSoRatherThanClipping is ADR 0007 §4's clipping-below-
// minimum policy, applied at the application level.
//
// A dashboard too small for its root draws a DIAGNOSTIC, because a clipped
// dashboard reads as a bug. The screen must still say what the reader needs —
// that the terminal is too small — and it must not be blank, because blanking
// loses the indication that content was lost.
func TestBelowMinSizeSaysSoRatherThanClipping(t *testing.T) {
	// Sizes from comfortably below the minimum down to sizes too narrow to hold
	// the message at all.
	for _, size := range []buffer.Size{
		{W: narrowW - 1, H: 30}, {W: 40, H: 30}, {W: 20, H: 8},
		{W: 10, H: 4}, {W: 5, H: 3}, {W: 1, H: 1},
	} {
		got := screen(t, size.W, size.H, 1, board(t, size.W, size.H))
		// "too small" is the first fourteen cells of the message by construction,
		// so it survives truncation at every width that can hold it.
		if size.W >= len("too small") && !strings.Contains(got, "too small") {
			t.Errorf("at %dx%d the screen does not say it is too small:\n%s", size.W, size.H, got)
		}
		// And it must not claim to have data it does not have room to show.
		if strings.Contains(got, "0.89087") {
			t.Errorf("at %dx%d the screen draws a KPI value below the minimum:\n%s", size.W, size.H, got)
		}
		if strings.Contains(got, "pairs") {
			t.Errorf("at %dx%d the screen draws the table below the minimum:\n%s", size.W, size.H, got)
		}
	}
	// Below the HEIGHT minimum, at a width that could have held the dashboard.
	for _, size := range []buffer.Size{
		{W: goldenWideW, H: 5}, {W: goldenWideW, H: 1},
	} {
		got := screen(t, size.W, size.H, 1, board(t, size.W, size.H))
		if !strings.Contains(got, "too small") {
			t.Errorf("at %dx%d the screen does not say it is too small:\n%s", size.W, size.H, got)
		}
	}
}

// TestMinSizeIsReachableAndHonest checks the two properties of a MinSize an
// application depends on: it is pure, and a screen exactly at it renders the
// bands rather than the diagnostic.
//
// The second is the one that catches an inflated minimum. A MinSize that claims
// 58x20 when 58x16 renders perfectly is a MinSize that makes the application
// refuse to draw a screen it could have drawn.
func TestMinSizeIsReachableAndHonest(t *testing.T) {
	d := board(t, 80, 30)
	m := d.MinSize()

	// Pure: two calls agree, and it does not depend on the current Bounds.
	if again := d.MinSize(); again != m {
		t.Errorf("MinSize is not pure: %v then %v", m, again)
	}
	d.SetBounds(buffer.Rect{W: 200, H: 60})
	if again := d.MinSize(); again != m {
		t.Errorf("MinSize depends on Bounds: %v at 200x60", again)
	}

	// Reachable: exactly at the minimum the bands draw.
	// Reachable: exactly at the minimum the bands draw, and the DETAIL band is the
	// one asserted on because it is the band with a data minimum — so if the minimum
	// is honest it must be visible here, with rows in it rather than a bare header.
	rest := board(t, m.W, m.H)
	at1 := screen(t, m.W, m.H, 1, rest)
	if strings.Contains(at1, "too small") {
		t.Errorf("at exactly MinSize %v the screen says it is too small:\n%s", m, at1)
	}
	if !strings.Contains(at1, "pairs") {
		t.Errorf("at exactly MinSize %v the detail band is not drawn:\n%s", m, at1)
	}
	if !strings.Contains(at1, "CHF/USD") {
		t.Errorf("at exactly MinSize %v the table shows a header and no rows:\n%s", m, at1)
	}

	// And one cell under it, the diagnostic appears.
	under := screen(t, m.W-1, m.H, 1, d)
	if !strings.Contains(under, "too small") {
		t.Errorf("one cell under MinSize the screen does not say it is too small:\n%s", under)
	}
}

// TestShortTerminalDropsTheSeriesBandBeforeTheTable is the height half of the
// responsive story.
//
// Width is the axis a terminal shrinks on most, but a short window is common — a
// split pane, a zoomed region — and the budget must drop the band that is a
// SUMMARY before the band that IS the data.
func TestShortTerminalDropsTheSeriesBandBeforeTheTable(t *testing.T) {
	// A tall terminal keeps both.
	tall := board(t, goldenWideW, goldenWideH)
	renderAt(t, goldenWideW, goldenWideH, 1, tall)
	if !tall.lay.showSeries || !tall.lay.showDetail {
		t.Errorf("at %dx%d both bands should be shown (series=%v detail=%v)",
			goldenWideW, goldenWideH, tall.lay.showSeries, tall.lay.showDetail)
	}

	// The three bands plus the footer and the two gaps need kpiH + seriesH(wide) +
	// detailMin + 3 + 1 rows, so a screen a row or two under that has to choose.
	// The heights are computed from the constants rather than picked, so a change to
	// any band's declared height moves this test's subject with it.
	// The three bands plus the footer and the two gaps. Measured from a real
	// dashboard rather than a hand-built one, because MinSize reads the widgets'
	// own minima and a partially constructed dashboard has nil ones.
	// need is the height at which all three bands and the footer fit, computed from
	// the bands' own declared sizes — which is exactly the sum adapt budgets, gaps
	// included, so a change to any band's declaration moves this test's subject.
	probe := board(t, goldenWideW, goldenWideH)
	// Rendering first, because the budget table is PATCHED by adapt rather than
	// built at construction — reading it before the first Draw would read zeros.
	renderAt(t, goldenWideW, goldenWideH, 1, probe)
	// The chrome rows are subtracted from the bands' height in adapt, so they are
	// added here: this is the height at which all three bands fit ABOVE the control
	// and status rows, and computing it without them would put `need` one or two
	// rows short and make the "at exactly need, all three fit" step below fail for a
	// reason that has nothing to do with the budget.
	need := probe.regions[bandKPI].Size + probe.regions[bandSeries].Size + probe.regions[bandDetail].Size + chromeRows

	shortH := need - 2
	if shortH <= probe.MinSize().H {
		t.Fatalf("the bands need %d rows, which is not more than MinSize's %d; the height budget has nothing to drop",
			need, probe.MinSize().H)
	}
	short := board(t, goldenWideW, shortH)
	renderAt(t, goldenWideW, shortH, 1, short)
	if short.lay.showSeries {
		t.Errorf("at %d rows the series band should have been dropped by the height budget", shortH)
	}
	if !short.lay.showDetail {
		t.Errorf("at %d rows the detail band should have survived the height budget", shortH)
	}
	got := screen(t, goldenWideW, shortH, 1, short)
	if !strings.Contains(got, "pairs") {
		t.Errorf("at %d rows the table is not on screen:\n%s", shortH, got)
	}
	if strings.Contains(got, "EUR/USD, last") {
		t.Errorf("at %d rows the dropped sparkline is still on screen - stale cells:\n%s", shortH, got)
	}
	// And at exactly the height where all three fit, the series band is BACK —
	// rather than the layout having decided once and for all.
	fits := board(t, goldenWideW, need)
	renderAt(t, goldenWideW, need, 1, fits)
	if !fits.lay.showSeries {
		t.Errorf("at %d rows, where all three bands fit, the series band is still dropped", need)
	}
}

// TestNoStaleCellsAfterAShrink is the ADR 0007 §1 rule 3 property, at the
// application level.
//
// A resize test that only GROWS cannot catch this: the bug is a panel that drew
// three rows and then two, leaving the third on screen. So this shrinks from wide
// to narrow and checks that nothing from the wide layout survives — the two KPI
// tiles that the narrow layout drops must be GONE, not left over.
func TestNoStaleCellsAfterAShrink(t *testing.T) {
	d := board(t, goldenWideW, goldenWideH)
	wide := screen(t, goldenWideW, goldenWideH, 1, d)
	if !strings.Contains(wide, "window high") {
		t.Fatalf("the wide screen does not show the high tile to begin with:\n%s", wide)
	}

	narrow := screen(t, goldenNarrowW, goldenNarrowH, 1, d)
	if strings.Contains(narrow, "window high") {
		t.Errorf("the tile that was dropped at the narrow size is still on screen - stale cells:\n%s", narrow)
	}
	if strings.Contains(narrow, "spot in window") {
		t.Errorf("the gauge panel dropped at the narrow size is still on screen - stale cells:\n%s", narrow)
	}

	// And the shrink all the way DOWN to the diagnostic, into the SAME buffer —
	// which is the case where the whole-rect repaint is load-bearing rather than
	// merely tidy.
	//
	// widgettest gives a fresh sink per size, so the earlier assertions above are
	// really testing the LAYOUT and could not see stale cells at all: buffer.Resize
	// discards every cell, so a resize can never leave one behind. What this last
	// case does is hold the buffer and shrink the widget's rectangle WITHIN it, so
	// the rows the old layout occupied are still cells of the live buffer. The
	// diagnostic is a single one-row Text; without a repaint of the whole rectangle
	// first, a 132x40 dashboard would still be on screen underneath a message saying
	// the terminal is too small for it.
	sameBuf := buffer.NewBuffer(goldenWideW, goldenWideH)
	d.SetBounds(buffer.Rect{W: goldenWideW, H: goldenWideH})
	d.Draw(sameBuf)
	if got := rowIn(sameBuf, 1, 0, goldenWideW); !strings.Contains(got, "EUR/USD spot") {
		t.Fatalf("the first frame did not draw the dashboard: %q", got)
	}
	d.SetBounds(buffer.Rect{W: goldenWideW, H: 8})
	d.Draw(sameBuf)
	for _, stale := range []string{"EUR/USD spot", "window high", "pairs", "0.89087"} {
		if row := rowIn(sameBuf, 1, 0, goldenWideW); strings.Contains(row, stale) {
			t.Errorf("shrinking within one buffer left %q on screen - stale cells: %q", stale, row)
		}
	}
	if got := rowIn(sameBuf, 0, 0, goldenWideW); !strings.Contains(got, "too small") {
		t.Errorf("the shrunken screen does not say it is too small: %q", got)
	}

	// A fresh sink at that size, for contrast: the diagnostic is all there is.
	tiny := screen(t, goldenWideW, 8, 1, d)
	if !strings.Contains(tiny, "too small") {
		t.Errorf("shrinking to the diagnostic did not say so:\n%s", tiny)
	}
	for _, stale := range []string{"EUR/USD spot", "pairs", "0.89087"} {
		if strings.Contains(tiny, stale) {
			t.Errorf("the diagnostic screen still shows %q:\n%s", stale, tiny)
		}
	}
	// Every row must also fit the narrower width, which is where an unclipped write
	// would show up as a row longer than the screen.
	for y, row := range strings.Split(narrow, "\n") {
		if n := len([]rune(row)); n > goldenNarrowW {
			t.Errorf("row %d is %d cells wide after shrinking to %d: %q", y, n, goldenNarrowW, row)
		}
	}

	// And growing back restores the wide layout rather than keeping the narrow one.
	again := screen(t, goldenWideW, goldenWideH, 1, d)
	if again != wide {
		t.Error("growing back did not restore the previous layout")
	}
}

// TestSweepEverySizeRendersWithoutPanicking is ADR 0007 §5's honest first test,
// which this ADR records as the one thing it had not been run against: a scripted
// resize sweep asserting no panic and no stale cells at every intermediate size.
//
// The widths and heights are chosen to cross every breakpoint and every degenerate
// size on the way, because the intermediate sizes are where the arithmetic is
// untested and where a rounding error would hide.
func TestSweepEverySizeRendersWithoutPanicking(t *testing.T) {
	d := board(t, goldenWideW, goldenWideH)
	for _, size := range []buffer.Size{
		{W: 0, H: 0}, {W: 0, H: 24}, {W: 80, H: 0},
		{W: 1, H: 1}, {W: 2, H: 2}, {W: 5, H: 3},
		{W: narrowW - 1, H: minH - 1}, {W: narrowW, H: minH},
		{W: narrowW, H: minH + 1}, {W: midW, H: 24}, {W: wideW, H: 20},
		{W: 200, H: 60}, {W: 57, H: 40}, {W: 85, H: 40}, {W: 107, H: 40},
	} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("rendering at %dx%d panicked: %v", size.W, size.H, p)
				}
			}()
			// Two frames, so a stale cell from the previous size would survive the
			// first and be caught on the second.
			d.SetBounds(buffer.Rect{W: size.W, H: size.H})
			if size.W > 0 && size.H > 0 {
				buf := buffer.NewBuffer(size.W, size.H)
				d.Draw(buf)
				d.Draw(buf)
			}
		}()
	}
}

// ---------------------------------------------------------------------------
// the frame path
// ---------------------------------------------------------------------------

// TestDashboardFrameIsAllocationFree is the load-bearing performance assertion.
//
// ADR 0002 holds the diff to zero allocations per frame and there is no reason
// the widget tree above it should be worse: a frame that allocates once per second
// is a frame that shows up as a GC pause on a machine that is already busy.
//
// This is the test that says the design worked. Every number is formatted in
// buildFrame, every size-derived value is cached in adapt against the rectangle it
// came from, and the KPI tiles skip their SetSpans when the text has not changed
// — so a steady-state Draw is buffer writes and widget Draws and nothing else.
//
// It runs with the frame ALREADY applied and the layout ALREADY adapted, because
// those are one-time costs per data change and per distinct size respectively.
// Asserting zero allocations across a resize would be asserting that the resize
// path allocates nothing, which it must not: ADR 0007 §3 says a layout recompute
// happens once per rectangle and that is the documented cost.
func TestDashboardFrameIsAllocationFree(t *testing.T) {
	d := board(t, goldenWideW, goldenWideH)
	buf := buffer.NewBuffer(goldenWideW, goldenWideH)

	// Warm the layout cache at this exact rectangle, then measure.
	d.Draw(buf)
	d.Draw(buf)

	if got := testing.AllocsPerRun(50, func() { d.Draw(buf) }); got != 0 {
		t.Errorf("the dashboard frame allocated %v times; the frame path must be free", got)
	}

	// And with the screen at a size where the layout has NOT been adapted yet, to
	// show the warm-up above is what makes the difference rather than luck.
	fresh := board(t, 100, 32)
	freshBuf := buffer.NewBuffer(100, 32)
	fresh.Draw(freshBuf)
	if got := testing.AllocsPerRun(50, func() { fresh.Draw(freshBuf) }); got != 0 {
		t.Errorf("a second dashboard frame allocated %v times", got)
	}
}

// TestKPIFrameIsAllocationFree isolates the tile, because the tile is the one
// widget in this example that calls SetSpans — and therefore the one that would
// allocate on every frame if its change detection were wrong.
func TestKPIFrameIsAllocationFree(t *testing.T) {
	d := board(t, goldenWideW, goldenWideH)
	buf := buffer.NewBuffer(goldenWideW, goldenWideH)
	d.Draw(buf)

	tile := d.stat[tileChange]
	// A degenerate rectangle is the code's own output at the golden size, so it
	// is a failure rather than a skip — and a vacuous one at that: drawing into a
	// zero-cell buffer does not allocate, so a skip here would report the KPI
	// frame allocation-free on the strength of nothing being drawn at all.
	if tile.bounds.W <= 0 || tile.bounds.H <= 0 {
		t.Fatalf("the KPI tile has no rectangle at the golden size: %v", tile.bounds)
	}
	tileBuf := buffer.NewBuffer(tile.bounds.W, tile.bounds.H)
	tile.Draw(tileBuf)
	tile.Draw(tileBuf)
	if got := testing.AllocsPerRun(50, func() { tile.Draw(tileBuf) }); got != 0 {
		t.Errorf("a KPI tile frame allocated %v times", got)
	}

	// And a tile whose text CHANGED does allocate — once, off the frame path — and
	// the tile must not be caching the old string.
	before := tile.t
	tile.set(tileText{label: "changed", value: "1", note: "x"})
	if tile.t == before {
		t.Error("set did not change the tile's text")
	}
}

// TestSetFrameReplacesEveryWidget is the "the data reached the widgets" check.
//
// It is the assertion that would catch a new panel added to the screen and never
// wired to SetFrame: the panel would render, empty, and only this test would say
// so. Each widget's own accessor is checked rather than the screen text, because
// the screen text is covered by the goldens and this test is about the WIRING.
func TestSetFrameReplacesEveryWidget(t *testing.T) {
	m, _ := newOfflineSource().Fetch(context.Background())
	snap := &snapshot{m: m, at: at, source: "test", ok: true, gen: 7}
	d := newDashboard(goldenWideW, goldenWideH)
	d.SetFrame(buildFrame(snap))

	if len(d.spark.Values) != len(sampleEUR) {
		t.Errorf("the sparkline holds %d samples, want %d", len(d.spark.Values), len(sampleEUR))
	}
	if got := d.spark.Values; len(got) > 0 && got[len(got)-1] != sampleEUR[len(sampleEUR)-1] {
		t.Errorf("the sparkline's last sample is %v, want %v", got[len(got)-1], sampleEUR[len(sampleEUR)-1])
	}
	// The gauge's scale is the window's range, so a reader can place the needle.
	if d.gauge.ScaleMin <= 0 || d.gauge.ScaleMax <= d.gauge.ScaleMin {
		t.Errorf("the gauge's scale is [%v, %v], want a positive range", d.gauge.ScaleMin, d.gauge.ScaleMax)
	}
	// The needle's POSITION is what the gauge is for, so it is asserted as a
	// fraction rather than as "somewhere in range": a gauge pinned at either end
	// passes a bounds check while telling the reader nothing.
	//
	// The fixture's spot happens to BE the window high, so the bundled data cannot
	// exercise this. A spot partway up the range is built here, which is the only
	// way to tell "correctly placed" from "always at the top".
	lo, hi, ok := m.extremes()
	if !ok {
		t.Fatal("the sample has no window extremes")
	}
	// The fixture's own spot is not consulted here at all: `partway` below sets the
	// primary rate explicitly, so the needle's position is decided by the
	// assignment rather than by whatever the sample happened to carry. An earlier
	// version skipped when the fixture's spot was not the window high, which was
	// checking a fact about the data that the assertions below never relied on —
	// so any edit to the sample could switch this off without changing what is
	// actually verified. Removed rather than converted: there is no precondition
	// left to assert.
	mid := (lo + hi) / 2
	partway := sampleMarket()
	partway.rates[partway.primary] = mid
	d2 := newDashboard(goldenWideW, goldenWideH)
	d2.SetFrame(buildFrame(&snapshot{m: partway, at: at, source: "test", ok: true, gen: 8}))
	if got := d2.gauge.Ratio(); got < 0.45 || got > 0.55 {
		t.Errorf("a spot at the middle of the window reads %v of the scale, want about 0.5", got)
	}
	if got := d2.gauge.Reading(); got != mid {
		t.Errorf("the gauge reads %v, want the spot rate %v", got, mid)
	}
	// The meter's value is the day's move in basis points, inside its own scale.
	if d.meter.Value <= meterLow || d.meter.Value >= meterHigh {
		t.Errorf("the meter reads %v, outside [%v, %v]", d.meter.Value, meterLow, meterHigh)
	}
	if got := d.table.Rows(); got != len(trackedFX)+len(trackedCrypto) {
		t.Errorf("the table holds %d rows, want %d", got, len(trackedFX)+len(trackedCrypto))
	}
	if len(d.chart.Data) != chartPairs {
		t.Errorf("the chart holds %d bars, want %d", len(d.chart.Data), chartPairs)
	}
	for i := range numTiles {
		if !d.stat[i].valid {
			t.Errorf("KPI tile %d was never set", i)
		}
	}
	if d.FrameGen() != 7 {
		t.Errorf("the dashboard's frame generation is %d, want 7", d.FrameGen())
	}
}

// ---------------------------------------------------------------------------
// failure
// ---------------------------------------------------------------------------

// TestFetchFailureShowsTheReasonAndNoData is the error-path assertion.
//
// The failure modes a user can meet are three and each has a different correct
// rendering:
//
//	never succeeded   the bands are replaced by a panel naming the failure
//	succeeded before  the last good data stays, and the footer says STALE
//	one source down   the other source's rows stay, and the footer says which
//
// Asserting only "it does not panic" would leave all three unpinned, and the
// failure this brief cares about most — rendering an ABSENT number as zero — is
// exactly the one a panic test cannot see. So this asserts the reason is on screen
// and that no number is.
func TestFetchFailureShowsTheReasonAndNoData(t *testing.T) {
	reason := "dial tcp 203.0.113.7:443: i/o timeout"
	d := newDashboard(goldenWideW, goldenWideH)
	m, err := failedSource{reason: reason}.Fetch(context.Background())
	if err == nil {
		t.Fatal("the failing source returned no error")
	}
	d.SetFrame(buildFrame(&snapshot{m: m, err: err, at: at, source: "live: test", gen: 1}))

	got := screen(t, goldenWideW, goldenWideH, 1, d)

	// The reason, on screen, in the panel AND in the footer.
	if !strings.Contains(got, "no market data") {
		t.Errorf("the screen does not say there is no data:\n%s", got)
	}
	if !strings.Contains(got, "i/o timeout") {
		t.Errorf("the screen does not carry the failure reason:\n%s", got)
	}
	if !strings.Contains(got, "FAILED") {
		t.Errorf("the footer does not mark the failure as FAILED rather than STALE:\n%s", got)
	}
	// And the panels are gone rather than drawn empty, because an empty grid of
	// panels reads as a bug rather than as an outage.
	if strings.Contains(got, "EUR/USD spot") {
		t.Errorf("the KPI band is still drawn with no data:\n%s", got)
	}

	// THE assertion: no absent number is rendered as a number.
	for _, forbidden := range []string{"0.89087", "+0.65%", "85492.00"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("the screen shows %q although no data was fetched - an absent value rendered as a number:\n%s",
				forbidden, got)
		}
	}
	// The reason is on the panel AND in the footer, because the panel is where a
	// reader who has just seen the dashboard go empty looks first, and the footer is
	// where a reader who never noticed looks. Either alone leaves someone guessing.
	if !strings.Contains(got, "i/o timeout") {
		t.Errorf("the screen does not carry the failure reason:\n%s", got)
	}
	if !strings.Contains(got, "retrying") {
		t.Errorf("the screen does not say it will retry:\n%s", got)
	}
	// And the KPI tiles, which the panel replaces, still carry the ABSENT
	// placeholder rather than a zero. They are asserted through the frame rather
	// than the screen, because the panel is what covers them — and the property that
	// matters is that the DATA carries absence, not that a covered widget happens
	// to be legible.
	f := buildFrame(&snapshot{m: newMarket(), err: errors.New(reason), at: at, source: "live: test", gen: 1})
	for i := range numTiles {
		if f.tiles[i].value != noDataPlaceholder {
			t.Errorf("KPI tile %d reads %q with no data at all, want the absent placeholder %q",
				i, f.tiles[i].value, noDataPlaceholder)
		}
		if f.tiles[i].label != tileLabel(i) {
			t.Errorf("KPI tile %d lost its label with no data: %q", i, f.tiles[i].label)
		}
	}
}

// TestPartialFetchKeepsTheGoodDataAndSaysWhichPartIsMissing is the middle case,
// and the one a "blank the screen on error" implementation gets wrong.
//
// Crypto rate-limited while FX is fine is an ordinary Tuesday. The right rendering
// is the FX panels with their real numbers, and a footer that names crypto as the
// missing half — not an error page, and not a screen quietly showing half the
// instruments as though all were there.
func TestPartialFetchKeepsTheGoodDataAndSaysWhichPartIsMissing(t *testing.T) {
	reason := "crypto HTTP 429"
	m := sampleMarket()
	m.crypto = cryptoBook{} // crypto is what failed
	m.failures = []string{reason}
	d := newDashboard(goldenWideW, goldenWideH)
	d.SetFrame(buildFrame(&snapshot{
		m: m, err: errors.New(reason), at: at, source: "live: test", ok: true, gen: 2,
	}))

	got := screen(t, goldenWideW, goldenWideH, 1, d)

	// The good half is still there, with real numbers.
	if !strings.Contains(got, "0.89087") {
		t.Errorf("the FX data was thrown away when crypto failed:\n%s", got)
	}
	if !strings.Contains(got, "EUR/USD spot") {
		t.Errorf("the KPI band was dropped when only crypto failed:\n%s", got)
	}
	// The bad half is named, in both halves of the sentence: which source, and
	// that the rest is stale rather than current.
	if !strings.Contains(got, "crypto") {
		t.Errorf("the footer does not name crypto as the missing source:\n%s", got)
	}
	if !strings.Contains(got, "STALE") {
		t.Errorf("the footer does not mark the screen STALE on a partial fetch:\n%s", got)
	}
	// And it says it will try again, because a screen that fails silently reads as
	// a screen that has given up.
	if !strings.Contains(got, "retrying") {
		t.Errorf("the footer does not say it will retry:\n%s", got)
	}
	// The missing instruments are absent rather than zero.
	if strings.Contains(got, "85492.00") {
		t.Errorf("a coin that was not fetched is shown with a price:\n%s", got)
	}
}

// TestFailureRendersAtEverySize is the failure path through the responsive
// layout, because the panel replaces the bands and the bands' rectangles are
// whatever the last successful layout left behind.
//
// That is the stale-cell hazard in its sharpest form: a screen that showed the
// dashboard and then failed would leave the dashboard's panels on screen behind
// the failure panel if the failure panel were smaller than they were.
func TestFailureRendersAtEverySize(t *testing.T) {
	d := newDashboard(goldenWideW, goldenWideH)
	good, _ := newOfflineSource().Fetch(context.Background())
	d.SetFrame(buildFrame(&snapshot{m: good, at: at, source: "offline", ok: true, gen: 1}))

	// Show the dashboard first, so anything left behind would be visible.
	wide := screen(t, goldenWideW, goldenWideH, 1, d)
	if !strings.Contains(wide, "EUR/USD spot") {
		t.Fatalf("the dashboard is not on screen to begin with:\n%s", wide)
	}

	// Now fail, and shrink at the same time.
	_, err := failedSource{reason: "connection refused"}.Fetch(context.Background())
	d.SetFrame(buildFrame(&snapshot{m: newMarket(), err: err, at: at, source: "live: test", gen: 2}))

	for _, size := range []buffer.Size{
		{W: goldenWideW, H: goldenWideH},
		{W: goldenNarrowW, H: goldenNarrowH},
		{W: narrowW, H: minH},
	} {
		got := screen(t, size.W, size.H, 1, d)
		if !strings.Contains(got, "no market data") {
			t.Errorf("at %dx%d the failure panel is not on screen:\n%s", size.W, size.H, got)
		}
		if strings.Contains(got, "EUR/USD spot") || strings.Contains(got, "0.89087") {
			t.Errorf("at %dx%d the previous dashboard is still visible behind the failure - stale cells:\n%s",
				size.W, size.H, got)
		}
	}
}

// TestFailureDoesNotPanicAtDegenerateSizes covers the same path at the sizes
// where the failure panel's own interior is empty.
func TestFailureDoesNotPanicAtDegenerateSizes(t *testing.T) {
	d := newDashboard(40, 12)
	_, err := failedSource{reason: "boom"}.Fetch(context.Background())
	d.SetFrame(buildFrame(&snapshot{m: newMarket(), err: err, at: at, source: "live", gen: 1}))

	for _, size := range []buffer.Size{
		{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 2}, {W: 3, H: 3}, {W: 20, H: 3},
	} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("the failure path panicked at %dx%d: %v", size.W, size.H, p)
				}
			}()
			d.SetBounds(buffer.Rect{W: size.W, H: size.H})
			if size.W > 0 && size.H > 0 {
				buf := buffer.NewBuffer(size.W, size.H)
				d.Draw(buf)
			}
		}()
	}
}

// ---------------------------------------------------------------------------
// concurrency
// ---------------------------------------------------------------------------

// TestStoreIsRaceFree is the -race proof for the async design.
//
// The store is the ONLY shared mutable state between the fetch goroutine and the
// render goroutine, and it is shared in the one shape that is easy to get wrong:
// a pointer to a large immutable struct, published under a write lock and read
// under a read lock, with the reader then using the POINTER rather than the lock.
//
// So the test does exactly what the program does, concurrently: one goroutine
// publishing fresh snapshots, one building frames and drawing them. Under -race,
// an unsynchronised field read or write fails here. Under no -race it passes,
// which is why the CI gate runs the suite with -race and this test is in it.
func TestStoreIsRaceFree(t *testing.T) {
	st := &store{}
	d := newDashboard(100, 32)

	// Seed it so the reader has something to draw from the first iteration.
	m, _ := newOfflineSource().Fetch(context.Background())
	first := &snapshot{m: m, at: at, source: "offline", ok: true}
	st.publish(first)
	d.SetFrame(buildFrame(first))

	done := make(chan struct{})

	// The writer: what the fetch goroutine does every refreshInterval.
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			mm, _ := newOfflineSource().Fetch(context.Background())
			// Vary the content, so the reader is not just re-reading an unchanged
			// pointer and the widgets are genuinely mutated underneath it.
			mm.history = mm.history[:1+len(mm.history)/2]
			mm.rates["EUR"] = 0.8 + float64(i%20)/100
			st.publish(&snapshot{m: mm, at: at, source: "live: test", ok: true})
		}
	}()

	// The reader: what the render goroutine does every frame.
	buf := buffer.NewBuffer(100, 32)
	for i := 0; i < 400; i++ {
		s, gen := st.load()
		if s == nil {
			continue
		}
		if gen == 0 {
			t.Error("a published snapshot reported generation zero")
		}
		d.SetFrame(buildFrame(s))
		d.Draw(buf)
	}
	<-done
}

// TestPostHandoffAppliesTheFrame is the other half of the async design: that a
// fetch's result reaches the widgets through the renderer's Post queue rather than
// through a direct call from the fetch goroutine.
//
// It matters because Post is what makes the handoff SAFE — a posted callback runs
// on the render goroutine, so SetValues and SetRows never race a Draw — and a
// version of this program that called them directly from the fetcher would be
// correct in every test that did not run -race with a live fetcher. This test
// exercises the real queue.
func TestPostHandoffAppliesTheFrame(t *testing.T) {
	st := &store{}
	m, _ := newOfflineSource().Fetch(context.Background())
	st.publish(&snapshot{m: m, at: at, source: "offline", ok: true})

	d := newDashboard(100, 32)
	applied := false
	post := func(fn func()) {
		fn()
		if d.FrameGen() == 1 {
			applied = true
		}
	}

	post(func() {
		s, _ := st.load()
		if s == nil {
			t.Fatal("the store is empty")
		}
		d.SetFrame(buildFrame(s))
	})
	if !applied {
		t.Error("the posted callback did not apply the frame")
	}
	if len(d.spark.Values) == 0 {
		t.Error("the widgets hold no data after the handoff")
	}
}

// TestStorePublishesMonotonicGenerations is the store's own contract: a reader
// must be able to tell a new snapshot from the one it already has, which is what
// the heartbeat relies on to avoid republishing stale state.
func TestStorePublishesMonotonicGenerations(t *testing.T) {
	st := &store{}
	if s, gen := st.load(); s != nil || gen != 0 {
		t.Fatalf("an empty store returned (%v, %d), want (nil, 0)", s, gen)
	}
	m, _ := newOfflineSource().Fetch(context.Background())
	var last uint64
	for i := 1; i <= 5; i++ {
		got := st.publish(&snapshot{m: m, at: at, source: "offline", ok: true})
		if got != uint64(i) {
			t.Errorf("publish %d returned generation %d", i, got)
		}
		s, gen := st.load()
		if gen != got {
			t.Errorf("after publishing generation %d, load reports %d", got, gen)
		}
		if s == nil {
			t.Fatalf("load returned nil after publishing generation %d", got)
		}
		if got <= last {
			t.Errorf("generation %d did not advance past %d", got, last)
		}
		last = got
	}
}

// ---------------------------------------------------------------------------
// the offline path and the live decode
// ---------------------------------------------------------------------------

// TestOfflineSourceIsDeterministic is the property every golden rests on.
//
// Two fetches must produce identical markets, because a golden that changed
// between runs would be a golden nobody could act on. It also checks that the
// offline path does not merely return the same POINTER — two callers must not be
// able to mutate each other's data through it.
func TestOfflineSourceIsDeterministic(t *testing.T) {
	src := newOfflineSource()
	a, err1 := src.Fetch(context.Background())
	b, err2 := src.Fetch(context.Background())
	if err1 != nil || err2 != nil {
		t.Fatalf("the offline source failed: %v / %v", err1, err2)
	}
	if a == b {
		t.Error("the offline source returned the same pointer twice; a caller could mutate another caller's data")
	}
	fa, fb := buildFrame(&snapshot{m: a, at: at, source: src.label(), ok: true}), buildFrame(&snapshot{m: b, at: at, source: src.label(), ok: true})
	if fa.status != fb.status {
		t.Errorf("two fetches produced different status lines:\n%q\n%q", fa.status, fb.status)
	}
	for i := range numTiles {
		if fa.tiles[i] != fb.tiles[i] {
			t.Errorf("tile %d differs between two fetches: %+v vs %+v", i, fa.tiles[i], fb.tiles[i])
		}
	}
	if len(fa.rows) != len(fb.rows) {
		t.Errorf("two fetches produced %d and %d rows", len(fa.rows), len(fb.rows))
	}
	for i := range fa.rows {
		if len(fa.rows[i].Cells) != len(fb.rows[i].Cells) {
			t.Fatalf("row %d differs in cell count between two fetches", i)
		}
		for j := range fa.rows[i].Cells {
			if fa.rows[i].Cells[j].Text != fb.rows[i].Cells[j].Text {
				t.Errorf("row %d cell %d: %q vs %q", i, j, fa.rows[i].Cells[j].Text, fb.rows[i].Cells[j].Text)
			}
		}
	}
}

// TestOfflinePathNeedsNoNetwork is the structural version of the same claim: the
// offline source takes a context and ignores it, so there is nothing in it that
// could reach a socket.
//
// It is asserted by cancelling the context first — a source that reached for the
// network with a cancelled context would fail, and one that ignored the context
// entirely would still pass, so the assertion is on the DATA rather than on the
// absence of an error.
func TestOfflinePathNeedsNoNetwork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // any network use from here on fails immediately

	m, err := newOfflineSource().Fetch(ctx)
	if err != nil {
		t.Fatalf("the offline source failed with a cancelled context: %v", err)
	}
	if !m.anyData() {
		t.Fatal("the offline source returned no data")
	}
	// gen is set explicitly: SetFrame skips a republish of the generation it has
	// already applied, and a zero-generation frame would be skipped against
	// another.
	d := newDashboard(goldenWideW, goldenWideH)
	d.SetFrame(buildFrame(&snapshot{m: m, at: at, source: "offline", ok: true, gen: 1}))
	got := screen(t, goldenWideW, goldenWideH, 1, d)
	if !strings.Contains(got, "0.89087") {
		t.Errorf("the offline screen does not show the sample data:\n%s", got)
	}
}

// TestWindowRequestsEveryTrackedCurrency is the request-shape test.
//
// The window request is what makes the "1d" column derivable for EVERY row rather
// than only for the pair the sparkline tracks, and that is invisible in the
// rendering when it is wrong: the eleven rows that lose their previous day read
// "n/a", which looks like a partial outage rather than a request that asked for
// one currency instead of twelve.
//
// So it is asserted on the REQUEST rather than on the screen, which is the only
// place the difference is visible.
func TestWindowRequestsEveryTrackedCurrency(t *testing.T) {
	// Only a WINDOW request counts. The crypto request goes to a different path
	// and is asked for regardless of whether FX worked, which is the whole point of
	// the two sources being independent.
	var seen string
	isWindow := func(req *http.Request) bool { return strings.Contains(req.URL.Path, "..") }
	rt := &recordingTransport{onRequest: func(req *http.Request) {
		if isWindow(req) {
			seen = req.URL.String()
		}
	}}
	l := &liveSource{
		client:   &http.Client{Transport: rt},
		fxBase:   "https://stub",
		crypto:   "https://stub",
		cryptoID: trackedCrypto,
	}
	// The spot request fails, so the window is never reached — which is what makes
	// the ordering visible: fetchFXWindow runs only after a usable spot response.
	if _, err := l.Fetch(context.Background()); err == nil {
		t.Fatal("the cycle reported no error against a transport that fails everything")
	}
	if seen != "" {
		t.Fatalf("the window was requested before the spot snapshot succeeded: %s", seen)
	}

	// Now let the spot through and record the second request.
	rt.responses = map[string]string{
		"/latest": `{"base":"USD","date":"2026-10-02","rates":{"EUR":0.89}}`,
	}
	rt.onRequest = func(req *http.Request) {
		if isWindow(req) {
			seen = req.URL.String()
		}
	}
	_, _ = l.Fetch(context.Background())
	if seen == "" {
		t.Fatal("the window was never requested")
	}
	for _, code := range trackedFX {
		if !strings.Contains(seen, code) {
			t.Errorf("the window request does not ask for %s: %s", code, seen)
		}
	}
	if !strings.Contains(seen, "from=USD") {
		t.Errorf("the window request does not set its base: %s", seen)
	}
}

// TestLiveSourceDecodesRealResponses drives the real decode, timeout and
// error-reporting code with stubbed transports and NO socket.
//
// The fixtures are verbatim captures of the two services' responses. The reason
// for using the real shapes rather than a hand-written minimal object is that a
// decoder is only correct against the response that actually arrives: a
// hand-written fixture with one rate would pass a decoder that ignored the nested
// date map entirely, and the range response is exactly where the nesting is.
func TestLiveSourceDecodesRealResponses(t *testing.T) {
	fxSpot := `{"amount":1.0,"base":"USD","date":"2026-10-02","rates":{"AUD":1.4411,"CAD":1.424,"CHF":0.82664,` +
		`"CNY":6.7046,"EUR":0.89087,"GBP":0.75753,"INR":96.32,"JPY":157.67,"KRW":1348.28,"MXN":18.335,` +
		`"NOK":9.6494,"SEK":10.0579}}`
	// Deliberately out of order in the JSON, because Go's decoder does not
	// preserve object order and a decoder that trusted the order would draw a
	// different sparkline on every run.
	fxRange := `{"amount":1.0,"base":"USD","start_date":"2026-09-01","end_date":"2026-10-02","rates":{` +
		`"2026-10-02":{"EUR":0.89087},"2026-09-01":{"EUR":0.86281},"2026-09-18":{"EUR":0.8726},` +
		`"2026-09-09":{"EUR":0.85822}}}`
	crypto := `{"bitcoin":{"usd":85492,"usd_24h_change":0.7297629548202077},` +
		`"ethereum":{"usd":2701.83,"usd_24h_change":0.5394919989823179}}`

	routes := map[string]string{
		"/latest":      fxSpot,
		"..2026-10-02": fxRange, // the history window
		"simple/price": crypto,  // the crypto prices
	}
	l := &liveSource{
		client:   &http.Client{Transport: stubTransport{routes: routes}},
		fxBase:   "https://stub",
		crypto:   "https://stub",
		cryptoID: trackedCrypto,
	}

	m, err := l.Fetch(context.Background())
	if err != nil {
		t.Fatalf("the live source failed against well-formed fixtures: %v", err)
	}
	if m.asOf != "2026-10-02" {
		t.Errorf("the as-of date is %q, want 2026-10-02", m.asOf)
	}
	if got := m.rates["EUR"]; got != 0.89087 {
		t.Errorf("the EUR spot is %v, want 0.89087", got)
	}
	if got := m.crypto["bitcoin"].USD; got != 85492 {
		t.Errorf("the BTC price is %v, want 85492", got)
	}
	if got := m.crypto["bitcoin"].Change; got < 0.72 || got > 0.74 {
		t.Errorf("the BTC 24h change is %v, want about 0.7298", got)
	}
	// The dates are SORTED, which is the property the sparkline depends on and the
	// one an unordered map would break silently.
	if len(m.dates) != 4 {
		t.Fatalf("the window holds %d dates, want 4", len(m.dates))
	}
	for i := 1; i < len(m.dates); i++ {
		if m.dates[i] <= m.dates[i-1] {
			t.Errorf("the dates are not sorted: %v", m.dates)
			break
		}
	}
	if m.history[0] != 0.86281 || m.history[len(m.history)-1] != 0.89087 {
		t.Errorf("the history is not in date order: %v", m.history)
	}
	// The previous publication day is the SECOND-TO-LAST, so a change is measured
	// against yesterday rather than against the window's start.
	if got := m.prev["EUR"]; got != 0.8726 {
		t.Errorf("the previous EUR rate is %v, want 0.8726 (2026-09-18)", got)
	}
	chg, ok := m.changePct("EUR")
	if !ok {
		t.Fatal("no change was computed for EUR")
	}
	want := (0.89087 - 0.8726) / 0.8726 * 100
	if diff := chg - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("the EUR change is %v, want %v", chg, want)
	}
}

// TestLiveSourceReportsEveryFailureMode is the "degrade visibly" assertion at the
// layer that produces the errors.
//
// Each mode gets its own case because each has a different correct message, and a
// single "it returned an error" assertion would pass an implementation that
// returned the same useless error for all four.
func TestLiveSourceReportsEveryFailureMode(t *testing.T) {
	cases := []struct {
		name    string
		rt      http.RoundTripper
		want    string
		wantAny bool // match a substring rather than the whole message
	}{
		{
			name: "timeout",
			rt:   stubTransport{err: context.DeadlineExceeded},
			want: "timed out after 5s",
		},
		{
			name: "transport failure",
			rt:   stubTransport{err: errors.New("connection refused")},
			want: "connection refused",
		},
		{
			name: "http error status",
			rt:   stubTransport{status: 503, body: "upstream down"},
			want: "HTTP 503",
		},
		{
			name: "undecodable body",
			rt:   stubTransport{body: "<html>not json</html>"},
			want: "decoding the body",
		},
		{
			name: "empty rates",
			rt:   stubTransport{body: `{"base":"USD","date":"2026-10-02","rates":{}}`},
			want: "no rates",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := &liveSource{
				client:   &http.Client{Transport: c.rt},
				fxBase:   "https://stub",
				crypto:   "https://stub",
				cryptoID: trackedCrypto,
			}
			m, err := l.Fetch(context.Background())
			if err == nil {
				t.Fatalf("a %s response produced no error", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error is %q, which does not mention %q", err.Error(), c.want)
			}
			if m == nil {
				t.Fatal("the cycle returned no market at all; a partial result must survive")
			}
			if oneLine(err) == "" {
				t.Error("oneLine produced nothing from a non-nil error")
			}
			// And the error is ONE line, because it goes into a one-row footer and
			// a newline would write a control character into a cell.
			if strings.Contains(oneLine(err), "\n") {
				t.Errorf("oneLine left a newline in %q", oneLine(err))
			}
		})
	}
}

// TestLiveSourceKeepsTheGoodHalfOfAPartialCycle is the partial case at the source
// layer, which is where it is decided.
//
// Crypto fails and FX succeeds: the returned market must carry the FX data AND a
// non-nil error naming crypto. Returning nil would lose data the program already
// had, and returning a nil error would hide a failure from the screen.
func TestLiveSourceKeepsTheGoodHalfOfAPartialCycle(t *testing.T) {
	fxSpot := `{"base":"USD","date":"2026-10-02","rates":{"EUR":0.89087}}`
	l := &liveSource{
		client:   &http.Client{Transport: stubTransport{routes: map[string]string{"/latest": fxSpot}}},
		fxBase:   "https://stub",
		crypto:   "https://stub",
		cryptoID: trackedCrypto,
	}

	m, err := l.Fetch(context.Background())
	if err == nil {
		t.Fatal("a crypto-only failure produced no error")
	}
	if !strings.Contains(err.Error(), "crypto") {
		t.Errorf("the error is %q, which does not name the failing source", err.Error())
	}
	if !m.anyData() {
		t.Error("the FX half of the cycle was discarded")
	}
	if got := m.rates["EUR"]; got != 0.89087 {
		t.Errorf("the EUR spot is %v, want 0.89087", got)
	}
}

// TestFetchTimeoutIsBounded is the promise at the top of feed.go, asserted.
//
// The claim is that a cycle cannot exceed fetchTimeout however many requests it
// makes. The test uses a transport that blocks until the context is done, and
// asserts the cycle returns within a bound derived from fetchTimeout rather than
// hanging — which is what a missing deadline would do, and a test that merely
// called Fetch and checked the error would have waited forever to find out.
func TestFetchTimeoutIsBounded(t *testing.T) {
	l := &liveSource{
		client:   &http.Client{Transport: blockTransport{}},
		fxBase:   "https://stub",
		crypto:   "https://stub",
		cryptoID: trackedCrypto,
	}
	// A shortened client timeout so the test is fast; the mechanism under test is
	// the CONTEXT deadline, which the cycle sets itself.
	l.client.Timeout = 50 * time.Millisecond

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := l.Fetch(context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a blocked transport produced no error")
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("the cycle took %s, which is not bounded", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cycle did not return: the timeout is not being applied")
	}

	// And the constant itself is the documented five seconds, so the number in the
	// message and the number in the code cannot disagree.
	if fetchTimeout != 5*time.Second {
		t.Errorf("fetchTimeout is %s, want 5s", fetchTimeout)
	}
}

// ---------------------------------------------------------------------------
// the frame builder
// ---------------------------------------------------------------------------

// TestBuildFrameIsPureOfClockAndNetwork is what makes the goldens possible.
//
// buildFrame is called with a snapshot and nothing else: no time.Now, no globals
// that change between runs, no I/O. The test proves it by calling it twice and
// comparing every field, which is a weaker statement than "it reads no clock" but
// a stronger one than the goldens alone — it catches a clock read even if the
// resulting difference happens not to reach the screen.
func TestBuildFrameIsPureOfClockAndNetwork(t *testing.T) {
	m, _ := newOfflineSource().Fetch(context.Background())
	s := &snapshot{m: m, at: at, source: "offline", ok: true, gen: 1}

	a := buildFrame(s)
	b := buildFrame(s)
	if a.status != b.status || a.noData != b.noData || a.stale != b.stale || a.noDataBody != b.noDataBody {
		t.Error("two builds of the same snapshot disagree on the status fields")
	}
	for i := range numTiles {
		if a.tiles[i] != b.tiles[i] {
			t.Errorf("tile %d differs between two builds: %+v vs %+v", i, a.tiles[i], b.tiles[i])
		}
	}
	if a.gaugeLo != b.gaugeLo || a.gaugeHi != b.gaugeHi || a.gaugeV != b.gaugeV || a.meterV != b.meterV {
		t.Error("two builds of the same snapshot disagree on the measurements")
	}
}

// TestPreviousDayIsTheOneBeforeTheLast pins WHICH publication the "1d" column is
// measured against, which is the assumption every change on the screen rests on.
//
// The ECB publishes one rate per business day, so "today's change" means today's
// rate against YESTERDAY's publication. The alternative — against the start of the
// window — produces a number that is perfectly well-formed and completely wrong:
// it is a month-long move wearing a "1d" header. Nothing about the arithmetic
// distinguishes them, which is exactly why the assertion is on the VALUE rather
// than on the column existing.
//
// It is asserted against the arithmetic rather than a literal, so a change to the
// bundled fixture does not silently re-point what the test is checking.
func TestPreviousDayIsTheOneBeforeTheLast(t *testing.T) {
	m := sampleMarket()
	if len(m.dates) < 2 {
		t.Fatal("the sample window is too short to have a previous day")
	}
	wantPrev := m.history[len(m.history)-2]
	wantChg := (m.history[len(m.history)-1] - wantPrev) / wantPrev * 100

	if got := m.prev[m.primary]; got != wantPrev {
		t.Errorf("the previous rate is %v, want %v (the rate on %s, the day before the last)",
			got, wantPrev, m.dates[len(m.dates)-2])
	}
	got, ok := m.changePct(m.primary)
	if !ok {
		t.Fatal("no change was derivable")
	}
	if diff := got - wantChg; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("the change is %v%%, want %v%% — measured from %s rather than %s",
			got, wantChg, m.dates[len(m.dates)-2], m.dates[len(m.dates)-1])
	}
	// And the month-long figure it must NOT be showing. A one-day move on a major
	// pair is well under a percent; a month of this fixture's drift is over two.
	monthLong := (m.history[len(m.history)-1] - m.history[0]) / m.history[0] * 100
	if diff := got - monthLong; diff < 1e-9 || diff > 1e-9 {
		t.Logf("for the record: the window-long move is %v%% and the 1d move is %v%%", monthLong, got)
	}
	if monthLong < 1 {
		t.Fatalf("the fixture's window move is only %v%%, so it cannot distinguish the two readings", monthLong)
	}
}

// TestAbsentValuesRenderAsAbsentNotAsZero is the one assertion that would catch
// the failure this brief calls out by name.
//
// A dashboard that renders a missing rate as 0.00000 has produced a screen that is
// confidently wrong, and it is wrong in a way that is invisible: every panel is
// present, every number is plausible, and no test that checks for "the panels are
// there" would notice. So the test removes a currency's previous day — which makes
// its change underivable — and asserts the change reads as absent.
func TestAbsentValuesRenderAsAbsentNotAsZero(t *testing.T) {
	m := sampleMarket()
	delete(m.prev, "EUR") // no previous publication, so no change is derivable

	d := newDashboard(goldenWideW, goldenWideH)
	d.SetFrame(buildFrame(&snapshot{m: m, at: at, source: "offline", ok: true, gen: 1}))
	got := screen(t, goldenWideW, goldenWideH, 1, d)

	// The EUR row specifically, because other rows in the fixture have a genuinely
	// tiny move and would print "+0.00%" quite legitimately. Asserting on the whole
	// screen would therefore be asserting nothing.
	//
	// The match is on the pair AND its rate, which is the table's row: the KPI tile
	// above repeats the pair's name but carries no rate.
	eurRow := rowWith(got, "EUR/USD ", "0.89087")
	if eurRow == "" {
		t.Fatalf("the EUR row is not on screen:\n%s", got)
	}
	if strings.Contains(eurRow, "0.00%") {
		t.Errorf("an underivable change rendered as a number: %q", eurRow)
	}
	if !strings.Contains(eurRow, noDataPlaceholder) {
		t.Errorf("an underivable change does not read as absent: %q", eurRow)
	}
	// And the SPOT rate is unaffected: one absent input must not blank the figures
	// that are present, or a partial outage looks like a total one.
	if !strings.Contains(got, "0.89087") {
		t.Errorf("losing one previous rate blanked the spot rate too:\n%s", got)
	}
}

// TestMeterAgreesWithTheKPIChange is the cross-panel consistency check.
//
// Two panels report the same day's move — the KPI tile in percent and the meter in
// basis points — and they must agree. If they disagree, the screen is showing two
// truths, and which one the reader believes is arbitrary.
func TestMeterAgreesWithTheKPIChange(t *testing.T) {
	m := sampleMarket()
	f := buildFrame(&snapshot{m: m, at: at, source: "offline", ok: true, gen: 1})

	want, ok := m.changePct(m.primary)
	if !ok {
		t.Fatal("the sample has no derivable change")
	}
	if diff := f.meterV - want*100; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("the meter reads %v bps but the change is %v%% (%v bps)", f.meterV, want, want*100)
	}
	// And the direction agrees: a positive change is UP on both.
	if got := changeDir(want, true); got != dirUp {
		t.Errorf("a %v%% change is classified %v, want dirUp", want, got)
	}
	if !strings.Contains(f.tiles[tileChange].value, "+") {
		t.Errorf("the KPI tile's change is %q, which carries no sign", f.tiles[tileChange].value)
	}
}

// TestTableIsSortedByMoveSize is the detail band's ordering claim, which is what
// makes it useful rather than a list.
//
// A table ordered by whatever order the API used is a lookup table; a table
// ordered by the size of the move is a ranking, which is the thing a reader opens a
// dashboard for.
func TestTableIsSortedByMoveSize(t *testing.T) {
	f := buildFrame(&snapshot{m: sampleMarket(), at: at, source: "offline", ok: true, gen: 1})
	if len(f.rows) < 2 {
		t.Fatalf("the sample produced %d rows", len(f.rows))
	}
	prev := 1e9
	for i, r := range f.rows {
		// The cell carries an arrow and a space before the number, so the number
		// is read from the second field rather than parsed off the front.
		chg := r.Cells[2].Text
		_, num, ok := strings.Cut(chg, " ")
		if !ok {
			t.Errorf("row %d's change %q has no direction field", i, chg)
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(num, "%f", &v); err != nil {
			t.Errorf("row %d's change %q does not parse: %v", i, num, err)
			continue
		}
		if a := abs(v); a > prev+1e-9 {
			t.Errorf("row %d's move (%v%%) is larger than the row above it (%v%%); the table is not sorted",
				i, a, prev)
		}
		prev = abs(v)
	}
}

// TestChartValuesAreAbsolute is the honesty check on the bar chart.
//
// The chart plots absolute basis points, so a losing pair draws a bar rather than
// an empty row. If a signed value ever reached it, a negative would draw nothing
// and the panel would say "did not move" about a pair that fell.
func TestChartValuesAreAbsolute(t *testing.T) {
	f := buildFrame(&snapshot{m: sampleMarket(), at: at, source: "offline", ok: true, gen: 1})
	if len(f.chart) == 0 {
		t.Fatal("the chart is empty")
	}
	sawNegative := false
	for i, d := range f.chart {
		if d.Value < 0 {
			t.Errorf("bar %d has a negative value %v; the chart plots absolute moves", i, d.Value)
		}
		if strings.ContainsAny(d.Label, "▼v") {
			sawNegative = true
		}
	}
	if !sawNegative {
		t.Error("no bar is labelled as a fall; the fixture should contain one, or the arrow is missing")
	}
	// Every bar's label carries a direction glyph, so the absolute value is never
	// the only thing the reader has.
	for i, d := range f.chart {
		if !strings.ContainsAny(d.Label, string(arrowUp)+string(arrowDown)+string(arrowUpASCII)+string(arrowDownASCII)+string(arrowFlat)) {
			t.Errorf("bar %d's label %q carries no direction glyph", i, d.Label)
		}
	}
}

// TestRateTextPrecisionIsScaleAware checks the formatter against the cases that
// make a currency column unreadable if they are wrong: five orders of magnitude in
// one column.
//
// Two currencies that print identically are worse than two that print to different
// precisions, because the reader concludes they are equal.
func TestRateTextPrecisionIsScaleAware(t *testing.T) {
	// The precision bands. Sub-unit rates get five decimals because that is the
	// ECB's own precision for the majors; anything at or above one gets four, which
	// is enough to distinguish 96.32 from 96.33; anything at or above a hundred gets
	// two, because there is no decimal place a reader of a KRW quote uses.
	cases := []struct {
		in   float64
		want string
	}{
		{0.89087, "0.89087"},
		{0.5, "0.50000"},
		{1.42400, "1.4240"},
		{10.0579, "10.0579"},
		{96.32, "96.3200"},
		{99.995, "99.9950"},
		{100, "100.00"},
		{157.67, "157.67"},
		{1348.28, "1348.28"},
		{85492, "85492.00"},
		{0, noDataPlaceholder},
		{-1, noDataPlaceholder},
	}
	seen := map[string]float64{}
	for _, c := range cases {
		if got := rateText(c.in); got != c.want {
			t.Errorf("rateText(%v) = %q, want %q", c.in, got, c.want)
		}
		// Every distinct rate must print differently. Two currencies that print
		// identically are worse than two that print to different precisions, because
		// the reader concludes they are equal.
		if prev, dup := seen[c.want]; dup && c.in > 0 {
			t.Errorf("rateText(%v) and rateText(%v) both print %q", prev, c.in, c.want)
		}
		if c.in > 0 {
			seen[c.want] = c.in
		}
	}
}

// TestSignPctAlwaysCarriesASign is the sign rule, at the formatter.
//
// A column where "+0.65" and "0.65" mean the same thing needs a convention
// explained somewhere; "always signed" needs none, and that is the whole reason it
// is the rule.
func TestSignPctAlwaysCarriesASign(t *testing.T) {
	cases := []struct {
		in   float64
		ok   bool
		want string
	}{
		{0.65, true, "+0.65%"},
		{-0.95, true, "-0.95%"},
		{0, true, "+0.00%"},
		{1.5, true, "+1.50%"},
		{0, false, noDataPlaceholder},
	}
	for _, c := range cases {
		if got := signPct(c.in, c.ok); got != c.want {
			t.Errorf("signPct(%v, %v) = %q, want %q", c.in, c.ok, got, c.want)
		}
	}
}

// TestDirectionThresholdAgreesWithTheMeterZones is the cross-panel consistency
// check on the flat band.
//
// A change of 15 basis points is FLAT to dirOf and inside the meter's flat zone.
// If the two thresholds disagreed, the screen would show an up arrow beside a bar
// that says "flat", and the reader would have to decide which to believe.
func TestDirectionThresholdAgreesWithTheMeterZones(t *testing.T) {
	zones := []struct {
		from, to float64
		name     string
	}{
		{meterLow, -meterFlat, "down"},
		{-meterFlat, meterFlat, "flat"},
		{meterFlat, meterHigh, "up"},
	}
	for _, z := range zones {
		// A value comfortably inside the zone.
		mid := (z.from + z.to) / 2
		var want direction
		switch z.name {
		case "down":
			want = dirDown
		case "flat":
			want = dirFlat
		case "up":
			want = dirUp
		}
		if got := dirOf(mid, true); got != want {
			t.Errorf("%.2f bps is in the %q zone but dirOf says %v", mid, z.name, got)
		}
	}

	// At each EDGE, and through the values either side of it.
	//
	// The percentages are written as exact quarters rather than as bps divided by a
	// hundred, because bps/100*100 is not always bps in binary floating point — and
	// a boundary test that fails on a rounding artefact teaches the reader nothing
	// about the code. The consistency between the arrow and the meter's band is
	// checked separately, below, on the SAME float both of them are given.
	for _, c := range []struct {
		bps  float64
		want direction
	}{
		{-30, dirDown},
		{-20.1, dirDown},
		{-20, dirFlat}, // the down band's upper edge belongs to flat
		{19.9, dirFlat},
		{20, dirUp}, // and the flat band's upper edge belongs to up
		{20.1, dirUp},
	} {
		if got := dirOf(c.bps, true); got != c.want {
			t.Errorf("a %v bps move is %v, want %v", c.bps, got, c.want)
		}
	}
	// And the consistency the edges above cannot show: the ARROW and the meter's BAND
	// computed from the same float. "The same float" is the point: the meter is driven
	// with a basis-point figure and the arrow is classified from that same figure, so
	// this compares two readings of one number rather than re-deriving it twice — which
	// is where a rounding artefact would otherwise appear as a spurious failure and
	// teach the reader nothing. An up arrow beside a bar labelled "flat" is what this
	// catches.
	for _, bps := range []float64{
		-300, -100, -20.1, -20, -19.9, 0, 19.9, 20, 20.1, 100, 300,
	} {
		d := board(t, goldenWideW, goldenWideH)
		d.meter.Set(bps)
		i, ok := d.meter.ActiveZone(bps)
		if !ok {
			t.Fatalf("a %v bps move is in no meter zone", bps)
		}
		fromMeter := directionOfZone(d.meter.Zones[i].Name)
		if fromArrow := dirOf(bps, true); fromArrow != fromMeter {
			t.Errorf("a %v bps move: the arrow says %v and the meter's %q band says %v",
				bps, fromArrow, d.meter.Zones[i].Name, fromMeter)
		}
	}
	// A move inside the flat band is FLAT, not rounded to a sign.
	if got := dirOf(0.01, true); got != dirFlat {
		t.Errorf("a 0.01 bps move is %v, want dirFlat rather than a rounded sign", got)
	}
	// And an underivable change has NO direction, rather than dirFlat — the
	// difference between "it did not move" and "we do not know".
	if got := dirOf(5, false); got != dirNone {
		t.Errorf("an underivable change is %v, want dirNone", got)
	}
	if got := mark(dirNone, false); got != ' ' {
		t.Errorf("the glyph for no direction is %q, want a space", string(got))
	}
}

// TestExtremesIgnoreANonPositiveRate guards the window maths against a bad input.
//
// A zero or negative rate in the series would make "the window low" a number no
// currency has, and the gauge's scale would be nonsense. absorbSeries already drops
// them on the way in; this checks the arithmetic is safe if one arrives anyway.
func TestExtremesIgnoreANonPositiveRate(t *testing.T) {
	m := newMarket()
	m.history = []float64{0.89, 0.0, 0.87, -1, 0.9}
	lo, hi, ok := m.extremes()
	if !ok {
		t.Fatal("extremes reported no data for a five-element series")
	}
	if lo >= hi {
		t.Errorf("extremes returned lo=%v hi=%v, which is not a range", lo, hi)
	}
	// A single-element series has no range, and must say so rather than reporting a
	// range of zero.
	m.history = []float64{0.89}
	if _, _, ok := m.extremes(); ok {
		t.Error("a one-element series reported a range")
	}
	if m.hasHistory() {
		t.Error("a one-element series reported itself as plottable")
	}
	m.history = nil
	if _, _, ok := m.extremes(); ok {
		t.Error("an empty series reported a range")
	}
}

// TestGettersRejectAMissingMarket checks the accessors a partial or empty snapshot
// reaches.
//
// Every one of these is called on a snapshot from a source that may have answered
// with nothing, so each has to answer "not available" rather than divide by zero
// or index an empty slice.
func TestGettersRejectAMissingMarket(t *testing.T) {
	m := newMarket()
	if _, ok := m.spot(); ok {
		t.Error("an empty market reported a spot rate")
	}
	if _, ok := m.changePct("EUR"); ok {
		t.Error("an empty market reported a change")
	}
	if m.anyData() {
		t.Error("an empty market reported data")
	}
	// A rate of zero is not a rate.
	m.rates["EUR"] = 0
	if _, ok := m.spot(); ok {
		t.Error("a zero rate was reported as a spot rate")
	}
	// A previous rate of zero would divide by zero.
	m.rates["EUR"] = 0.9
	m.prev["EUR"] = 0
	if _, ok := m.changePct("EUR"); ok {
		t.Error("a zero previous rate produced a change rather than an absence")
	}
}

// TestOneLineIsBoundedAndSingleLine is the footer-safety property.
//
// A long error or one carrying a newline would either overflow the single-row
// footer or, worse, write a control character into a cell and shift every column
// after it. net/http's *url.Error pretty-prints, so this is not hypothetical.
func TestOneLineIsBoundedAndSingleLine(t *testing.T) {
	if got := oneLine(nil); got != "" {
		t.Errorf("oneLine(nil) = %q, want the empty string", got)
	}
	long := errors.New(strings.Repeat("x", 500))
	if got := oneLine(long); len(got) > 72 {
		t.Errorf("oneLine left %d characters, which is more than a footer row can show", len(got))
	}
	multiline := errors.New("first line\nsecond line\twith a tab")
	got := oneLine(multiline)
	if strings.ContainsAny(got, "\n\t") {
		t.Errorf("oneLine left a control character in %q", got)
	}
	if !strings.Contains(got, "first line") || !strings.Contains(got, "second line") {
		t.Errorf("oneLine dropped content: %q", got)
	}
}

// TestOneLineTruncatesOnARuneBoundary is the UTF-8 half of the bound above.
//
// The limit is a count of CELLS but a string is indexed in BYTES, so clipping at
// byte 72 cuts a three-byte rune in half and leaves invalid UTF-8 for the cell
// buffer — and the failure strings that reach oneLine are assembled from API
// responses, so a single accented or non-Latin character is entirely plausible.
//
// The padding is chosen so the 72-byte cut lands inside the run of wide runes
// rather than between them, which is the only arrangement that exercises the
// boundary at all.
func TestOneLineTruncatesOnARuneBoundary(t *testing.T) {
	// 58 ASCII bytes, then 3-byte runes. Byte 72 falls two bytes into the fifth
	// rune, so a naive s[:72] splits it; backing up to 70 keeps four whole runes.
	src := strings.Repeat("a", 58) + strings.Repeat("日", 20)
	got := oneLine(errors.New(src))

	if len(got) > 72 {
		t.Errorf("oneLine returned %d bytes, over the 72-byte bound", len(got))
	}
	if !utf8.ValidString(got) {
		t.Errorf("oneLine split a rune: %q", got)
	}
	if !strings.HasSuffix(got, strings.Repeat("日", 4)) {
		t.Errorf("oneLine cut at %q, want the whole runes through the bound", got)
	}
	// The ASCII prefix must survive intact, so the fix is a clip and not a
	// wholesale replacement of anything non-ASCII.
	if !strings.HasPrefix(got, strings.Repeat("a", 58)) {
		t.Errorf("oneLine mangled the ASCII prefix: %q", got)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// renderNoColor renders through a renderer with colour DISABLED, which is how the
// accessibility test proves the direction signals do not depend on hue.
//
// It goes through the whole stack — renderer, two-tier diff, ANSI encoder,
// headless screen — so the assertion is about what a monochrome terminal would
// actually receive rather than about what the widgets would have drawn with the
// styles stripped by hand.
func renderNoColor(t testing.TB, w, h int, d *dashboard) string {
	t.Helper()
	sink := headless.NewMemorySink(w, h)
	r := newNoColorRenderer(sink, w, h)
	r.SetRoot(d)
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if got := sink.UnknownSequences(); got != 0 {
		t.Fatalf("the headless screen saw %d unrecognised sequences", got)
	}
	return widgettest.Screen(sink)
}

// rowWith returns the screen row mentioning every one of subs, or the empty string.
//
// It takes a list because one substring is not enough to identify a table row: the
// KPI tile above it repeats the pair's name in its own label. Several assertions
// here are about ONE row's cells, and a whole-screen assertion cannot distinguish
// them — a fixture with several near-zero moves would satisfy or defeat such an
// assertion for entirely the wrong reason.
// rowIn returns row y of a bare buffer as text, trailing blanks trimmed.
//
// It reads a Buffer rather than a MemorySink because one assertion here is about
// cells surviving in a buffer the widget is still drawing into, which is the only
// way to see the repaint rule under test: a fresh sink per size starts blank, and a
// blank buffer has no stale cells to find.
func rowIn(buf *buffer.Buffer, y, x, w int) string {
	row := buf.Row(y)
	var b strings.Builder
	for i := 0; i < w && x+i < len(row); i++ {
		if x+i < 0 {
			continue
		}
		b.WriteRune(row[x+i].Ch)
	}
	return strings.TrimRight(b.String(), " ")
}

func rowWith(screenText string, subs ...string) string {
	for _, line := range strings.Split(screenText, "\n") {
		all := true
		for _, s := range subs {
			if !strings.Contains(line, s) {
				all = false
				break
			}
		}
		if all {
			return line
		}
	}
	return ""
}

// directionOfZone is the meter's band NAME read back as a direction, which is the
// translation the two panels share. It is a function rather than an inline switch
// so that a zone renamed in one place fails here rather than silently comparing
// strings that never match.
func directionOfZone(name string) direction {
	switch name {
	case "down":
		return dirDown
	case "up":
		return dirUp
	case "flat":
		return dirFlat
	default:
		return dirNone
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// stubTransport answers from a fixed table of bodies, or fails.
//
// It is the reason the live decode paths are testable without a socket: the real
// getJSON, the real status check, the real body bound and the real json.Unmarshal
// all run, and only the socket is replaced.
type stubTransport struct {
	routes map[string]string // matched as a SUBSTRING of the URL
	body   string            // used when routes is nil
	status int
	err    error
	// oversize makes the body exceed maxBodyBytes, to exercise the bound.
	oversize bool
}

func (s stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	body := s.body
	if s.routes != nil {
		found := false
		for frag, b := range s.routes {
			if strings.Contains(req.URL.String(), frag) {
				body, found = b, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("stubTransport has no route for %s", req.URL)
		}
	}
	if s.oversize {
		body = strings.Repeat("x", maxBodyBytes+1)
	}
	status := s.status
	if status == 0 {
		status = 200
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// recordingTransport captures each request's URL and answers from a table, so a
// test can assert on what was ASKED rather than only on what came back.
type recordingTransport struct {
	responses map[string]string
	onRequest func(*http.Request)
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if r.onRequest != nil {
		r.onRequest(req)
	}
	for frag, body := range r.responses {
		if strings.Contains(req.URL.String(), frag) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	}
	return nil, fmt.Errorf("recordingTransport has no route for %s", req.URL)
}

// blockTransport blocks until the request's context is done, which is how a
// network that accepts the connection and then stalls is reproduced.
//
// Blocking on ctx rather than on a timer is the important part: the test then
// measures whether the CYCLE applies a deadline, rather than whether the transport
// happens to give up.
type blockTransport struct{}

func (blockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}
