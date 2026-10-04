// Package form provides the input widgets: TextInput, TextArea, Select,
// Checkbox, Radio, Toggle, Tabs, Button and KeyHint.
//
// # What the whole package shares
//
// Four rules, each of which is a consequence of something decided elsewhere, and
// each of which is enforced by this package's tests rather than by convention:
//
//  1. A widget's space is Bounds(), never buf.Size() (ADR 0007 §1 rule 1).
//  2. Every widget repaints its entire Bounds() before drawing content, because
//     the renderer diffs and never clears (ADR 0007 §1 rule 3).
//  3. Draw is total for every rect, including 0×0, 1×1 and anything below
//     MinSize, and it clips rather than blanking (ADR 0007 §4).
//  4. Nothing derived from the size or from the text is built inside Draw.
//     Wrap, Truncate and span construction allocate, so each widget caches the
//     result against the rect it was computed for and rebuilds when the rect,
//     the text or the styles change (ADR 0007 §3, ADR 0008 §4).
//
// # Colour is never the only signal
//
// Every widget in this package marks its state with a SHAPE, not a colour:
// a checkbox is "[ ]", "[x]" or "[-]"; a toggle is "[on]" or "[off]"; a
// selected tab is bracketed and an unselected one is not; a selected option
// carries a ">" marker in its own column and an unselected one carries a space
// there. Those markers are plain ASCII, which is deliberate: they are one cell
// wide, they cannot be mismeasured by the width functions, they render on a
// terminal with no Unicode support, and every assertion in the tests below is
// an exact string comparison. A user who cannot see colour — or whose terminal
// has NO_COLOR set, which suppresses colour at encode time — still reads the
// state of every widget in this package.
//
// # Focus
//
// The focusable widgets consume keys only while focused, and take focus on a
// click inside their own bounds. That combination is what makes a form
// composable: an unfocused field cannot be typed into by a key that was meant
// for a sibling, and a click is always a request to interact with whatever was
// clicked.
//
// # Paste
//
// TextInput and TextArea handle EventPaste as ONE operation. That is ADR 0005
// §4's second requirement, and this package is the reason it was written: a
// 10,000-character paste arriving as 10,000 key events would push 10,000 undo
// entries, making one Ctrl-Z useless.
package form

import (
	"unicode"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// undoLimit is how many undo steps a field keeps. It bounds memory for a field
// someone has typed into for a long session, and it is the response ADR 0005
// §7 anticipates ("undo groups large enough to be obviously wrong on paste"):
// a paste is one step, so the cap evicts ordinary typing long before it could
// ever break a paste's atomicity.
const undoLimit = 256

// undoStep is one reversible edit: the runes that were removed at start and the
// runes that were put there instead.
//
// It stores the replacement rather than a snapshot of the whole buffer, so an
// undo stack of 256 steps costs the size of the edits rather than 256 copies of
// the text.
type undoStep struct {
	// start is the rune index the edit began at.
	start int
	// removed is the text that was there, empty for a pure insertion.
	removed []rune
	// added is the text that replaced it, empty for a pure deletion.
	added []rune
}

// editor is the text-editing core shared by TextInput and TextArea: the runes,
// the caret, the selection anchor, the undo stack and the horizontal scroll
// offset, plus every operation that changes them.
//
// It exists because the two field types differ only in what they do with a
// newline and in how they scroll. The operations themselves — insert, delete,
// delete-a-word, undo, word motion, and the arithmetic that turns a mouse column
// into a caret position — are identical, and two copies of them would drift in
// exactly the way ADR 0007 §1 rule 4 exists to prevent.
//
// The zero editor is an empty field with the caret at 0 and no undo history, so
// a TextInput or TextArea is usable as constructed.
type editor struct {
	// text is the content as runes, not as a string. A caret index into a
	// string is a byte offset, which makes every motion operation a UTF-8
	// boundary check; a rune index makes it an integer comparison.
	text []rune
	// cursor is the caret's rune index. It is always in [0, len(text)].
	cursor int
	// anchor is the selection's fixed end. anchor == cursor means no selection.
	anchor int
	// scroll is the rune index of the first visible column. TextInput uses it
	// for horizontal scrolling and TextArea leaves it at 0, because a wrapped
	// area has no horizontal scroll.
	scroll int
	// undo is the step stack, oldest first.
	undo []undoStep
	// stale reports that the cached display must be rebuilt before drawing.
	// Every mutating operation sets it and every successful Draw clears it,
	// which is what keeps a keystroke's rebuild to one frame rather than to one
	// per frame.
	stale bool
	// multiLine reports whether a newline is content. It is true for TextArea and
	// false for TextInput, and it is the ONLY thing the two disagree about.
	multiLine bool
}

// setText replaces the whole content and resets the caret, the selection, the
// scroll offset and the undo history.
//
// The undo history is dropped deliberately: an application replacing a field's
// entire value is not an edit the user performed one keystroke at a time, and
// keeping steps that describe text that no longer existed would make undo
// produce content nobody typed.
func (e *editor) setText(s string) {
	if s == "" {
		e.text = nil
	} else {
		e.text = []rune(s)
	}
	e.cursor, e.anchor, e.scroll = 0, 0, 0
	e.undo = nil
	e.stale = true
}

// String returns the content as a string.
func (e *editor) String() string { return string(e.text) }

// setRunes replaces the whole content with a copy of rs. It exists so a caller
// that already has runes does not have to round-trip through a string.
func (e *editor) setRunes(rs []rune) {
	if len(rs) == 0 {
		e.text = nil
	} else {
		e.text = append([]rune(nil), rs...)
	}
	e.cursor, e.anchor, e.scroll = 0, 0, 0
	e.undo = nil
	e.stale = true
}

// normalise puts the caret and the anchor back inside the text.
//
// It runs at the top of every mutating path, because the state can be made
// inconsistent from outside: an application can set a cursor past the end of a
// value it has just replaced, or an undo step can be evicted. A widget whose
// state is inconsistent must degrade rather than panic, and clamping is the
// cheapest form of degrading.
func (e *editor) normalise() {
	n := len(e.text)
	if e.cursor < 0 {
		e.cursor = 0
	}
	if e.cursor > n {
		e.cursor = n
	}
	if e.anchor < 0 {
		e.anchor = 0
	}
	if e.anchor > n {
		e.anchor = n
	}
	if e.scroll < 0 {
		e.scroll = 0
	}
	if e.scroll > n {
		e.scroll = n
	}
}

// selection returns the selected range as [lo, hi) and whether there is one.
func (e *editor) selection() (lo, hi int, ok bool) {
	lo, hi = e.anchor, e.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi, hi > lo
}

// clearSelection collapses the selection to the caret without moving it.
func (e *editor) clearSelection() { e.anchor = e.cursor }

// insertText inserts s at the caret, replacing the selection if there is one.
//
// It is one undo step whether it is a keystroke or a 10,000-character paste,
// which is the whole point of EventPaste arriving as one event (ADR 0005 §4).
func (e *editor) insertText(s string) { e.replaceSelection(s) }

// replaceSelection inserts s over the selection, or at the caret when nothing is
// selected. An empty s with nothing selected does nothing at all: it must not
// push an empty undo step, because that would make Ctrl-Z appear to do nothing
// once instead of undoing the previous real edit.
func (e *editor) replaceSelection(s string) {
	e.normalise()
	lo, hi, sel := e.selection()
	at := lo
	if !sel {
		at = e.cursor
		hi = lo
	}
	e.replace(at, hi, s)
}

// replace swaps text[lo:hi] for s, records one undo step, collapses the
// selection and leaves the caret after the inserted text.
//
// Newline filtering is the single difference between a single-line field and a
// multi-line area: a TextInput drops CR and LF from an inserted payload,
// because a one-row field has no way to show a newline and dropping it is
// better than silently replacing it with a glyph the user did not type. It does
// NOT normalise anything else, per ADR 0005 §4's third requirement that Text
// carries the payload as-is.
func (e *editor) replace(lo, hi int, s string) {
	e.normalise()
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo < 0 {
		lo = 0
	}
	if hi > len(e.text) {
		hi = len(e.text)
	}
	added := e.filter(s)
	if lo == hi && len(added) == 0 {
		return
	}
	removed := make([]rune, hi-lo)
	copy(removed, e.text[lo:hi])

	e.pushUndo(lo, removed, added)

	out := make([]rune, 0, len(e.text)-(hi-lo)+len(added))
	out = append(out, e.text[:lo]...)
	out = append(out, added...)
	out = append(out, e.text[hi:]...)
	e.text = out

	e.cursor = lo + len(added)
	e.anchor = e.cursor
	// The horizontal offset is deliberately left alone except for this clamp:
	// scroll is the window's position, not the caret's, and an edit must not
	// move the window to the edit site. scrollIntoView, which every widget calls
	// after an edit, is what brings the caret into view.
	if e.scroll > e.cursor {
		e.scroll = e.cursor
	}
	e.stale = true
}

// filter applies the single-line rule to an inserted payload.
func (e *editor) filter(s string) []rune {
	if e.multiLine {
		return []rune(s)
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' {
			continue
		}
		out = append(out, r)
	}
	return out
}

// pushUndo records one step, coalescing a run of single-character insertions
// into the step before them.
//
// The coalescing rule is deliberately narrow: a step merges only when the
// previous step removed nothing and the new edit is one rune inserted
// immediately after what that step added. A delete, a selection replacement and a
// paste therefore all push their own step, which is precisely the set of
// operations a user expects Ctrl-Z to take back one at a time.
func (e *editor) pushUndo(start int, removed, added []rune) {
	if len(removed) == 0 && len(added) == 1 {
		if n := len(e.undo); n > 0 {
			last := &e.undo[n-1]
			if len(last.removed) == 0 && last.start+len(last.added) == start {
				last.added = append(last.added, added[0])
				return
			}
		}
	}
	if len(e.undo) >= undoLimit {
		// Evict the oldest step rather than refusing to record the newest: a
		// field that has stopped recording edits is a field whose Ctrl-Z does
		// nothing, and dropping the oldest is the lesser failure.
		e.undo = append(e.undo[:0], e.undo[1:]...)
	}
	e.undo = append(e.undo, undoStep{start: start, removed: removed, added: added})
}

// undoStep reverts the most recent step and reports whether there was one.
//
// It returns false rather than doing nothing silently, so a widget can pass the
// answer to its caller as "the key was consumed but nothing changed".
func (e *editor) undoLast() bool {
	e.normalise()
	n := len(e.undo)
	if n == 0 {
		return false
	}
	st := e.undo[n-1]
	e.undo = e.undo[:n-1]

	lo := st.start
	if lo < 0 {
		lo = 0
	}
	hi := lo + len(st.added)
	if hi > len(e.text) {
		hi = len(e.text)
	}
	if lo > hi {
		lo = hi
	}
	out := make([]rune, 0, len(e.text)-(hi-lo)+len(st.removed))
	out = append(out, e.text[:lo]...)
	out = append(out, st.removed...)
	out = append(out, e.text[hi:]...)
	e.text = out

	e.cursor = st.start
	e.anchor = e.cursor
	if e.scroll > e.cursor {
		e.scroll = e.cursor
	}
	e.stale = true
	return true
}

// backspace deletes backwards from the caret: the selection if there is one,
// otherwise the rune before the caret, otherwise the word before it when word
// is set. It reports whether anything was deleted.
func (e *editor) backspace(word bool) bool {
	e.normalise()
	if lo, hi, sel := e.selection(); sel {
		e.replace(lo, hi, "")
		return true
	}
	if e.cursor == 0 {
		return false
	}
	from := e.cursor - 1
	if word {
		from = wordLeft(e.text, from)
	}
	e.replace(from, e.cursor, "")
	return true
}

// forwardDelete deletes forwards from the caret: the selection if there is one,
// otherwise the rune after it, otherwise the word after it when word is set. It
// reports whether anything was deleted.
func (e *editor) forwardDelete(word bool) bool {
	e.normalise()
	if lo, hi, sel := e.selection(); sel {
		e.replace(lo, hi, "")
		return true
	}
	if e.cursor >= len(e.text) {
		return false
	}
	to := e.cursor + 1
	if word {
		to = wordRight(e.text, e.cursor)
	}
	e.replace(e.cursor, to, "")
	return true
}

// setCursor moves the caret to rune index i, clamping it into the text, and
// either extends the selection from the existing anchor or collapses it.
//
// extend is what distinguishes Shift+Right from Right: an extending move leaves
// the anchor alone so the selection grows, and a plain move puts the anchor back
// on the caret so the selection is empty.
func (e *editor) setCursor(i int, extend bool) {
	e.normalise()
	if i < 0 {
		i = 0
	}
	if i > len(e.text) {
		i = len(e.text)
	}
	e.cursor = i
	if !extend {
		e.anchor = i
	}
	e.stale = true
}

// selectAll selects the whole content. The caret moves to the end, which is
// where typing then replaces from.
func (e *editor) selectAll() {
	e.normalise()
	e.anchor = 0
	e.cursor = len(e.text)
	e.stale = true
}

// scrollIntoView adjusts the horizontal offset so the caret is visible in a
// field width cells wide, which is the clamp-not-recentre rule of ADR 0007 §6
// applied to a one-row viewport.
//
// It moves the offset the minimum amount, so a caret that is already visible
// does not scroll the field at all — the behaviour every text field has, and
// the reason it is not expressed as a general recentring rule.
func (e *editor) scrollIntoView(width int) {
	e.normalise()
	if width <= 0 {
		e.scroll = 0
		return
	}
	if e.cursor < e.scroll {
		e.scroll = e.cursor
	}
	w := 0
	for i := e.scroll; i < e.cursor; i++ {
		w += buffer.RuneWidth(e.text[i])
	}
	for e.scroll < e.cursor && w >= width {
		w -= buffer.RuneWidth(e.text[e.scroll])
		e.scroll++
	}
}

// runesFitting returns how many runes from index from onwards fit in width
// cells.
//
// It exists because two callers need that count before they build anything: the
// field has to know whether to reserve a cell for the truncation marker, and the
// scroll logic has to know whether the caret is off the right edge. Both are
// arithmetic, not allocation, so both may run per frame — but neither is written
// twice.
func runesFitting(text []rune, from, width int) int {
	if width <= 0 {
		return 0
	}
	w := 0
	n := from
	for ; n < len(text); n++ {
		rw := buffer.RuneWidth(text[n])
		if w+rw > width {
			break
		}
		w += rw
	}
	return n - from
}

// runeAtColumn returns the rune index for a click at cell column col within
// the visible window of width cells.
//
// A double-width rune straddling the click is never split: the caret lands
// before it, matching buffer.SetSpans' rule that a wide glyph is not written
// half. A click past the right edge lands after the last visible rune, which is
// what a user clicking the far margin expects.
func (e *editor) runeAtColumn(col, width int) int {
	w := 0
	for i := e.scroll; i < len(e.text); i++ {
		rw := buffer.RuneWidth(e.text[i])
		if rw == 0 {
			continue
		}
		if w >= col {
			return i
		}
		if w+rw > width {
			return i
		}
		w += rw
	}
	return len(e.text)
}

// cellWidth returns the cell width of text[lo:hi]. It is the width arithmetic
// every field needs and the reason RuneWidth rather than len() appears at all:
// a field of CJK text is half as many runes as it is cells wide.
func cellWidth(text []rune, lo, hi int) int {
	if lo < 0 {
		lo = 0
	}
	if hi > len(text) {
		hi = len(text)
	}
	w := 0
	for i := lo; i < hi; i++ {
		w += buffer.RuneWidth(text[i])
	}
	return w
}

// isWordRune reports whether r belongs to a word for the purpose of word motion.
//
// Underscore counts, because in every identifier syntax in common use it does;
// treating "snake_case" as two words would make Ctrl-Left stop in the middle of
// a name.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// wordLeft returns the index of the start of the word at or before i.
//
// It skips the run of non-word runes backwards, then the run of word runes
// backwards, which is the behaviour every editor has: Ctrl-Left from the middle
// of a word goes to that word's start, and Ctrl-Left again goes to the previous
// one's start.
func wordLeft(text []rune, i int) int {
	if i > len(text) {
		i = len(text)
	}
	j := i
	for j > 0 && !isWordRune(text[j-1]) {
		j--
	}
	for j > 0 && isWordRune(text[j-1]) {
		j--
	}
	return j
}

// wordRight returns the index of the start of the word after the one at i,
// skipping the run of word runes and then the spaces that follow them.
//
// Landing on the START of the next word, rather than the end of the current one,
// is what makes Ctrl-Delete remove "beta " from "alpha beta gamma" instead of
// "beta": the trailing space has to go too, or a delete-a-word keystroke leaves
// a stray space behind every time.
func wordRight(text []rune, i int) int {
	if i < 0 {
		i = 0
	}
	if i > len(text) {
		return len(text)
	}
	j := i
	for j < len(text) && !isWordRune(text[j]) {
		j++
	}
	for j < len(text) && isWordRune(text[j]) {
		j++
	}
	for j < len(text) && text[j] == ' ' {
		j++
	}
	return j
}

// ctrlAction names what a control-key binding asks a field to do.
//
// It exists because TextInput and TextArea have the same six control bindings
// and would otherwise each write their own switch — the drift ADR 0007 §1 rule 4
// exists to prevent. The mapping is shared; what each widget does about the
// resulting edit (scrolling, firing OnChange) is its own, because it is the part
// that genuinely differs.
type ctrlAction uint8

// The control bindings both fields share.
const (
	// ctrlNone means the rune is not a control binding and the key belongs to
	// the rest of the form.
	ctrlNone ctrlAction = iota
	// ctrlSelectAll is ModCtrl+'a': select the whole content.
	ctrlSelectAll
	// ctrlUndo is ModCtrl+'z': revert the most recent edit.
	ctrlUndo
	// ctrlWordBackspace is ModCtrl+'w' — readline's binding, and what a terminal
	// sends as 0x17 — or ModCtrl+'h', which is what the kitty keyboard protocol
	// reports for the same physical chord.
	ctrlWordBackspace
	// ctrlWordDelete is ModCtrl+'d': delete the word after the caret.
	ctrlWordDelete
	// ctrlEnd is ModCtrl+'e': move to the end of the text, matching Ctrl-End on a
	// terminal that reports it as a rune rather than as a key.
	ctrlEnd
)

// controlAction maps a control-modified rune to the binding it stands for, and
// returns ctrlNone for every other rune — including a bare printable character,
// which is text rather than a command.
//
// The caller passes the rune already upper-cased or lower-cased as the terminal
// sent it; Shift is not required for any of these, because Shift+Ctrl+'a' is the
// same chord to every terminal that reports it.
func controlAction(r rune) ctrlAction {
	switch r {
	case 'a', 'A':
		return ctrlSelectAll
	case 'z', 'Z':
		return ctrlUndo
	case 'w', 'W', 'h', 'H':
		return ctrlWordBackspace
	case 'd', 'D':
		return ctrlWordDelete
	case 'e', 'E':
		return ctrlEnd
	}
	return ctrlNone
}

// clipText returns the longest prefix of s whose cell width is at most w.
//
// It exists because Buffer.SetString and SetSpans bounds-check against the
// BUFFER, not against a widget's own rect: a widget whose fixed-width chrome — a
// three-cell marker, a five-cell on/off switch — is wider than the rect it was
// given would otherwise paint straight over whatever is composed beside it. A
// field of two cells showing a three-cell checkbox marker is exactly that case.
//
// The result is a substring of s and therefore allocates nothing, so it is safe on
// the frame path.
func clipText(s string, w int) string {
	if w <= 0 {
		return ""
	}
	used := 0
	for i, r := range s {
		rw := buffer.RuneWidth(r)
		if rw == 0 {
			continue
		}
		if used+rw > w {
			return s[:i]
		}
		used += rw
	}
	return s
}

// labelGap is the one space between a state marker and its label, and
// labelGapW its width. Checkbox and Toggle share them because "the marker is
// pressed against the label" is the same mistake made twice, and because MinSize
// has to reserve exactly the cell the Draw path writes.
const (
	labelGap  = " "
	labelGapW = 1
)

// activateKey reports whether ev is the activation key: Enter, or the space bar.
//
// The space bar arrives as a RUNE on most terminals and as KeySpace only under
// the kitty keyboard protocol, so a widget that handles one spelling is broken on
// half the terminals that exist. Both are accepted here rather than in each
// widget, because "Enter and Space activate" is a catalog-wide contract.
//
// A modified space is not an activation: Ctrl-Space is what input.Decode
// reports for a NUL byte, and no widget in this package has a binding for it.
func activateKey(ev termmosaic.Event) bool {
	if ev.Key == termmosaic.KeyEnter || ev.Key == termmosaic.KeySpace {
		return true
	}
	if ev.Key != termmosaic.KeyNone || ev.Rune != ' ' {
		return false
	}
	return !ev.Mod.Has(termmosaic.ModCtrl) && !ev.Mod.Has(termmosaic.ModAlt)
}

// runBuilder accumulates maximal runs of runes that share one style into a
// []buffer.Span.
//
// It exists because a field with a selection and a caret has three styles
// interleaved on one row, and writing that row through SetString would either
// lose the styling or need a string built per frame. Grouping the runs once and
// handing SetSpans a slice is what keeps the row a single allocation-free write
// and keeps a wide glyph's two halves wearing the same style — the flicker
// invariant ADR 0008 states for SetSpans.
//
// It is used only while REBUILDING a cached value, never on the frame path.
type runBuilder struct {
	// runes is the text the runs are cut from.
	runes []rune
	// out is the accumulating slice of spans.
	out []buffer.Span
	// start is the first rune of the open run.
	start int
	// style is the open run's style.
	style buffer.Style
	// open reports whether a run is currently open.
	open bool
}

// newRunBuilder returns a builder over runes with room for a typical row's
// handful of style changes, so the common rebuild is a single allocation.
func newRunBuilder(runes []rune) runBuilder {
	return runBuilder{runes: runes, out: make([]buffer.Span, 0, 4)}
}

// at records that rune i carries style st, opening a new run when the style
// changed. Consecutive runes of the same style extend the open run.
func (b *runBuilder) at(i int, st buffer.Style) {
	if !b.open {
		b.start, b.style, b.open = i, st, true
		return
	}
	if st != b.style {
		b.close(i)
		b.start, b.style, b.open = i, st, true
	}
}

// close ends the open run at rune index end.
func (b *runBuilder) close(end int) {
	if !b.open {
		return
	}
	if end > b.start {
		b.out = append(b.out, buffer.NewSpan(string(b.runes[b.start:end]), b.style))
	}
	b.open = false
}

// done closes any open run and returns the spans. The result aliases the
// builder's own storage and is owned by the caller once returned.
func (b *runBuilder) done() []buffer.Span {
	b.close(len(b.runes))
	if len(b.out) == 0 {
		return nil
	}
	return b.out
}

// visibleCache is the cache bookkeeping every field in this package needs: the
// rect a cached value was computed for, and whether that value is still
// current.
//
// The styles are compared separately by the widget, because which fields matter
// is per-widget, while the rect and the staleness flag are not.
type visibleCache struct {
	// rect is the bounds the cached value was built for.
	rect buffer.Rect
	// ok reports whether rect has been populated at all. A zero rect with ok
	// false is the state before the first Draw.
	ok bool
}

// matches reports whether a cache built for r is current, or whether the caller
// must rebuild because the rect moved or the content changed.
//
// stale is the caller's own invalidation flag: a widget whose editors set a flag
// passes it, and a widget whose mutators clear ok directly passes false. There is
// no default, because a widget that forgets to pass its flag would repaint nothing
// and a widget that invents one would rebuild every frame.
func (c *visibleCache) matches(r buffer.Rect, stale bool) bool {
	return c.ok && !stale && c.rect == r
}

// store records r as the rect the cache was built for.
func (c *visibleCache) store(r buffer.Rect) {
	c.rect, c.ok = r, true
}
