package main

// The offline dataset: a captured snapshot, and the flag plumbing that selects it.
//
// # Why this file exists at all
//
// Three requirements converge on it. CI must not depend on the internet, a test
// that renders a dashboard twice must render the same dashboard twice, and a
// person on a plane must be able to run the example. All three are the same
// requirement — a deterministic source — and all three are met by making the
// offline path a SOURCE rather than a set of test-only overrides deep inside the
// widgets.
//
// The consequence is the property that makes this file worth having: the offline
// path builds a market and then goes through exactly the same view-building,
// layout and rendering code as the live one. There is no "offline rendering". If
// the offline screen is right, the live screen's layout is right, and a golden
// file over it is a real regression net for the layout rather than a picture of a
// test-only arrangement.
//
// # Where the numbers came from
//
// They are a capture of Frankfurter and CoinGecko responses from 2026-10-02,
// transcribed rather than generated: the shape of real data is part of what the
// golden files are pinning, and a smooth synthetic walk would pin the wrong
// shape. The window is the twenty-four business days the ECB published between
// 2026-09-01 and 2026-10-02 inclusive.

import (
	"context"
	"fmt"
	"time"
)

// sampleAsOf is the publication date the captured snapshot is dated, which is also
// what the status line shows in offline mode.
const sampleAsOf = "2026-10-02"

// offlineSource serves the captured snapshot.
//
// It satisfies source exactly as the live one does, including returning an error
// when it has been told to: the failure path is reachable offline, which is what
// lets its rendering be asserted without a network that has to fail on cue.
type offlineSource struct {
	m   *market
	err error
}

func newOfflineSource() *offlineSource {
	return &offlineSource{m: sampleMarket()}
}

func (o *offlineSource) label() string { return "offline: bundled sample, no network" }

// Fetch returns the captured snapshot.
//
// The error is returned even though the data comes back, deliberately: that is
// exactly the partial case the live path produces when one of its two sources
// fails, and the screen has to render it correctly.
func (o *offlineSource) Fetch(context.Context) (*market, error) {
	if o.err != nil {
		m := *o.m
		m.failures = []string{oneLine(o.err)}
		return &m, o.err
	}
	m := *o.m
	return &m, nil
}

// sampleMarket builds the captured snapshot.
//
// Every slice and map is freshly allocated here, on every call. The offline
// source hands the same market to more than one render in a test, and a widget
// that holds a series by reference would otherwise be reading a slice another
// render had also written to — which is a real bug in a real dashboard and is not
// worth importing to make a fixture cheaper.
func sampleMarket() *market {
	m := newMarket()
	m.asOf = sampleAsOf

	// The spot snapshot, as returned by /latest?from=USD on 2026-10-02: how many
	// units of each currency one US dollar buys.
	m.rates = map[string]float64{
		"AUD": 1.44110, "CAD": 1.42400, "CHF": 0.82664, "CNY": 6.70460,
		"EUR": 0.89087, "GBP": 0.75753, "INR": 96.3200, "JPY": 157.670,
		"KRW": 1348.280, "MXN": 18.3350, "NOK": 9.64940, "SEK": 10.0579,
	}
	// The previous publication day, which is what every "1d" figure is measured
	// against. Several of these move the other way from EUR, which is the point of
	// having more than one row: a column that is all one colour is not a test of
	// the colour rule.
	m.prev = map[string]float64{
		"AUD": 1.43880, "CAD": 1.42460, "CHF": 0.83528, "CNY": 6.70450,
		"EUR": 0.88511, "GBP": 0.75565, "INR": 96.3300, "JPY": 157.980,
		"KRW": 1361.270, "MXN": 18.1669, "NOK": 9.62560, "SEK": 10.0292,
	}
	m.dates = sampleDates
	m.history = sampleEUR

	m.crypto = cryptoBook{
		"bitcoin":  {USD: 85492.00, Change: 0.7297630},
		"ethereum": {USD: 2701.830, Change: 0.5394920},
	}
	return m
}

// sampleDates are the publication dates in the captured window, oldest first.
//
// They are kept alongside the series rather than derived from it so that a date
// missing from the rates — which absorbSeries drops along with its value — cannot
// silently shift the two slices out of alignment in the fixture.
var sampleDates = []string{
	"2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04",
	"2026-09-07", "2026-09-08", "2026-09-09", "2026-09-10",
	"2026-09-11", "2026-09-14", "2026-09-15", "2026-09-16",
	"2026-09-17", "2026-09-18", "2026-09-21", "2026-09-22",
	"2026-09-23", "2026-09-24", "2026-09-25", "2026-09-28",
	"2026-09-29", "2026-09-30", "2026-10-01", "2026-10-02",
}

// sampleEUR is the primary pair's rate on each of sampleDates: a slow grind up
// from 0.8628 with two visible pullbacks, which is what a month of a major pair
// actually looks like and which gives the sparkline a shape worth reading.
var sampleEUR = []float64{
	0.86281, 0.86371, 0.86096, 0.86044,
	0.86044, 0.86103, 0.85822, 0.86088,
	0.86266, 0.86573, 0.86663, 0.86678,
	0.87100, 0.87260, 0.87032, 0.87237,
	0.87635, 0.87974, 0.87696, 0.87889,
	0.88067, 0.88067, 0.88511, 0.89087,
}

// failedSource is a source that always fails, used by the test that pins what the
// screen shows when there is nothing to show. It returns an EMPTY market as well
// as an error, so the "no data at all" path is reachable without a network.
type failedSource struct{ reason string }

func (f failedSource) label() string { return "offline: no data" }

func (f failedSource) Fetch(context.Context) (*market, error) {
	m := newMarket()
	err := fmt.Errorf("%s", f.reason)
	m.failures = []string{oneLine(err)}
	return m, err
}

// staleSource is a source that returns a snapshot together with an error, which
// is the partial case: the screen keeps showing what it had and says which half
// of the feed is missing.
type staleSource struct {
	m   *market
	err error
}

func (s staleSource) label() string { return "offline: partial sample" }

func (s staleSource) Fetch(context.Context) (*market, error) {
	m := *s.m
	m.failures = []string{oneLine(s.err)}
	return &m, s.err
}

// clock is the time source, injected rather than read from the time package
// directly, so that a test can render a frame and know what the status line says
// without the golden depending on when the suite ran.
type clock func() time.Time

// wallClock is the real clock.
func wallClock() time.Time { return time.Now() }

// fixedClock is a clock stopped at an instant, for tests.
func fixedClock(at time.Time) clock { return func() time.Time { return at } }
