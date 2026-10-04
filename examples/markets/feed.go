package main

// The data layer: two public endpoints, one fetch cycle, and a plain struct at the
// end of it.
//
// # Why these two
//
// The brief for this example was "real data, no API key, no signup", which in
// practice means two services and two shapes:
//
//   - the European Central Bank's reference rates, republished by Frankfurter.
//     `latest` gives a spot snapshot and the publication date it is dated;
//     a `start..end` range gives a date-keyed map of every currency's rate on
//     every publication day, which is the time series the sparkline wants.
//     Business days only, so the series has no weekend gaps and a sparkline of it
//     does not invent any.
//   - CoinGecko's `simple/price`, which carries a 24-hour change alongside the
//     price. That is the one figure in this dashboard that is not derivable from
//     the FX series, so it is worth a second request.
//
// # What "degrade visibly" means here
//
// Every cycle is bounded by fetchTimeout, at the level of the whole cycle rather
// than of each request, so a cycle cannot cost three times the budget on a network
// that accepts the connection and then stalls. A cycle that fails reports WHICH
// source failed and keeps whatever the other one returned: a dashboard that loses
// its FX feed should still show crypto, labelled as such, rather than going blank.
// An error is carried as a value on the snapshot, never as a panic and never as a
// silently-empty field.
//
// Nothing in this file draws, and nothing here imports a widget: the network layer
// and the presentation layer are separate so that the offline path exercises
// exactly the same formatting code as the live one (see view.go and sample.go).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// The endpoints, and the two knobs that decide how patient this example is.
const (
	// fxBase is Frankfurter's root. It answers with a 301 to its current host,
	// which net/http follows by default; a client configured not to would see an
	// empty body rather than rates, which is why nothing here disables redirect
	// following.
	fxBase = "https://api.frankfurter.app"

	// cryptoBase is CoinGecko's API root. The free tier is anonymous and
	// rate-limited, which is why it is asked for two coins once every
	// refreshInterval rather than per keystroke or per frame.
	cryptoBase = "https://api.coingecko.com/api/v3"

	// fetchTimeout bounds ONE fetch cycle — every request in it together. It is
	// the whole of the "a dashboard that hangs on a dead network is worse than one
	// that says no data" requirement: at worst a cycle costs this long and then
	// the screen says what went wrong.
	fetchTimeout = 5 * time.Second

	// refreshInterval is how often a cycle runs. FX publishes once a business day
	// and crypto moves continuously, so anything much faster is polling a rate
	// limit to redraw the same numbers.
	refreshInterval = 30 * time.Second

	// maxBodyBytes bounds what a response may claim to be. Both services return a
	// few kilobytes; anything an order of magnitude larger is a misconfigured
	// endpoint or an error page, and reading that unbounded is how a dashboard
	// becomes a memory leak.
	maxBodyBytes = 1 << 20
)

// primaryPair is the currency the history, the sparkline and the gauge are about.
// EUR is the one pair every other currency on this screen is quoted against,
// because the base is USD.
const primaryPair = "EUR"

// selectablePairs are the currencies the on-screen chooser offers, in the order it
// shows them.
//
// Three, and only three: they are the majors this example's window response has
// carried reliably, and the list is deliberately short so the tab row fits without
// scrolling at the narrowest width the dashboard draws at — a control that has to
// be scrolled to discover what else it offers is worse than a short one. EUR is
// first because every other row on the screen is quoted against USD and EUR is the
// pair the rest of the dashboard was built around, so the default selection makes
// the whole screen self-consistent.
//
// It is a SUBSET of trackedFX rather than a separate list, and a test asserts
// that: a chooser offering a currency the table never fetches would be a control
// that can select a pair with no rates at all.
var selectablePairs = []string{"EUR", "GBP", "JPY"}

// pairLabels are the chooser's tab labels, which are the PAIRS rather than the
// bare currency codes. "/USD" is what makes a tab unambiguous at a glance: a row
// of three three-letter codes is a list a reader has to decode.
func pairLabels() []string {
	out := make([]string, len(selectablePairs))
	for i, c := range selectablePairs {
		out[i] = c + "/USD"
	}
	return out
}

// pairTabIndex is the chooser index of a currency code, and whether it is offered.
func pairTabIndex(code string) (int, bool) {
	for i, c := range selectablePairs {
		if c == code {
			return i, true
		}
	}
	return 0, false
}

// pairCodeAt is the currency a chooser index names, and whether the index is in
// range. It is the inverse of pairTabIndex because the tab row speaks in indices
// and the rest of the program speaks in codes, and translating at the boundary is
// what stops an index from being read as a code.
func pairCodeAt(i int) (string, bool) {
	if i < 0 || i >= len(selectablePairs) {
		return "", false
	}
	return selectablePairs[i], true
}

// indexOfOrZero is pairTabIndex without the boolean, for the one caller — the
// chooser's initial selection — where "not offered" has no better answer than the
// first tab and where a test asserts the default pair is in the list.
func indexOfOrZero(code string) int {
	if i, ok := pairTabIndex(code); ok {
		return i
	}
	return 0
}

// windowDays is how many calendar days of history the sparkline is given. Thirty
// is about twenty-two publications — a month of trading — which is enough to show
// a trend rather than a wobble.
const windowDays = 30

// trackedFX are the currencies whose rates this dashboard follows, in the order
// they appear in the detail table.
//
// A fixed list rather than "whatever the API returned", for two reasons: the
// table's row count is then known before the first fetch, so the layout does not
// jump when a currency is added upstream; and a currency the endpoint stops
// returning is visibly missing rather than silently gone.
var trackedFX = []string{
	"AUD", "CAD", "CHF", "CNY", "EUR", "GBP",
	"INR", "JPY", "KRW", "MXN", "NOK", "SEK",
}

// trackedCrypto are the CoinGecko ids fetched, in display order.
var trackedCrypto = []string{"bitcoin", "ethereum"}

// cryptoSymbols maps a CoinGecko id onto the ticker this dashboard shows, because
// "bitcoin" is a seven-cell label in a symbol column and "BTC" is a three-cell one.
var cryptoSymbols = map[string]string{
	"bitcoin":  "BTC",
	"ethereum": "ETH",
}

// fxLatest is Frankfurter's spot response.
//
// Rates is a map rather than a fixed struct because the ECB publishes around
// thirty currencies and the set changes; decoding into a struct would silently
// drop the ones this build does not know about.
type fxLatest struct {
	Amount float64            `json:"amount"`
	Base   string             `json:"base"`
	Date   string             `json:"date"`
	Rates  map[string]float64 `json:"rates"`
}

// fxSeries is Frankfurter's range response: a date-keyed map of currency rates.
//
// The inner maps are keyed by currency and the outer by ISO date, and Go's JSON
// decoder does not preserve object order, so the dates are sorted on use. That
// matters more than it looks: a sparkline fed an unordered map would draw a
// different shape on every run, and a golden test over one would be worthless.
type fxSeries struct {
	StartDate string                        `json:"start_date"`
	EndDate   string                        `json:"end_date"`
	Rates     map[string]map[string]float64 `json:"rates"`
}

// cryptoPrice is one entry of CoinGecko's simple/price response.
type cryptoPrice struct {
	USD    float64 `json:"usd"`
	Change float64 `json:"usd_24h_change"`
}

// cryptoBook is the whole response, keyed by coin id.
type cryptoBook map[string]cryptoPrice

// market is one fetch cycle's worth of data: plain numbers, no widget types and
// no formatting.
//
// A market is immutable once built. Every slice in it is owned by it and is never
// written again, which is what lets the widgets hold those slices BY REFERENCE —
// Sparkline.SetValues does not copy, and neither does BarChart.SetData — with no
// copy on the frame path and no data race against the next cycle.
type market struct {
	// asOf is the publication date the spot rates are dated, as "2006-01-02".
	asOf string
	// rates is the spot snapshot, keyed by currency code.
	rates map[string]float64
	// prev is the previous publication day's snapshot, keyed the same way. It is
	// what makes a day-on-day change possible at all: the ECB publishes one rate
	// a day, so "today's change" is measured against yesterday's publication and
	// not against a tick nobody has.
	prev map[string]float64
	// dates are the publication dates in the window, oldest first.
	dates []string
	// history is the primary pair's rate on each of those dates, aligned to dates.
	history []float64
	// series is EVERY tracked currency's window, not only the primary's.
	//
	// The range request already asks for all of trackedFX — one request, twelve
	// currencies — and this map is where those eleven answers were being thrown
	// away. Keeping them is what makes the pair control honest: switching the
	// primary pair has to be able to switch the window too, because a sparkline
	// titled "GBP/USD" drawn from the EUR series is a lie told in shapes rather
	// than in words.
	//
	// A currency absent from this map has no window, and the screen says so by
	// dropping the panels that need one rather than by drawing another currency's
	// shape.
	series map[string]series
	// primary is the currency history tracks, and the pair the sparkline and the
	// gauge are about. It is a field rather than a bare reference to the constant
	// so that a market built from a fixture can be about a different pair without
	// the window request and the data disagreeing about which one.
	primary string
	// crypto is the CoinGecko snapshot, keyed by coin id.
	crypto cryptoBook
	// failures records which sources did not answer this cycle. It lives on the
	// snapshot rather than only in a returned error precisely so a PARTIAL result
	// survives: crypto can be missing while FX is fine, and the screen should say
	// which rather than going blank.
	failures []string
}

// newMarket returns an empty market for primary, with every map allocated: a nil
// map read is legal in Go but a nil map written is a panic, and this type is
// written in one place and read in several.
func newMarket() *market {
	return &market{
		primary: primaryPair,
		rates:   map[string]float64{},
		prev:    map[string]float64{},
		crypto:  cryptoBook{},
		series:  map[string]series{},
	}
}

// series is one currency's window: the publication dates it has a positive rate
// on, oldest first, and the rate on each, aligned to those dates.
//
// It is a pair of slices rather than a map for the same reason m.dates and
// m.history are: a sparkline is an ORDERED thing, and a date-keyed map would
// hand the widget an order Go does not promise.
type series struct {
	dates []string
	rates []float64
}

// withPair returns a copy of m whose primary pair is code, with the window
// fields taken from that currency's own series.
//
// A copy rather than a mutation, because a market is immutable once built and
// three of them — the store's, the frame builder's and the widgets' — hold the
// same pointer at once. Sharing the maps and slices is safe for the same reason
// the original sharing was: nothing writes to a market after Fetch returns.
//
// A currency with no series yields a market with an EMPTY window rather than the
// primary's. That is the whole point: the sparkline, the gauge and the two window
// extremes then say there is no history instead of drawing another currency's
// shape under this one's name.
// PrimaryPair reports the currency this market's window and headline figures are
// about. It is the read side of withPair, and it exists so a caller can ask the
// market rather than reaching into its fields.
func (m *market) PrimaryPair() string {
	if m == nil {
		return ""
	}
	return m.primary
}

func (m *market) withPair(code string) *market {
	if m == nil {
		return nil
	}
	out := *m
	out.primary = code
	s, ok := m.series[code]
	if !ok {
		out.dates, out.history = nil, nil
		return &out
	}
	out.dates, out.history = s.dates, s.rates
	return &out
}

// primary is the currency history and the sparkline track.
func (m *market) hasHistory() bool { return len(m.history) >= 2 }

// extremes returns the primary series' low and high, and whether it has any.
func (m *market) extremes() (lo, hi float64, ok bool) {
	if !m.hasHistory() {
		return 0, 0, false
	}
	lo, hi = m.history[0], m.history[0]
	for _, v := range m.history[1:] {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi, hi > lo
}

// spot returns the primary pair's current rate and whether there is one.
func (m *market) spot() (float64, bool) {
	v, ok := m.rates[m.primary]
	if !ok || v <= 0 {
		return 0, false
	}
	return v, true
}

// changePct returns code's day-on-day change in percent, and whether both
// publication days are present.
//
// A percentage rather than an absolute difference because the panel this feeds is
// read at a glance down a column of a dozen rows, and "0.65%" is comparable
// between EUR and JPY in a way that "0.0058" is not.
func (m *market) changePct(code string) (float64, bool) {
	now, ok := m.rates[code]
	if !ok {
		return 0, false
	}
	was, ok := m.prev[code]
	if !ok || was == 0 {
		return 0, false
	}
	return (now - was) / was * 100, true
}

// anyData reports whether the snapshot carries enough to draw a screen at all.
func (m *market) anyData() bool {
	return len(m.rates) > 0 || len(m.crypto) > 0
}

// source is the one thing a dashboard cannot do for itself: turn the outside world
// into a market. It is an interface so that the offline path, the live path and
// the deliberately-failing path in the tests are three lines of wiring rather than
// three different programs.
type source interface {
	// Fetch performs one cycle. It must honour ctx's deadline and must return
	// whatever it managed to gather even when err is non-nil.
	Fetch(ctx context.Context) (*market, error)
	// label names the source, so the status line can say where the numbers came
	// from rather than only whether they arrived.
	label() string
}

// liveSource fetches from Frankfurter and CoinGecko.
//
// Its fields are injectable so that a test can drive every failure mode — a
// refused connection, a 500, a malformed body, a body that never arrives — through
// the real decode and timeout code with a stub RoundTripper and no socket at all.
type liveSource struct {
	client   *http.Client
	fxBase   string
	crypto   string
	cryptoID []string
}

func newLiveSource() *liveSource {
	return &liveSource{
		client:   &http.Client{Timeout: fetchTimeout},
		fxBase:   fxBase,
		crypto:   cryptoBase,
		cryptoID: trackedCrypto,
	}
}

func (l *liveSource) label() string { return "live: ECB via Frankfurter + CoinGecko" }

// Fetch runs one cycle.
//
// The two sources are independent: each records its own failure and the cycle
// returns whatever succeeded alongside the error. A dashboard that loses one feed
// and blanks the screen has thrown away information it already had, which is the
// opposite of what a status display is for.
func (l *liveSource) Fetch(ctx context.Context) (*market, error) {
	// One deadline for the whole cycle rather than one per request. A per-request
	// budget would let three stalled requests cost three waits, and the user's
	// experience of "the numbers stopped updating" is a single interval, not a sum
	// of them.
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	m := newMarket()
	var failures []string

	if err := l.fetchFX(ctx, m); err != nil {
		failures = append(failures, "fx "+oneLine(err))
	}
	if err := l.fetchCrypto(ctx, m); err != nil {
		failures = append(failures, "crypto "+oneLine(err))
	}
	m.failures = failures

	if len(failures) == 0 {
		return m, nil
	}
	if !m.anyData() {
		return m, errors.New(strings.Join(failures, "; "))
	}
	return m, errors.New(strings.Join(failures, "; "))
}

// fetchFX asks for the spot snapshot and the window, in that order, and fills m.
//
// The spot request comes first because its `date` is the authority on how fresh
// the screen is AND the end of the history window: a range window can end on a
// publication day the spot has already moved past. Two requests rather than one
// because Frankfurter has no endpoint returning both.
//
// A failure of the SECOND request is not a failure of the cycle. A table of spot
// rates with no changes and no sparkline is a real screen; an error page is not.
func (l *liveSource) fetchFX(ctx context.Context, m *market) error {
	var spot fxLatest
	if err := l.getJSON(ctx, l.fxBase+"/latest?from=USD", &spot); err != nil {
		return err
	}
	if len(spot.Rates) == 0 {
		return errors.New("spot response carried no rates")
	}
	m.asOf = spot.Date
	for _, code := range trackedFX {
		if v, ok := spot.Rates[code]; ok {
			m.rates[code] = v
		}
	}

	win, err := l.fetchFXWindow(ctx, spot.Date)
	if err != nil {
		m.failures = append(m.failures, "history "+oneLine(err))
		return nil
	}
	absorbSeries(m, win)
	return nil
}

// absorbSeries copies a range response into m: EVERY tracked currency's window in
// date order, and the previous publication day's full snapshot for the change
// column.
//
// Every currency rather than only the primary, because the response already
// carries all twelve and the pair control needs to be able to switch the window
// as well as the headline figures. A currency the response does not carry gets no
// entry at all, so withPair reports the absence rather than substituting one.
//
// A date whose rate is missing is dropped from THAT currency's two slices rather
// than having a zero substituted for it: a series silently shifted by one entry is
// a worse lie than a shorter series, and zero is a rate no currency has. Per
// currency rather than globally, which is what keeps GBP's window aligned with
// GBP's dates on a day the ECB published EUR but not GBP.
//
// The parameter is named in rather than series, because `series` is now this
// package's type name and a parameter shadowing it would make the two impossible
// to tell apart in the body.
func absorbSeries(m *market, in *fxSeries) {
	dates := make([]string, 0, len(in.Rates))
	for d := range in.Rates {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	for _, code := range trackedFX {
		keptDates := make([]string, 0, len(dates))
		keptRates := make([]float64, 0, len(dates))
		for _, d := range dates {
			v, ok := in.Rates[d][code]
			if !ok || v <= 0 {
				continue
			}
			keptDates = append(keptDates, d)
			keptRates = append(keptRates, v)
		}
		if len(keptDates) < 2 {
			continue
		}
		m.series[code] = series{dates: keptDates, rates: keptRates}

		// The change column needs the day before the last one, for every tracked
		// currency rather than only the primary pair. It is taken from the PRIMARY's
		// window, because a change is always measured against the previous
		// publication of the same instrument, and the primary's window is the one
		// whose end date the spot snapshot is dated to.
		if code == m.primary {
			prevDate := keptDates[len(keptDates)-2]
			for _, c := range trackedFX {
				if v, ok := in.Rates[prevDate][c]; ok {
					m.prev[c] = v
				}
			}
		}
	}

	// The primary's window is a VIEW of the map rather than a second copy of it, so
	// withPair and the map cannot disagree about what the primary's history is. The
	// two slices are shared, not copied, which is safe because a market is immutable
	// once built.
	if s, ok := m.series[m.primary]; ok {
		m.dates, m.history = s.dates, s.rates
	}
}

// fetchFXWindow fetches the primary pair's history over the window ending on the
// given publication date.
func (l *liveSource) fetchFXWindow(ctx context.Context, endDate string) (*fxSeries, error) {
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return nil, fmt.Errorf("unusable as-of date %q", endDate)
	}
	start := end.AddDate(0, 0, -windowDays).Format("2006-01-02")
	// Every tracked currency, not just the primary pair. The spot snapshot gives a
	// rate per currency but no previous day for any of them, so without this the
	// "1d" column is only derivable for the one pair the sparkline happens to track
	// and the other eleven rows read "n/a" — which is what this dashboard did the
	// first time it was run against the real API.
	url := fmt.Sprintf("%s/%s..%s?from=USD&to=%s", l.fxBase, start, endDate, strings.Join(trackedFX, ","))
	var series fxSeries
	if err := l.getJSON(ctx, url, &series); err != nil {
		return nil, err
	}
	if len(series.Rates) == 0 {
		return nil, errors.New("range response carried no dates")
	}
	return &series, nil
}

// fetchCrypto asks CoinGecko for the tracked coins with their 24-hour change.
func (l *liveSource) fetchCrypto(ctx context.Context, m *market) error {
	url := fmt.Sprintf("%s/simple/price?ids=%s&vs_currencies=usd&include_24hr_change=true",
		l.crypto, strings.Join(l.cryptoID, ","))
	var book cryptoBook
	if err := l.getJSON(ctx, url, &book); err != nil {
		return err
	}
	if len(book) == 0 {
		return errors.New("response carried no prices")
	}
	m.crypto = book
	return nil
}

// getJSON performs one GET and decodes the body into out.
//
// Every failure mode a caller has to distinguish gets its own message — transport
// failure, timeout, non-200 status, undecodable body, oversized body — because the
// status line is the only place a user learns any of it, and "fetch failed" with
// no reason is the failure this file exists to avoid.
func (l *liveSource) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("timed out after %s", fetchTimeout)
		}
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Bounded rather than trusted: an endpoint answering 200 with a body of
	// megabytes is a different thing from one answering with rates, and the
	// difference should be an error rather than an out-of-memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("reading the body: %w", err)
	}
	if len(body) > maxBodyBytes {
		return fmt.Errorf("response larger than %d bytes", maxBodyBytes)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding the body: %w", err)
	}
	return nil
}

// oneLine flattens err onto one line and clips it.
//
// The status line is a single row of cells, so a message carrying a newline would
// write a control character into a cell and shift every column after it. Errors
// from net/http routinely contain newlines — the *url.Error chain pretty-prints
// the request — so this is not hypothetical.
func oneLine(err error) string {
	if err == nil {
		return ""
	}
	s := strings.Join(strings.Fields(err.Error()), " ")
	const maxErrCells = 72
	if len(s) > maxErrCells {
		s = s[:maxErrCells]
	}
	return s
}
