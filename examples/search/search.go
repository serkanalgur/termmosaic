package main

// The screen: a query field, a results table, a detail pane, and a key contract
// in which something ACTUALLY holds focus.
//
// # This is the first example with a focusable widget
//
// hello and markets compose screens out of widgets that hold no keyboard focus:
// their "focus ring" is an integer the screen owns, and the widgets are cells the
// screen draws itself or panels it routes to. That is why hello could put every
// navigation binding at ScopeScreen and be right to.
//
// Here two catalog widgets implement termmosaic.Focusable — form.TextInput and
// data.Table — and both have key contracts of their own. Two consequences follow,
// and they are why this file is the interesting one:
//
//  1. A WIDGET OWNS ITS KEYS and the registry must not take them away. The field
//     consumes printable runes, backspace, delete, left, right, home and end while
//     it has focus; the table consumes up, down, page up/down, home, end, left and
//     right, enter and space. This screen binds NONE of those at ScopeScreen,
//     because a screen-scoped binding outranks the focused widget by specificity
//     and would make the field untypable and a twenty-row table unnavigable.
//  2. The keys the widgets DELIBERATELY DECLINE are the screen's to take, and
//     taking them at the wrong scope breaks the screen in the other direction.
//     See newRegistry.
//
// # The declined keys, and what each one is bound to
//
// widgets/form/textinput.go declines four keys on purpose — KeyUp and KeyDown at
// textinput.go:506, and KeyEnter and KeyTab, which have no case at all — and its
// own comment says why: "a single-line field has no vertical motion; the key
// belongs to the form, which moves between fields with it". This screen is that
// form, and each of the four gets the treatment its semantics demand:
//
//	Tab / Backtab    focus.next / focus.back     always live, from either pane
//	Up / Down        focus.results / focus.query live only while the FIELD is focused
//	Enter            search.submit               live only while the FIELD is focused
//	Home / End       NOT BOUND                   the widgets own them, see below
//
// # The scope decision, argued
//
// Every binding in this file is ScopeScreen (owned by this screen, with SetScreen
// naming it) or ScopeGlobal (owned by nobody), and the CONTEXT each one needs is
// expressed with keymap.Command.Enabled rather than with ScopeFocus. That is the
// decision this example exists to make, and it is worth stating in full because
// ScopeFocus is the answer that looks right.
//
// The case for ScopeFocus is real. A focus-scoped binding is live only while its
// OWNER holds focus, which is exactly the property the arrows need: Up and Down
// should mean "move the row" when the table has focus and "leave the field" when
// the field has focus, and the field is the only widget that declines them.
//
// Three things are wrong with reaching for it here.
//
// First, NO catalog widget implements keymap.Commandable — not the TextInput, not
// the Table, not any of them — so there is no focus-scoped binding for the
// registry to learn from the widgets. Binding one would mean the APPLICATION
// declaring keys on a widget's behalf, with an owner it chose, which is the one
// thing Commandable's existence was meant to stop being necessary for.
//
// Second, and decisively: DescribeGrouped(ScopeFocus) CANNOT ANSWER "which pane's
// bindings are in force". Registry.SetScreen exists and is what makes a
// ScopeScreen binding live at all; there is no SetFocus, so the registry learns
// the focused widget only from the focus argument handed to Dispatch, and
// inScope short-circuits an exact-scope match without consulting liveness at all.
// A hint built from a focus-scoped query therefore advertises the FIELD's arrow
// bindings on a screen where the TABLE has focus and those arrows move a
// selection — a hint that is wrong in exactly the pane the reader is looking at.
// keymap.Registry has no SetFocus (deferred), so an application that wants
// focus-scoped help cannot get it, and one that builds its hint from the
// registry's own query gets an answer that does not depend on the focus. That is
// a real gap and it is worth fixing — see the report — but it is a reason NOT to
// depend on ScopeFocus, not a reason to.
//
// Third, the behaviour does not need it. Command.Enabled is documented as "an
// unavailable command is not run by a key press", and dispatchChord SKIPS an
// unavailable command and keeps looking. So a screen-scoped arrow binding whose
// Enabled is false while the table has focus is simply not claimed, the event is
// reported unconsumed, and the event loop offers it to the tree — where the
// table's own arrow contract gets it. The gating is exact, the fallthrough is the
// framework's own, and the hint can filter on the same predicate the dispatcher
// uses, because Registry.Has is documented as "registered and currently
// available".
//
// So: ScopeScreen for everything this screen owns, Enabled for everything that is
// context-dependent, and not one binding on a key a focused widget wants. The
// consequence a reader can check is that this screen's bindings never take a key
// away from a widget that has focus, and TestNoScreenBindingStealsAFocusedWidgetsKey
// checks it.
//
// # Why Home and End are not bound at all
//
// Both focusable widgets claim the bare forms — the field moves the caret, the
// table selects the first and last row — and a ScopeScreen binding outranks both,
// so binding Home here would silently break caret movement AND row jumps. The ring
// still needs a way to its ends, so focus.first and focus.last are bound to
// Ctrl+Home and Ctrl+End, which neither widget consumes and which every line
// editor already uses for exactly this.
//
// # Layout
//
// Three bands inside the chrome, and the bands MOVE as the terminal narrows rather
// than merely getting narrower:
//
//	W >= wideW    query, then results and the detail pane SIDE BY SIDE
//	W >= narrowW  query, then results, then the detail pane BELOW
//	W <  narrowW  a one-line diagnostic, never a clipped screen
//
// and the detail pane is DROPPED before the results table when the terminal is
// short, because the table is the answer to the query and the pane is a summary
// of one row of it.

import (
	"strconv"
	"strings"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/keymap"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/data"
	"github.com/serkanalgur/termmosaic/widgets/form"
)

// The commands this screen answers to.
//
// The IDs are the commands' names; the registry holds the chords that reach them
// and the descriptions shown for them, so a binding and what the screen says
// about it are written in ONE place. Nothing below names a key that is not bound,
// and the hint line is rendered from keymap.Registry.DescribeGrouped.
const (
	// cmdQuit ends the program. ScopeGlobal: quitting is the application's, and it
	// must be live on whatever pane is current.
	cmdQuit = keymap.CommandID("app.quit")
	// cmdSubmit runs the search for whatever is in the query field.
	cmdSubmit = keymap.CommandID("search.submit")
	// cmdNext and cmdBack move the focus ring on by one pane and back by one.
	cmdNext = keymap.CommandID("focus.next")
	cmdBack = keymap.CommandID("focus.back")
	// cmdFirst and cmdLast jump the ring to its ends. They are bound to the Ctrl
	// forms of Home and End because the bare forms belong to the focused widget.
	cmdFirst = keymap.CommandID("focus.first")
	cmdLast  = keymap.CommandID("focus.last")
	// cmdResults and cmdQuery are what the vertical arrows mean when the FIELD has
	// focus: the field has no vertical motion and declines them, so the screen
	// takes them, and they mean "move to that pane" rather than "move the ring".
	//
	// They are separate commands from cmdNext and cmdBack rather than extra chords
	// on them because Enabled is a property of the COMMAND, and a command whose
	// Enabled is "the field has focus" would take Tab down with it.
	cmdResults = keymap.CommandID("focus.results")
	cmdQuery   = keymap.CommandID("focus.query")
)

// Groups for the three halves of the key contract: the application's own keys,
// the keys that move the focus ring, and the key that runs a search.
//
// They are what Describe sorts on, which is what makes its output stable across
// runs and diffable across versions, and they are what a command palette would
// group its rows by.
const (
	groupApp    = "app"
	groupFocus  = "focus"
	groupSearch = "search"
)

// paneHint is what the pinned hint line names, per pane, in the order it names
// them.
//
// It is a list of COMMANDS and not of chords, so adding a chord to a command
// changes what the hint says without anything here changing. It is PER PANE
// because the panes do not have the same live bindings — Enter means "search" in
// one and "open this article" in the other, and the arrows mean different things —
// and a hint that listed both at once would name a key that does one thing on a
// screen where it is showing the reader the other.
//
// Each list leads with what a reader in that pane most needs to know, and every
// list ends with quit, because a hint whose last visible binding is not the one
// that gets you out is a worse affordance than no hint.
var paneHint = [numPanes][]keymap.CommandID{
	paneQuery:   {cmdSubmit, cmdNext, cmdResults, cmdQuit},
	paneResults: {cmdQuery, cmdNext, cmdFirst, cmdQuit},
}

// focusLabels names each pane, for the tests and for a reader of this file. They
// appear on screen nowhere: the field's non-colour focus signal is its caret and
// the gutter marker beside it, and the table's is its selection marker.
var focusLabels = [...]string{"query", "results"}

// Pane indices into focusLabels and paneHint. Named so that a reordering of the
// ring cannot silently move Enter's meaning from the table to the field.
const (
	paneQuery = iota
	paneResults
	numPanes
)

// The screen's own geometry. Every one is a local named constant: ADR 0007 §1
// rule 5 says a widget's thresholds are its own policy, and a shared breakpoint
// constant would be a second source of truth for space that already has one — the
// rect.
const (
	// narrowW is the width below which the screen draws a diagnostic rather than a
	// clipped search, and it is also the width MinSize reports, so "too small for
	// this" and "too small for anything" are one threshold rather than two that
	// could disagree.
	//
	// It is derived from the results table's own columns: a title wide enough to
	// read an article name, the word column, the date column, the selection
	// gutter, the scrollbar, the border and the padding.
	narrowW = 46

	// wideW is the interior width at which the detail pane moves BESIDE the results
	// table rather than below it. It is interior rather than bounds because the
	// interior is the space the bands are laid out in.
	wideW = 88

	// queryH is the query band's height: a top border, the field row, and a bottom
	// border. Fixed rather than solved because a field whose row moved as the
	// terminal narrowed would reflow every band below it.
	queryH = 3

	// resultMinH is the shortest results table worth drawing: a border, the header,
	// one row, and a border.
	resultMinH = 4

	// detailRows is the height the detail pane asks for when there is room for it.
	// It is a REQUEST, not a guarantee: geometry.Budget decides whether the pane is
	// affordable, and a short terminal drops it before the table.
	detailRows = 7

	// chromeRows are the rows below the bands that are not bands: the hint and the
	// status line. Both are pinned to the bottom rather than stacked after the
	// body, so they are visible at every height instead of being the first thing a
	// grow would reveal.
	chromeRows = 2

	// bandGap is the cells between two bands. Panels are bordered, so a gap of zero
	// would butt two borders together and read as one panel with a seam.
	bandGap = 1

	// detailMinH is the shortest detail pane worth drawing: a border, the article's
	// title, and two lines of extract. Below that the pane is not worth the rows it
	// would take from the results table, and adapt drops it.
	detailMinH = 4

	// minH is the shortest screen that shows a query field and a results table: the
	// query band, a gap, the table's own floor, and the two rows of chrome.
	minH = queryH + bandGap + resultMinH + chromeRows

	// detailMaxW is the most the detail pane takes when it sits BESIDE the table.
	//
	// It is a ceiling and not a share because the pane's content is an extract,
	// which wraps and is finished well before sixty cells, while the table's title
	// column is the one column whose content varies without limit. Capping the pane
	// is what makes a wide terminal show LONGER ARTICLE NAMES rather than a wider
	// empty box.
	detailMaxW = 60
)

// tooSmallText is what the screen says instead of a clipped layout.
//
// It NAMES the minimum, which is the one thing a too-small diagnostic has to do
// that a clipped screen does not, and it puts "too small" in the FIRST FOURTEEN
// CELLS — the constraint the message is written against, because basic.Text
// truncates from the right and a diagnostic whose key words sat at the end would
// become unreadable exactly at the widths where it is the only thing on screen.
var tooSmallText = "too small: need " + strconv.Itoa(narrowW) + "x" + strconv.Itoa(minH)

// labelW is the width of the query band's label, and the number of cells between
// the label's start and the field's.
//
// It is a constant rather than a measurement because the label is a literal and a
// measurement of it would be a way for the two to disagree. It also fixes the
// field's left edge, which is what keeps the field from shifting sideways when the
// focus moves.
const labelW = 5

// search is the screen.
//
// It is a plain termmosaic.Widget rather than a container widget from the catalog:
// the catalog has no search screen, and inventing one would put an application
// decision in the framework. What it composes IS the catalog — block, basic, data,
// form — and the nesting is expressed with layout.Solve and geometry.Budget, both
// of which are the documented mechanism rather than a private solver.
type search struct {
	bounds buffer.Rect

	// blk draws the border, the background and the title, and reports the interior
	// rectangle the bands draw into. It is a value field rather than a pointer
	// because it has no identity: it is chrome.
	blk block.Block

	// query is the field, and the FIRST focusable. It starts focused, because a
	// search screen a reader has to click before they can type into is a screen
	// that ignores its own purpose.
	query *form.TextInput
	// results is the table, and the second focusable.
	results *data.Table

	// detailBlk and detail are the pane: a bordered block whose title names the
	// selected article, and the body beneath it.
	detailBlk *block.Block
	detail    *basic.Paragraph

	// hint is the pinned key line, a form.KeyHint fed from the registry.
	hint *form.KeyHint
	// status is the one-row footer.
	status *basic.Text
	// diag is the below-minimum diagnostic, built once so the too-small path
	// allocates nothing: it is drawn every frame for as long as the terminal stays
	// small, which is the one place an allocation would be least excusable.
	diag *basic.Text

	// km is this screen's command registry. The screen owns it because this example
	// IS the application — one screen, one focus ring, and no second place a
	// binding could be declared — and Dispatch needs it, which is what lets a
	// golden construct the widget alone and still get a fully wired example.
	km *keymap.Registry

	// focus is which pane the keyboard is aimed at.
	focus int

	// onSubmit, onOpen and quitFn are the screen's three exits into the
	// application. They are FIELDS rather than constructor parameters because the
	// application cannot build them until it has built the screen: each of them
	// needs the renderer to Post its result, and the renderer needs the screen as
	// its root. Every one is nil-checked at the point of use, so a screen built
	// with none of them set is fully usable and simply does nothing — which is what
	// lets a golden construct the widget alone and still get a wired example.
	//
	// They are fields rather than calls into a source because reaching the network
	// is the application's business, and a widget that could start a request would
	// take that decision away from the one place that can undo it. The tests assign
	// funcs that record the calls instead.
	onSubmit func(string)
	onOpen   func(string)
	quitFn   func()

	// cur is the frame the widgets hold, and curGen the store generation it came
	// from, so a republish of the same generation can be skipped.
	cur    *frame
	curGen uint64

	// detailSnap is the article fetch's own snapshot, and pending the title of one
	// that has been started and has not answered. They are separate from cur because
	// their lifecycle is different: a detail fetch is triggered by moving the
	// selection, not by running a search.
	detailSnap *detailSnapshot
	pending    string

	// searching is the in-flight flag the status line reads. It is a field rather
	// than a cur.state lookup because it changes WITHOUT a new frame arriving: the
	// request starts before any response does, and that gap is exactly the window
	// the reader needs to see "searching…" in.
	searching bool

	// statusText is the footer's current string, held so a republish of unchanged
	// text costs a string comparison rather than a SetText — which copies, and would
	// allocate on the heartbeat.
	statusText string

	// detailShown is the detail pane's last-built key, so a republish of the same
	// selection costs an integer comparison rather than a re-strip of the snippet.
	// newSearch sets it to detailStale rather than leaving it zero; see that constant
	// for why the zero value is not a safe "nothing yet".
	detailShown detailKey

	// lay is the derived layout, cached against the interior it came from.
	lay layoutCache
	// regions is the vertical budget table, held so adapt patches it rather than
	// building a fresh one per size change.
	regions [numBands]geometry.Region
}

// detailKey is what the detail pane's content depends on: which row is selected,
// and which detail fetch generation answered.
//
// Two integers rather than a string key, because the comparison happens on the
// interaction path on every arrow key and must not allocate.
type detailKey struct {
	row int
	gen uint64
}

// detailStale is the detailKey that means "the pane's content does not describe
// anything yet", and it is deliberately NOT the zero value.
//
// Row 0 with generation 0 is a perfectly ordinary key — it is what the pane holds
// after the first response, with the first row selected and no article loaded — so
// invalidating the cache to the zero value would make SetFrame believe the pane
// already described exactly what it is about to describe, and it would draw an
// empty pane over a selected row. A row index no table can report is a sentinel
// that cannot be confused with a real one.
const detailStaleRow = noSelection - 1

// staleDetailKey is the detailKey that means "the pane describes nothing yet".
//
// It is a function rather than a var because a package-level var could be mutated
// by a test, and every comparison against it would then be against something that
// had changed; a function cannot be.
func staleDetailKey() detailKey { return detailKey{row: detailStaleRow} }

// layoutCache is everything derived from the screen's rectangle.
//
// Every field is a function of the interior and of nothing else, which is what
// makes keying the cache on that rectangle sufficient (ADR 0007 §3 and its
// amendment about caches keyed on the wrong thing).
type layoutCache struct {
	// key is the interior this was derived from. valid distinguishes "derived
	// from the zero rect" from "derived from a zero-size rect", which are the same
	// value and different answers.
	key   buffer.Rect
	valid bool
	// wide reports whether the detail pane sits beside the table.
	wide bool
	// show is geometry.Budget's answer: one entry per band, true to draw it.
	show [numBands]bool
	// tableR and detailR are the two bands' rectangles for this interior, SOLVED
	// here rather than in Draw.
	//
	// layout.Solve allocates a fresh slice per call, and it was being called from
	// Draw — which is the difference between a frame path that allocates six objects
	// and one that allocates none. Everything a size implies is derived HERE, once
	// per rect, which is the whole of ADR 0007 §3's lazy rule applied to this
	// screen.
	tableR, detailR buffer.Rect
}

// Band indices into regions and layoutCache.show, in draw order.
const (
	bandQuery = iota
	bandResults
	bandDetail
	numBands
)

// newSearch returns the screen with its chrome, its widgets and its command
// registry configured.
//
// Every size-independent thing is built here rather than in Draw: the chrome, the
// columns, the hint, the diagnostic, and the whole key contract. The band
// rectangles are the only size-derived values, and they are recomputed from
// Draw's own rect check (ADR 0007 §3).
func newSearch(r buffer.Rect) *search {
	s := &search{bounds: r, cur: &frame{}, detailShown: staleDetailKey()}

	s.blk = *newChrome(r)
	// The title is "search - wikipedia", not " search - wikipedia ": Block inserts
	// the one space on each side itself, and a caller that added them would shift
	// the title two columns right of where the border's interior says it belongs.
	//
	// The separator is an ASCII HYPHEN rather than an em-dash, and that is a
	// deliberate rung decision rather than an accident of typing: a block's Ascii
	// flag switches its BORDER, not its title, so an em-dash here would survive onto
	// a terminal whose caps report no Unicode — the one place the degradation ladder
	// would leak. Every glyph this screen draws is either ASCII or selected by a
	// widget's own Ascii flag.
	s.blk.SetTitleString("search - wikipedia", stTitle)

	s.query = form.NewTextInput(buffer.Rect{})
	s.query.TextStyle = stField
	s.query.Background = stPanel
	s.query.PlaceholderStyle = stMuted
	s.query.Placeholder = "type a query, then press enter"

	s.results = data.NewTable(buffer.Rect{}, resultColumns()...)
	framePanel(s.results.Block(), "results")
	s.results.Header = true
	s.results.ItemStyle = stPanel
	s.results.SelectedStyle = stSelected
	s.results.ScrollbarStyle = stAccent
	// The selection marker is the CATALOG's ASCII Select marker rather than the
	// table's own default, for two reasons. It makes the table's marker and the
	// query field's the same character, so one vocabulary covers the screen's focus
	// signal. And the table has no Ascii flag for its marker — the widget's default
	// is a single non-ASCII glyph with no ASCII rung — so a screen that left it
	// alone would leak "›" onto a terminal whose caps report no Unicode, which is
	// the one place the degradation ladder must not leak.
	s.results.Marker = form.SelectDefaultMarker
	// OnActivate is what Enter does when the TABLE has focus. The submit command
	// is gated OFF while the table has focus, so the event is never claimed by the
	// registry and reaches the tree, where the table's own key contract runs this.
	// That is the whole mechanism, and it is why no binding shadows the table's
	// Enter: see the file comment.
	s.results.OnActivate = func(i int) { s.openSelected(i) }

	s.detailBlk = block.New(buffer.Rect{})
	s.detailBlk.SetBorder(buffer.BorderPlain)
	s.detailBlk.SetBorderStyle(stEdge)
	s.detailBlk.SetBackground(stPanel)
	s.detail = basic.NewParagraphString(buffer.Rect{}, "", stMuted)
	s.detail.SetBackground(stPanel)

	// The footer starts EMPTY rather than empty-looking: SetFrame is what fills it,
	// and a screen constructed but not yet fed would otherwise show a stale status
	// from a previous run.
	s.status = basic.NewTextString(buffer.Rect{}, "", stMuted)
	s.status.SetBackground(stBody)

	s.diag = basic.NewTextString(buffer.Rect{}, tooSmallText, stMuted)
	s.diag.SetBackground(stBody)

	// The registry is built before the hint, because the hint is rendered from it.
	s.km = newRegistry(s)
	s.hint = newHintLine(s)

	// The ring starts on the field.
	s.setFocus(paneQuery)
	// The footer's first paint. syncStatus is the only writer of the status text, so
	// calling it here rather than special-casing a literal is what keeps the footer
	// from starting empty — which would be a screen whose status line says nothing
	// until its first search.
	s.syncStatus()

	s.regions[bandQuery] = geometry.Region{Size: queryH, Prio: geometry.PrioHigh}
	s.regions[bandResults] = geometry.Region{Size: resultMinH, Prio: geometry.PrioHigh}
	s.regions[bandDetail] = geometry.Region{Size: detailRows, Prio: geometry.PrioLow}

	s.applyRung()
	return s
}

// newChrome returns the block's chrome — border, background, padding — for a
// rectangle r. It is separate from newSearch because a test that wants the
// example's own chrome at an exact width needs the chrome without the body's
// minimum-size gate in the way.
func newChrome(r buffer.Rect) *block.Block {
	b := block.New(r)
	b.SetBorder(buffer.BorderPlain)
	b.SetBorderStyle(stEdge)
	b.SetBackground(stBody)
	b.SetPadding(1)
	return b
}

// newRegistry builds the screen's command registry: the commands, the chords that
// reach them, the scopes those chords are live in, and the predicates that say
// which of them are available right now.
//
// The scopes are all ScopeScreen or ScopeGlobal and the table below is the whole
// of the argument; the file comment states it at length and the three paragraphs
// on each entry here say only what is specific to that entry.
func newRegistry(s *search) *keymap.Registry {
	km := keymap.New()

	// The context predicates come first because every Enabled below is one of them.
	//
	// fieldFocused is the single most important expression in this file: it is the
	// whole of the screen's relationship with a widget that holds focus. Every
	// binding whose meaning depends on WHICH pane has the keyboard is gated on it,
	// so that when the table has focus the registry simply stops claiming the key
	// and the table's own contract receives it.
	fieldFocused := func() bool { return s.FocusIndex() == paneQuery }

	km.Register(
		keymap.Command{
			ID: cmdQuit, Desc: "quit", Group: groupApp,
			// The DECLINE is the whole of this handler's cleverness, and it is
			// load-bearing rather than defensive. ADR 0009 §2 asks the registry before
			// the tree, so a global 'q' reaches this Run before the field ever sees
			// it; without the decline the example would be a search box no reader
			// could type "quit" into.
			//
			// It declines on the PRINTABLE quit chords only, and not on Esc or
			// Ctrl+c: those two are not text, no field wants them, and a reader who
			// presses Escape mid-query expects to leave, not to type an escape
			// sequence into a search box.
			Run: func(c keymap.Ctx) bool {
				if fieldFocused() && isPrintableChord(c.Chord) {
					return false
				}
				s.quit()
				return true
			},
		},
		keymap.Command{
			ID: cmdSubmit, Desc: "run the search", Group: groupSearch,
			// Gated on the field, and NOT on the query being non-empty: an empty
			// query is a request the application accepts and then declines, and the
			// status line says so. Gating on it here instead would make Enter dead on
			// an empty field and the reader would learn nothing about why.
			//
			// The refusal is reported by a declined Run rather than by a false
			// Enabled, because the two have different meanings here: a declined Run
			// means "this command is not what this key means", and the event then
			// reaches the tree. Nothing behind the field wants Enter, so either
			// would work; the decline is the one that cannot be confused with the
			// command being absent from help.
			Enabled: fieldFocused,
			Run:     func(keymap.Ctx) bool { return s.submit() },
		},
		keymap.Command{
			ID: cmdNext, Desc: "next pane", Group: groupFocus,
			Run: func(keymap.Ctx) bool { s.moveFocus(1); return true },
		},
		keymap.Command{
			ID: cmdBack, Desc: "previous pane", Group: groupFocus,
			Run: func(keymap.Ctx) bool { s.moveFocus(-1); return true },
		},
		keymap.Command{
			ID: cmdFirst, Desc: "first pane", Group: groupFocus,
			Run: func(keymap.Ctx) bool { s.setFocus(0); return true },
		},
		keymap.Command{
			ID: cmdLast, Desc: "last pane", Group: groupFocus,
			Run: func(keymap.Ctx) bool { s.setFocus(numPanes - 1); return true },
		},
		// The two vertical moves. They are commands of their own rather than extra
		// chords on cmdNext and cmdBack because Enabled belongs to the command: a
		// "next pane" gated on the field would take Tab down with it, and Tab has
		// to work from the table too.
		keymap.Command{
			ID: cmdResults, Desc: "the results", Group: groupFocus,
			Enabled: fieldFocused,
			Run:     func(keymap.Ctx) bool { s.setFocus(paneResults); return true },
		},
		keymap.Command{
			ID: cmdQuery, Desc: "the query", Group: groupFocus,
			Enabled: fieldFocused,
			Run:     func(keymap.Ctx) bool { s.setFocus(paneQuery); return true },
		},
	)

	// BindString rather than a hand-built keymap.Chord, so the bindings are written
	// in the same notation the hint prints them in: what the hint says is what
	// ParseChord accepts, which is the whole point of keymap owning one spelling of
	// a chord.
	//
	// The spellings worth reading twice are the ones the DECODER produces rather
	// than the ones that read nicely. "Ctrl+c" is not "Ctrl+C": the decoder reports
	// Ctrl-C as Ctrl+'c', and ADR 0009's amendment 4 says a Shift-modified rune is a
	// DIFFERENT chord rather than a spelling of the same one, so "Ctrl+C" would be a
	// binding no terminal sends. "Backtab" is what the decoder calls Shift-Tab and
	// what Chord.String prints, so the hint says what the keymap calls the key.
	for _, spec := range []struct {
		id     keymap.CommandID
		scope  keymap.Scope
		owner  termmosaic.Widget
		chords []string
	}{
		// The application's own keys: live everywhere, owned by nobody, and bound
		// ONCE — a second screen-scoped copy of the same chord would make
		// Registry.Chords report each of them twice and the hint would print
		// "[q Q Esc Ctrl+c q Q Esc Ctrl+c] quit".
		//
		// 'q' and 'Q' here are the one genuinely dangerous binding in the file, and
		// it is safe for a reason worth stating: the registry is asked BEFORE the
		// field, so a global 'q' would swallow every q typed into a query. The screen
		// therefore DECLINES quit while the field has focus — the declined Run is the
		// keymap's only fallthrough mechanism, and this is the case it exists for.
		// Without the decline this example would be a search box you cannot type
		// "quit" into.
		{cmdQuit, keymap.ScopeGlobal, nil, []string{"q", "Q", "Esc", "Ctrl+c"}},
		// Submit. Enter only, and only while the field has focus, so the table's own
		// Enter activation is never shadowed.
		{cmdSubmit, keymap.ScopeScreen, s, []string{"Enter"}},
		// The ring's horizontal steps. Neither widget consumes Tab or Backtab, and
		// both have to work from either pane.
		{cmdNext, keymap.ScopeScreen, s, []string{"Tab"}},
		{cmdBack, keymap.ScopeScreen, s, []string{"Backtab"}},
		// The ring's vertical steps, which only the field has given up.
		{cmdResults, keymap.ScopeScreen, s, []string{"Down"}},
		{cmdQuery, keymap.ScopeScreen, s, []string{"Up"}},
		// The ring's ends. The Ctrl forms, because the bare Home and End belong to
		// the focused widget and a screen-scoped binding would outrank both.
		{cmdFirst, keymap.ScopeScreen, s, []string{"Ctrl+Home"}},
		{cmdLast, keymap.ScopeScreen, s, []string{"Ctrl+End"}},
	} {
		for _, chord := range spec.chords {
			// A malformed binding string is a programming error in a literal a few
			// lines above, so it panics here rather than being carried around as an
			// error a caller has to remember to check.
			if err := km.BindString(chord, spec.id, spec.scope, spec.owner); err != nil {
				panic("search: " + err.Error())
			}
		}
	}

	km.SetScreen(s)
	// Neither this screen nor either widget in its ring implements
	// keymap.Commandable — no widget in the catalog does — so Attach contributes no
	// bindings. It is still called, because it is what records the widgets as
	// attached, and an unattached owner is a Warnings entry.
	km.Attach(s, s.query, s.results)
	km.Seal()
	return km
}

// newHintLine builds the pinned hint: the registry's answer for the FOCUSED pane's
// commands, on one line.
//
// It is a KeyHint because a key hint IS a key hint, and the same accessibility
// rule applies — each key is bracketed, so the line reads without colour — and
// using a different widget for the same job is how an example ends up with two
// spellings of one idea.
//
// The entries come from DescribeGrouped and are then FILTERED on Registry.Has,
// which is documented as "registered and currently available" — the same predicate
// Dispatch uses through Command.Enabled. That filter is what makes the hint honest:
// without it the line would advertise Enter as "run the search" on a screen where
// the table has focus and Enter opens the article instead.
//
// DescribeGrouped and not Describe, because a hint line is a hint rather than a
// key list: it is the query whose one command is one row, so a command with three
// chords contributes one bracketed key column instead of three identical
// descriptions.
func newHintLine(s *search) *form.KeyHint {
	k := form.NewKeyHint(buffer.Rect{}, nil)
	k.SetEntries(s.hintEntries())
	k.KeyStyle = stAccent
	k.HelpStyle = stMuted
	k.SeparatorStyle = stMuted
	k.Background = stBody
	return k
}

// hintEntries returns the rows the hint shows for the focused pane.
//
// Two registry queries and a filter, in that order. DescribeGrouped is the
// authority on what IS bound and what it is called; Chords is the cheap
// single-command query that says which chords reach ONE of them, which is what
// keeps this from asking for the whole table on every focus change; and Has is
// the liveness filter that keeps an unavailable command off a line that would
// otherwise promise a key does something it cannot.
func (s *search) hintEntries() []keymap.Entry {
	all := s.km.DescribeGrouped(keymap.ScopeScreen)
	out := make([]keymap.Entry, 0, len(paneHint[s.focus]))
	for _, id := range paneHint[s.focus] {
		if !s.km.Has(id) {
			// Unavailable right now: the pane it belongs to does not have focus. It
			// is dropped rather than shown greyed, because a hint line has one row
			// per command and a row for a key that does nothing is worse than its
			// absence.
			continue
		}
		e, ok := entryFor(all, id)
		if !ok {
			continue
		}
		e.Chords = s.km.Chords(id, keymap.ScopeScreen)
		out = append(out, e)
	}
	return out
}

// entryFor returns the entry for id out of a DescribeGrouped list.
//
// A command that is not present yields nothing rather than a blank row, so a
// command removed from the registry leaves no trace on screen instead of a
// description with no key beside it.
func entryFor(entries []keymap.Entry, id keymap.CommandID) (keymap.Entry, bool) {
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return keymap.Entry{}, false
}

// submit asks the application to run a search for the field's contents.
//
// It reports whether it accepted the request, and a declined submit is a REAL
// outcome rather than an error path: pressing Enter on an empty field does
// nothing, and pressing it while a request is already in flight does not start a
// second one. Both report false so the footer can say which.
//
// The in-flight flag is set HERE, on the render goroutine, and not when the
// response arrives. That is the whole of "a TUI must not freeze while a request is
// in flight" from the user's side: the screen says "searching…" on the very next
// frame rather than sitting on the previous results looking like nothing happened.
func (s *search) submit() bool {
	if s.searching {
		return false
	}
	q := strings.TrimSpace(s.query.Text())
	if q == "" {
		s.statusText = "search  nothing to search for yet"
		s.status.SetText(s.statusText, stMuted)
		return false
	}
	s.searching = true
	s.syncStatus()
	if s.onSubmit != nil {
		s.onSubmit(q)
	}
	return true
}

// quit is what the quit command runs. It is a field rather than a call into the
// application's stop channel because the screen must not know how the program
// ends; a test replaces it and the screen has no idea.
//
// It is nil-checked because quitFn is assigned after construction, and a screen
// built for a golden never needs to quit at all.
func (s *search) quit() {
	if s.quitFn != nil {
		s.quitFn()
	}
}

// openSelected asks the application for the selected article's extract.
//
// It is wired to the table's OnActivate, so it runs only when the reader asks for
// an article with Enter rather than on every arrow key. That is both the polite
// choice against a public API and the one that keeps the detail pane meaningful
// instead of a blur of whichever articles the reader passed through.
func (s *search) openSelected(i int) {
	if s.onOpen == nil || i < 0 || i >= len(s.cur.hits) {
		return
	}
	s.onOpen(s.cur.hits[i].title)
}

// SetSearching records that a request is in flight, or that it finished.
//
// It is called from the render goroutine only — from the submit command's Run, and
// from the Post that applies the response — because the footer reads the flag and
// the footer is built from it.
func (s *search) SetSearching(v bool) {
	if s.searching == v {
		return
	}
	s.searching = v
	s.syncStatus()
}

// applyRung passes the terminal's Unicode capability to every block on the screen.
//
// It is a separate pass rather than something the per-widget configuration does,
// because the ASCII flag is a property of the TERMINAL rather than of any one
// panel. Doing it in one place also means a panel added later cannot forget it,
// which is the failure mode of a convention.
func (s *search) applyRung() {
	for _, b := range []*block.Block{&s.blk, s.results.Block(), s.detailBlk} {
		b.Ascii = asciiRung
	}
	// The field's truncation marker and the hint's are glyphs of their own, so the
	// ASCII rung has to reach them too.
	s.query.Ascii = asciiRung
	s.hint.Ascii = asciiRung
}

// Bounds returns the screen's rectangle.
func (s *search) Bounds() buffer.Rect { return s.bounds }

// MinSize returns the smallest rectangle in which the screen says something worth
// reading: its chrome, the query field, one header plus one result row, and the
// pinned hint and status.
//
// It is pure, reads no state Draw mutates, and is safe before the first draw —
// which is why search satisfies Minimizable at all: ADR 0007 §4 asks a widget to
// be total at 1x1, and a widget that can say "I need 46x11" is the only way an
// application can decide what to do about the sizes where it cannot.
func (s *search) MinSize() buffer.Size { return buffer.Size{W: narrowW, H: minH} }

// Invalidate satisfies termmosaic.Widget. The screen repaints in full every frame,
// so there is nothing finer-grained to mark.
func (s *search) Invalidate() {}

// SetBounds resizes the screen and drops the layout cache, so the next Draw
// re-derives the arrangement from the new rectangle (ADR 0007 §3).
func (s *search) SetBounds(r buffer.Rect) {
	s.bounds = r
	s.lay.valid = false
}

// FocusIndex reports which pane the keyboard is aimed at, which is what the tests
// assert on after a simulated Tab.
func (s *search) FocusIndex() int { return s.focus }

// FocusLabel names the focused pane.
func (s *search) FocusLabel() string {
	if s.focus < 0 || s.focus >= len(focusLabels) {
		return "none"
	}
	return focusLabels[s.focus]
}

// FocusWidget returns the widget holding keyboard focus, which is what the event
// loop hands Registry.Dispatch.
//
// It matters that this is the FOCUSED WIDGET rather than the screen: a
// ScopeFocus binding's liveness is decided by comparing its owner against this
// argument, so passing the screen would make every focus-scoped binding dead. This
// example binds none — the file comment explains why — and a test asserts the
// argument anyway, because the next person to add one will get this right by
// reading the signature and not by reading a comment in another file.
func (s *search) FocusWidget() termmosaic.Widget {
	switch s.focus {
	case paneQuery:
		return s.query
	case paneResults:
		return s.results
	default:
		return nil
	}
}

// Query returns the field's contents, which is what the tests assert survives a
// resize.
func (s *search) Query() string { return s.query.Text() }

// Searching reports whether a request is in flight.
func (s *search) Searching() bool { return s.searching }

// Selected reports the selected row, or noSelection when there is no selection.
func (s *search) Selected() int { return s.results.Selected() }

// SetFrame feeds a search response to the widgets.
//
// It runs on the render goroutine — the fetch goroutine hands the snapshot over
// through render.Renderer.Post — and it is the only place result content changes.
// Everything it writes is derived from the frame, which is immutable, so there is
// no window in which two halves of the screen disagree about the results.
//
// The layout cache is invalidated here rather than left keyed on the rect alone,
// because a response changes the SHAPE of the content — nine rows become none,
// none becomes nine — without the rectangle changing at all. ADR 0007's amendment
// names exactly this hazard: a cache keyed only on its rect is permanently wrong
// for a field change, because nothing will ever produce a different rect to
// invalidate it.
func (s *search) SetFrame(f *frame) {
	// A republish of the generation already applied is skipped. That is what the
	// heartbeat does, and skipping it is what keeps the heartbeat from
	// re-normalising twenty rows a second for nothing.
	//
	// The generation rather than the frame pointer is the key, because the heartbeat
	// rebuilds the frame every tick from the same snapshot: a pointer comparison
	// would miss every one of them.
	if s.cur != nil && s.curGen == f.gen {
		return
	}
	s.cur = f
	s.curGen = f.gen

	s.results.SetRows(f.rows)
	// The table derives its column widths from the VISIBLE ROWS and caches them
	// against the interior, so a new set of rows — nine becoming none, or a title
	// longer than any before — is a new layout.
	s.results.Invalidate()

	s.syncStatus()
	// The selection survives a new result set where it still exists and is clamped
	// to nothing where it does not, which SetRows already does; the detail pane's
	// key is reset because the row it described may now mean a different article.
	s.detailShown = staleDetailKey()
	s.syncDetail()
	s.lay.valid = false
}

// SetDetail feeds an article's extract to the detail pane.
//
// It takes the whole detail state rather than an extract alone because the pane
// has to be able to tell four states apart — nothing selected, loading, loaded,
// failed — and a parameter per state would be a way for two call sites to disagree
// about which one they meant.
func (s *search) SetDetail(d *detailSnapshot) {
	s.detailSnap = d
	s.syncDetail()
}

// SetPending records that an article fetch has been started, so the pane can say
// "loading" rather than showing the previous article's text under a new title.
func (s *search) SetPending(title string) {
	if s.pending == title {
		return
	}
	s.pending = title
	s.syncDetail()
}

// syncStatus republishes the footer, which is a function of the frame AND of the
// in-flight flag.
//
// It is separate from SetFrame for exactly that reason: starting a search changes
// no data, so there is no new frame to apply, and a footer that only updated with
// data would leave a reader who pressed Enter looking at an unchanged screen and
// conclude the key did nothing.
func (s *search) syncStatus() {
	want := s.statusWant()
	if want == s.statusText {
		return
	}
	s.statusText = want
	s.status.SetText(want, s.statusStyle())
}

// statusWant is the footer's text for the current state.
//
// The in-flight word wins over everything else: while a request is out, the
// previous results are still on screen and saying "9 of 3027 results" would be
// describing data the reader has just been told is being replaced.
func (s *search) statusWant() string {
	if s.searching {
		return searchingStatus
	}
	if s.cur == nil || s.cur.state == fetchIdle {
		return idleStatus
	}
	return s.cur.status
}

// statusStyle is the footer's one style decision: a footer carrying a failure is
// styled differently from a healthy one, which is emphasis on a footer whose TEXT
// already says which of the two it is.
func (s *search) statusStyle() buffer.Style {
	if s.cur != nil && s.cur.warn {
		return stWarn
	}
	return stMuted
}

// syncDetail rebuilds the detail pane when its content has changed.
//
// The key is two integers — the selected row and the detail fetch's generation —
// so the "has it changed?" test costs nothing and happens on the interaction path,
// never in Draw. Everything it writes is prebuilt: the snippet is stripped to spans
// in buildDetail, and block.SetTitleString COPIES, so calling it per frame would
// allocate on the frame path for as long as the pane is open.
//
// The generation in the key is the INCOMING snapshot's, not the one the frame
// records: the pane's subject changes when an answer ARRIVES, which is the moment
// the pane must be rebuilt, and reading the frame's own record of the generation it
// was built with would make every arrival look like no change at all.
func (s *search) syncDetail() {
	if s.cur == nil {
		return
	}
	var gen uint64
	if s.detailSnap != nil {
		gen = s.detailSnap.gen
	}
	key := detailKey{row: s.results.Selected(), gen: gen}
	if key == s.detailShown {
		return
	}
	s.detailShown = key

	buildDetail(s.cur, key.row, s.pending, s.detailSnap)
	// The title is the article's name, or the pane's own name when there is no
	// selection, so the pane is never an unlabelled box.
	title := s.cur.dTitle
	if title == "" {
		title = "detail"
	}
	s.detailBlk.SetTitleString(title, stTitle)
	s.detail.SetSpans(s.cur.dBody)
}

// isPrintableChord reports whether c is a gesture that inserts text.
//
// It requires BOTH a non-zero Rune and NO modifiers, which is stricter than "has a
// rune" and is the second half of the quit decline working correctly. Ctrl+C
// arrives as Ctrl+'c' — a printable rune with a modifier held — so a predicate that
// only asked about the rune would let a reader's interrupt keystroke be typed into
// the search box instead of quitting. Esc and Ctrl+C are the two chords a reader
// reaches for when they want out, and neither may ever reach a field.
func isPrintableChord(c keymap.Chord) bool { return c.Rune != 0 && c.Mod == 0 }

// sideBySide is the constraint list for the two bands sharing a row: the results
// table, a fixed gap, and the detail pane.
//
// The gap is Min rather than Length so that a narrow interior narrows the PANES
// rather than the space between them.
//
// The detail pane is Max rather than Fill, and that is the whole of the wide
// arrangement's shape. A Fill would split a wide terminal in proportion, which on a
// 200-column screen gives the pane ninety cells to show an extract that is already
// finished at sixty — and starves the table's TITLE column, the one column whose
// content genuinely varies, until every article name is an ellipsis. Capping the
// pane and letting the table absorb the rest is what makes a wide terminal show
// LONGER ARTICLE NAMES rather than more empty panel.
func sideBySide() []layout.Constraint {
	return []layout.Constraint{layout.Fill(1), layout.Min(bandGap), layout.Max(detailMaxW)}
}

// Draw paints the screen.
//
// It is total for every rectangle including empty, and it allocates nothing in
// steady state. Everything derived from the size is in adapt, which runs at most
// once per distinct interior, and every string and span on the screen was built
// when the data changed rather than here.
func (s *search) Draw(buf *buffer.Buffer) {
	r := s.bounds
	if r.Empty() {
		// ADR 0007 §4: an empty Bounds returns immediately.
		return
	}
	// Repaint the whole rect first. The renderer diffs and never clears, so a
	// screen that drew nine rows and a detail pane at 120x40 and then a diagnostic
	// at 40x12 would otherwise leave the table on screen (ADR 0007 §1 rule 3).
	buf.FillRect(r, stBody.Blank())

	if !s.enough(r) {
		s.drawTooSmall(buf, r)
		return
	}

	s.blk.SetBounds(r)
	s.blk.Draw(buf)

	inner := s.blk.Interior()
	if inner.Empty() {
		return
	}
	// Adapt on the INTERIOR, not on the screen rectangle. The bands live inside the
	// chrome, so deriving them from the outer rect puts the results band on top of
	// the padding and the pinned status line on top of the bottom border — a screen
	// that looks almost right, which is the hardest kind of wrong to notice.
	s.adapt(inner)

	// The query band is drawn first and unconditionally, because its height never
	// changes: a field that moved because the terminal got shorter would be a field
	// the reader could lose mid-query.
	s.drawQuery(buf, inner)

	if s.lay.show[bandResults] && !s.lay.tableR.Empty() {
		s.results.SetBounds(s.lay.tableR)
		s.results.Draw(buf)
	}
	if s.lay.show[bandDetail] && !s.lay.detailR.Empty() {
		s.drawDetail(buf, s.lay.detailR)
	}

	s.drawChrome(buf, inner)
}

// solveBands returns the results and detail rectangles for the rows left below the
// query band: SIDE BY SIDE at a wide interior, STACKED otherwise.
//
// It is called from adapt and not from Draw, because layout.Solve allocates a fresh
// slice on every call and the frame path may not. The one layout decision this
// screen makes is expressed as a named constraint list and a named threshold rather
// than as hand-rounded arithmetic.
//
// A solve that returns a zero-width slice is not clipped here, because bandGap is a
// Min rather than a Length and the interior is known to be at least narrowW: the
// width bands never exist, so a Solve here cannot squeeze a band out of existence
// the way a fixed margin could.
func solveBands(inner buffer.Rect, y, rest int, wide, showDetail bool) (table, detail buffer.Rect) {
	if !showDetail {
		return buffer.Rect{X: inner.X, Y: y, W: inner.W, H: rest}, buffer.Rect{}
	}
	if wide {
		xs := layout.Solve(layout.Horizontal, sideBySide(), bandGap, inner.W)
		return buffer.Rect{
			X: inner.X + layout.Offset(xs, bandGap, 0),
			Y: y, W: xs[0], H: rest,
		}, buffer.Rect{
			X: inner.X + layout.Offset(xs, bandGap, 2),
			Y: y, W: xs[2], H: rest,
		}
	}
	h := rest - detailRows - bandGap
	if h < resultMinH {
		h = resultMinH
	}
	return buffer.Rect{X: inner.X, Y: y, W: inner.W, H: h},
		buffer.Rect{X: inner.X, Y: y + h + bandGap, W: inner.W, H: rest - h - bandGap}
}

// drawQuery paints the query band: the focus marker, the label and the field.
//
// The marker is the non-colour focus signal: a glyph in its own column, in the
// same place whichever pane has focus, so the field's left edge cannot move when
// the focus does. It is the catalog's own Select marker rather than a character
// invented here, which is the rule ADR 0008 §2 is about, and its ASCII form is
// the same character, so the marker needs no second rung.
func (s *search) drawQuery(buf *buffer.Buffer, inner buffer.Rect) {
	row := buffer.Rect{X: inner.X, Y: inner.Y, W: inner.W, H: 1}
	buf.FillRect(row, stPanel.Blank())

	mark := ' '
	if s.focus == paneQuery {
		mark = firstRune(form.SelectDefaultMarker)
	}
	buf.Set(inner.X, inner.Y, mark, stAccent)

	// The label starts one cell after the marker, so the two never read as one
	// token: a gutter with no gap makes ">query" look like a label prefixed with a
	// punctuation mark rather than a marker with a label beside it.
	labelX := inner.X + 2
	buf.SetStringIn(labelX, inner.Right(), inner.Y, "query", stHead)

	fieldX := labelX + labelW + 1
	if fieldX >= inner.Right() {
		// Too narrow for a field at all: keep the label and drop the field, which is
		// clipping rather than blanking. Reaching this needs an interior narrower
		// than the label, which the minimum size already excludes — so it is a guard
		// rather than a layout.
		return
	}
	s.query.SetBounds(buffer.Rect{X: fieldX, Y: inner.Y, W: inner.Right() - fieldX, H: 1})
	s.query.Draw(buf)
}

// drawDetail paints the pane: a bordered block whose title is the selected
// article's name, and the body beneath it.
//
// The body is drawn only when there is a selection to describe, because a bordered
// pane with an empty interior reads as a broken widget rather than as "nothing
// selected" — and buildDetail already writes the words that say which it is.
func (s *search) drawDetail(buf *buffer.Buffer, r buffer.Rect) {
	s.detailBlk.SetBounds(r)
	s.detailBlk.Draw(buf)
	in := s.detailBlk.Interior()
	if in.Empty() {
		return
	}
	s.detail.SetBounds(in)
	s.detail.Draw(buf)
}

// drawChrome paints the two rows below the bands: the hint and the status.
//
// The hint is ABOVE the status because the bindings are what a reader looks for
// first and the provenance is what they look for when something is wrong.
func (s *search) drawChrome(buf *buffer.Buffer, inner buffer.Rect) {
	s.hint.SetBounds(buffer.Rect{X: inner.X, Y: inner.Bottom() - chromeRows, W: inner.W, H: 1})
	s.hint.Draw(buf)
	s.status.SetBounds(buffer.Rect{X: inner.X, Y: inner.Bottom() - 1, W: inner.W, H: 1})
	s.status.Draw(buf)
}

// enough reports whether r is large enough for the layout at all.
func (s *search) enough(r buffer.Rect) bool {
	m := s.MinSize()
	return r.W >= m.W && r.H >= m.H
}

// drawTooSmall paints the one-line diagnostic. The whole rect was repainted by
// Draw before this was called, because the renderer never clears.
func (s *search) drawTooSmall(buf *buffer.Buffer, r buffer.Rect) {
	s.diag.SetBounds(buffer.Rect{X: r.X, Y: r.Y, W: r.W, H: 1})
	s.diag.Draw(buf)
}

// adapt derives the layout from the interior, or returns the existing one when the
// interior is unchanged.
//
// It is the one place in this file that allocates — geometry.Budget returns a
// fresh []bool — and it allocates once per rect, not once per frame, which is what
// keeps the frame-path allocation test at zero. This is ADR 0007 §3's whole
// mechanism: the check IS the computation, so a widget cannot forget to re-adapt.
func (s *search) adapt(inner buffer.Rect) {
	if s.lay.valid && s.lay.key == inner {
		return
	}
	lay := &s.lay
	lay.key = inner
	lay.valid = true
	lay.wide = inner.W >= wideW

	// The budget's input is the bands that are not pinned: the two rows of chrome
	// are subtracted here rather than declared in the table, so the budget and
	// MinSize count the same rows from the same constants.
	copy(lay.show[:], geometry.Budget(s.regions[:], inner.H-chromeRows))

	// The detail pane's own floor, in the stacked arrangement. geometry.Budget says
	// whether the requested rows are AFFORDABLE; this says whether what would be left
	// for the table is worth its border. Below the table's own floor the pane is
	// dropped, because taking seven rows from a table that has four would leave the
	// answer to the query unreadable.
	//
	// The check does not apply side by side: there the pane shares the height with
	// the table rather than competing for it, so both get all of it.
	if !lay.wide && lay.show[bandDetail] &&
		inner.H-chromeRows-queryH-bandGap-detailMinH < resultMinH {
		lay.show[bandDetail] = false
	}

	// The two bands' rectangles, solved here rather than in Draw because layout.Solve
	// allocates a fresh slice on every call.
	y := inner.Y + queryH
	lay.tableR, lay.detailR = solveBands(inner, y, inner.Bottom()-chromeRows-y,
		lay.wide, lay.show[bandDetail])
}

// Handle offers an event to the screen.
//
// It is the fall-through path ADR 0009 §2 specifies: the event loop offers every
// event to the registry FIRST, and calls this only when nothing claimed it. By
// then every key this screen answers to as a command is already gone, so what
// arrives here is a key one of the catalog widgets wants, an event no widget wants,
// or a paste.
//
// The routing is one line per case and it is the whole of the interaction design:
// widgets own their keys and the screen owns the routing. Neither half knows about
// the other, which is why adding a pane to this screen means adding it to the ring
// and nothing else.
//
// The caller must invoke this on the render goroutine: Handle mutates the focus
// index, and Draw reads it.
func (s *search) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventResize:
		s.SetBounds(buffer.Rect{W: ev.Size.W, H: ev.Size.H})
		return true
	case termmosaic.EventMouse:
		return s.afterWidget(s.handleMouse(ev))
	case termmosaic.EventKey:
		// The one case worth reading. Up and Down arrive HERE whenever the table
		// has focus, because the registry's arrow bindings are gated off by their
		// Enabled in that pane — and the table moves its selection by a row. That is
		// the whole mechanism by which a screen-scoped binding "releases" a key, and
		// it is why this screen never binds a key a focused widget wants.
		return s.afterWidget(s.focusedWidget().Handle(ev))
	default:
		// A paste goes straight to the tree and never through Dispatch (ADR 0009 §2
		// step 1), which is how a paste stays ONE undoable operation in the field
		// rather than being re-expanded into a resolution loop per character.
		w := s.focusedWidget()
		if w == nil {
			return false
		}
		return s.afterWidget(w.Handle(ev))
	}
}

// afterWidget reports whatever the widget reported, having first given the detail
// pane a chance to catch up with the selection.
//
// It exists because the pane's subject is a widget's state: the table owns the
// selected row, and the screen owns the pane that describes it. Nothing tells the
// screen that the selection moved except this method, because the table has no way
// to know a pane exists. Calling it here — once, on the path every interaction takes
// — is what keeps the pane describing the row the reader is actually on, and
// syncDetail's integer key means an interaction that did not move the selection costs
// one comparison.
func (s *search) afterWidget(handled bool) bool {
	if handled {
		s.syncDetail()
	}
	return handled
}

// handleMouse offers the event to every focusable widget and takes focus from the
// one that consumed it: a click on a table row both selects the row and makes the
// table the keyboard's target, and a click on the field places the caret and takes
// the keyboard. Neither is something a widget can do on its own.
//
// The order is the reading order, so an event that two widgets could claim — none
// currently overlap — goes to the one a reader would have aimed at.
func (s *search) handleMouse(ev termmosaic.Event) bool {
	for i, w := range []termmosaic.Widget{s.query, s.results} {
		if w.Handle(ev) {
			s.setFocus(i)
			return true
		}
	}
	return false
}

// focusedWidget returns the pane holding focus, or nil when the ring is empty.
//
// It is a switch rather than a slice index so that a pane added without a case
// here fails in review rather than returning nil at runtime.
func (s *search) focusedWidget() termmosaic.Widget {
	switch s.focus {
	case paneQuery:
		return s.query
	case paneResults:
		return s.results
	default:
		return nil
	}
}

// moveFocus moves the focus by delta panes, wrapping at both ends.
//
// Wrapping rather than clamping, because a focus ring that stops at the end is a
// dead end: with two panes, clamping would make Tab a no-op on the second one and
// the reader would conclude the key was broken. The modulo is guarded for an empty
// ring, which would otherwise divide by zero.
func (s *search) moveFocus(delta int) {
	if numPanes == 0 {
		return
	}
	s.setFocus(((s.focus+delta)%numPanes + numPanes) % numPanes)
}

// setFocus focuses the i-th pane and unfocuses the rest.
//
// A widget that keeps its selection while unfocused is what makes tabbing back to
// the results table useful, and SetFocused on both is what moves the ring's
// NON-COLOUR signal: the field's caret appears and the gutter marker appears, and
// the table's selection marker stays where the reader left it. Colour is not
// involved in any of that.
//
// It also rebuilds the hint, because the hint is the FOCUSED pane's bindings and
// the panes do not have the same live ones. That rebuild is on the interaction
// path, where an allocation costs nothing, and it filters on Registry.Has — the
// same predicate Dispatch uses — so the line cannot promise a key that does
// nothing.
func (s *search) setFocus(i int) {
	if i < 0 || i >= numPanes {
		return
	}
	s.focus = i
	s.query.SetFocused(i == paneQuery)
	s.results.SetFocused(i == paneResults)
	s.hint.SetEntries(s.hintEntries())
	// The detail pane's layout depends on the focus only through its title, but the
	// marker beside the field and the table's own chrome do not move, so the cache
	// is dropped rather than reasoned about: one invalidation per key press, costing
	// nothing because adapt allocates once per rect and not per frame.
	s.lay.valid = false
	s.syncDetail()
}

// Compile-time proof that the screen is Minimizable. It is what lets a caller ask
// the screen what it needs, which is the whole of what the framework offers an
// application facing a too-small terminal.
var _ termmosaic.Minimizable = (*search)(nil)

// asciiRung is the one boolean every widget in this example passes to buffer's
// shared ASCII switch, set from the terminal's negotiated caps at construction.
// Nothing in the widget path branches on caps directly (ADR 0008 Decision 4).
var asciiRung bool

// setRung records the terminal's Unicode capability for the whole example.
func setRung(unicode bool) { asciiRung = !unicode }
