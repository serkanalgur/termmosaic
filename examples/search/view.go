package main

// The frame: a search response turned into everything the draw path will need,
// already formatted.
//
// # Why formatting happens here and not in Draw
//
// Every string on this screen is built HERE, off the draw path, and stored as a
// plain string or a prebuilt []buffer.Span. Draw then does nothing but read them
// and write cells.
//
// That is what makes the frame-path allocation test a real assertion rather than
// a hope: the only thing between the frame and the screen is buffer.SetSpans and
// the widgets' own cached truncation. It is also why the snippet's HTML is
// stripped HERE and not in the widget tree — the stripping allocates, and there
// is no reason for it to allocate once per frame.
//
// # The snippet is markup, and it is treated as markup
//
// The action API wraps every matched term in <span class="searchmatch"> and
// encodes &quot;, &#039; and &amp; as entities. Rendering that literally would put
// "<span class=" in the detail pane, which is worse than useless: the reader is
// looking at the passage that explains why the row matched, and that passage is
// the one piece of the response no other field duplicates.
//
// So the markup is CONVERTED rather than merely removed. A matched term becomes a
// run in stMatch, which is underlined as well as coloured, so "this is why it
// matched" survives NO_COLOR and a monochrome terminal. Everything else becomes
// plain text, and any other tag — of which the endpoint currently emits none —
// is dropped rather than shown.
//
// # Accessibility is decided here too
//
// Every state this screen reports is a WORD, not a colour: "searching…", "no
// results", "search failed: …". There is no state a reader has to infer from a
// hue, which is what makes the NO_COLOR golden differ from the colour one in
// exactly one place — the matched terms' colour — and in no place at all in what
// the screen SAYS.

import (
	"strconv"
	"strings"
	"time"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/widgets/data"
)

// The markup the action API wraps a matched term in, spelled once.
const (
	matchOpen  = `<span class="searchmatch">`
	matchClose = `</span>`
)

// fetchState is what the screen knows about the request behind the results.
//
// It is a value rather than a set of booleans because the states are exclusive
// and every one of them has to be RENDERED differently: a screen that showed
// "searching…" and a stale result count at the same time would be describing two
// requests, and only one of them happened.
type fetchState uint8

const (
	// fetchIdle is before the first search: no query has been run yet, so there is
	// nothing to say about the results beyond how to run one.
	fetchIdle fetchState = iota
	// fetchReady is a response that carried hits.
	fetchReady
	// fetchEmpty is a response that carried none. It is NOT a failure and the UI
	// must not present it as one: "no results for xyzzy" is an answer.
	fetchEmpty
	// fetchFailed is a request that did not answer. It carries the reason.
	fetchFailed
)

// detailState is the same distinction for the article extract, and it exists for
// one reason.
//
// A detail pane has one more state than a search does, and it is the interesting
// one: a request in flight for an article that has NOT been loaded yet looks
// exactly like a request in flight for the one already on screen, and the
// difference matters because re-showing the previous article's text under a new
// article's title is a lie told in two panels at once.
type detailState uint8

const (
	// detailNone is "no article is selected", which is different from every other
	// state here and says so rather than showing an empty pane.
	detailNone detailState = iota
	// detailLoading is a request in flight for the SELECTED article.
	detailLoading
	// detailReady is a loaded extract.
	detailReady
	// detailPending is the selected article with no extract loaded and none in
	// flight: the pane shows the snippet the search already gave us.
	detailPending
	// detailFailed is a request that did not answer.
	detailFailed
)

// snapshot is one search's outcome: the data, the error, when it arrived and
// where it came from. It is immutable once stored, which is what lets the store
// hand the same pointer to a reader that is building a frame while the fetcher is
// already building the next one.
type snapshot struct {
	res    *results
	err    error
	at     time.Time
	source string
	gen    uint64
}

// detailSnapshot is one article fetch's outcome, held separately because it has
// its own lifecycle: it is requested by MOVING THE SELECTION and pressing Enter,
// and it outlives several result sets.
type detailSnapshot struct {
	title string
	sum   *summary
	err   error
	gen   uint64
}

// frame is everything the draw path reads. Built once per data change; the status
// line alone is rebuilt on the heartbeat.
type frame struct {
	// gen is the store generation this frame was built from, so the screen can
	// tell a republish of the frame it already applied from a new one.
	gen uint64

	state fetchState
	query string

	// rows is the table's content and hits the same response, kept in the same
	// order so that a selection index means the same thing in both. Rows are
	// pre-formatted strings; hits are the raw values the detail pane needs.
	rows []data.Row
	hits []hit

	// shown and total are the number of rows on screen and the number of matches
	// in the whole index, which are almost never equal.
	shown int
	total int

	// status is the single-row footer, pre-formatted.
	status string
	// warn marks a status carrying a failure, which is the only thing that changes
	// its colour — emphasis on a sentence that already says what happened.
	warn bool

	// detailFor is the row the detail fields below describe, and detailGen the
	// detail fetch's generation they came from. Both are integers, so the
	// comparison that decides whether to rebuild the pane costs nothing.
	detailFor int
	detailGen uint64
	// pending is the title of a fetch that has been STARTED but has not answered.
	// It is empty when nothing is in flight, which is what lets the pane
	// distinguish "loading this article" from "showing the previous one".
	pending string

	// dTitle is the pane's block title, dBody its content, dState which of the
	// states above applies. All prebuilt.
	dTitle string
	dBody  []buffer.Span
	dState detailState
}

// buildFrame turns a snapshot into a frame.
//
// It is a pure function of its arguments plus the formatting helpers: no clock, no
// globals that change, no widget state. That is what lets the offline and live
// paths produce identical output from identical inputs, which is what makes the
// goldens worth having.
func buildFrame(s *snapshot) *frame {
	f := &frame{gen: s.gen, state: fetchIdle, detailFor: noSelection}

	res := s.res
	if res == nil {
		res = &results{}
	}
	f.query = res.query

	switch {
	case s.err != nil && res.anyData():
		// A failure with data behind it — a stale response, or one half of a pair
		// that failed after the other succeeded. The rows STAY and the status says
		// the answer is stale, which is the case a reader most needs to be told
		// about and the one markets treats as its own.
		f.state = fetchReady
		f.fillResults(res)
		f.status = statusLine(s, true)
		f.warn = true

	case s.err != nil:
		// A failure with nothing behind it. The status line carries the reason and
		// the table is empty, which is a real answer rather than a blank screen.
		f.state = fetchFailed
		f.status = "search failed: " + oneLine(s.err)
		f.warn = true

	case res.anyData():
		f.state = fetchReady
		f.fillResults(res)
		f.status = statusLine(s, false)

	default:
		// An empty result set is a NORMAL answer and it gets its own state so the
		// screen can say which happened. Collapsing it into fetchIdle would make
		// "you have not searched yet" and "nothing matched" look the same, and
		// they are the two answers a reader most needs told apart.
		f.state = fetchEmpty
		f.status = statusLine(s, false)
	}

	f.dState = detailNone
	return f
}

// noSelection is the row index that means "nothing is selected", which is also
// what data.Table reports as its zero-selection value. It is named because -1 is
// otherwise an unexplained literal in three places.
const noSelection = -1

// fillResults copies a response into the frame's rows, keeping hits aligned.
//
// The rows are built HERE because every cell is a formatted string and Draw may
// not format. The word count is rendered with strconv rather than fmt: it is the
// one number on the screen, and fmt.Sprintf would allocate for a field of at most
// five digits whose column is seven cells wide.
func (f *frame) fillResults(res *results) {
	f.shown = len(res.hits)
	f.total = res.total
	if len(res.hits) == 0 {
		f.rows, f.hits = nil, nil
		return
	}
	f.rows = make([]data.Row, 0, len(res.hits))
	f.hits = make([]hit, 0, len(res.hits))
	for _, h := range res.hits {
		f.rows = append(f.rows, data.Row{Cells: []data.Cell{
			{Text: h.title},
			{Text: strconv.Itoa(h.words)},
			{Text: h.updated},
		}})
		// Copied rather than aliased: the frame is immutable, and a caller holding
		// the pointer must not be able to reach into it through a hit.
		f.hits = append(f.hits, h)
	}
}

// statusLine is the one-row footer: what was searched, where the answer came
// from, how many results there were, and what is wrong if anything is.
//
// The parts are separated by two spaces rather than a bar so the row does not
// read as a table column, and each part is short enough to survive truncation at
// the narrowest width this screen draws at.
func statusLine(s *snapshot, stale bool) string {
	res := s.res
	if res == nil {
		res = &results{}
	}
	var b strings.Builder
	b.WriteString("search")
	b.WriteString("  ")
	b.WriteString(s.source)
	if res.anyData() {
		b.WriteString("  ")
		b.WriteString(strconv.Itoa(len(res.hits)))
		if res.total > len(res.hits) {
			b.WriteString(" of ")
			b.WriteString(strconv.Itoa(res.total))
		}
		b.WriteString(" results")
		b.WriteString("  ")
		b.WriteString(s.at.UTC().Format("15:04:05") + "Z")
	} else {
		b.WriteString("  no results")
		if res.query != "" {
			b.WriteString(" for ")
			b.WriteString(strconv.Quote(res.query))
		}
	}
	if stale {
		b.WriteString("  STALE: ")
		b.WriteString(oneLine(s.err))
	}
	return b.String()
}

// searchingStatus is the footer while a request is in flight.
//
// It is a constant rather than a formatted string because it depends on nothing:
// the query it would name is already in the field the reader is looking at, and a
// footer that repeated it would spend cells on the one row that has to stay short.
//
// The ellipsis is THREE DOTS rather than U+2026, and so is every other piece of
// application prose on this screen. A block's Ascii flag switches its BORDER and a
// widget's switches its truncation marker, but neither one rewrites the strings an
// application hands them — so an app that wants total ASCII degradation writes ASCII
// prose. That is an application decision, and it is made once here rather than
// discovered later as a lone glyph on somebody's terminal.
//
// It is also deliberately the SAME STYLE as every other state: the word is the
// signal, which is what makes the degradation total rather than partial.
const searchingStatus = "search  searching..."

// idleStatus is the footer before the first search.
const idleStatus = "search  type a query, then press enter"

// buildDetail fills the detail pane's fields for the selected row.
//
// It is a separate function rather than part of buildFrame because its trigger is
// different: the result set changes when a search completes, and the detail pane
// changes when the SELECTION moves or an article fetch answers, which can happen
// many times between two searches. Merging them would rebuild the pane on every
// keystroke in the query field.
func buildDetail(f *frame, sel int, pending string, d *detailSnapshot) {
	f.detailFor = sel
	f.pending = pending
	f.setDetailGen(d)

	if sel < 0 || sel >= len(f.hits) {
		// No selection, or a selection left over from a result set that has since
		// been replaced by a shorter one. Saying so beats showing the previous
		// article's text under no title at all.
		f.dTitle, f.dBody, f.dState = "", nil, detailNone
		return
	}

	h := f.hits[sel]
	f.dTitle = h.title

	switch {
	case pending != "" && pending == h.title:
		f.dState = detailLoading
		f.dBody = bodySpans("", mutedSpans(loadingLine))
	case d != nil && d.title != h.title:
		// The answer on hand is for a DIFFERENT article than the selected one,
		// which is what happens when a reader holds the down arrow. The pane must
		// not show the older article's text under the newer article's title.
		f.dState = detailLoading
		f.dBody = bodySpans("", mutedSpans(loadingLine))
	case d != nil && d.err != nil:
		f.dState = detailFailed
		f.dBody = bodySpans("", mutedSpans(oneLine(d.err)))
	case d != nil && d.sum != nil:
		f.dState = detailReady
		f.dBody = bodySpans(d.sum.description, mutedSpans(d.sum.extract))
	default:
		// Nothing loaded and nothing in flight: show the snippet the search
		// already gave us. It is the passage that explains why the row matched, it
		// needs no second request to be useful, and it makes the not-yet-loaded
		// case informative rather than blank.
		f.dState = detailPending
		f.dBody = bodySpans(snippetHint, snippetSpans(h.snippet))
	}
}

// setDetailGen is detailSnapshot's generation, or zero when there is none. It is a method
// so the one place that decides "which fetch generation" is written once.
func (f *frame) setDetailGen(d *detailSnapshot) {
	if d == nil {
		f.detailGen = 0
		return
	}
	f.detailGen = d.gen
}

// snippetHint is the line above a not-yet-loaded snippet, so the reader knows the
// passage is the search's own and not the article's text.
const snippetHint = "matching passage - press enter on the table for the article"

// loadingLine is the detail pane's body while a fetch is in flight.
const loadingLine = "loading the article..."

// bodySpans composes the pane's body: an optional lead line, a blank line, then
// the content spans.
//
// It is one function so that the "one blank line between the lead and the body"
// rule is written once rather than at each call site. An empty lead means there
// is no heading and the content starts at the pane's first row, which is why the
// blank line is skipped rather than drawn as an empty row.
func bodySpans(lead string, content []buffer.Span) []buffer.Span {
	out := make([]buffer.Span, 0, len(content)+2)
	if lead != "" {
		out = append(out, buffer.NewSpan(lead, stSnipHead))
		out = append(out, buffer.NewSpan(" ", buffer.DefaultStyle))
	}
	return append(out, content...)
}

// mutedSpans is a one-run body in the muted style, which is what every state that
// has a sentence and nothing else uses.
func mutedSpans(s string) []buffer.Span { return []buffer.Span{buffer.NewSpan(s, stMuted)} }

// snippetSpans converts the action API's snippet markup into display spans.
//
// It is a single pass with a small state machine rather than a tag stripper plus
// a separate unescape, because the two have to agree: an entity decoded after the
// tags were stripped can produce a "<" that was not markup, and a match span cut
// at the wrong place marks half a term.
//
// An unrecognised tag is DROPPED rather than shown, and an unrecognised entity is
// left as written rather than guessed at. Both are silent, and both are the right
// default for a display string: a reader would rather see a stray "&eacute;" than
// a rendering of a tag the endpoint started emitting without warning.
func snippetSpans(raw string) []buffer.Span {
	var out []buffer.Span
	var cur strings.Builder
	marked := false

	flush := func() {
		if cur.Len() == 0 {
			return
		}
		st := stMuted
		if marked {
			st = stMatch
		}
		out = append(out, buffer.NewSpan(cur.String(), st))
		cur.Reset()
	}

	for i := 0; i < len(raw); {
		switch {
		case strings.HasPrefix(raw[i:], matchOpen):
			// Close the current run before changing style, or the text before the
			// term would be painted in the term's style.
			flush()
			marked = true
			i += len(matchOpen)
		case strings.HasPrefix(raw[i:], matchClose):
			if marked {
				flush()
				marked = false
			}
			i += len(matchClose)
		case raw[i] == '<':
			j := strings.IndexByte(raw[i:], '>')
			if j < 0 {
				// An unterminated tag: everything from here on would be markup, and
				// showing it would put a '<' in a cell. Drop the tail.
				i = len(raw)
			} else {
				i += j + 1
			}
		case raw[i] == '&':
			if s, n := decodeEntity(raw[i:]); n > 0 {
				cur.WriteString(s)
				i += n
				continue
			}
			cur.WriteByte(raw[i])
			i++
		default:
			cur.WriteByte(raw[i])
			i++
		}
	}
	flush()
	return out
}

// entities are the references the action API actually emits in a snippet.
//
// A map rather than a switch because the set is data, not control flow, and
// because a new reference should be one line rather than a new case.
var entities = map[string]string{
	"&quot;":  `"`,
	"&#039;":  `'`,
	"&apos;":  `'`,
	"&amp;":   "&",
	"&lt;":    "<",
	"&gt;":    ">",
	"&ndash;": "–",
	"&mdash;": "—",
}

// utf8MaxRune is the largest value a Unicode reference may name. It is spelled
// rather than imported from unicode/utf8 so that the bound sits next to the code
// that enforces it, and so the reason — a reference naming a value that is not a
// rune at all — is where the check is.
const utf8MaxRune = 0x10FFFF

// decodeEntity decodes the HTML reference at the start of s, and reports how many
// bytes it consumed. A zero length means s does not begin with a reference this
// decoder knows, and the caller writes the '&' literally.
//
// Numeric references are decoded rather than looked up, because the endpoint
// escapes apostrophes as &#039; and a lookup table that happened to lack it would
// leave a visible "&" in the middle of a word.
func decodeEntity(s string) (string, int) {
	end := strings.IndexByte(s, ';')
	if end < 0 || end > 10 {
		// Ten is the widest reference this decoder can produce (&#x10FFFF;), so a
		// longer run is prose that happens to contain a semicolon.
		return "", 0
	}
	ref := s[:end+1]
	if v, ok := entities[ref]; ok {
		return v, len(ref)
	}
	body := ref[1:end]
	if body == "" {
		return "", 0
	}
	base := 10
	if strings.HasPrefix(body, "#x") || strings.HasPrefix(body, "#X") {
		base, body = 16, body[2:]
	} else if strings.HasPrefix(body, "#") {
		body = body[1:]
	}
	n, err := strconv.ParseInt(body, base, 32)
	if err != nil || n <= 0 || n > utf8MaxRune {
		return "", 0
	}
	return string(rune(n)), len(ref)
}

// firstRune returns the first rune of s, or 0 when s is empty.
//
// It is for a one-cell marker string: a caller may write a multi-byte glyph and
// must not have to index its bytes, which would split the UTF-8 sequence and
// write a replacement character.
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}
