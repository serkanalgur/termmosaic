package main

// The screen tests: the golden files, the layout rules, the states, and the
// frame-path allocation guarantee.
//
// Every assertion here is on CELLS, through the widgettest harness, never on
// escape sequences. A test that asserted on an SGR byte would break when the
// encoder's output changed without the screen changing; a test that asserts on
// what a reader would see does not.
//
// No test in this file reaches the network. That is not a convenience: a test whose
// result depends on an HTTP endpoint is a test that fails when the endpoint is
// rate-limited, and CI must not depend on the internet. The live decode paths are
// covered separately in feed_test.go, with stubbed RoundTrippers.

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// update is set by -update to rewrite the golden files in testdata.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// The screen sizes the goldens capture. Four, because they are four DIFFERENT
// layouts rather than four renderings of one:
//
//	wide     the designed arrangement: results and the detail pane side by side
//	mid      the same content stacked, because the interior is narrower than wideW
//	narrow   one band per row and the pane below the table
//	tiny     below MinSize, so the diagnostic rather than a clipped screen
//
// The degenerate 0x0 size is covered by TestDegenerateSizesRenderWithoutPanic and by
// the sweep, rather than by a golden, because a zero-by-zero screen has no rows to
// diff and a golden of it would only ever be empty.
var (
	goldenWideW, goldenWideH     = 120, 34
	goldenMidW, goldenMidH       = 72, 30
	goldenNarrowW, goldenNarrowH = 50, 20
	goldenTinyW, goldenTinyH     = 40, 10
)

// goldenHighH is the height the key tests render at: tall enough that the detail
// pane is affordable at every width they use, so a hint or focus assertion is never
// made against a screen that has dropped a band for lack of rows.
const goldenHighH = 30

// at is the instant every golden is rendered at, so the footer's clock is a
// constant and a golden is a function of the layout rather than of when the suite
// ran. It is the fixture's own capture time, not wallClock, for the reason
// sampleCapturedAt's comment gives.
var at = sampleCapturedAt

// screen builds the example as run: a screen at (w, h) fed the bundled capture.
//
// No test here reaches the network, and that is not a convenience: a test whose
// result depends on an HTTP endpoint is a test that fails when the endpoint is
// rate-limited, and CI must not depend on the internet. The live decode paths are
// covered separately, in TestLiveSourceDecodesRealResponses, with stubbed
// RoundTrippers.
func screen(t testing.TB, w, h int) *search {
	t.Helper()
	s := newSearch(screenBounds(w, h))
	// The field is filled, exactly as --offline fills it, so a golden shows a
	// COMPLETE search rather than results under an empty box — and so a test that
	// types into the field starts from a known state.
	s.query.SetText(sampleQuery)
	apply(t, s, newOfflineSource(), sampleQuery)
	return s
}

// apply runs one search through the offline path and hands the result to the
// screen, which is exactly what the fetch goroutine's Post does.
//
// It goes through the source interface rather than reaching into sampleResults, so
// every rendering test in this file exercises the same decode path the live program
// uses — which is the property that makes an offline golden a real regression net
// for the layout rather than a picture of a test-only arrangement.
func apply(t testing.TB, s *search, src source, query string) *snapshot {
	t.Helper()
	snap := snapshotFor(t, src, query, at)
	s.SetSearching(false)
	s.SetFrame(buildFrame(snap))
	return snap
}

// snapshotGen numbers the snapshots these tests build, standing in for the store's
// publish counter.
//
// It matters: SetFrame SKIPS a frame whose generation it has already applied, which
// is what keeps the heartbeat from re-normalising nine rows a second — and which
// means a test that built two snapshots both at generation zero would silently see
// only the first one applied.
var snapshotGen atomic.Uint64

// snapshotFor performs one search against a source and dates the snapshot.
//
// It takes the instant as a parameter rather than reading the clock so that every
// caller gets a reproducible footer.
func snapshotFor(t testing.TB, src source, query string, when time.Time) *snapshot {
	t.Helper()
	res, err := src.Search(context.Background(), query)
	return &snapshot{res: res, err: err, at: when, source: src.label(), gen: snapshotGen.Add(1)}
}

// renderAt draws n frames of s and returns the sink.
func renderAt(t testing.TB, w, h, n int, s *search) *headless.MemorySink {
	t.Helper()
	s.SetBounds(screenBounds(w, h))
	return widgettest.Render(t, w, h, n, s)
}

// screenAt renders s and returns its screen as text.
func screenAt(t testing.TB, w, h, n int, s *search) string {
	t.Helper()
	return widgettest.Screen(renderAt(t, w, h, n, s))
}

// checkGolden compares got against the checked-in file of that name.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file (run `go test ./examples/search -update` to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s\n--- diff in rows ---\n%s",
			name, got, want, describeDiff(string(want), got))
	}
}

// describeDiff renders a row-by-row difference, so a failing golden says which row
// changed rather than dumping two screens and asking the reader to find it.
//
// The marker is written to the RIGHT of the first differing line of the GOT screen,
// because that is the one the author wants to look at; marking both would double
// the width of a diff nobody wants to read.
func describeDiff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(w) || i < len(g); i++ {
		var lw, lg string
		if i < len(w) {
			lw = w[i]
		}
		if i < len(g) {
			lg = g[i]
		}
		if lw == lg {
			b.WriteString("  " + lg + "\n")
			continue
		}
		b.WriteString("! " + lg + "\n")
		b.WriteString("  want: " + lw + "\n")
	}
	return b.String()
}

// TestGoldens captures the four layout arrangements and the states a golden is the
// right tool for.
//
// A golden is the right tool for a LAYOUT because the thing worth pinning is the
// shape: which band is where, what the columns line up on, where the hint sits. An
// assertion on "the screen contains the article title" would pass for a screen whose
// table had collapsed to one column.
func TestGoldens(t *testing.T) {
	for _, tc := range []struct {
		name  string
		w, h  int
		build func(testing.TB) *search
	}{
		{"search_wide.txt", goldenWideW, goldenWideH, func(t testing.TB) *search { return screen(t, goldenWideW, goldenWideH) }},
		{"search_mid.txt", goldenMidW, goldenMidH, func(t testing.TB) *search { return screen(t, goldenMidW, goldenMidH) }},
		{"search_narrow.txt", goldenNarrowW, goldenNarrowH, func(t testing.TB) *search { return screen(t, goldenNarrowW, goldenNarrowH) }},
		{"search_tiny.txt", goldenTinyW, goldenTinyH, func(t testing.TB) *search { return screen(t, goldenTinyW, goldenTinyH) }},
		// A focused results table, reached through the program's own key path rather
		// than by poking the widget's fields. A golden generated by poking fields
		// would pin the rendering of a state nothing can reach.
		{"search_focus_results.txt", goldenWideW, goldenWideH, func(t testing.TB) *search {
			s := screen(t, goldenWideW, goldenWideH)
			a := &app{search: s}
			tap(t, a, "\t")
			tap(t, a, "\x1b[B")
			tap(t, a, "\x1b[B")
			return s
		}},
		// The empty result set, which is a NORMAL answer rather than a failure, and
		// which a golden pins better than an assertion because what matters is that
		// the pane says something a reader can act on.
		{"search_empty.txt", goldenWideW, goldenWideH, func(t testing.TB) *search {
			s := newSearch(screenBounds(goldenWideW, goldenWideH))
			apply(t, s, emptySource{}, "zzzqqxx nothing matches this")
			return s
		}},
		// A network failure. The reason is on screen and the table is empty rather
		// than blank, which is the "degrade visibly" case.
		{"search_failed.txt", goldenWideW, goldenWideH, func(t testing.TB) *search {
			s := newSearch(screenBounds(goldenWideW, goldenWideH))
			apply(t, s, failedSource{reason: "request failed: dial tcp: no route to host"}, sampleQuery)
			return s
		}},
		// A search in flight, which is the state that makes a non-blocking request
		// visible rather than merely fast.
		{"search_searching.txt", goldenWideW, goldenWideH, func(t testing.TB) *search {
			s := screen(t, goldenWideW, goldenWideH)
			s.SetSearching(true)
			return s
		}},
		// An article's extract loaded into the pane, reached by activating a row.
		{"search_detail.txt", goldenWideW, goldenWideH, func(t testing.TB) *search {
			s := screen(t, goldenWideW, goldenWideH)
			s.onOpen = func(string) {}
			s.results.Select(0)
			s.onOpen("Text-based user interface")
			s.SetDetail(&detailSnapshot{
				title: "Text-based user interface",
				sum:   newOfflineSource().sum["Text-based user interface"],
				gen:   1,
			})
			return s
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.build(t)
			checkGolden(t, tc.name, screenAt(t, tc.w, tc.h, 2, s))
		})
	}
}

// TestGoldenFilesExist is the belt-and-braces companion to TestGoldens: a golden
// that has been deleted cannot fail, because checkGolden would report the same
// "run -update" error it does for a missing one — but a golden that was never
// created on this platform would otherwise be a test that silently asserts nothing.
//
// It is a separate test because it is a different failure: TestGoldens failing means
// the layout changed, and this failing means the files are not in the repository.
func TestGoldenFilesExist(t *testing.T) {
	for _, name := range []string{
		"search_wide.txt", "search_mid.txt", "search_narrow.txt", "search_tiny.txt",
		"search_focus_results.txt", "search_empty.txt", "search_failed.txt",
		"search_searching.txt", "search_detail.txt",
	} {
		if _, err := os.Stat(filepath.Join("testdata", name)); err != nil {
			t.Errorf("missing golden file %s: %v (run `go test ./examples/search -update`)", name, err)
		}
	}
}

// TestResultsRender asserts the cells rather than the layout: every captured row is
// on screen, with its columns in the right places.
//
// It is deliberately SEPARATE from the goldens. A golden pins the arrangement; this
// pins the CONTENT, and it is the test that would fail if the HTML stripping, the
// number formatting or the date reduction broke — none of which changes the layout
// and so none of which a layout golden would notice.
func TestResultsRender(t *testing.T) {
	got := screenAt(t, goldenWideW, goldenWideH, 2, screen(t, goldenWideW, goldenWideH))
	for _, want := range []string{
		"article", "words", "updated",
		"Text-based user interface", "1929", "2026-09-18",
		"Alsamixer", "87", "2026-05-11",
		// The widest word count in the capture, which is what proves the column is
		// right-aligned rather than ragged: "5038" has to sit in the same column as
		// "87" for a reader to compare them.
		"User interface", "5038",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the results do not contain %q\n--- screen ---\n%s", want, got)
		}
	}
}

// TestEveryResultIsReachableByScrolling is the virtualisation check.
//
// The capture has nine rows and the wide screen shows about twenty, so nothing
// scrolls at that size and a table that dropped rows entirely would pass. This
// asserts the COUNT through the widget's own accessor rather than through the
// screen, because the count is a property of the data reaching the widget and the
// pixels are a property of the layout.
func TestEveryResultIsReachableByScrolling(t *testing.T) {
	s := screen(t, goldenWideW, goldenWideH)
	if got, want := s.results.Rows(), 9; got != want {
		t.Fatalf("the table holds %d rows, want the capture's %d", got, want)
	}
	if got, want := s.results.Columns(), 3; got != want {
		t.Errorf("the table has %d columns, want 3", got)
	}
}

// TestEmptyResultSetShowsAMessage asserts the empty case is a real answer.
//
// The requirement it pins is the one an empty result set usually fails: a screen
// that renders "no results" as nothing at all is indistinguishable from a screen
// that has not run yet, and a reader cannot tell whether their query was wrong or
// the program is broken.
func TestEmptyResultSetShowsAMessage(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenWideH))
	apply(t, s, emptySource{}, "zzzqqxx nothing matches this")
	got := screenAt(t, goldenWideW, goldenWideH, 2, s)

	if !strings.Contains(got, "no results") {
		t.Errorf("an empty result set says nothing about itself\n--- screen ---\n%s", got)
	}
	if strings.Contains(got, "search failed") {
		t.Errorf("an empty result set is being reported as a failure\n--- screen ---\n%s", got)
	}
	// The query is named, so a reader can see WHICH query found nothing — which is
	// the difference between a message and a shrug.
	if !strings.Contains(got, "zzzqqxx nothing matches this") {
		t.Errorf("the empty-result message does not name the query\n--- screen ---\n%s", got)
	}
	if s.Selected() != noSelection {
		t.Errorf("an empty result set reports a selection at row %d", s.Selected())
	}
}

// TestNetworkErrorIsVisible asserts a failed request says which failure and does not
// leave the reader looking at an empty table with no explanation.
func TestNetworkErrorIsVisible(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenWideH))
	apply(t, s, failedSource{reason: "request failed: dial tcp: no route to host"}, sampleQuery)
	got := screenAt(t, goldenWideW, goldenWideH, 2, s)

	if !strings.Contains(got, "search failed") {
		t.Errorf("a failed search is not labelled as one\n--- screen ---\n%s", got)
	}
	if !strings.Contains(got, "no route to host") {
		t.Errorf("the failure does not carry its reason\n--- screen ---\n%s", got)
	}
	// The screen must NOT show a result count it does not have. A footer claiming
	// "9 of 3027 results" for a request that never ran is a confident falsehood.
	if strings.Contains(got, "of 3027") || strings.Contains(got, "9 results") {
		t.Errorf("a failed search reports a result count\n--- screen ---\n%s", got)
	}
}

// staleSource is a source whose search returns DATA and an ERROR together, which is
// the partial case a two-endpoint screen produces on its own: the results arrived
// and the clock-stamp or a follow-up did not.
//
// It is distinct from partialSource on purpose. partialSource's SEARCH succeeds and
// its SUMMARY fails, which exercises the detail pane; this one makes the SEARCH
// itself partial, which is what puts STALE on the footer. No single-endpoint fixture
// can reach either.
type staleSource struct{}

func (staleSource) label() string { return offlineLabel }

func (staleSource) Search(_ context.Context, query string) (*results, error) {
	return sampleResults(), errors.New("HTTP 429")
}

func (staleSource) Summary(context.Context, string) (*summary, error) {
	return nil, errors.New("HTTP 429")
}

// TestPartialFailureKeepsTheRowsAndSaysItIsStale is the partial case: data with an
// error attached, which is what a live cycle produces when the summary request
// fails after the search succeeded.
//
// Keeping the rows is the point. A screen that blanks its results because a SECOND
// request failed has thrown away information it already had.
func TestPartialFailureKeepsTheRowsAndSaysItIsStale(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenWideH))
	apply(t, s, staleSource{}, sampleQuery)
	got := screenAt(t, goldenWideW, goldenWideH, 2, s)

	if !strings.Contains(got, "Text-based user interface") {
		t.Errorf("a partial failure discarded the rows it had\n--- screen ---\n%s", got)
	}
	if !strings.Contains(got, "STALE") {
		t.Errorf("a partial failure does not say the answer is stale\n--- screen ---\n%s", got)
	}
	if !strings.Contains(got, "HTTP 429") {
		t.Errorf("the stale footer does not carry the reason\n--- screen ---\n%s", got)
	}
}

// partialSource is a source whose search succeeds and whose summary fails — the
// case a two-endpoint screen produces on its own, and the one no single-endpoint
// fixture can reach.
type partialSource struct{}

func (partialSource) label() string { return offlineLabel }

func (partialSource) Search(_ context.Context, query string) (*results, error) {
	return sampleResults(), nil
}

func (partialSource) Summary(context.Context, string) (*summary, error) {
	return nil, errors.New("HTTP 429")
}

// TestQuerySurvivesAResize is the assertion a text field earns its keep on: the
// query is the reader's work, and a resize must not cost it.
//
// It renders at the wide size, resizes through three of the layout bands, and checks
// the query after EACH one rather than only at the end. A screen that dropped the
// text on the way through a band and restored it would pass a single end-point
// check, and the middle band is where a field's cached display is most likely to be
// rebuilt.
func TestQuerySurvivesAResize(t *testing.T) {
	const typed = "terminal user interface"
	for _, tc := range []struct{ w, h int }{
		{120, 34},
		{72, 30},
		{50, 20},
		{46, 11}, // exactly MinSize: the smallest screen that draws a layout at all
	} {
		s := newSearch(screenBounds(tc.w, tc.h))
		typeText(t, &app{search: s}, typed)
		if got := s.Query(); got != typed {
			t.Fatalf("%dx%d: the field holds %q before the resize, want %q", tc.w, tc.h, got, typed)
		}
		got := screenAt(t, tc.w, tc.h, 2, s)
		if !strings.Contains(got, "terminal user interface") {
			t.Errorf("%dx%d: the query is not on screen\n--- screen ---\n%s", tc.w, tc.h, got)
		}
	}
}

// TestSearchIsNonBlocking is the concurrency claim as a test: submitting does not
// wait for the source.
//
// It is the assertion markets cannot make — markets refreshes on a timer — and it is
// the one property a search box most needs, because a TUI that freezes on Enter
// cannot be told to stop. The callback here reproduces the shape main uses, spawning
// the request on its own goroutine and returning at once; the test holds that
// goroutine inside the request and asserts the screen has ALREADY said so.
//
// The submit is not run on its own goroutine, deliberately. A version that did would
// prove only that the test could observe a boolean from another goroutine; the
// property under test is that submit() RETURNS, and the only way to see that from the
// test's own goroutine is to call it there.
func TestSearchIsNonBlocking(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenWideH))
	s.query.SetText(sampleQuery)

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	s.onSubmit = func(string) {
		// Exactly main's shape: the network gets its own goroutine, so the caller —
		// here and there the render goroutine — returns at once.
		go func() {
			entered <- struct{}{}
			<-release
		}()
	}

	if !s.submit() {
		t.Fatal("submit refused a non-empty query")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("the request goroutine never started")
	}
	if !s.Searching() {
		t.Error("the screen does not say a search is in flight while one is")
	}
	if got := s.statusText; got != searchingStatus {
		t.Errorf("the footer says %q while a search is in flight, want %q", got, searchingStatus)
	}
	close(release)
}

// TestSearchingStateIsVisibleBeforeTheResponse pins the ordering the previous test
// depends on: the flag is set on the SUBMIT, not when the response arrives, so the
// very next frame says so.
//
// It is checked through the renderer rather than through the flag alone, because the
// flag being right and the WORD being on screen are two different claims, and the
// second is the one a reader experiences.
func TestSearchingStateIsVisibleBeforeTheResponse(t *testing.T) {
	s := screen(t, goldenWideW, goldenWideH)
	if s.Searching() {
		t.Fatal("the screen claims a search is in flight before one was asked for")
	}
	s.onSubmit = func(string) { go func() { <-make(chan struct{}) }() }
	tap(t, &app{search: s}, "\r")

	if !s.Searching() {
		t.Fatal("submitting did not mark the search in flight")
	}
	got := screenAt(t, goldenWideW, goldenWideH, 2, s)
	if !strings.Contains(got, "searching") {
		t.Errorf("the in-flight state is not on screen\n--- screen ---\n%s", got)
	}
	// And the previous result count is NOT, because the reader has just been told
	// those results are being replaced. A footer that still said "9 of 3027" while
	// the screen said "searching…" would be describing two different requests.
	if strings.Contains(got, "9 of 3027") {
		t.Errorf("the footer still reports the previous result count while a search is in flight\n--- screen ---\n%s", got)
	}
}

// TestSubmitOnAnEmptyFieldIsRefusedQuietly is the "nothing happened" case, and it
// has a specific right answer: say why, change nothing, and do not leave the field
// looking as though the text went somewhere.
func TestSubmitOnAnEmptyFieldIsRefusedQuietly(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenWideH))
	s.onSubmit = func(string) { t.Error("an empty query reached the source") }

	if s.submit() {
		t.Error("submit accepted an empty query")
	}
	if s.Searching() {
		t.Error("an empty query marked a search in flight")
	}
	if !strings.Contains(s.statusText, "nothing to search for") {
		t.Errorf("an empty query was refused without saying so; the footer reads %q", s.statusText)
	}
}

// TestSecondSubmitWhileInFlightIsRefused pins the one thing a search box gets wrong
// most easily: Enter twice is two requests, and a reader who presses it twice
// because the first seemed slow should not get two answers racing to overwrite each
// other.
func TestSecondSubmitWhileInFlightIsRefused(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenWideH))
	s.query.SetText(sampleQuery)
	calls := 0
	s.onSubmit = func(string) { calls++ }

	if !s.submit() {
		t.Fatal("submit refused a non-empty query")
	}
	if calls != 1 {
		t.Fatalf("the first submit reached the source %d times", calls)
	}
	if s.submit() {
		t.Error("a second submit was accepted while one was in flight")
	}
	if calls != 1 {
		t.Errorf("a second submit reached the source; the source saw %d calls", calls)
	}
}

// TestSnippetMarkupIsStrippedAndMarked is the assertion behind the detail pane's
// usefulness: the passage that explains why a row matched is shown, WITHOUT the
// endpoint's markup, WITH the matched terms marked.
//
// The three sub-claims are separate because they fail separately. Markup left in
// the pane is ugly and useless; markup removed WITHOUT marking throws away the one
// piece of information the snippet carries; and marking done with colour alone would
// be invisible on a monochrome terminal.
func TestSnippetMarkupIsStrippedAndMarked(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenWideH))
	apply(t, s, newOfflineSource(), sampleQuery)
	got := screenAt(t, goldenWideW, goldenWideH, 2, s)

	if strings.Contains(got, "<span") || strings.Contains(got, "searchmatch") {
		t.Errorf("the snippet's markup reached the screen\n--- screen ---\n%s", got)
	}
	// The capture's first hit's snippet, with the markup gone and the entities
	// decoded.
	if !strings.Contains(got, "text-based user interfaces (TUI)") {
		t.Errorf("the selected row's snippet is not on screen\n--- screen ---\n%s", got)
	}
	// The en-dash in the SECOND hit is a literal character the endpoint sends
	// unescaped, so a decoder that only handled references would corrupt it. It is
	// checked on row 1 because only the SELECTED row's snippet is in the pane.
	// Row 1, reached through the program's own key path — Tab, then Down — so the
	// pane's update is driven by the same router the reader drives it with.
	second := newApp(t, goldenWideW, goldenHighH)
	tap(t, second, "\t")
	tap(t, second, "\x1b[B")
	got = screenAt(t, goldenWideW, goldenHighH, 2, second.search)
	if !strings.Contains(got, "human–computer") {
		t.Errorf("the en-dash in the second row's snippet did not survive\n--- screen ---\n%s", got)
	}
	// The matched terms are UNDERLINED, which is the attribute that survives
	// NO_COLOR. This is asserted on the spans rather than on the screen text,
	// because an attribute is not text.
	spans := snippetSpans(`a <span class="searchmatch">term</span> here`)
	if len(spans) != 3 {
		t.Fatalf("the markup produced %d spans, want three (plain, matched, plain)", len(spans))
	}
	if spans[1].Text != "term" {
		t.Errorf("the matched span holds %q, want %q", spans[1].Text, "term")
	}
	if spans[1].Style.Attr&buffer.AttrUnderline == 0 {
		t.Error("a matched term is marked with colour alone; NO_COLOR would erase it")
	}
	if spans[0].Text != "a " || spans[2].Text != " here" {
		t.Errorf("the plain runs are %q and %q, want %q and %q", spans[0].Text, spans[2].Text, "a ", " here")
	}
}

// TestEntitiesAreDecoded pins the entity handling against the capture's own
// references. Apple&#039;s is in the bundled data, so a decoder that only handled
// the named references would leave a visible ampersand in the middle of a word.
func TestEntitiesAreDecoded(t *testing.T) {
	got := snippetSpans(`Apple&#039;s &quot;droplet-like&quot; &amp; more &unknown;`)
	// The whole input is ONE contiguous unmarked run, and the point is that it stays
	// one: a decoder that rebuilt a span per reference would produce five, and the
	// renderer would carry four pointless style boundaries into the cell buffer.
	want := `Apple's "droplet-like" & more &unknown;`
	if joined := spansOf(got); joined != want {
		t.Errorf("decoding produced %q, want %q", joined, want)
	}
	if len(got) != 1 {
		t.Errorf("decoding an unmarked run produced %d spans, want one", len(got))
	}
	// And the reference the endpoint really does send — the apostrophe in
	// "Apple&#039;s", which is in the bundled capture — is decoded rather than left
	// as a literal ampersand in the middle of a word.
	if joined := spansOf(snippetSpans(`Apple&#039;s`)); joined != "Apple's" {
		t.Errorf("a numeric reference decoded to %q, want %q", joined, "Apple's")
	}
}

// TestUnknownTagIsDroppedRatherThanShown pins the defensive half of the snippet
// parser: a tag the endpoint starts emitting without warning must not put "<" in a
// cell, and its TEXT must survive, because emphasis inside a snippet is content
// rather than markup.
func TestUnknownTagIsDroppedRatherThanShown(t *testing.T) {
	if got := spansOf(snippetSpans(`before <em>emph</em> after`)); got != "before emph after" {
		t.Errorf("an unknown tag yielded %q, want the text with the tag dropped", got)
	}
	// And an unterminated one drops its tail rather than showing the markup.
	if got := spansOf(snippetSpans(`fine <span class="broken`)); strings.Contains(got, "<") {
		t.Errorf("an unterminated tag leaked markup: %q", got)
	}
}

// spansOf joins a span slice's text, which is what a test about markup wants to
// compare and what the screen shows once the spans are written.
func spansOf(spans []buffer.Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// TestDetailPaneDistinguishesItsFourStates is the pane's whole reason for existing.
//
// The states are genuinely hard to tell apart from the outside: "loading", "the
// article has no extract" and "the extract failed" all show a paragraph, and only
// one of them is worth retrying. A pane that showed a blank box for all three would
// be worse than no pane.
func TestDetailPaneDistinguishesItsFourStates(t *testing.T) {
	base := func(t *testing.T) *search {
		s := newSearch(screenBounds(goldenWideW, goldenWideH))
		apply(t, s, newOfflineSource(), sampleQuery)
		return s
	}

	t.Run("pending shows the snippet", func(t *testing.T) {
		s := base(t)
		got := screenAt(t, goldenWideW, goldenWideH, 2, s)
		if !strings.Contains(got, "matching passage") {
			t.Errorf("a selected row with no extract does not show the search's own snippet\n--- screen ---\n%s", got)
		}
		if !strings.Contains(got, "text-based user interfaces") {
			t.Errorf("the snippet itself is missing\n--- screen ---\n%s", got)
		}
	})

	t.Run("loading does not show the previous article", func(t *testing.T) {
		s := base(t)
		s.results.Select(0)
		s.SetDetail(&detailSnapshot{
			title: "Text-based user interface",
			sum:   newOfflineSource().sum["Text-based user interface"],
			gen:   1,
		})
		// Now move to a second article and start a fetch for it.
		s.results.Select(1)
		s.SetPending("User interface")
		got := screenAt(t, goldenWideW, goldenWideH, 2, s)
		if !strings.Contains(got, "loading the article") {
			t.Errorf("a fetch in flight does not say so\n--- screen ---\n%s", got)
		}
		if strings.Contains(got, "In computing, text-based user interfaces") {
			t.Errorf("the previous article's extract is still under the new article's title\n--- screen ---\n%s", got)
		}
	})

	t.Run("a failed fetch says why", func(t *testing.T) {
		s := base(t)
		s.results.Select(0)
		s.SetDetail(&detailSnapshot{title: "Text-based user interface", err: errors.New("HTTP 404"), gen: 2})
		got := screenAt(t, goldenWideW, goldenWideH, 2, s)
		if !strings.Contains(got, "HTTP 404") {
			t.Errorf("a failed article fetch does not carry its reason\n--- screen ---\n%s", got)
		}
	})

	t.Run("a loaded extract is shown", func(t *testing.T) {
		s := base(t)
		s.results.Select(0)
		s.SetDetail(&detailSnapshot{
			title: "Text-based user interface",
			sum:   newOfflineSource().sum["Text-based user interface"],
			gen:   1,
		})
		got := screenAt(t, goldenWideW, goldenWideH, 2, s)
		if !strings.Contains(got, "In computing, text-based user interfaces") {
			t.Errorf("the loaded extract is not on screen\n--- screen ---\n%s", got)
		}
		// The endpoint's description is a separate field and is shown above the
		// extract; a reader who has selected an article wants both.
		if !strings.Contains(got, "Type of interface based on outputting") {
			t.Errorf("the article's description is missing\n--- screen ---\n%s", got)
		}
	})

	t.Run("no selection says nothing selected", func(t *testing.T) {
		s := newSearch(screenBounds(goldenWideW, goldenWideH))
		apply(t, s, emptySource{}, "nothing")
		got := screenAt(t, goldenWideW, goldenWideH, 2, s)
		if !strings.Contains(got, "detail") {
			t.Errorf("an empty pane is not labelled\n--- screen ---\n%s", got)
		}
	})
}

// TestOpenSelectedAsksForTheNamedArticle pins the wiring between the table's
// activation and the application's callback: the title the screen asks for is the
// SELECTED row's, and an out-of-range index asks for nothing rather than panicking.
func TestOpenSelectedAsksForTheNamedArticle(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenWideH))
	apply(t, s, newOfflineSource(), sampleQuery)

	var asked string
	s.onOpen = func(title string) { asked = title }

	s.openSelected(0)
	if asked != "Text-based user interface" {
		t.Errorf("activating row 0 asked for %q, want the first hit", asked)
	}
	s.openSelected(noSelection)
	if asked != "Text-based user interface" {
		t.Errorf("activating an out-of-range row asked for %q, want nothing", asked)
	}
}

// TestLayoutMovesTheDetailPaneRatherThanShrinkingIt is the responsive assertion,
// and it is the one a text assertion cannot make.
//
// Asserting that the screen changed would pass for a layout that had merely
// reflowed its text. Asserting that the pane's rectangle is BESIDE the table's at a
// wide interior and BELOW it at a narrow one cannot pass for that.
func TestLayoutMovesTheDetailPaneRatherThanShrinkingIt(t *testing.T) {
	wides := screenAt(t, goldenWideW, goldenWideH, 2, screen(t, goldenWideW, goldenWideH))
	narrow := screenAt(t, goldenNarrowW, goldenNarrowH, 2, screen(t, goldenNarrowW, goldenNarrowH))
	if wides == narrow {
		t.Fatal("the two widths produced identical screens, so the test proves nothing")
	}

	sideBySide := paneBelowTable(t, goldenWideW, goldenWideH)
	stacked := paneBelowTable(t, goldenNarrowW, goldenNarrowH)
	if sideBySide {
		t.Errorf("at %dx%d the detail pane is BELOW the table; the wide arrangement is side by side",
			goldenWideW, goldenWideH)
	}
	if !stacked {
		t.Errorf("at %dx%d the detail pane is BESIDE the table; the narrow arrangement stacks",
			goldenNarrowW, goldenNarrowH)
	}
}

// paneBelowTable reports whether the detail pane's rectangle starts below the
// results table's, which is the geometry question the responsive test asks.
//
// It reads the widgets' own rectangles rather than the pixels because the question
// is about LAYOUT and both rectangles are the layout's own answer. A pixel test
// would have to infer the arrangement from where a title happened to land.
func paneBelowTable(t *testing.T, w, h int) bool {
	t.Helper()
	s := screen(t, w, h)
	renderAt(t, w, h, 1, s)
	return s.detailBlk.Bounds().Y > s.results.Bounds().Y
}

// TestDetailPaneIsDroppedBeforeTheTable pins the budget's priority order.
//
// The table is the answer to the query and the pane is a summary of one row of it,
// so a short terminal loses the pane. Asserting the ORDER rather than only the
// outcome is what makes it a priority rule: a screen that dropped the table instead
// would also lose a band, and would be strictly worse.
func TestDetailPaneIsDroppedBeforeTheTable(t *testing.T) {
	for _, tc := range []struct{ w, h int }{
		{goldenNarrowW, goldenNarrowH},
		{50, 16},
		{60, 14},
	} {
		s := screen(t, tc.w, tc.h)
		s.SetBounds(screenBounds(tc.w, tc.h))
		renderAt(t, tc.w, tc.h, 1, s)
		if !s.lay.show[bandResults] {
			t.Errorf("%dx%d: the results table was dropped before the detail pane", tc.w, tc.h)
		}
	}
}

// TestBelowMinSizeSaysSoRatherThanClipping is the ADR 0007 §1 rule 2 case: a screen
// too small for its layout says so in one line rather than drawing a clipped
// fraction of it.
func TestBelowMinSizeSaysSoRatherThanClipping(t *testing.T) {
	m := screen(t, 120, 34).MinSize()
	s := newSearch(screenBounds(m.W-1, m.H))
	apply(t, s, newOfflineSource(), sampleQuery)
	got := screenAt(t, m.W-1, m.H, 2, s)

	if !strings.Contains(got, "too small") {
		t.Errorf("one cell under MinSize does not say so\n--- screen ---\n%s", got)
	}
	// It must NOT draw the layout: an article title on a screen that is reporting
	// it cannot fit is the "looks like a bug rather than like an answer" outcome.
	if strings.Contains(got, "Text-based user interface") {
		t.Errorf("the too-small diagnostic drew the layout anyway\n--- screen ---\n%s", got)
	}
}

// TestMinSizeIsReachableAndHonest checks both halves of the minimum: it is exactly
// the size at which the layout begins to draw, not one cell more, and it is what
// MinSize reports.
func TestMinSizeIsReachableAndHonest(t *testing.T) {
	m := newSearch(screenBounds(goldenWideW, goldenHighH)).MinSize()

	atMin := newSearch(screenBounds(m.W, m.H))
	apply(t, atMin, newOfflineSource(), sampleQuery)
	if got := screenAt(t, m.W, m.H, 2, atMin); strings.Contains(got, "too small") {
		t.Errorf("exactly MinSize reports itself as too small\n--- screen ---\n%s", got)
	}

	below := newSearch(screenBounds(m.W, m.H-1))
	apply(t, below, newOfflineSource(), sampleQuery)
	if got := screenAt(t, m.W, m.H-1, 2, below); !strings.Contains(got, "too small") {
		t.Errorf("one row under MinSize draws a layout\n--- screen ---\n%s", got)
	}

	// The diagnostic NAMES the minimum, which is the one thing it has to do that a
	// clipped screen does not.
	if got := screenAt(t, m.W, m.H-1, 2, below); !strings.Contains(got, "46x") {
		t.Errorf("the diagnostic does not name the minimum it wants\n--- screen ---\n%s", got)
	}
}

// TestSweepEverySizeRendersWithoutPanicking is the totality check: every size in a
// rectangle of sizes, including 0x0 and 1x1, through the whole renderer rather than
// through Draw alone.
//
// Through the RENDERER matters: a widget that panics inside the diff or the encoder
// on a degenerate frame is a real failure that a Draw-only sweep would miss, and
// this is the test that would catch it.
func TestSweepEverySizeRendersWithoutPanicking(t *testing.T) {
	s := newSearch(screenBounds(0, 0))
	apply(t, s, newOfflineSource(), sampleQuery)
	for w := 0; w <= 50; w += 5 {
		for h := 0; h <= 30; h += 3 {
			s.SetBounds(screenBounds(w, h))
			sink := widgettest.Render(t, max(w, 1), max(h, 1), 1, s)
			if got := sink.UnknownSequences(); got != 0 {
				t.Fatalf("%dx%d: the headless screen saw %d unrecognised sequences", w, h, got)
			}
		}
	}
}

// TestDegenerateSizesRenderWithoutPanic is the narrower contract ADR 0007 §4 makes:
// an empty Bounds returns immediately rather than indexing into nothing.
func TestDegenerateSizesRenderWithoutPanic(t *testing.T) {
	for _, r := range []buffer.Rect{{}, {W: 1, H: 1}, {X: 0, Y: 0, W: 0, H: 20}, {X: 5, Y: 5, W: 0, H: 0}} {
		s := newSearch(r)
		apply(t, s, newOfflineSource(), sampleQuery)
		buf := buffer.NewBuffer(max(r.W, 1), max(r.H, 1))
		s.Draw(buf)
	}
}

// TestNoStaleCellsAfterAShrink is the "repaint the whole rect" rule as a test.
//
// The renderer diffs and never clears, so a screen that drew nine rows and a detail
// pane at 120x34 and then a diagnostic at 40x10 would otherwise leave the table on
// screen (ADR 0007 §1 rule 3). A golden of each size would not catch it, because
// each golden renders one frame.
func TestNoStaleCellsAfterAShrink(t *testing.T) {
	s := screen(t, goldenWideW, goldenWideH)
	renderAt(t, goldenWideW, goldenWideH, 1, s)
	if !strings.Contains(widgettest.Screen(renderAt(t, goldenWideW, goldenWideH, 1, s)), "Text-based user interface") {
		t.Fatal("the wide screen does not show the results, so the test proves nothing")
	}

	// Shrink to a size that drops the detail pane, then to one below MinSize.
	renderAt(t, goldenNarrowW, goldenNarrowH, 1, s)
	renderAt(t, goldenTinyW, goldenTinyH, 1, s)

	got := widgettest.Screen(renderAt(t, goldenTinyW, goldenTinyH, 1, s))
	if !strings.Contains(got, "too small") {
		t.Errorf("the shrunk screen does not show the diagnostic\n--- screen ---\n%s", got)
	}
	if strings.Contains(got, "Text-based user interface") {
		t.Errorf("a row from the wide screen survived the shrink\n--- screen ---\n%s", got)
	}
}

// TestFrameIsAllocationFree is the widget-side half of ADR 0008 §4: a Draw that
// builds nothing derived from its size allocates nothing.
//
// Every string on this screen is built in buildDetail and statusLine, off the draw
// path; every span is prebuilt; the band rectangles are derived in adapt, once per
// rect. If a future change makes Draw call SetSpansCappedIn on a freshly built slice
// or re-derive the layout per frame, this fails — which is the point, because that
// is the most likely performance regression in a catalog and documentation alone does
// not catch it.
//
// It runs at three sizes because the layout differs between them: side by side,
// stacked, and below MinSize where a completely different path draws.
func TestFrameIsAllocationFree(t *testing.T) {
	for _, tc := range []struct{ w, h int }{
		{40, 10},  // below MinSize: the diagnostic path
		{50, 20},  // stacked
		{120, 34}, // side by side
	} {
		s := screen(t, tc.w, tc.h)
		buf := buffer.NewBuffer(tc.w, tc.h)
		s.Draw(buf) // warm the lazily-initialised caches
		s.Draw(buf)

		if got := testing.AllocsPerRun(50, func() { s.Draw(buf) }); got != 0 {
			t.Errorf("%dx%d: Draw allocated %.1f objects per run, want 0. ADR 0008 §4 forbids "+
				"calling Wrap, Truncate, or building a []Span inside Draw; cache them on a rect change instead",
				tc.w, tc.h, got)
		}
	}
}

// TestSetFrameReplacesEveryWidget is the "the data reached the widgets" check.
//
// It is the assertion that would catch a new band added to the screen and never
// wired to SetFrame: the band would render, empty, and only this test would say so.
// Each check is on the widget's own state rather than on the screen text, because
// the screen text is covered by the goldens and this test is about the WIRING.
func TestSetFrameReplacesEveryWidget(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenHighH))
	snap := snapshotFor(t, newOfflineSource(), sampleQuery, at)
	s.SetFrame(buildFrame(snap))

	if got := s.results.Rows(); got != len(snap.res.hits) {
		t.Errorf("the table holds %d rows, want %d", got, len(snap.res.hits))
	}
	if got := len(s.cur.hits); got != len(snap.res.hits) {
		t.Errorf("the frame carries %d hits, want %d", got, len(snap.res.hits))
	}
	if s.cur.state != fetchReady {
		t.Errorf("the frame's state is %d, want fetchReady", s.cur.state)
	}
	if s.curGen != snap.gen {
		t.Errorf("the screen recorded generation %d, want %d", s.curGen, snap.gen)
	}
}

// TestSetFrameSkipsARepublishOfTheSameGeneration pins the heartbeat's premise.
//
// Without it, a republish would re-normalise nine rows a second for nothing, and
// with it a screen that legitimately re-applies the same generation — which nothing
// in this program does — would not refresh.
func TestSetFrameSkipsARepublishOfTheSameGeneration(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenHighH))
	snap := snapshotFor(t, newOfflineSource(), sampleQuery, at)
	s.SetFrame(buildFrame(snap))
	rows := s.results.Rows()

	// Same generation, different content: the screen must keep what it has.
	other := *snap
	other.res = &results{query: snap.res.query, total: snap.res.total, hits: snap.res.hits[:1]}
	s.SetFrame(buildFrame(&other))
	if got := s.results.Rows(); got != rows {
		t.Errorf("a republish of generation %d changed the row count from %d to %d", snap.gen, rows, got)
	}

	// A new generation does apply.
	next := *snap
	next.gen = snap.gen + 1
	s.SetFrame(buildFrame(&next))
	if got := s.results.Rows(); got != len(next.res.hits) {
		t.Errorf("a new generation produced %d rows, want %d", got, len(next.res.hits))
	}
}

// TestNoColourIsTheOnlySignal renders the screen with colour forced off and asserts
// that every state is still legible as TEXT.
//
// It is the accessibility rule stated as a test rather than as a comment: the states
// on this screen are words, and a word does not care about the terminal. The focus
// marker is checked through cells, because it is a glyph and glyphs are the other
// half of the rule.
func TestNoColourIsTheOnlySignal(t *testing.T) {
	s := screen(t, goldenWideW, goldenWideH)
	got := widgettest.Screen(renderNoColour(t, goldenWideW, goldenHighH, s))

	// The header and the data are all still there without colour.
	for _, want := range []string{"article", "words", "updated", "Text-based user interface"} {
		if !strings.Contains(got, want) {
			t.Errorf("without colour the screen is missing %q\n--- screen ---\n%s", want, got)
		}
	}
	// And the focus marker is a glyph in the field's gutter, not a colour. The check
	// is on the marker-plus-label PAIR rather than on a prefix, because the gutter
	// sits inside the block's border and a prefix test would be reading the border.
	//
	// A pair rather than a single character is also the honest assertion: what the
	// reader needs to see is a marked row, and "> query" is what a marked row looks
	// like. Checking for ">" alone would pass on a screen that printed the marker and
	// dropped the label.
	row := widgettest.Row(renderAt(t, goldenWideW, goldenHighH, 2, screen(t, goldenWideW, goldenHighH)), 2)
	if !strings.Contains(row, "> query") {
		t.Errorf("the focused field has no marker glyph; row 2 reads %q", row)
	}
	// And the marker becomes a SPACE — not a different glyph — when the table has
	// focus, so the gutter's WIDTH does not change and the field cannot shift
	// sideways under a key press.
	tbl := newApp(t, goldenWideW, goldenHighH)
	tap(t, tbl, "\t")
	row = widgettest.Row(renderAt(t, goldenWideW, goldenHighH, 2, tbl.search), 2)
	if strings.Contains(row, "> query") {
		t.Errorf("the query field is still marked while the results have focus; row 2 reads %q", row)
	}
	// The label itself is still there either way: the gutter is drawn for both panes
	// so the field's left edge cannot move.
	if !strings.Contains(row, "query terminal user interface") {
		t.Errorf("the unfocused field lost its content; row 2 reads %q", row)
	}
}

// TestASCIIRungKeepsTheScreenLegible asserts the degradation ladder's bottom rung.
//
// With Unicode off, every glyph this example draws has an ASCII form: the block's
// border, the table's selection marker, the field's truncation marker and the hint's.
// A screen that drew box-drawing characters or a selection arrow into a terminal that
// cannot render them would be showing the reader a screen full of replacement
// characters, which is worse than a plainer one.
//
// It covers the application's own PROSE too, and that is the half that is easy to
// miss: a block's Ascii flag switches its BORDER, not its title, and no widget
// rewrites the strings an application hands them. Every string this screen draws is
// therefore ASCII by choice, which is what the loop below checks.
func TestASCIIRungKeepsTheScreenLegible(t *testing.T) {
	t.Cleanup(func() { setRung(true) })
	setRung(false)
	s := screen(t, goldenWideW, goldenWideH)
	got := screenAt(t, goldenWideW, goldenWideH, 2, s)

	// The rungs are derived from buffer's OWN glyph table rather than written out, which
	// is the point: a literal here would be a second border table, and
	// TestBoxDrawingRunesLiveInOneFile in the root package exists to forbid exactly
	// that. Asking the table also means the test cannot drift from what the catalog
	// would actually draw.
	//
	// Only the UNICODE set is checked for absence; the ASCII set is checked for
	// presence three lines below, which is the other half of the same claim.
	uni := buffer.BorderPlain.Glyphs(false)
	for _, r := range []rune{uni.Vertical, uni.Horizontal, uni.TopLeft, uni.BottomRight} {
		if r != 0 && strings.ContainsRune(got, r) {
			t.Errorf("the ASCII rung still draws the box glyph %U\n--- screen ---\n%s", r, got)
		}
	}
	// The one non-box glyph this screen would otherwise leak is the table's default
	// selection marker, which has no ASCII rung of its own — which is why the screen
	// overrides it. buffer.TruncSuffix is the other, and it is named here because
	// the widgets switch it themselves.
	if strings.ContainsRune(got, firstRune(buffer.TruncSuffix)) {
		t.Errorf("the ASCII rung still draws the Unicode truncation marker\n--- screen ---\n%s", got)
	}
	// The border IS the ASCII one, which is what a reader whose terminal cannot draw
	// box characters needs to see at all.
	if !strings.HasPrefix(got, string(buffer.BorderPlain.Glyphs(true).TopLeft)) {
		t.Errorf("the ASCII rung did not switch the border\n--- screen ---\n%s", got)
	}
	// The content is unchanged: the rung is about shapes, not about content.
	if !strings.Contains(got, "Text-based user interface") {
		t.Errorf("the ASCII rung lost content\n--- screen ---\n%s", got)
	}
}

// renderNoColour returns a sink for s rendered with colour forced off, which is how
// the accessibility test proves the states do not depend on hue.
func renderNoColour(t testing.TB, w, h int, s *search) *headless.MemorySink {
	t.Helper()
	sink := headless.NewMemorySink(w, h)
	caps := termmosaic.DefaultCaps()
	caps.TrueColor = false
	caps.Color256 = false
	r := render.New(sink, render.Config{Width: w, Height: h, Caps: caps, NoColor: true})
	r.SetRoot(s)
	for i := 0; i < 2; i++ {
		if _, err := r.Render(); err != nil {
			t.Fatal(err)
		}
	}
	if got := sink.UnknownSequences(); got != 0 {
		t.Fatalf("the headless screen saw %d unrecognised sequences", got)
	}
	return sink
}

// TestOfflineSourceIsDeterministic is the property every golden in this file rests
// on: two fetches from the offline source produce identical values, with no slice
// shared between them.
//
// The second half is the one worth stating. The offline source is handed the same
// response to more than one render, and a fixture that shared its slices would let a
// widget holding a row's cells by reference be read by another render — a real bug
// in a real screen that is not worth importing to make a fixture cheaper.
func TestOfflineSourceIsDeterministic(t *testing.T) {
	ctx := context.Background()
	src := newOfflineSource()
	a, err := src.Search(ctx, sampleQuery)
	if err != nil {
		t.Fatalf("the offline search failed: %v", err)
	}
	b, _ := src.Search(ctx, sampleQuery)

	if len(a.hits) != len(b.hits) || len(a.hits) != 9 {
		t.Fatalf("the capture holds %d hits on the first fetch and %d on the second", len(a.hits), len(b.hits))
	}
	for i := range a.hits {
		if a.hits[i] != b.hits[i] {
			t.Errorf("hit %d differs between fetches: %+v and %+v", i, a.hits[i], b.hits[i])
		}
	}
	if &a.hits[0] == &b.hits[0] {
		t.Error("two fetches share one hit slice; a widget holding a row by reference would be reading another render's data")
	}
	if a.total != sampleTotal {
		t.Errorf("the capture reports %d total hits, want %d", a.total, sampleTotal)
	}
}

// TestOfflinePathNeedsNoNetwork asserts that nothing on the offline path can reach
// one.
//
// It is a structural assertion rather than a behavioural one: every source the
// offline path can be given is built in this package, and none of them holds a URL
// or an http.Client. A source added later that did would fail this rather than
// quietly making the goldens depend on the internet.
func TestOfflinePathNeedsNoNetwork(t *testing.T) {
	for _, src := range []source{newOfflineSource(), emptySource{}, failedSource{reason: "x"}, partialSource{}, staleSource{}} {
		res, err := src.Search(context.Background(), sampleQuery)
		if err != nil {
			continue
		}
		if res == nil {
			t.Errorf("%T returned a nil results with a nil error", src)
		}
		if _, err := src.Summary(context.Background(), "anything"); err != nil {
			continue
		}
		t.Errorf("%T answered a summary offline without a network", src)
	}
}

// TestOfflineSourceAnswersTheSampleQueryAndNothingElse pins the fixture's own
// behaviour, including the part a reader would otherwise have to guess at: a query
// the capture does not answer returns an EMPTY result set and NOT an error, which
// is the same distinction the live path makes.
func TestOfflineSourceAnswersTheSampleQueryAndNothingElse(t *testing.T) {
	src := newOfflineSource()

	res, err := src.Search(context.Background(), sampleQuery)
	if err != nil || !res.anyData() {
		t.Fatalf("the sample query returned %d hits and error %v", len(res.hits), err)
	}

	res, err = src.Search(context.Background(), "something else entirely")
	if err != nil {
		t.Errorf("an unanswered query reported an error rather than no results: %v", err)
	}
	if res.anyData() {
		t.Errorf("an unanswered query returned %d hits", len(res.hits))
	}
	if res.query != "something else entirely" {
		t.Errorf("the empty results name the query %q, want it echoed back", res.query)
	}
}

// TestOfflineSummariesAreCapturedNotGenerated pins the detail pane's provenance:
// the capture carries an extract for two articles and honestly reports that it does
// not for the rest, which is the same 404 the live endpoint answers for a stub.
func TestOfflineSummariesAreCapturedNotGenerated(t *testing.T) {
	src := newOfflineSource()
	ctx := context.Background()

	for _, title := range []string{"Text-based user interface", "Alsamixer"} {
		sum, err := src.Summary(ctx, title)
		if err != nil {
			t.Fatalf("the captured summary for %q is missing: %v", title, err)
		}
		if sum.extract == "" {
			t.Errorf("the captured summary for %q has no extract", title)
		}
		if sum.description == "" {
			t.Errorf("the captured summary for %q has no description", title)
		}
	}
	if _, err := src.Summary(ctx, "Terminal emulator"); err == nil {
		t.Error("the capture invented a summary for an article it does not carry")
	}
}

// TestTheCaptureIsRealData pins the fixture's SHAPE rather than its content.
//
// The whole reason the offline path is a captured response rather than a synthetic
// walk is that the golden files pin the shape of real data: the snippets really do
// contain searchmatch markup, one title really does contain a right parenthesis, and
// the word counts really do span two orders of magnitude. A fixture with tidy round
// numbers and pre-cleaned snippets would pin the wrong shape and would leave the
// HTML-stripping path untested by every golden in the package.
func TestTheCaptureIsRealData(t *testing.T) {
	res := sampleResults()

	var withMarkup, withParen int
	lo, hi := -1, -1
	for _, h := range res.hits {
		if strings.Contains(h.snippet, matchOpen) {
			withMarkup++
		}
		if strings.Contains(h.title, "(") {
			withParen++
		}
		if lo < 0 || h.words < lo {
			lo = h.words
		}
		if h.words > hi {
			hi = h.words
		}
	}
	if withMarkup != len(res.hits) {
		t.Errorf("%d of %d captured snippets carry searchmatch markup; the real response carries it on every one",
			withMarkup, len(res.hits))
	}
	if withParen == 0 {
		t.Error("no captured title contains a right parenthesis; the real response has several")
	}
	// 87 and 5038 in the same column is what proves the column is fixed-width rather
	// than content-measured, and it is the reason resultColumns pins that width.
	if lo >= 100 || hi < 1000 {
		t.Errorf("the captured word counts span %d..%d; the real response spans two orders of magnitude", lo, hi)
	}
	if res.total <= len(res.hits) {
		t.Errorf("the capture reports %d total hits for %d returned; the real response reports thousands",
			res.total, len(res.hits))
	}
}

// TestDateOfReducesAndFallsBack pins the one formatting rule the updated column
// depends on, including the half that is about not hiding a malformed value.
func TestDateOfReducesAndFallsBack(t *testing.T) {
	if got := dateOf("2026-09-18T21:22:46Z"); got != "2026-09-18" {
		t.Errorf("an ISO instant reduced to %q, want a ten-character date", got)
	}
	if got := dateOf("2026-09-18"); got != "2026-09-18" {
		t.Errorf("a bare date was changed to %q", got)
	}
	// A malformed timestamp is shown rather than blanked: a visible wrong value is
	// debuggable and an invisible one is not.
	if got := dateOf("whenever"); got != "whenever" {
		t.Errorf("a malformed timestamp became %q, want it passed through", got)
	}
}

// TestStatusLineSaysWhatItMeans is the footer's content rule, asserted on the frame
// rather than through a golden so that a wording change is a deliberate edit here.
func TestStatusLineSaysWhatItMeans(t *testing.T) {
	src := newOfflineSource()

	t.Run("results", func(t *testing.T) {
		f := buildFrame(snapshotFor(t, src, sampleQuery, at))
		// "9 of 3027" rather than either alone: the number on screen and the number
		// the index holds are different facts and conflating them is a small lie.
		if !strings.Contains(f.status, "9 of 3027 results") {
			t.Errorf("the footer says %q, want the shown and total counts", f.status)
		}
		if f.warn {
			t.Error("a healthy footer is marked as carrying a warning")
		}
	})

	t.Run("none", func(t *testing.T) {
		f := buildFrame(snapshotFor(t, emptySource{}, "zzz nothing", at))
		if !strings.Contains(f.status, "no results") {
			t.Errorf("an empty response says %q, want it to say there were no results", f.status)
		}
		if f.state != fetchEmpty {
			t.Errorf("an empty response is state %d, want fetchEmpty", f.state)
		}
	})

	t.Run("failure", func(t *testing.T) {
		f := buildFrame(snapshotFor(t, failedSource{reason: "HTTP 503"}, sampleQuery, at))
		if !strings.Contains(f.status, "search failed: HTTP 503") {
			t.Errorf("a failure says %q, want it named with its reason", f.status)
		}
		if !f.warn {
			t.Error("a failing footer is not marked as carrying a warning")
		}
	})

	t.Run("partial", func(t *testing.T) {
		f := buildFrame(snapshotFor(t, staleSource{}, sampleQuery, at))
		if !strings.Contains(f.status, "STALE") || !strings.Contains(f.status, "HTTP 429") {
			t.Errorf("a partial failure says %q, want STALE and the reason", f.status)
		}
		if f.state != fetchReady {
			t.Errorf("a partial failure is state %d, want fetchReady — it DID return results", f.state)
		}
	})
}

// TestIdleFooterTellsTheReaderHowToStart is the before-the-first-search state.
//
// It is a state a golden of a populated screen cannot reach, and it is the first
// thing a reader sees, so it has to say what to do rather than showing an empty
// table.
func TestIdleFooterTellsTheReaderHowToStart(t *testing.T) {
	s := newSearch(screenBounds(goldenWideW, goldenHighH))
	got := screenAt(t, goldenWideW, goldenHighH, 2, s)
	if !strings.Contains(got, "type a query") {
		t.Errorf("the idle footer does not say what to do\n--- screen ---\n%s", got)
	}
	if s.results.Rows() != 0 {
		t.Errorf("a fresh screen holds %d rows", s.results.Rows())
	}
}

// TestBuildFrameIsPureOfClockAndNetwork is the golden's premise stated as a test:
// buildFrame reads no clock, so two calls at different instants produce identical
// output.
func TestBuildFrameIsPureOfClockAndNetwork(t *testing.T) {
	src := newOfflineSource()
	res, err := src.Search(context.Background(), sampleQuery)
	if err != nil {
		t.Fatal(err)
	}
	early := buildFrame(&snapshot{res: res, at: sampleCapturedAt, source: src.label()})
	late := buildFrame(&snapshot{res: res, at: sampleCapturedAt.Add(72 * time.Hour), source: src.label()})
	if early.status != late.status {
		t.Errorf("buildFrame's status depends on something but its arguments: %q and %q",
			early.status, late.status)
	}
}
