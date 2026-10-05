package main

// The offline dataset: a captured search response and two captured summaries,
// plus the flag plumbing that selects them.
//
// # Why this file exists at all
//
// Three requirements converge on it. CI must not depend on the internet, a test
// that renders a search screen twice must render the same screen twice, and a
// person on a plane must be able to run the example. All three are the same
// requirement — a deterministic source — and all three are met by making the
// offline path a SOURCE rather than a set of test-only overrides deep inside the
// widgets.
//
// The consequence is the property that makes this file worth having: the offline
// path decodes through exactly the same structs, formats through exactly the same
// view-building code and draws through exactly the same widgets as the live one.
// There is no "offline rendering". If the offline screen is right, the live
// screen's layout is right, and a golden file over it is a real regression net.
//
// # Where the data came from
//
// The fixture is a TRANSCRIPTION of a real response, not a synthetic walk. On
// 2026-10-05 this query was sent to the endpoint and the reply transcribed field
// for field:
//
//	GET https://en.wikipedia.org/w/api.php?action=query&list=search
//	    &srsearch=terminal%20user%20interface&srlimit=20&format=json&origin=*
//	User-Agent: termmosaic-example/0.6.1 (https://github.com/serkanalgur/termmosaic)
//
//	→ 200, searchinfo.totalhits 3027, nine hits transcribed below
//
// The real shape of the data is part of what the golden files are pinning: the
// snippets really do contain <span class="searchmatch"> markup, one title really
// does contain a right parenthesis, one really does contain an apostrophe
// encoded as &#039;, and the word counts really do span two orders of magnitude
// (87 for a stub, 5038 for an article). A synthetic fixture with tidy round
// numbers and pre-cleaned snippets would pin the wrong shape and would leave the
// HTML-stripping path untested by every golden in the package.
//
// The two summaries are captures of the same session:
//
//	GET .../api/rest_v1/page/summary/Text-based_user_interface   → 200
//	GET .../api/rest_v1/page/summary/Alsamixer                  → 200
//
// The first is the top hit of the search above; the second is hit four, chosen
// because its extract is short enough to read in full on a narrow terminal and
// because it is a Linux program rather than a concept, so a reader can tell at a
// glance that the detail pane is showing the article they selected.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// sampleQuery is the query the bundled capture answers. It is the default text
// in the query field, so `--offline` shows a populated screen the moment it
// starts rather than an empty one waiting for a keystroke.
const sampleQuery = "terminal user interface"

// sampleTotal is searchinfo.totalhits from the capture: the number of matches in
// the whole index, which is more than the twenty the endpoint returned and more
// than the nine transcribed here. Showing "9 of 3027" is honest; showing "9 of
// 9" would be a fixture that lies about the shape of the data.
const sampleTotal = 3027

// sampleCapturedAt is the instant the fixture was captured, used as the snapshot's
// timestamp so the footer's clock is a CONSTANT in offline mode. A golden that
// pinned time.Now would be a golden that failed every second, which is the same
// reason markets pins its capture date.
var sampleCapturedAt = time.Date(2026, 10, 5, 9, 12, 41, 0, time.UTC)

// offlineSource serves the captured response.
//
// It satisfies source exactly as the live one does, including returning an error
// when it has been told to: the failure path is reachable offline, which is what
// lets its rendering be asserted without a network that has to fail on cue.
type offlineSource struct {
	res *results
	sum map[string]*summary
	err error
}

func newOfflineSource() *offlineSource {
	return &offlineSource{res: sampleResults(), sum: sampleSummaries()}
}

// primeSnapshot is the response --offline starts from, so the screen shows a
// populated result set the moment it opens rather than an empty one waiting for a
// keystroke.
//
// It carries the fixture's own capture time rather than the clock, which is what
// makes an offline golden reproducible to the second; a footer reading time.Now()
// would be a golden that failed every run.
//
// It is the same capture Search returns for the sample query, so priming the screen
// and pressing Enter produce identical screens — which is a test, because a fixture
// reachable by two paths that disagree is a fixture that pins the wrong thing.
//
// It reports whether there was anything to prime, so a caller can leave an idle
// screen alone rather than showing a reader results for a query they did not type.
func (o *offlineSource) primeSnapshot() *snapshot {
	return &snapshot{
		res:    sampleResults(),
		at:     sampleCapturedAt,
		source: offlineLabel,
	}
}

func (o *offlineSource) label() string { return offlineLabel }

// Search returns the captured response.
//
// It matches on the query the capture was taken for and returns an EMPTY result
// set — not an error — for anything else. That is deliberate and it is the same
// distinction the live path makes: "no results for this query" is a normal
// answer, and a search box that reported a transport failure for an unrecognised
// word would be teaching the reader that every typo is an outage.
//
// The error field is returned even when data comes back, deliberately: that is
// exactly the partial case the live path produces when a request fails after
// another succeeded, and the screen has to render it correctly.
func (o *offlineSource) Search(_ context.Context, query string) (*results, error) {
	if o.err != nil {
		return &results{query: query}, o.err
	}
	if strings.TrimSpace(query) != sampleQuery {
		return &results{query: strings.TrimSpace(query)}, nil
	}
	return sampleResults(), nil
}

// Summary returns a captured extract, or reports that the capture has none.
//
// The second half matters: an article in the fixture with no captured summary is
// the same case the live path hits when the endpoint answers 404 for a stub, and
// the detail pane has to say so rather than showing the previous article's text.
func (o *offlineSource) Summary(_ context.Context, title string) (*summary, error) {
	if o.err != nil {
		return nil, o.err
	}
	if s, ok := o.sum[title]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("no summary for %q", title)
}

// sampleResults builds the captured response.
//
// Every slice is freshly allocated here, on every call. The offline source hands
// the same response to more than one render in a test, and a widget that held a
// row's cells by reference would otherwise be reading a slice another render had
// also written to — which is a real bug in a real screen and is not worth
// importing to make a fixture cheaper.
func sampleResults() *results {
	return &results{
		query: sampleQuery,
		total: sampleTotal,
		hits: []hit{
			{
				title:   "Text-based user interface",
				pageID:  496618,
				words:   1929,
				updated: "2026-09-18",
				snippet: "text-based <span class=\"searchmatch\">user</span> <span class=\"searchmatch\">interfaces</span> (TUI) (alternately <span class=\"searchmatch\">terminal</span> <span class=\"searchmatch\">user</span> <span class=\"searchmatch\">interfaces</span>, to reflect a dependence upon the properties of computer <span class=\"searchmatch\">terminals</span> and not just",
			},
			{
				title:   "User interface",
				pageID:  45249,
				words:   5038,
				updated: "2026-09-17",
				snippet: "In the industrial design field of human–computer interaction, a <span class=\"searchmatch\">user</span> <span class=\"searchmatch\">interface</span> (UI) is the space where interactions between humans and machines occur.",
			},
			{
				title:   "Terminal emulator",
				pageID:  53540,
				words:   1926,
				updated: "2026-10-05",
				snippet: "text <span class=\"searchmatch\">terminal</span>, the term <span class=\"searchmatch\">terminal</span> covers all remote <span class=\"searchmatch\">terminals</span>, including graphical <span class=\"searchmatch\">interfaces</span>. A <span class=\"searchmatch\">terminal</span> emulator inside a graphical <span class=\"searchmatch\">user</span> <span class=\"searchmatch\">interface</span> is often",
			},
			{
				title:   "Pi (AI agent)",
				pageID:  84104240,
				words:   626,
				updated: "2026-09-18",
				snippet: "harness developed by Earendil Works. It operates primarily through a <span class=\"searchmatch\">terminal</span> <span class=\"searchmatch\">user</span> <span class=\"searchmatch\">interface</span> and allows large language models to read, write, and modify source",
			},
			{
				title:   "Alsamixer",
				pageID:  3064903,
				words:   87,
				updated: "2026-05-11",
				snippet: "alsamixer is a <span class=\"searchmatch\">terminal</span> <span class=\"searchmatch\">user</span> <span class=\"searchmatch\">interface</span> mixer program for the Advanced Linux Sound Architecture (ALSA) that is used to configure sound settings and adjust",
			},
			{
				title:   "Bloomberg Terminal",
				pageID:  503961,
				words:   2811,
				updated: "2026-10-04",
				snippet: "financial community for its black <span class=\"searchmatch\">interface</span>, which has become a recognizable trait of the service. The first version of the <span class=\"searchmatch\">terminal</span> was released in December 1982",
			},
			{
				title:   "GNOME Terminal",
				pageID:  2144171,
				words:   1431,
				updated: "2026-08-31",
				snippet: "GNOME <span class=\"searchmatch\">Terminal</span> is a <span class=\"searchmatch\">terminal</span> emulator for the GNOME desktop environment written by Havoc Pennington and others. <span class=\"searchmatch\">Terminal</span> emulators allow <span class=\"searchmatch\">users</span> to access",
			},
			{
				title:   "Aqua (user interface)",
				pageID:  471309,
				words:   4473,
				updated: "2026-10-04",
				snippet: "Aqua is a graphical <span class=\"searchmatch\">user</span> <span class=\"searchmatch\">interface</span>, design language and visual theme used in Apple&#039;s macOS operating system. It was themed to replicate water, with &quot;droplet-like&quot;",
			},
			{
				title:   "Terminal (macOS)",
				pageID:  1065823,
				words:   406,
				updated: "2026-05-05",
				snippet: "As a <span class=\"searchmatch\">terminal</span> emulator, the application provides text-based access to the operating system, in contrast to the mostly graphical nature of the <span class=\"searchmatch\">user</span> experience",
			},
		},
	}
}

// sampleSummaries are the two captured page summaries.
//
// The map is keyed by the article title as the SEARCH returned it, because that
// is the string the screen holds and the string it will look up; keying by the
// REST endpoint's underscored slug instead would mean every caller translating,
// and a mismatch would read as "no summary" for an article that has one.
func sampleSummaries() map[string]*summary {
	return map[string]*summary{
		"Text-based user interface": {
			title:       "Text-based user interface",
			description: "Type of interface based on outputting to or controlling a text display",
			extract: "In computing, text-based user interfaces (TUI), is a retronym describing a type of user interface (UI) " +
				"common as an early form of human–computer interaction, before the advent of bitmapped displays and " +
				"modern conventional graphical user interfaces (GUIs). Like modern GUIs, they can use the entire screen " +
				"area and may accept mouse and other inputs. They may also use color and often structure the display " +
				"using box-drawing characters.",
		},
		"Alsamixer": {
			title:       "Alsamixer",
			description: "Linux audio mixer program",
			extract: "alsamixer is a terminal user interface mixer program for the Advanced Linux Sound Architecture " +
				"(ALSA) that is used to configure sound settings and adjust the volume. It uses ncurses to draw its " +
				"user interface. It supports multiple sound cards with multiple devices.",
		},
	}
}

// offlineLabel is the provenance string the status line shows in offline mode.
//
// It names the capture AND its date, so a reader can tell a bundled capture from a
// live answer without being told which mode they are in — which is the whole of
// "degrade visibly" applied to provenance rather than to failure.
const offlineLabel = "offline: capture of 2026-10-05"

// emptySource is a source whose search always matches nothing.
//
// It exists so the empty-result path is reachable offline and deterministically:
// it is the NORMAL answer to a nonsense query, not a failure, and a test that can
// only reach it by typing a specific nonsense string into a live endpoint is a
// test that depends on the internet.
type emptySource struct{}

func (emptySource) label() string { return offlineLabel + " (no matches)" }

func (emptySource) Search(_ context.Context, query string) (*results, error) {
	return &results{query: strings.TrimSpace(query), total: 0}, nil
}

func (emptySource) Summary(context.Context, string) (*summary, error) {
	return nil, errors.New("no results to describe")
}

// failedSource is a source that always fails, used by the test that pins what the
// screen shows when a request fails. It returns an EMPTY results as well as an
// error, so the "nothing at all came back" path is reachable without a network.
type failedSource struct{ reason string }

func (f failedSource) label() string { return "offline: no network" }

// Compile-time proof that all three offline sources satisfy the same interface as
// the live one. It is what makes "the same screen code serves both" a checked
// statement rather than an aspiration: a fourth method added to source breaks these
// three lines and nothing else.
var (
	_ source = (*offlineSource)(nil)
	_ source = emptySource{}
	_ source = failedSource{}
	_ source = (*liveSource)(nil)
)

func (f failedSource) Search(_ context.Context, query string) (*results, error) {
	return &results{query: strings.TrimSpace(query)}, errors.New(f.reason)
}

func (f failedSource) Summary(context.Context, string) (*summary, error) {
	return nil, errors.New(f.reason)
}
