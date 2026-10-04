// Command hello is TermMosaic's runnable example: it clears the screen, draws a
// bordered block that fills the terminal, shows a live frame counter and the
// negotiated colour depth, and exits on q.
//
// It is also the project's smoke test against a real terminal. The golden tests
// beside it render exactly this widget into a MemorySink and compare the result
// against checked-in files, which is the regression net for the buffer, the
// diff, the layout solver, the renderer and the encoder with no terminal
// involved at all.
//
// Input goes through the real input subsystem: one goroutine over
// source.Events() receives keys, mouse events and resizes on one ordered
// channel, instead of the two-goroutine input-plus-resize dance and the raw-byte
// scan for 'q' this example used before ADR 0005. Everything about how those
// bytes got decoded is the input package's business, not the example's.
//
// # What is interactive here
//
// The fact grid is a real focus ring, not decoration. The arrows, Tab and
// Home/End move a focus marker through the facts, and the focused fact is marked
// BOTH ways — a marker glyph in its own gutter and reverse video — so the focus is
// legible on a monochrome terminal and in a golden file, neither of which can see
// a colour. '?' reveals two rows of key help. Every one of those keys is decoded
// by the real input decoder in the tests, so a test cannot pass on an event no
// terminal would ever send.
//
// The focus state is MUTATED only on the render goroutine: the input goroutine
// posts the event through render.Renderer.Post rather than calling Handle itself,
// because Handle writes the same fields Draw reads and those are different
// goroutines (ADR 0003).
//
// # Keys
//
//	arrows          move the focus marker through the facts
//	tab / shift-tab next / previous fact
//	home / end      first / last fact
//	?               show or hide the key help
//	q / esc / ctrl-c quit
//
// # What "responsive" means here
//
// The block used to ask for a fixed 46x9 and clamp it to the screen, so a
// 200x60 terminal still showed a 46x9 box floating in the middle. A clamp is not
// a responsive layout (ADR 0007), and it is the exact anti-pattern ADR 0007 was
// written about. This version fills the terminal and spends whatever space it
// has on four genuinely different arrangements:
//
//   - one column below twoColW interior cells, two up to threeColW, three above
//     it — the column boundaries come from layout.Solve, not from hand-rounding
//   - a tagline row that truncates with an ellipsis when it does not fit and
//     reads in full when it does
//   - a two-row explanatory paragraph that is DROPPED, lowest priority first,
//     when the terminal is too short to hold it (geometry.Budget)
//   - a two-row help panel that '?' reveals and that is dropped before the
//     paragraph, because a key the user has not found yet is worth less than the
//     explanation of what they are looking at
//   - a "press q" hint pinned to the bottom row, which is PrioAlways and is
//     therefore never the thing that gets dropped
//
// Below MinSize the block says so in one line instead of clipping into
// nonsense. At 0x0, 1x1 and every other degenerate size it does nothing at all,
// because ADR 0007 §4 makes that a contract rather than a hope.
//
// The border and the title belong to widgets/block, which is the catalog's only
// owner of both (ADR 0008 §2), and the tagline, the note and the hint are
// widgets/basic. This example composes the catalog and invents no chrome, no
// text widget and no threshold of its own except the two column breakpoints,
// which ADR 0007 §1 rule 5 says are local named constants.
package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/input"
	"github.com/serkanalgur/termmosaic/layout"
	"github.com/serkanalgur/termmosaic/render"
	"github.com/serkanalgur/termmosaic/term"
	"github.com/serkanalgur/termmosaic/widgets/basic"
	"github.com/serkanalgur/termmosaic/widgets/block"
	"github.com/serkanalgur/termmosaic/widgets/form"
)

// The example's palette: four plain colours. This is an application's styling
// decision, which is where ADR 0008 puts it — the framework ships no default
// colours at all, only attribute-only named styles.
var (
	bg      = buffer.NewColour(0x10, 0x14, 0x1c)
	fg      = buffer.NewColour(0xd8, 0xdc, 0xe4)
	titleFg = buffer.NewColour(0x30, 0xc0, 0x80)
	dim     = buffer.NewColour(0x60, 0x6a, 0x7a)
	edge    = buffer.NewColour(0x30, 0x36, 0x40)
)

// The styles the example draws with, each built through NewStyle rather than a
// composite literal: a partial literal would silently leave a channel at opaque
// black, which is Style's documented footgun.
var (
	stBody   = buffer.NewStyle(fg, bg, 0)
	stMuted  = buffer.NewStyle(dim, bg, 0)
	stValue  = buffer.NewStyle(fg, bg, 0)
	stAccent = buffer.NewStyle(titleFg, bg, 0)
	stTitle  = buffer.NewStyle(titleFg, bg, buffer.AttrBold)
	stEdge   = buffer.NewStyle(edge, bg, 0)
)

// The example's own responsive thresholds and geometry. ADR 0007 §1 rule 5 is
// explicit that these are local named constants and NOT framework vocabulary: a
// size class or a shared breakpoint constant would be a second source of truth
// for space that already has one, and the thresholds are a product decision
// rather than a framework fact.
const (
	// screenMargin is the empty border drawn around the block on both axes. It is
	// a margin rather than a size, so it is expressed as Min: a margin yields
	// when the terminal is narrower than the margin itself, where a Length
	// would overflow the axis and hand the composition an off-screen rect.
	screenMargin = 1

	// minCols and maxCols bound the fact grid's column count.
	minCols = 1
	maxCols = 3

	// colGap is the cells between two fact columns.
	colGap = 2

	// focusCol is the gutter in front of every fact's label: the marker cell and
	// the space that separates it from the label.
	//
	// It is a GUTTER and not part of the label because a marker that shifted the
	// label would move every value in the column with it, and a fact grid whose
	// values step sideways when the focus moves is worse than one with no marker
	// at all. It is two cells rather than one because a marker hard against the
	// label reads as a prefix on the label, which is exactly the reading the
	// gutter exists to prevent.
	//
	// The gutter is also what makes focus a non-colour signal: the focused fact is
	// marked by a shape in its own column, which survives NO_COLOR and a
	// monochrome terminal, where a colour alone would not.
	focusCol = 2

	// labelCol is where a fact's label starts, relative to its column: just past
	// the focus gutter.
	labelCol = focusCol

	// valueCol is where a fact's value starts, relative to its column. It is the
	// focus gutter plus the longest label ("frame") plus one space, so values line
	// up down the column rather than stepping with their labels.
	valueCol = labelCol + 6

	// helpRows is how many rows the key help asks for. It is two because the
	// bindings are grouped into two lines — navigation on one, the rest on the
	// other — and a single line long enough to hold them all would be truncated at
	// every width the example is used at.
	helpRows = 2

	// twoColW and threeColW are the INTERIOR widths at which the fact grid gains
	// a column. They are interior rather than bounds widths because the interior
	// is the space the grid is actually laid out in.
	//
	// They are chosen so the two arrangements differ visibly: one column stacks
	// "frame" above "depth", two puts "cols" beside "frame", three puts all three
	// on one row. A breakpoint that only changed a padding would not demonstrate
	// anything. twoColW is set above the narrowest interior MinSize permits, so
	// every band is reachable: the one-column band spans screen widths 40 to 45
	// and is therefore covered by a golden rather than being dead code.
	twoColW   = 40
	threeColW = 60

	// minW and minH are the block's smallest meaningful size, chrome included,
	// and are what MinSize reports. They are the point at which the fact grid,
	// the hint and the border all still fit.
	minW = 38
	minH = 8

	// noteRows is the height the explanatory paragraph asks for. It asks for two
	// rather than one because it WRAPS: at a narrow width it needs the second
	// row, and at a wide one it has a row to spare, which is the honest way to
	// express "this text is worth two rows".
	noteRows = 2
)

// tooSmallText is what the block says instead of clipping into nonsense when
// Bounds() is below MinSize.
//
// It is a constant rather than something formatted per frame, for the same
// reason the frame counter is written digit by digit: fmt.Sprintf allocates, and
// the draw path does not. It is also deliberately short — it has to be readable
// at the sizes where it is the ONLY thing on screen, so the message that would
// most need explaining is the one that cannot afford to explain itself.
const tooSmallText = "too small — needs 38x8"

// truncMark is the ellipsis the range-clipped writers take as their truncation
// marker. It is derived from buffer's own TruncSuffix rather than spelled as a
// literal, so the one-cell marker is the same character the rest of the
// framework uses and this file contains no glyph of its own.
var truncMark = []rune(buffer.TruncSuffix)[0]

// columnNames are the values the "columns" fact shows. They are indexed by
// column count minus one, so the value is a table lookup rather than a
// formatted string — a fact whose value changed on resize would otherwise force
// either an allocation per frame or a cache invalidated by something other than
// the rect.
var columnNames = [...]string{"1 column", "2 columns", "3 columns"}

// factKind says where a fact's value comes from. The two cases are the two
// reasons a value cannot be a field: it changes every frame, or it is derived
// from the layout.
type factKind uint8

const (
	// staticValue draws value, which never changes for the life of the widget.
	staticValue factKind = iota
	// liveFrame draws the frame counter, digit by digit.
	liveFrame
)

// fact is one label/value row of the grid. value is the static text, and live
// selects what to draw instead when the value is not static.
type fact struct {
	label string
	value string
	st    buffer.Style
	live  factKind
}

// factCols is the index of the one fact whose value is derived from the layout.
// It is named because adapt writes to it, and an index literal there would be a
// silent break the day a fourth fact is added above it.
const factCols = 2

// tagline is the one-line summary that truncates with an ellipsis when the
// block is narrow and reads in full when it is not. At the narrowest useful
// width it is cut short, which is the point: a hello example that only ever
// prints text which already fits has not demonstrated responsiveness.
const tagline = "hello from termmosaic — resize this terminal"

// noteText is the paragraph revealed when there is height for it and dropped
// when there is not.
const noteText = "the layout above is recomputed only when Bounds() changes, so a resize repaints in full and a steady frame costs nothing."

// The two help lines, and the bindings each one names.
//
// They are constants rather than something built from a key table because the
// example's whole point on this axis is that the text on screen and the keys
// Handle accepts cannot drift apart silently — and a table of bindings that a
// test then checks against Handle is the mechanism that makes that true. helpNav
// and helpExit below are the same two lines.
const (
	helpNav  = "move  arrows, tab, shift-tab, home, end"
	helpExit = "help  ?            quit  q, esc, ctrl-c"
)

// The hint pinned to the bottom row.
//
// It leads with the quit binding because that is the one a first-time reader is
// looking for, and it names '?' rather than spelling out the navigation, because
// the navigation is on the other side of a key press and this line is the only
// thing guaranteed to be on screen.
//
// The goldens are the regression net for it: TestGoldenScreen and the per-size
// files contain this exact string, so changing a binding without changing this
// line fails the diff rather than leaving a lie on screen.
const hintText = "press q to quit  ·  ? keys  ·  arrows move focus"

// Region indices into the budget table, in draw order.
//
// The help sits between the tagline and the note on purpose. It is PrioNormal, so
// within that priority the DECLARATION ORDER is the tiebreak and Budget keeps the
// tagline before the help — while both still outrank the PrioLow note. That is the
// ranking the priorities are for: an explanation of what you are looking at is
// worth less than the keys that move it, and both are worth more than a paragraph
// the reader has already seen once.
const (
	regionFacts = iota
	regionTagline
	regionHelp
	regionNote
	regionHint
	numRegions
)

// bodyLayout is everything this example derives from its rectangle: the fact
// grid's column geometry and the Budget answer for the current height.
//
// Every field in it is a function of the interior rectangle and of nothing else,
// which is what makes keying the cache on that rectangle sufficient (ADR 0007
// §3, and its amendment about caches keyed on the wrong thing): the depth, the
// frame counter and the region priorities are either drawn live every frame or
// never change.
type bodyLayout struct {
	// key is the interior this layout was derived from. valid distinguishes
	// "derived from the zero rect" from "derived from a zero-size rect", which
	// are the same value and different answers.
	key   buffer.Rect
	valid bool

	// cols is the fact grid's column count.
	cols int
	// factRows is how many rows the grid occupies, which is how many facts each
	// column holds.
	factRows int
	// colX and colW are the column offsets and widths, from layout.Solve. They are
	// arrays rather than a slice because the count is a compile-time constant
	// and the layout struct is a field of a widget that must not allocate.
	colX [maxCols]int
	colW [maxCols]int
	// show is geometry.Budget's answer: one entry per region, true to draw it.
	show []bool
}

// hello is the example's widget: a Block for the chrome, three composed text
// widgets, and a body that changes shape as the terminal does.
//
// The Block is a value field rather than a pointer because it has no identity: it is
// chrome, and a widget tree holding a pointer to a border would be one more thing to
// keep alive for no reason.
type hello struct {
	bounds buffer.Rect
	// blk draws the border, the background and the title, and reports the interior
	// rectangle the body draws into. It is the only thing in this file that knows
	// what a border is.
	blk block.Block
	// depth is the colour depth the renderer negotiated. It is shown in the body
	// so the degradation ladder is visible in a real terminal.
	depth buffer.ColourDepth
	// ticks counts the frames drawn, so the block has something dynamic for the
	// diff to skip past.
	ticks int

	// facts is the grid's content, built once at construction because two of its
	// three rows are static and the third is drawn live.
	facts []fact
	// regions is the vertical budget table, built once at construction for the
	// same reason. Only the facts region's size varies, and that is patched in
	// when the layout is derived rather than rebuilt.
	regions [numRegions]geometry.Region

	// tagline, note and hint are widgets/basic widgets, so the example composes
	// the catalog for its prose instead of owning a text renderer. They carry the
	// block's background, without which composing them would paint the block's
	// own background away.
	tagline *basic.Text
	note    *basic.Paragraph
	hint    *basic.Text

	// help is the key help '?' toggles. It is a pair of widgets/form KeyHints rather
	// than two more basic.Texts because a key hint is a widget the catalog already
	// owns, it already truncates through buffer's shared writers with a marker
	// rather than a clip, and reusing it is what stops the example from growing a
	// second spelling of "here are the keys".
	help *helpPair

	// focus is which fact the keyboard is aimed at, and helpOpen whether the key
	// help is showing. Both are mutated only through Handle, which the application
	// calls on the render goroutine — see the file comment on why that matters.
	focus    int
	helpOpen bool

	// lay is the derived layout, cached against the interior it came from.
	lay bodyLayout

	// tooSmall is the diagnostic span, built once at construction. A composite
	// literal inside Draw would allocate on a path that otherwise does not, and
	// the too-small path is drawn every frame for as long as the terminal stays
	// small, so it is the one place an allocation would be least excusable.
	tooSmall []buffer.Span
}

// newHello returns the example's widget with its chrome and its content
// configured.
//
// Every size-independent thing is built here rather than in Draw: the title
// spans, the tagline, the note, the hint, the fact list and the budget table.
// The block's cached truncation and the body layout are the only size-derived
// values, and both are recomputed from Draw's own rect check (ADR 0007 §3).
func newHello(r buffer.Rect, depth buffer.ColourDepth) *hello {
	h := &hello{bounds: r, depth: depth}

	// The Block is configured ONCE, here: the title is constant, so the span
	// slice and the truncated form are built at construction and the draw path
	// only reads them (ADR 0008 §4).
	h.blk = *newChrome(r)

	// The title is "termmosaic", not " termmosaic ": Block inserts the one space
	// on each side itself, and a caller that added them would shift the title two
	// columns right of where the border's interior says it belongs.
	h.blk.SetTitleString("termmosaic", stTitle)

	h.facts = []fact{
		{label: "frame", st: stValue, live: liveFrame},
		{label: "depth", value: depth.String(), st: stValue, live: staticValue},
		{label: "cols", value: columnNames[0], st: stAccent, live: staticValue},
	}
	// Declared priorities, once. The order is the draw order, and within a
	// priority Budget keeps declaration order — so the facts go before the
	// tagline, which goes before the note, and the hint is never a candidate.
	h.regions[regionFacts] = geometry.Region{Size: len(h.facts), Prio: geometry.PrioHigh}
	h.regions[regionTagline] = geometry.Region{Size: 1, Prio: geometry.PrioNormal}
	// The help's SIZE is zero while it is closed, and adapt patches it when '?'
	// toggles. A region that asked for its rows while hidden would push the note
	// down for content nobody can see, so the two-row gap would be the visible
	// cost of a closed help.
	//
	// That makes the budget's answer depend on a mode as well as on the rectangle,
	// which is why toggling invalidates the layout cache: it is the same
	// "a field the layout depends on changed" rule SetFrame follows in markets, and
	// keeping the cache keyed on the rect alone would leave a stale answer behind.
	h.regions[regionHelp] = geometry.Region{Size: 0, Prio: geometry.PrioNormal}
	h.regions[regionNote] = geometry.Region{Size: noteRows, Prio: geometry.PrioLow}
	h.regions[regionHint] = geometry.Region{Size: 1, Prio: geometry.PrioAlways}

	h.tagline = basic.NewTextString(buffer.Rect{}, tagline, stMuted)
	h.tagline.SetBackground(stBody)
	h.note = basic.NewParagraphString(buffer.Rect{}, noteText, stMuted)
	h.note.SetBackground(stBody)
	h.hint = basic.NewTextString(buffer.Rect{}, hintText, stAccent)
	h.hint.SetBackground(stBody)

	// The help is two rows of a KeyHint, which is a ONE-LINE widget, so the two
	// lines are two KeyHints: a second kind of help widget would have been a second
	// spelling of the same thing. They are held as a pair because they are always
	// shown and always hidden together.
	h.help = newHelpPair(stMuted, stAccent, stBody)

	h.tooSmall = []buffer.Span{buffer.NewSpan(tooSmallText, stMuted)}

	return h
}

// newChrome returns the block's chrome — border, background, padding — for a
// rectangle r. It is separate from newHello because a test that wants the
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

// Bounds returns the block's rectangle.
func (h *hello) Bounds() buffer.Rect { return h.bounds }

// MinSize returns the smallest rectangle in which the block says something
// worth reading: its border, its title, the three facts and the pinned hint.
//
// It includes the chrome, which is the convention termmosaic.Minimizable pins
// for the whole catalog. It is pure, reads no state Draw mutates, and is safe
// before the first draw — which is why hello satisfies Minimizable at all:
// ADR 0007 §4 asks a widget to be total at 1x1, and a widget that can say "I
// need 38x8" is the only way an application can decide what to do about the
// sizes where it cannot.
func (h *hello) MinSize() buffer.Size { return buffer.Size{W: minW, H: minH} }

// Invalidate satisfies termmosaic.Widget. The block redraws in full every frame,
// so there is nothing finer-grained to mark.
func (h *hello) Invalidate() {}

// Draw paints the block.
//
// It runs every frame, as ADR 0003 specifies: widgets describe themselves on
// demand and are not required to implement incremental drawing, because Go cannot
// enforce invalidation discipline and silent invalidation bugs are the worst failure
// mode a TUI has.
//
// It allocates nothing in steady state. Everything derived from the rectangle —
// the column geometry, the budget answer, the tagline's truncation and the note's
// wrap — is computed on the first Draw at a new size and cached against that
// rectangle, which is ADR 0007 §3's lazy rule applied to the whole body. The only
// per-frame work is the frame counter, written digit by digit.
func (h *hello) Draw(buf *buffer.Buffer) {
	r := h.bounds
	if r.Empty() {
		// The degenerate-size contract: an empty Bounds returns immediately
		// (ADR 0007 §4).
		return
	}
	if !h.enough(r) {
		// Below MinSize. Say so in one line rather than drawing a clipped
		// fraction of a layout: the alternative is a screen that looks like a bug
		// rather than like an answer.
		h.drawTooSmall(buf, r)
		return
	}

	// The Block is told the bounds on every frame rather than only at construction:
	// the application recomputes them on a resize, and a Block whose rectangle were
	// stale would paint chrome in the wrong place (ADR 0007 §3).
	h.blk.SetBounds(r)
	h.blk.Draw(buf)

	// The body draws into the Block's interior, so it cannot touch the border, the
	// title or the padding however the block is resized.
	inner := h.blk.Interior()
	if inner.Empty() {
		return
	}

	// Derive the body from the rect, or reuse what was derived from the same one.
	h.adapt(inner)
	a := &h.lay

	y := inner.Y
	if a.show[regionFacts] {
		h.drawFacts(buf, inner, y, a.factRows)
		y += a.factRows
	}
	if a.show[regionTagline] {
		row := buffer.Rect{X: inner.X, Y: y, W: inner.W, H: 1}
		h.tagline.SetBounds(row)
		h.tagline.Draw(buf)
		y++
	}
	// Both halves of the test, because Budget reports a zero-size region as KEPT
	// (dropping it would not help), so the budget's answer alone cannot say whether
	// the help is open. The flag is the mode; the budget only says whether the two
	// rows it would need are affordable.
	if h.helpOpen && a.show[regionHelp] {
		h.help.Draw(buf, buffer.Rect{X: inner.X, Y: y, W: inner.W, H: helpRows})
		y += helpRows
	}
	if a.show[regionNote] {
		// The paragraph gets the rows the budget kept for it and no more, and it
		// shows the top of its own wrap: it wraps at this width, so at a narrow one
		// it has more lines than rows and the tail is clipped rather than blanked.
		h.note.SetBounds(buffer.Rect{X: inner.X, Y: y, W: inner.W, H: noteRows})
		h.note.Draw(buf)
		y += noteRows
	}
	if a.show[regionHint] {
		// Pinned to the bottom row rather than stacked below the body, so it is
		// visible at every height instead of being the first thing a grow would
		// reveal. Budget already counts it, so this does not double-spend a cell.
		h.hint.SetBounds(buffer.Rect{
			X: inner.X,
			Y: inner.Bottom() - 1,
			W: inner.W,
			H: 1,
		})
		h.hint.Draw(buf)
	}
}

// enough reports whether r is large enough for the layout at all.
func (h *hello) enough(r buffer.Rect) bool {
	m := h.MinSize()
	return r.W >= m.W && r.H >= m.H
}

// drawTooSmall paints the one-line diagnostic for a block with no room.
//
// The whole rect is repainted first, not just the line: the renderer diffs and
// never clears, so a block that drew three facts at 60x14 and then a message at
// 40x10 would otherwise leave the third fact on screen (ADR 0007 §1 rule 3).
func (h *hello) drawTooSmall(buf *buffer.Buffer, r buffer.Rect) {
	buf.FillRect(r, stBody.Blank())
	buf.SetSpansCappedIn(r.X, r.Right(), r.Y, h.tooSmall, truncMark)
}

// drawFacts paints the label/value grid into the first rows rows of the interior,
// arranged in the column geometry adapt derived.
//
// Facts are laid out column-major: fact i goes in column i/rows at row i%rows,
// which is what makes a two-column arrangement put "frame" beside "depth" rather
// than stacking them and leaving the right column empty.
func (h *hello) drawFacts(buf *buffer.Buffer, inner buffer.Rect, y, rows int) {
	// The counter advances here rather than at the top of Draw, so it counts the
	// frames on which the fact grid was actually drawn. A frame spent on the
	// too-small diagnostic is not a frame of the example's body, and counting it
	// would make the number change for a reason the user cannot see.
	h.ticks++
	a := &h.lay
	for i := range h.facts {
		col := i / rows
		if col >= a.cols {
			break
		}
		fy := y + i%rows
		if fy >= inner.Bottom() {
			// The budget asked for rows that do not exist, which happens when
			// PrioAlways content alone overflows and Budget reports everything
			// kept (ADR 0007 §4). Clip rather than draw past the interior.
			break
		}
		x := inner.X + a.colX[col]
		x1 := x + a.colW[col]
		// The focus marker goes in the gutter, which is drawn for EVERY fact rather
		// than only the focused one. A gutter that appears when the focus moves
		// would shift every label and every value in the column to the right by one
		// cell, so the grid would reflow on the first key press.
		h.drawFocusMark(buf, x, x1, fy, i == h.focus)
		buf.SetStringIn(x+labelCol, x1, fy, h.facts[i].label, stMuted)
		vx := x + valueCol
		if vx >= x1 {
			// Too narrow for a value at all: keep the label and drop the value,
			// which is clipping rather than blanking.
			continue
		}
		switch h.facts[i].live {
		case liveFrame:
			// The frame counter is written digit by digit rather than formatted
			// into a string. fmt.Sprintf allocates, and one allocation per frame is
			// exactly what the 0-allocs draw path forbids; this is the same hazard
			// ADR 0008 §4 names for Wrap and Truncate, one function call earlier in
			// the pipeline.
			h.writeIntIn(buf, vx, x1, fy, h.ticks)
		default:
			buf.SetStringIn(vx, x1, fy, h.facts[i].value, h.facts[i].st)
		}
	}
}

// drawFocusMark paints the focus marker in the gutter in front of fact i's label.
//
// The unfocused state is a SPACE rather than nothing at all, for the reason given
// at the call site: the gutter's width must not depend on where the focus is, or
// the grid would shift under a key press. It is the reason the marker lives in its
// own column instead of being prefixed to the label.
func (h *hello) drawFocusMark(buf *buffer.Buffer, x, x1, y int, focused bool) {
	if x >= x1 {
		// No gutter in this column, which happens when the column is narrower than
		// the marker. Clip: a marker drawn one cell past the column's edge would
		// overwrite the gap or the neighbouring column.
		return
	}
	r := ' '
	if focused {
		r = rune(focusMark[0])
	}
	buf.Set(x, y, r, stAccent)
}

// adapt derives the body layout from the block's interior, or returns the
// existing one when the interior is unchanged.
//
// This is ADR 0007 §3's whole mechanism: no fourth Widget method, no reflow
// pass, no invalidation to forget. The check IS the computation, so a widget
// cannot forget to re-adapt; it can only adapt one frame late, which the next
// frame repairs.
//
// It is the one place in this file that allocates — layout.Solve returns a fresh
// slice and geometry.Budget a fresh []bool — and it allocates once per rect, not
// once per frame, which is what keeps TestDrawIsAllocationFree at zero.
func (h *hello) adapt(inner buffer.Rect) {
	if h.lay.valid && h.lay.key == inner {
		return
	}
	a := &h.lay
	a.key = inner
	a.valid = true

	a.cols = columnsFor(inner.W)
	sizes := layout.Solve(layout.Horizontal, layout.Split(a.cols), colGap, inner.W)
	for i := 0; i < a.cols; i++ {
		a.colX[i] = layout.Offset(sizes, colGap, i)
		a.colW[i] = sizes[i]
	}
	a.factRows = (len(h.facts) + a.cols - 1) / a.cols

	// The facts region's size is the only part of the table that depends on the
	// rect, so it is patched rather than the table rebuilt.
	h.regions[regionFacts].Size = a.factRows

	// The help's size is patched rather than the table rebuilt, and it is patched
	// on the focus path too — see toggleHelp, which invalidates the cache so this
	// re-runs after a '?'.
	h.regions[regionHelp].Size = 0
	if h.helpOpen {
		h.regions[regionHelp].Size = helpRows
	}

	a.show = geometry.Budget(h.regions[:], inner.H)

	// The one fact whose value depends on the layout is the one reporting the
	// layout. It is a table lookup rather than a formatted string so the frame
	// path stays allocation-free and so nothing cached here depends on anything
	// but the rect.
	h.facts[factCols].value = columnNames[a.cols-minCols]
}

// columnsFor returns the fact grid's column count for an interior width.
//
// The thresholds are the two named constants at the top of this file, and the
// count is clamped to [minCols, maxCols] so a pathological interior cannot ask
// for zero columns (which would divide by zero in adapt) or for more columns
// than there are facts (which would leave empty columns on screen).
func columnsFor(innerW int) int {
	cols := minCols
	for _, threshold := range [...]int{twoColW, threeColW} {
		if innerW < threshold {
			break
		}
		cols++
	}
	if cols > maxCols {
		cols = maxCols
	}
	return cols
}

// rootBounds returns the example's block rectangle inside an sw-by-sh screen.
//
// It is expressed as two layout.Solve calls — one per axis — because that is the
// documented path rather than the anti-pattern ADR 0007 was written about. The
// centred(sw, sh, 46, 9) this replaces hard-coded the block's size, clamped it by
// hand, and used centre-of-screen arithmetic that no amount of shrinking made
// correct. It also never GREW: on a 200x60 terminal it produced the same 46x9
// box, which is the user's actual complaint. Fill(1) in the middle is what makes
// the block take every cell the margins do not, so it grows as well as shrinks.
//
// The margins are Min rather than Length for the reason given at their
// declaration, and marginFor drops them entirely on an axis too narrow to carry
// them, so Solve can never squeeze the block out of existence. The Clip below
// is then a belt-and-braces for the degenerate sizes ADR 0007 §4 requires to be
// valid.
//
// This is not on the frame path: it is called once at startup and once per
// resize, which is what ADR 0003's dirty-rectangle model makes affordable.
func rootBounds(sw, sh int) buffer.Rect {
	xs := layout.Solve(layout.Horizontal, marginOnAxis(marginFor(sw, screenMargin)), 0, sw)
	ys := layout.Solve(layout.Vertical, marginOnAxis(marginFor(sh, screenMargin)), 0, sh)
	return buffer.Rect{
		X: layout.Offset(xs, 0, 1),
		Y: layout.Offset(ys, 0, 1),
		W: xs[1],
		H: ys[1],
	}.Clip(sw, sh)
}

// marginFor returns the margin to reserve on an axis of avail cells.
//
// It yields to zero when the axis is too narrow to carry both margins AND at
// least one cell of block, which is what Min alone does NOT do: on a 1-wide
// axis Min(1) resolves to 1 on both sides, the two margins overflow the space
// between them, and the block's Fill receives nothing — a 1x1 terminal would
// draw no block at all rather than a 1x1 block.
//
// A margin is decoration, and ADR 0007's rule for a widget is clip rather than
// blank. Collapsing the block to nothing at the sizes where it has least room
// is the same mistake in miniature as the clamp this example was rewritten to
// remove: it optimises for the margin instead of for the content.
func marginFor(avail, margin int) int {
	if avail < 2*margin+1 {
		return 0
	}
	return margin
}

// marginOnAxis returns the constraint list for one axis: a margin, everything
// else, the same margin.
//
// It is a []layout.Constraint rather than three values because the solver's
// input is a list and building one here keeps rootBounds reading as two solves
// rather than as an arithmetic expression pretending to be a layout.
//
// The margins are Min rather than Length for the reason given at their
// declaration, and marginFor has already ensured the axis can carry them, so the
// Fill between them is never squeezed to nothing on a real terminal.
func marginOnAxis(margin int) []layout.Constraint {
	return []layout.Constraint{
		layout.Min(margin),
		layout.Fill(1),
		layout.Min(margin),
	}
}

// writeInt writes v's decimal digits left to right at (x, y) in stValue and
// returns the x coordinate just past them. It is the allocation-free replacement
// for fmt.Sprintf("%d", v) on the draw path.
//
// v is clamped at zero: a negative frame count would need a sign, and no
// meaningful number here is negative, so the extra branch would be dead weight.
func (h *hello) writeInt(buf *buffer.Buffer, x, y, v int) int {
	return h.writeIntIn(buf, x, buf.Width(), y, v)
}

// writeIntIn is writeInt with an explicit right edge at x1, so a number cannot
// run past its own column into the next one. A partially written number is worse
// than none, so it stops at the edge rather than overflowing.
func (h *hello) writeIntIn(buf *buffer.Buffer, x, x1, y, v int) int {
	if v < 0 {
		v = 0
	}
	if x >= x1 {
		return x
	}
	// Walk to the highest decimal place, then step back down emitting digits.
	div := 1
	for d := v / 10; d > 0; d /= 10 {
		div *= 10
	}
	for {
		if x >= x1 {
			return x
		}
		buf.Set(x, y, rune('0'+v/div%10), stValue)
		x++
		if div == 1 {
			return x
		}
		div /= 10
	}
}

// helpPair is the two-row key help: two KeyHints, because KeyHint is a one-line
// widget and a two-line hint needs two of them.
//
// It is a type rather than two fields so that showing and hiding the help is one
// field access and cannot be done halfway — a help with only its first line on
// screen is the kind of half-state that survives a refactor.
type helpPair struct {
	nav, exit *form.KeyHint
}

// newHelpPair builds the two lines.
//
// The bindings are written out here rather than derived from a key table because
// a key table the example also dispatches on would be a second source of truth
// for the key contract, and the whole point of the contract being written down is
// that the two cannot disagree. The tests assert that Handle accepts exactly the
// bindings these two lines name.
func newHelpPair(helpSt, keySt, bg buffer.Style) *helpPair {
	p := &helpPair{
		nav:  form.NewKeyHint(buffer.Rect{}, []form.Binding{{Key: "move", Help: helpNav}}),
		exit: form.NewKeyHint(buffer.Rect{}, []form.Binding{{Key: "help", Help: helpExit}}),
	}
	for _, k := range []*form.KeyHint{p.nav, p.exit} {
		k.KeyStyle = keySt
		k.HelpStyle = helpSt
		k.SeparatorStyle = helpSt
		k.Background = bg
	}
	return p
}

// Draw paints the two lines into the top row of r.
//
// The rect is given rather than remembered so that the two hints cannot disagree
// about where they are: KeyHint caches its line against its own bounds, and two
// widgets handed the same rect here are two caches with one key.
func (p *helpPair) Draw(buf *buffer.Buffer, r buffer.Rect) {
	if r.Empty() {
		return
	}
	first := buffer.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}
	if r.H > 1 {
		p.nav.SetBounds(first)
		p.nav.Draw(buf)
		p.exit.SetBounds(buffer.Rect{X: r.X, Y: r.Y + 1, W: r.W, H: 1})
		p.exit.Draw(buf)
		return
	}
	// One row: the FIRST line only, because the navigation bindings are the ones
	// whose absence leaves the reader stuck rather than merely uninformed.
	p.nav.SetBounds(first)
	p.nav.Draw(buf)
}

// focusMark is the marker drawn in the focused fact's gutter.
//
// It is a plain ASCII character on purpose. Every alternative — a bullet, a
// right-pointing triangle, a block — is a glyph the reader may not have, and the
// one cell the gutter costs is not worth spending on a character set the example
// does not otherwise negotiate. It is also the marker form.Select uses, taken from
// the catalog rather than invented here, which is the rule ADR 0008 §2 is about.
const focusMark = form.SelectDefaultMarker

// FocusIndex reports which fact the keyboard is aimed at, which is what the tests
// assert on after a simulated key and what an application facing a real terminal
// would read to build its own chrome.
func (h *hello) FocusIndex() int { return h.focus }

// HelpOpen reports whether the key help is showing.
func (h *hello) HelpOpen() bool { return h.helpOpen }

// Handle moves the focus marker, toggles the help, and consumes the keys it acts
// on.
//
// It is the whole of the example's key contract, and it is written as a routing
// table rather than as a widget tree's focus ring because there is nothing to
// route: the facts are cells this widget draws itself, not catalog widgets with
// their own Handle. An example that wanted a real focus ring over real widgets
// would compose widgets/form's Select instead, which is what examples/markets
// does for its currency pair.
//
// Two conventions, both about honesty rather than mechanics:
//
//   - A key it does not act on is NOT consumed. Returning true for a key that
//     changed nothing would make the caller believe the example had handled it.
//   - The quit keys ARE consumed even though this method does not quit, because
//     the caller does and would otherwise see a key it had already dealt with.
func (h *hello) Handle(ev termmosaic.Event) bool {
	if ev.Kind != termmosaic.EventKey {
		// A resize belongs to the application, which recomputes bounds and resizes
		// the renderer; a mouse event belongs to nobody here, because the facts are
		// not hit targets and claiming one would be a lie about what is clickable.
		return false
	}
	switch ev.Key {
	case termmosaic.KeyLeft, termmosaic.KeyUp:
		h.moveFocus(-1)
		return true
	case termmosaic.KeyRight, termmosaic.KeyDown:
		h.moveFocus(1)
		return true
	case termmosaic.KeyTab:
		h.moveFocus(1)
		return true
	case termmosaic.KeyBacktab:
		h.moveFocus(-1)
		return true
	case termmosaic.KeyHome:
		h.setFocus(0)
		return true
	case termmosaic.KeyEnd:
		h.setFocus(len(h.facts) - 1)
		return true
	case termmosaic.KeyEscape:
		return true // the caller quits; see run
	}
	// A modified rune is a CHORD, not the bare key, which is why the modifiers are
	// checked rather than only the rune: ctrl-r must not be read as 'r', and a bare
	// 0x03 must not be read as ctrl-c. The decoder has already made that distinction
	// — Ctrl-C arrives as Ctrl+'c' rather than as 0x03 — so the check is on the
	// decoder's own vocabulary rather than on bytes.
	switch {
	case ev.Mod == termmosaic.ModCtrl && (ev.Rune == 'c' || ev.Rune == 'C'):
		return true
	case ev.Mod == 0:
		switch ev.Rune {
		case 'q', 'Q':
			return true // the caller quits; see run
		case '?':
			h.toggleHelp()
			return true
		}
	}
	return false
}

// toggleHelp shows or hides the key help and invalidates the layout cache.
//
// The invalidation is the load-bearing part. The budget's answer depends on
// whether the help is open (adapt sizes its region from the flag), and the cache
// is keyed on the interior rectangle alone — so without this the next Draw would
// reuse the answer computed for the other mode and the screen would show the help
// in a two-row gap that belonged to a note, or leave a gap where the help was.
// That is ADR 0007's amendment about caches keyed on too little, applied to a
// field rather than to a rect.
func (h *hello) toggleHelp() {
	h.helpOpen = !h.helpOpen
	h.lay.valid = false
}

// moveFocus moves the focus by delta facts, wrapping at both ends.
//
// Wrapping rather than clamping, because a focus ring that stops at the end is a
// dead end: with three facts, clamping would make the right arrow a no-op on the
// third fact and the reader would conclude the key was broken. The modulo is
// guarded for an empty list, which is reachable only if every fact is removed and
// which would otherwise divide by zero.
func (h *hello) moveFocus(delta int) {
	n := len(h.facts)
	if n == 0 {
		return
	}
	h.setFocus(((h.focus+delta)%n + n) % n)
}

// setFocus aims the keyboard at fact i, clamped into range.
func (h *hello) setFocus(i int) {
	if len(h.facts) == 0 {
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(h.facts) {
		i = len(h.facts) - 1
	}
	h.focus = i
}

// Compile-time proof that the example's widget is Minimizable. It is what lets a
// caller ask the block what it needs, which is the whole of what the framework
// offers an application facing a too-small terminal.
var _ termmosaic.Minimizable = (*hello)(nil)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hello:", err)
		os.Exit(1)
	}
}

func run() error {
	// Fall back to the headless path when stdout is not a terminal, so the
	// example is runnable in CI and under redirection instead of erroring out.
	if !isTerminal(os.Stdout) {
		fmt.Println("hello: stdout is not a terminal, so there is nothing to draw onto.")
		fmt.Println("hello: run it in a real terminal, or run the golden tests:")
		fmt.Println("           go test ./examples/hello/")
		return nil
	}

	t, err := term.Open(os.Stdin, os.Stdout, os.Getenv)
	if err != nil {
		return err
	}
	// Restoring the terminal is not optional on any exit path: a program that
	// leaves a terminal in raw mode has broken the user's shell.
	defer t.Close()

	w, h := t.Size()
	caps := t.Capabilities()

	r := render.New(term.NewSink(os.Stdout), render.Config{
		Width:   w,
		Height:  h,
		Caps:    caps,
		NoColor: render.NoColorFromEnv(os.Getenv),
	})
	// This application has no text input, so a cursor blinking in the last cell
	// drawn would look like a bug. It is managed-but-hidden: the renderer still
	// positions it, it is just never visible.
	r.SetCursor(render.Cursor{Valid: true, Visible: false})

	root := newHello(rootBounds(w, h), caps.ColourDepth())
	r.SetRoot(root)

	if err := t.EnterRawMode(); err != nil {
		return err
	}
	if err := t.EnterAltScreen(); err != nil {
		_ = t.LeaveRawMode()
		return err
	}
	if err := r.Reset(); err != nil {
		return err
	}

	// quit is closed by whichever goroutine decides the program is finished.
	// Both the input loop and a signal handler may want to stop the program, and
	// closing a channel twice panics, so the close goes through sync.Once.
	quit := make(chan struct{})
	var quitOnce sync.Once
	stop := func() { quitOnce.Do(func() { close(quit) }) }

	// Input. The Source is built AFTER raw mode, because the kitty keyboard query
	// it sends is answered on the input stream and is meaningless without raw
	// mode. WriteProbe is wired to the example's own sink rather than to a method
	// on Terminal, which is the point of ADR 0005 not widening ADR 0001's
	// interface.
	sink := term.NewSink(os.Stdout)
	src := input.NewSource(t, withProbe(input.DefaultConfig(), sink))

	// The body shows a frame counter that only changes when a frame is drawn, so
	// the example needs a heartbeat to keep the block alive at the target rate.
	// A real application would be invalidated by whatever it is displaying.
	stopBeat := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopBeat:
				return
			case <-quit:
				return
			case <-ticker.C:
				r.InvalidateAll()
			}
		}
	}()

	// Keys and resizes arrive on ONE ordered channel, so a resize can never be
	// delivered between the bytes of a half-read escape sequence, and no input
	// event is ever dropped to make room for a resize.
	go func() {
		for {
			select {
			case <-quit:
				return
			case ev, ok := <-src.Events():
				if !ok {
					// The terminal reached EOF.
					stop()
					return
				}
				switch ev.Kind {
				case termmosaic.EventKey:
					if isQuitKey(ev) {
						stop()
						return
					}
					// Everything else goes to the widget, ON THE RENDER GOROUTINE.
					// Handle writes the focus index and the help flag and Draw reads
					// both, so calling it here would be a data race between the input
					// goroutine and the frame path. Post is the framework's own answer
					// to exactly that (ADR 0003), and the reason this example posts
					// rather than calling Handle directly is that it is the pattern an
					// application is expected to copy.
					key := ev
					r.Post(func() {
						if root.Handle(key) {
							// A handled key changed the widget's state, so the frame
							// that described the old state is stale. InvalidateAll
							// rather than a sub-rectangle: the marker moves between
							// facts, and naming the two rects it could move between
							// is a cache this example has no reason to keep.
							r.InvalidateAll()
						}
					})
				case termmosaic.EventResize:
					// ADR 0007 §5's order, and nothing else between the two
					// steps: recompute the root's rectangle, then resize the
					// renderer. Renderer.Resize never draws, so the whole
					// interval between the event and the next Render is
					// available to recompute bounds, and Resize already forces a
					// full repaint — so the r.InvalidateAll() that used to sit
					// here was redundant. The pacer then decides when to paint,
					// which is what coalesces a drag burst into one repaint.
					w, h = ev.Size.W, ev.Size.H
					root.bounds = rootBounds(w, h)
					r.Resize(w, h)
				}
			}
		}
	}()

	pacer := render.NewPacer(r)
	frameErr := make(chan error, 1)
	go func() { frameErr <- pacer.Run(quit) }()

	err = <-frameErr
	close(stopBeat)
	// Closing the Source restores the terminal modes it enabled and stops its
	// goroutines. It does not close the terminal, which the deferred t.Close()
	// above owns.
	_ = src.Close()
	_ = r.LeaveAltScreen()
	_ = t.LeaveRawMode()
	return err
}

// withProbe returns cfg with its WriteProbe wired to sink.
//
// The sink is what the renderer already writes frames through, so the enable
// sequences and the kitty query go out on the same path as the UI rather than
// through a second writer that could interleave with it.
func withProbe(cfg input.Config, sink termmosaic.Sink) input.Config {
	cfg.WriteProbe = func(p []byte) error {
		if _, err := sink.Write(p); err != nil {
			return err
		}
		return sink.Flush()
	}
	return cfg
}

// isQuitKey reports whether ev should end the example.
//
// These are the keys a person actually reaches for, expressed in the decoder's
// vocabulary rather than as raw bytes: Ctrl-C arrives as Ctrl+'c' rather than as
// 0x03, and Escape arrives as KeyEscape only after the decoder has waited out its
// ambiguity delay. The raw-byte version could not tell an arrow key from four
// unrelated bytes.
func isQuitKey(ev termmosaic.Event) bool {
	switch {
	case ev.Key == termmosaic.KeyEscape:
		return true
	case ev.Rune == 'q' || ev.Rune == 'Q':
		return ev.Mod == 0
	case ev.Rune == 'c':
		return ev.Mod == termmosaic.ModCtrl
	}
	return false
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
