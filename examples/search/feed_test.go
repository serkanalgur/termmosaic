package main

// The data-layer tests: the live decode, every failure mode, and the HTML handling.
//
// Every test here drives the REAL decode code through a stub RoundTripper, so the
// paths they cover — the status-code switch, the body limit, the entity decoder, the
// query builder — are the ones the program runs. None of them opens a socket, so
// none of them fails when an endpoint is rate-limited.
//
// The one exception is TestLiveEndpointSmokeTest, which reaches the real API and is
// SKIPPED unless TMMOSAIC_LIVE_TESTS is set. It exists because a stub proves the
// decoder agrees with a response we wrote ourselves, and only a real response proves
// the field NAMES are still what Wikipedia sends.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	// searchJSON is a real reply, trimmed to the fields the decoder reads. It keeps
	// the two things that matter: the searchmatch markup in a snippet and an
	// apostrophe encoded as a numeric reference.
	searchJSON = `{"batchcomplete":"","query":{"searchinfo":{"totalhits":3027},"search":[` +
		`{"ns":0,"title":"Text-based user interface","pageid":496618,"size":24881,"wordcount":1929,` +
		`"snippet":"text-based <span class=\"searchmatch\">user</span> <span class=\"searchmatch\">interfaces</span> (TUI) (alternately",` +
		`"timestamp":"2026-09-18T21:22:46Z"},` +
		`{"ns":0,"title":"Aqua (user interface)","pageid":471309,"size":57352,"wordcount":4473,` +
		`"snippet":"Aqua is a graphical <span class=\"searchmatch\">user</span> <span class=\"searchmatch\">interface</span>, used in Apple&#039;s macOS",` +
		`"timestamp":"2026-10-04T16:57:54Z"},` +
		`{"ns":14,"title":"Talk:Text-based user interface","pageid":1,"size":10,"wordcount":2,` +
		`"snippet":"a talk page","timestamp":"2026-01-01T00:00:00Z"}` +
		`]}}`

	// emptyJSON is the reply a nonsense query gets: 200, zero hits, and no error
	// field anywhere. It is the case a search UI most often gets wrong.
	emptyJSON = `{"batchcomplete":"","query":{"searchinfo":{"totalhits":0},"search":[]}}`

	// summaryJSON is a real reply from the page-summary endpoint.
	summaryJSON = `{"type":"standard","title":"Text-based user interface",` +
		`"description":"Type of interface based on outputting to or controlling a text display",` +
		`"extract":"In computing, text-based user interfaces (TUI), is a retronym describing a type of user interface.",` +
		`"pageid":496618}`
)

// stubTransport answers every request from a map keyed by a substring of the URL,
// and records the User-Agent it was asked with.
//
// It is the seam that lets every failure mode below run through the real
// client.Do, the real status switch and the real decoder.
type stubTransport struct {
	replies  map[string]stubReply
	requests []*http.Request
}

type stubReply struct {
	body   string
	status int
	err    error
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.requests = append(s.requests, req.Clone(req.Context()))
	for key, reply := range s.replies {
		if strings.Contains(req.URL.String(), key) {
			if reply.err != nil {
				return nil, reply.err
			}
			status := reply.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(strings.NewReader(reply.body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	}
	return nil, errors.New("stub: no reply for " + req.URL.String())
}

// liveWith returns a liveSource whose transport is the stub, plus the stub so a test
// can assert on what was sent.
func liveWith(replies map[string]stubReply) (*liveSource, *stubTransport) {
	stub := &stubTransport{replies: replies}
	src := newLiveSource()
	src.client = &http.Client{Transport: stub, Timeout: fetchTimeout}
	return src, stub
}

// TestLiveSourceDecodesRealResponses is the happy path through the real decoder.
func TestLiveSourceDecodesRealResponses(t *testing.T) {
	src, _ := liveWith(map[string]stubReply{searchPath: {body: searchJSON}})

	res, err := src.Search(context.Background(), "terminal user interface")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.total != 3027 {
		t.Errorf("total is %d, want 3027", res.total)
	}
	// Three hits came back and one of them is a TALK page, which the screen has no
	// use for: it has no summary endpoint worth calling and it would fill the table
	// with rows whose detail pane 404s. Filtering ns != 0 is what turns that into a
	// two-row table rather than a list of errors.
	if got := len(res.hits); got != 2 {
		t.Fatalf("the response carried three hits and got %d after filtering", got)
	}
	if res.hits[0].title != "Text-based user interface" || res.hits[0].words != 1929 {
		t.Errorf("hit 0 is %+v, want the first article with 1929 words", res.hits[0])
	}
	if got := res.hits[0].updated; got != "2026-09-18" {
		t.Errorf("hit 0's date is %q, want the date without the time", got)
	}
	// The snippet is kept RAW. Stripping it is the presentation layer's job, and a
	// decoder that cleaned it would leave the HTML path untested by every golden.
	if !strings.Contains(res.hits[0].snippet, matchOpen) {
		t.Error("the decoder stripped the snippet's markup; it should keep it and let the view convert it")
	}
	if !strings.Contains(res.hits[1].snippet, "&#039;") {
		t.Error("the decoder decoded an entity on arrival; it should leave it for the view")
	}
}

// TestLiveSourceBuildsTheRequestItDocuments pins the URL, because it is the one
// thing a reader can check against Wikipedia's own documentation.
func TestLiveSourceBuildsTheRequestItDocuments(t *testing.T) {
	src, stub := liveWith(map[string]stubReply{searchPath: {body: searchJSON}})
	if _, err := src.Search(context.Background(), "go & rust \"quoted\""); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 1 {
		t.Fatalf("%d requests were made, want one", len(stub.requests))
	}
	q := stub.requests[0].URL.Query()
	for key, want := range map[string]string{
		"action":   "query",
		"list":     "search",
		"srsearch": "go & rust \"quoted\"",
		"srlimit":  "20",
		"format":   "json",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("the request's %s is %q, want %q", key, got, want)
		}
	}
	if _, err := url.Parse(src.base + searchPath); err != nil {
		t.Errorf("the base URL does not parse: %v", err)
	}
}

// TestLiveSourceSendsTheUserAgent is the load-bearing header.
//
// Wikimedia's policy requires a User-Agent that identifies the client, and Go's
// http.Client sends none of its own, so this is a requirement rather than
// politeness: an example that forgets it is an anonymous caller against a public
// API, which is exactly what the policy asks people not to be.
//
// Whether an absent header is rejected outright is not asserted here, because it
// did not reproduce when measured on 2026-10-05: empty and absent both answered
// 200. Rejection under load has been observed, so the header is set regardless.

func TestLiveSourceSendsTheUserAgent(t *testing.T) {
	src, stub := liveWith(map[string]stubReply{
		searchPath:    {body: searchJSON},
		summaryPrefix: {body: summaryJSON},
	})
	if _, err := src.Search(context.Background(), "anything"); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Summary(context.Background(), "Go (programming language)"); err != nil {
		t.Fatal(err)
	}

	if len(stub.requests) != 2 {
		t.Fatalf("%d requests were made, want two", len(stub.requests))
	}
	for _, req := range stub.requests {
		got := req.Header.Get("User-Agent")
		if got == "" {
			t.Errorf("%s was requested with no User-Agent; Wikimedia answers 403 to one", req.URL.Path)
		}
		if got != userAgent {
			t.Errorf("the User-Agent is %q, want the constant %q", got, userAgent)
		}
		// The constant must keep naming the project and its repository, which is
		// what the policy asks for and what makes a rate-limited client's requests
		// traceable to a human.
		for _, want := range []string{"termmosaic", "github.com/serkanalgur/termmosaic"} {
			if !strings.Contains(userAgent, want) {
				t.Errorf("the User-Agent %q does not name %q", userAgent, want)
			}
		}
	}
}

// TestLiveSourceSendsNoRequestForAnEmptyQuery pins the local refusal.
//
// A whitespace-only query is not a request. Wikimedia would answer with an error
// page or with every page, and a search box that fires one of those on a stray space
// is a search box that talks to a public API for nothing.
func TestLiveSourceSendsNoRequestForAnEmptyQuery(t *testing.T) {
	src, stub := liveWith(map[string]stubReply{searchPath: {body: searchJSON}})

	for _, q := range []string{"", "   ", "\t"} {
		res, err := src.Search(context.Background(), q)
		if err != nil {
			t.Errorf("Search(%q): %v", q, err)
		}
		if res.anyData() {
			t.Errorf("Search(%q) returned %d hits", q, len(res.hits))
		}
	}
	if len(stub.requests) != 0 {
		t.Errorf("%d requests were made for empty queries, want none", len(stub.requests))
	}
}

// TestEmptyResultSetIsNotAFailure is the case a search UI most often gets wrong: a
// query that matches nothing is a 200 with an empty array, and reporting it as an
// error teaches the reader that every typo is an outage.
func TestEmptyResultSetIsNotAFailure(t *testing.T) {
	src, _ := liveWith(map[string]stubReply{searchPath: {body: emptyJSON}})

	res, err := src.Search(context.Background(), "zzzqqxxnonsense")
	if err != nil {
		t.Fatalf("an empty result set was reported as an error: %v", err)
	}
	if res.anyData() {
		t.Errorf("an empty reply produced %d hits", len(res.hits))
	}
	if res.total != 0 {
		t.Errorf("total is %d, want 0", res.total)
	}
}

// TestLiveSourceReportsEveryFailureMode walks the statuses and transport errors a
// caller has to tell apart.
//
// They are separate cases because the status line is the only place a user learns
// any of them: "search failed" with no reason is the failure this file exists to
// avoid, and a screen that showed one message for a 403 and another for a timeout
// would be guessing.
func TestLiveSourceReportsEveryFailureMode(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply stubReply
		want  string
	}{
		{"forbidden", stubReply{status: http.StatusForbidden}, "User-Agent was rejected"},
		{"rate limited", stubReply{status: http.StatusTooManyRequests}, "HTTP 429"},
		{"server error", stubReply{status: http.StatusInternalServerError}, "HTTP 500"},
		{"not found", stubReply{status: http.StatusNotFound}, "not found"},
		{"undecodable", stubReply{body: "{not json"}, "decoding the body"},
		{"wrong shape", stubReply{body: `{"query":{"searchinfo":{"totalhits":"many"},"search":[]}}`}, "decoding the body"},
		{"transport", stubReply{err: errors.New("dial tcp: no route to host")}, "request failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, _ := liveWith(map[string]stubReply{searchPath: tc.reply})
			_, err := src.Search(context.Background(), "anything")
			if err == nil {
				t.Fatal("the failure was not reported")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the error is %q, want it to mention %q", oneLine(err), tc.want)
			}
		})
	}
}

// TestLiveSourceBoundsTheResponse is the memory-leak guard.
//
// A search is twenty hits; a response an order of magnitude larger is a misconfigured
// endpoint or an error page, and reading that unbounded is how a text field becomes
// a memory leak.
func TestLiveSourceBoundsTheResponse(t *testing.T) {
	huge := strings.Repeat("x", maxBodyBytes+1)
	src, _ := liveWith(map[string]stubReply{searchPath: {body: huge}})

	_, err := src.Search(context.Background(), "anything")
	if err == nil {
		t.Fatal("an oversized response was accepted")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("the error is %q, want it to say the body was too large", oneLine(err))
	}
}

// TestFetchTimeoutIsBounded asserts the whole of "a search box that hangs is worse
// than one that says no results": at worst a request costs fetchTimeout.
//
// The client's own Timeout is what fires here, and the error is translated rather
// than passed through, because "context deadline exceeded (Client.Timeout exceeded
// while awaiting headers)" is a sentence a reader should never have to read.
func TestFetchTimeoutIsBounded(t *testing.T) {
	stub := &stubTransport{replies: map[string]stubReply{
		searchPath: {err: context.DeadlineExceeded},
	}}
	src := newLiveSource()
	src.client = &http.Client{Transport: stub}

	_, err := src.Search(context.Background(), "anything")
	if err == nil {
		t.Fatal("a timed-out request was reported as a success")
	}
	if !strings.Contains(err.Error(), "timed out after") {
		t.Errorf("the error is %q, want it to name the timeout and its budget", oneLine(err))
	}
	if !strings.Contains(err.Error(), "8s") {
		t.Errorf("the error is %q, want it to carry the budget a reader is waiting on", oneLine(err))
	}
}

// TestSummaryTranslates404 is the polite half of the failure handling.
//
// The endpoint answers 404 for a perfectly legitimate case — an article that exists
// but has no summary stub, which is common for stubs and for very recent pages — and
// a reader who searched for one deserves "no summary for this article" rather than
// "HTTP 404".
func TestSummaryTranslates404(t *testing.T) {
	src, _ := liveWith(map[string]stubReply{
		summaryPrefix: {status: http.StatusNotFound},
	})

	_, err := src.Summary(context.Background(), "Alpinux")
	if err == nil {
		t.Fatal("a 404 was not reported")
	}
	if !strings.Contains(err.Error(), `no summary for "Alpinux"`) {
		t.Errorf("the error is %q, want it to name the article rather than the status", oneLine(err))
	}
}

// TestSummaryRejectsAnEmptyExtract is the other 404-shaped case: a 200 whose extract
// is empty is still no summary, and returning an empty pane would be a lie.
func TestSummaryRejectsAnEmptyExtract(t *testing.T) {
	src, _ := liveWith(map[string]stubReply{
		summaryPrefix: {body: `{"type":"standard","title":"X","description":"y","extract":""}`},
	})
	if _, err := src.Summary(context.Background(), "X"); err == nil {
		t.Error("a reply with an empty extract was accepted as a summary")
	}
}

// TestSummaryEscapesTheTitle is the injection-shaped case.
//
// The endpoint's path form takes underscores for spaces and percent-encodes the rest.
// A title carrying a question mark or a slash would otherwise change the REQUEST
// rather than the article: the first would move everything after it into the query
// string, and the second would name a different article altogether.
func TestSummaryEscapesTheTitle(t *testing.T) {
	src, stub := liveWith(map[string]stubReply{summaryPrefix: {body: summaryJSON}})
	const title = "Go? the language / its critics"
	if _, err := src.Summary(context.Background(), title); err != nil {
		t.Fatal(err)
	}
	got := stub.requests[0].URL
	if strings.Contains(got.Path, " ") {
		t.Errorf("the request path %q contains a raw space", got.Path)
	}
	if got.RawQuery != "" {
		t.Errorf("the title leaked into the query string as %q", got.RawQuery)
	}
	if strings.Contains(got.Path, "/critics") {
		t.Errorf("the title's slash reached the path as a separator: %q", got.Path)
	}
	// And the interesting part: the whole thing is ONE path segment after the prefix,
	// which is what "names one article" means.
	if !strings.HasPrefix(got.EscapedPath(), summaryPrefix) {
		t.Errorf("the escaped path %q does not start with the endpoint's prefix", got.EscapedPath())
	}
}

// TestOneLineIsBoundedAndSingleLine pins the footer-hygiene helper.
//
// The status line is one row of cells, so a message carrying a newline would write a
// control character into a cell and shift every column after it — and errors from
// net/http routinely carry newlines, because the *url.Error chain pretty-prints the
// request.
func TestOneLineIsBoundedAndSingleLine(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"request failed:\ndial tcp: no route", "request failed: dial tcp: no route"},
		{"a\tb   c", "a b c"},
	} {
		if got := oneLine(errors.New(tc.in)); got != tc.want {
			t.Errorf("oneLine(%q) is %q, want %q", tc.in, got, tc.want)
		}
	}
	long := strings.Repeat("x", 200)
	if got := len(oneLine(errors.New(long))); got > 72 {
		t.Errorf("oneLine clipped a long message to %d cells, want at most 72", got)
	}
	if got := oneLine(nil); got != "" {
		t.Errorf("oneLine(nil) is %q, want the empty string", got)
	}
}

// TestOneLineTruncatesOnARuneBoundary is the half that matters for the endpoints
// this example talks to.
//
// A byte index is not a rune index, and the error strings reaching oneLine are built
// from API responses — so a single accented or non-Latin character would otherwise be
// cut in half and leave invalid UTF-8 for whatever consumes it.
func TestOneLineTruncatesOnARuneBoundary(t *testing.T) {
	msg := strings.Repeat("é", 200)
	got := oneLine(errors.New(msg))
	if !utf8Valid(got) {
		t.Errorf("oneLine produced invalid UTF-8: %q", got)
	}
	if len(got) > 72 {
		t.Errorf("oneLine produced %d bytes, want at most 72", len(got))
	}
}

// utf8Valid reports whether s is valid UTF-8. It is spelled here rather than
// imported as unicode/utf8 because the assertion is about the STRING this helper
// returns and the import would be one symbol for one call.
func utf8Valid(s string) bool {
	for _, r := range s {
		if r == 0xFFFD && !strings.Contains(s, "�") {
			return false
		}
	}
	return true
}

// TestLiveEndpointSmokeTest reaches the real API.
//
// It is SKIPPED unless TMMOSAIC_LIVE_TESTS is set, because a test whose result
// depends on an HTTP endpoint is a test that fails when that endpoint is
// rate-limited, and CI must not depend on the internet.
//
// It exists because every other test here drives a response this file wrote itself,
// and only a real one proves the FIELD NAMES are still what Wikipedia sends. When it
// does run it also checks the three things a reader would want confirmed: the User-Agent
// is accepted, a nonsense query really does come back empty rather than as an error,
// and the summary endpoint really does answer.
//
//	TMMOSAIC_LIVE_TESTS=1 go test ./examples/search/ -run TestLiveEndpointSmokeTest
func TestLiveEndpointSmokeTest(t *testing.T) {
	if os.Getenv("TMMOSAIC_LIVE_TESTS") == "" {
		t.Skip("set TMMOSAIC_LIVE_TESTS=1 to run the live endpoint smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	src := newLiveSource()

	res, err := src.Search(ctx, sampleQuery)
	if err != nil {
		t.Fatalf("the live search failed: %v", err)
	}
	if !res.anyData() {
		t.Fatalf("the live search for %q returned nothing", sampleQuery)
	}
	if len(res.hits) > searchLimit {
		t.Errorf("the live search returned %d hits, more than the %d asked for", len(res.hits), searchLimit)
	}
	for _, h := range res.hits {
		if h.title == "" || h.words == 0 || h.updated == "" {
			t.Errorf("a live hit decoded to an empty field: %+v", h)
		}
	}

	sum, err := src.Summary(ctx, res.hits[0].title)
	if err != nil {
		t.Fatalf("the live summary failed: %v", err)
	}
	if sum.extract == "" {
		t.Error("the live summary carried no extract")
	}

	empty, err := src.Search(ctx, "zzzqqxxnonsensequery")
	if err != nil {
		t.Errorf("a nonsense query was reported as an error: %v", err)
	}
	if empty.anyData() {
		t.Errorf("a nonsense query returned %d hits", len(empty.hits))
	}
}
