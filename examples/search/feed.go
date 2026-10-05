package main

// The data layer: one public endpoint for results, one for the article text, and
// a plain struct at the end of both.
//
// # Why these two
//
// The brief for this example was "real data, no API key, no signup", and
// Wikipedia's search API is the shortest path to a genuinely useful result set:
//
//   - en.wikipedia.org/w/api.php?action=query&list=search returns up to
//     srlimit hits, each with a title, a word count, a last-modified timestamp
//     and a SNIPPET — the passage that matched. The snippet is the field a
//     search UI is judged on, and most free endpoints do not return one.
//   - en.wikipedia.org/api/rest_v1/page/summary/<title> returns a plain-text
//     extract for one article, which is what makes a detail pane worth having.
//
// # The User-Agent is not optional
//
// Wikimedia's policy requires a descriptive User-Agent. Whether an
// unidentifiable client is rejected outright is not something this file asserts:
// measured on 2026-10-05, a request with an empty UA and a request with no UA
// header at all both answered 200 from both endpoints. Rejection is possible
// under load and has been observed, so the header is set unconditionally rather
// than reactively.
//
// Go's http.Client sends no User-Agent by default, so this is not something the
// example gets for free. The header below names the project and its version and
// points at the repository, which is what a well-behaved client sends. It is a
// constant rather than a computed string so that a test can assert the exact
// header the client sends, and so that no code path can send an empty one.
//
// # What "degrade visibly" means here
//
// Every request is bounded by fetchTimeout at the level of the request, so a
// screen can wait at most that long and then say what went wrong. An empty result
// set is NOT an error: it is the normal answer to a nonsense query, and the
// screen distinguishes the two by construction — Search returns an empty
// *results with a nil error, and the UI renders "no results" rather than a
// failure. An error is carried as a Go error and reaches the status line as text;
// nothing in this file draws and nothing here imports a widget, so the offline
// path in sample.go exercises exactly the same decode and formatting code as the
// live one.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The endpoints, and the two knobs that decide how patient this example is.
const (
	// wikiBase is the Wikimedia REST host serving both the action API and the
	// page-summary endpoint. They are one host on purpose: two hosts would mean
	// two failure surfaces for a screen that only wants to search articles.
	wikiBase = "https://en.wikipedia.org"

	// searchPath is the action-API path carrying list=search. It is a path and
	// not a whole URL because every request below builds its query string with
	// net/url, which is what keeps a query with a space, a quote or an ampersand
	// in it from producing a malformed URL.
	searchPath = "/w/api.php"

	// summaryPrefix is the prefix of the page-summary endpoint; the article title
	// is appended, url-escaped, after it.
	summaryPrefix = "/api/rest_v1/page/summary/"

	// userAgent is the User-Agent every request in this file sends. Wikimedia's
	// policy requires one that identifies the client, and Go sends none by
	// default, so this constant is load-bearing rather than decorative: it is
	// what makes the example a well-behaved caller rather than an anonymous one.
	// A 403 is still handled below, because rejection has been observed under
	// load even from a client that identifies itself.
	//
	// It names the project, its version and its repository, which is what
	// Wikimedia's User-Agent policy asks for and what makes a rate-limited
	// client's requests traceable to a human.
	userAgent = "termmosaic-example/0.6.1 (https://github.com/serkanalgur/termmosaic)"

	// searchLimit is how many hits one search asks for. Twenty is the most the
	// result table can usefully scroll on a terminal, and asking for more would
	// make every keystroke's Enter a larger request for rows nobody reaches.
	searchLimit = 20

	// fetchTimeout bounds ONE request. It is the whole of the "a search box that
	// hangs on a dead network is worse than one that says no results" requirement:
	// at worst a request costs this long and then the screen says what happened.
	fetchTimeout = 8 * time.Second

	// maxBodyBytes bounds what a response may claim to be. Twenty hits of search
	// results is a few tens of kilobytes and one summary is under two; anything
	// an order of magnitude larger is a misconfigured endpoint or an error page,
	// and reading that unbounded is how a text field becomes a memory leak.
	maxBodyBytes = 1 << 20
)

// hit is one search result: the fields the table shows plus the snippet the
// detail pane quotes.
//
// The snippet is kept as the RAW string Wikipedia returned, markup and all,
// rather than stripped on arrival. That is deliberate: stripping belongs to the
// presentation layer (see view.go), so the offline fixture stores exactly what
// the live path stores and the golden files pin the stripping rather than
// hiding it behind the decoder.
type hit struct {
	// title is the article title as displayed, spaces and all.
	title string
	// pageID is the numeric article id, carried so a caller can correlate a hit
	// with the summary it fetched rather than trusting the title as a key.
	pageID int
	// words is the article's word count, which is the table's comparable column.
	words int
	// updated is the last-modified timestamp reduced to a calendar date, because
	// a column of full ISO instants is four times wider than the fact it carries.
	updated string
	// snippet is the matching passage, containing <span class="searchmatch">
	// around each matched term and HTML entities such as &quot; and &#039;.
	snippet string
}

// results is one search's outcome: the query that produced it, the total number
// of hits the index has, and the page of hits actually returned.
//
// Immutable once built, which is what lets the widgets hold these by reference
// while the next search is already in flight.
type results struct {
	query string
	// total is searchinfo.totalhits — the number of matches in the whole index,
	// which is almost never the number returned. Showing the two together is the
	// honest answer: "20 of 3027".
	total int
	hits  []hit
}

// anyData reports whether there is anything to draw, which is false both for a
// nil *results and for a search that legitimately matched nothing.
func (r *results) anyData() bool { return r != nil && len(r.hits) > 0 }

// summary is one article's plain-text description, from the REST endpoint.
type summary struct {
	title       string
	description string
	extract     string
}

// source is the one thing a search screen cannot do for itself: turn the outside
// world into results and into article text. It is an interface so that the live
// path, the offline path, the empty-result path and the deliberately-failing path
// are four lines of wiring rather than four different programs — and so that no
// test in this package ever opens a socket.
type source interface {
	// Search performs one search. It honours ctx's deadline and returns an EMPTY
	// results with a nil error when the query matched nothing, because that is
	// the endpoint's normal answer rather than a failure.
	Search(ctx context.Context, query string) (*results, error)
	// Summary fetches one article's extract, or reports that it has none.
	Summary(ctx context.Context, title string) (*summary, error)
	// label names the source, so the status line can say where the results came
	// from rather than only whether they arrived.
	label() string
}

// liveSource queries Wikimedia.
//
// Its fields are injectable so a test can drive every failure mode — a refused
// connection, a 403 for a rejected request, a 404 for an article with no
// summary, a 429, an undecodable body, a body that never arrives — through the
// real decode and timeout code with a stub RoundTripper and no socket at all.
type liveSource struct {
	client *http.Client
	base   string
	limit  int
}

func newLiveSource() *liveSource {
	return &liveSource{
		client: &http.Client{Timeout: fetchTimeout},
		base:   wikiBase,
		limit:  searchLimit,
	}
}

func (l *liveSource) label() string { return "live: en.wikipedia.org (Wikimedia REST + action API)" }

// searchResponse is the action API's list=search reply, decoded into the fields
// this example reads and no others.
//
// TotalHits lives under query.searchinfo and is an int: the endpoint sends a
// number there, and decoding it as a string is how a total of 3027 becomes an
// error on a search that worked.
type searchResponse struct {
	Query struct {
		SearchInfo struct {
			TotalHits int `json:"totalhits"`
		} `json:"searchinfo"`
		Search []struct {
			NS        int    `json:"ns"`
			Title     string `json:"title"`
			PageID    int    `json:"pageid"`
			Size      int    `json:"size"`
			WordCount int    `json:"wordcount"`
			Snippet   string `json:"snippet"`
			Timestamp string `json:"timestamp"`
		} `json:"search"`
	} `json:"query"`
}

// Search asks the action API for a page of hits.
//
// NS is checked rather than ignored. The endpoint's default is namespace 0
// (articles), but a search can return File: and Talk: pages, which are not
// articles, have no summary endpoint worth calling, and would fill the table
// with rows whose detail pane 404s. Filtering them here is the difference between
// a screen that degrades and one that shows a list of errors.
func (l *liveSource) Search(ctx context.Context, query string) (*results, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		// An empty query is not a request. Wikimedia would answer with an error
		// page or with every page; refusing locally is both faster and the honest
		// answer to "the user has not typed anything yet".
		return &results{}, nil
	}

	v := url.Values{}
	v.Set("action", "query")
	v.Set("list", "search")
	v.Set("srsearch", q)
	v.Set("srlimit", strconv.Itoa(l.limit))
	v.Set("format", "json")
	// origin=* is the endpoint's own cross-origin relaxation for browser callers.
	// It is harmless from a terminal and it keeps the request identical to the one
	// documented in the endpoint's own examples, so a reader can paste this URL.
	v.Set("origin", "*")

	var resp searchResponse
	if err := l.getJSON(ctx, l.base+searchPath+"?"+v.Encode(), &resp); err != nil {
		return nil, err
	}

	out := &results{query: q, total: resp.Query.SearchInfo.TotalHits}
	for _, s := range resp.Query.Search {
		if s.NS != 0 {
			continue
		}
		out.hits = append(out.hits, hit{
			title:   s.Title,
			pageID:  s.PageID,
			words:   s.WordCount,
			updated: dateOf(s.Timestamp),
			snippet: s.Snippet,
		})
	}
	// A results value with no slice allocated is fine: every reader below asks
	// len(hits) first, and a nil slice is the honest representation of "no rows".
	return out, nil
}

// summaryResponse is the REST page-summary reply, decoded into the three fields
// the detail pane shows.
//
// The endpoint also returns a thumbnail URL, a canonical title and a revision
// timestamp. None of them is read: a thumbnail is an image a terminal cannot
// draw, and the rest are facts about the article rather than about the search.
type summaryResponse struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Extract     string `json:"extract"`
	Type        string `json:"type"`
}

// Summary fetches one article's extract.
//
// A 404 is translated rather than reported raw, because the endpoint answers 404
// for a perfectly legitimate case — an article that exists but has no summary
// stub, which is common for stubs and for very recent pages — and a reader who
// searched for it deserves "no summary for this article" rather than "HTTP 404".
func (l *liveSource) Summary(ctx context.Context, title string) (*summary, error) {
	t := strings.TrimSpace(title)
	if t == "" {
		return nil, errors.New("no article selected")
	}
	// The endpoint's path form uses underscores for spaces and takes the rest
	// percent-encoded. PathEscape leaves a space alone, so the substitution is
	// explicit rather than relying on the escape to do both jobs.
	slug := url.PathEscape(strings.ReplaceAll(t, " ", "_"))

	var resp summaryResponse
	if err := l.getJSON(ctx, l.base+summaryPrefix+slug, &resp); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, fmt.Errorf("no summary for %q", t)
		}
		return nil, err
	}
	if resp.Extract == "" {
		return nil, fmt.Errorf("no summary for %q", t)
	}
	return &summary{title: resp.Title, description: resp.Description, extract: resp.Extract}, nil
}

// errNotFound is how getJSON reports a 404 to the caller above it, so Summary
// can tell "this article has no summary" from "the network is down".
var errNotFound = errors.New("not found")

// getJSON performs one GET and decodes the body into out.
//
// Every failure mode a caller has to distinguish gets its own message —
// transport failure, timeout, non-200 status, undecodable body, oversized body
// — because the status line is the only place a user learns any of it, and
// "search failed" with no reason is the failure this file exists to avoid.
func (l *liveSource) getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	// Set, not defaulted: Go sends no User-Agent of its own, and this endpoint
	// sends one. See the file comment.
	req.Header.Set("User-Agent", userAgent)

	resp, err := l.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("timed out after %s", fetchTimeout)
		}
		return fmt.Errorf("request failed: %w", err)
	}
	// A read-side close error on an HTTP response body means the connection was
	// not cleanly drained, which net/http already surfaces through the read below.
	// Reporting it again here would turn a successful fetch into a spurious error.
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return errNotFound
	case http.StatusForbidden:
		// The one status whose cause is worth naming, because it is the one a
		// reader can act on: Wikimedia refuses an empty or unacceptable UA.
		return errors.New("HTTP 403 from Wikimedia: the User-Agent was rejected")
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Bounded rather than trusted: an endpoint answering 200 with megabytes is a
	// different thing from one answering with twenty hits.
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

// dateOf reduces an ISO-8601 instant to its calendar date, and returns the input
// unchanged when it is not one.
//
// The table's column is ten cells wide, which is a date and not an instant: a
// reader comparing two articles wants to know whether one was revised after the
// other, and twenty characters of "T09:06:11Z" per row would be a column of
// noise wrapped around a fact. Falling back to the raw string means a malformed
// timestamp is VISIBLE rather than silently blank.
func dateOf(ts string) string {
	if len(ts) >= len("2006-01-02") && ts[4] == '-' && ts[7] == '-' {
		return ts[:len("2006-01-02")]
	}
	return ts
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
		s = truncateAtRuneStart(s, maxErrCells)
	}
	return s
}

// truncateAtRuneStart clips s to at most maxBytes, never splitting a multi-byte
// rune.
//
// A byte index is not a rune index: the error strings reaching oneLine are built
// from API responses, so a single accented or non-Latin character would otherwise
// be cut in half and leave invalid UTF-8 for whatever consumes the string. Backing
// up to the last rune start at or before maxBytes drops at most three bytes and
// costs no allocation, which is all a display string needs — this is a boundary
// guarantee, not a cell-accurate measurement, and the status line truncates again
// on the way to the cell buffer if that distinction matters.
func truncateAtRuneStart(s string, maxBytes int) string {
	if maxBytes >= len(s) {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// wallClock is the real clock, named so a test can reason about the two paths
// that use time without this file reading the clock directly.
func wallClock() time.Time { return time.Now() }
