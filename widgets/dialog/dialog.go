// Package dialog provides Dialog: the catalog's modal — a box with a title, a
// body, and either a row of buttons or a list of choices.
//
// # Why it is plain Go types and not a themed thing
//
// ADR 0008 §Decision 3 rejected a theme abstraction for v1: every styleable
// widget carries exported Style fields, and the framework ships no default
// colours. So a Dialog is a handful of Style fields, a body of []buffer.Span, a
// []Action and a []string. There is no role registry, no variant-to-style table,
// and no colour anywhere in this file.
//
// # What it composes, and what it spells
//
// It composes widgets/block for its border, title, padding and background, so
// it contains no box-drawing rune literal and invents no title threshold — the
// same rule the whole catalog obeys (ADR 0008 §Decision 4), enforced over every
// .go file in the module by TestBoxDrawingRunesLiveInOneFile.
//
// Its own text goes out through buffer's range-clipped writers (SetSpansIn,
// SetSpansCappedIn), which are allocation-free, so a row can be drawn every
// frame without a per-frame slice.
//
// # The three shapes, and why there are three
//
// A dialog does one of three jobs, and the difference is what the keys mean:
//
//   - VariantInfo is a message. One button, which dismisses. Nothing is chosen,
//     so there is nothing to get wrong.
//   - VariantConfirm asks a yes/no question. Two buttons, and the default focus
//     is the affirmative one — a dialog that opens with Cancel focused makes the
//     safe answer the easy one to press by accident.
//   - VariantChoice asks the user to pick from a list.
//
// Variant is a PRESET, not a mode: it chooses the default actions and the
// default focus, and SetActions and SetChoices replace them. A caller that
// wants a third button on a confirm dialog adds one and the dialog becomes a
// choice with a body — nothing in the widget prevents it, because nothing in the
// widget needs to.
//
// # The focus ring
//
// The ring is the ordered list of things a dialog can focus: the CHOICES first,
// then the ACTIONS. It is one-dimensional, so Tab, Shift-Tab and all four
// arrows traverse it identically and mean the same thing in every variant. A
// dialog's focus is a ring, not a grid, and the reason is that a dialog with
// three choices and a Cancel button has no second dimension to move in.
//
// # The key contract, in full
//
// Keys are consumed only while the dialog has focus, and none of them is ever
// "nothing happened": every binding below either moves the focus, activates
// something, or dismisses the dialog.
//
//	enter, space      activate the focused ring item — a choice is chosen, a
//	                  button is pressed
//	tab, down, right  move the focus one item forward through the ring
//	backtab, up, left move the focus one item backward through the ring
//	home, end         the first / last item of the ring
//	page up/down      move the focus a screenful of items through the ring,
//	                  which is how a choice list longer than the dialog is
//	                  crossed without ninety presses
//	escape            dismiss: the cancel action when the dialog has one, and
//	                  OnCancel always
//	wheel up/down     scroll the choice list WITHOUT moving the focus
//	press             on a choice, choose it; on a button, press it; elsewhere
//	                  inside the box, take focus and change nothing else
//
// The one documented non-consumption: Tab, Shift-Tab, the four arrows, Home and
// End are NOT consumed when the ring holds a single item, because there is nowhere
// to move to. An application is then free to read Tab as "leave this modal", which
// is the only way focus gets out of a one-button dialog at all.
//
// Everything else in that table is consumed whenever the dialog has focus —
// including Escape on a dialog with no actions at all, and Enter on a dialog
// with nothing in the ring. A modal that lets a key fall through is a modal the
// user can get behind, which is the one failure this contract exists to prevent.
//
// # Focus is a SHAPE
//
// The focused action is ringed — "[OK]" against "  OK  " — and wears
// AttrReverse; the focused choice carries ChoiceMarker in its own marker column
// and wears AttrReverse. Both rings are the same width as the unfocused one, so
// moving the focus never reflows the row, and the reinforcement is an
// ATTRIBUTE rather than a colour, so it survives NO_COLOR (ADR 0008 §Decision 5:
// colour is suppressed at encode time and attributes are not).
//
// # Cost
//
// Draw allocates nothing. Everything derived from the interior — the wrapped
// body, the truncated labels, the action row's wrapping, the budget answer — is
// computed in adapt, which runs at most once per distinct interior rect and
// whenever a mutator marks the cache stale (ADR 0007 §3, ADR 0008 §4). The
// choice list is virtualized through virtual.Model, so a dialog with a thousand
// choices costs what one with three costs.
package dialog

import (
	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/geometry"
	"github.com/serkanalgur/termmosaic/virtual"
	"github.com/serkanalgur/termmosaic/widgets/block"
)

// The focus marks. They are exported because they are part of this widget's
// contract rather than an implementation detail: an application laying out beside
// a dialog, a test and a golden file all need to name the same strings.
//
// They are ASCII, and two cells and one cell wide respectively, deliberately:
// ASCII renders on a terminal with no Unicode support, and a width the width
// functions can measure exactly is a width that cannot shift a layout by being
// miscounted.
const (
	// ActionRingFocused brackets the label of the focused action.
	ActionRingFocused = "[]"
	// ActionRingIdle pads the label of every other action. It is two spaces, so
	// an action is the same width focused or not and moving the focus never
	// reflows the row.
	ActionRingIdle = "  "
	// ChoiceMarker is written in the marker column of the focused choice. Every
	// other choice carries a space there, so the marker is a SHAPE and the
	// focused row is identifiable with no colour at all.
	ChoiceMarker = ">"
)

// The default action labels, so a preset dialog says the same thing as every
// other preset dialog. Exported so a test and an application that wants to look
// a button up rather than count it name the same strings.
const (
	// DefaultDismissLabel is the single action of VariantInfo.
	DefaultDismissLabel = "Dismiss"
	// DefaultCancelLabel is the cancel action of every preset.
	DefaultCancelLabel = "Cancel"
	// DefaultOKLabel is the affirmative action of VariantConfirm.
	DefaultOKLabel = "OK"
)

// Local thresholds and constants, per ADR 0007 §1 rule 5: a breakpoint belongs
// to the widget that uses it, as a named constant beside its use.
const (
	// actionGap is the blank cells between two actions on the same row.
	actionGap = 2
	// actionRing is how many cells a label's focus ring costs: the '[' and the
	// ']' of ActionRingFocused.
	actionRing = 2
	// choiceMarkPad is the blank cells between the marker column and a choice's
	// label, so the marker reads as a gutter rather than as part of the label.
	choiceMarkPad = 1
	// minBodyW is the narrowest body line worth showing: a dialog carries a short
	// message, and narrower than this is a word per line.
	minBodyW = 16
	// minChoiceW is the narrowest choice label worth showing beside its marker.
	minChoiceW = 12
	// wheelLines is how many choices one wheel notch moves.
	wheelLines = 3
)

// Region indexes into the vertical budget, in the order the rows are stacked.
const (
	regionBody = iota
	regionChoices
	regionActions
	nRegions
)

// Variant presets a dialog's default actions and its default focus.
type Variant uint8

// The three dialog shapes.
const (
	// VariantInfo is a message with one dismissing button and the focus on it.
	VariantInfo Variant = iota
	// VariantConfirm is a question with Cancel and OK, focused on OK: the
	// affirmative answer is the one a stray Enter should reach, and a dialog
	// that opens with Cancel focused makes "walk away" the accidental default.
	VariantConfirm
	// VariantChoice is a list of choices with the focus on the first one.
	VariantChoice
)

// String returns the variant's name, for diagnostics and golden-test failure
// messages. An out-of-range value renders in decimal rather than panicking,
// because a Variant arriving from a persisted config is untrusted input and a
// TUI that panics on a bad byte is unusable (ADR 0007 §4).
func (v Variant) String() string {
	switch v {
	case VariantInfo:
		return "info"
	case VariantConfirm:
		return "confirm"
	case VariantChoice:
		return "choice"
	default:
		return "variant(" + itoa(int(v)) + ")"
	}
}

// Action is one button in a dialog's action row.
//
// A plain struct with no behaviour, because a button in a dialog is a label and
// an index: what it does is the caller's OnAction, and the dialog's job is to
// draw the row and report which one was pressed.
type Action struct {
	// Label is the button's text. It is truncated with a marker rather than
	// clipped, so a button too narrow for its label says so.
	Label string
	// Style is the button's rendition. An unset value resolves to Dialog's
	// ActionStyle.
	Style buffer.Style
}

// Dialog is a modal: a bordered box with a title, a body, and a row of actions
// or a list of choices.
//
// It is Focusable, and its keys are consumed only while it has focus. It is
// Minimizable, and MinSize is the whole widget including the border and padding
// the block contributes (ADR 0007 §2).
//
// A Dialog is a plain value in every respect that matters: it is not safe for
// concurrent use, because SetBounds and Draw race by design — the renderer owns
// Bounds between frames (ADR 0003).
type Dialog struct {
	blk    *block.Block
	bounds buffer.Rect

	// variant is the preset this dialog was built with. It is recorded rather
	// than consulted: everything it decided — the default actions and the
	// default focus — has already been applied to the fields below, so replacing
	// the actions cannot leave the variant disagreeing with what is drawn.
	variant Variant

	// body is the message, as styled spans.
	body []buffer.Span
	// actions are the buttons, in order.
	actions []Action
	// choices are the labels of the choice list.
	choices []string

	// vm is the choice list's scroll engine. A dialog with no choices still has
	// one, allocated once at construction, because that costs one allocation
	// ever and keeps every other path free of a nil check.
	vm *virtual.Model

	// focus is the index into the ring: the choices first, then the actions.
	focus int
	// focused reports whether the dialog has keyboard focus.
	focused bool
	// dismissed reports whether the dialog has ended, by activation or by the
	// cancel path.
	dismissed bool

	// BodyStyle is the rendition of a body line passed as a plain string through
	// SetBodyString. Spans set through SetBody carry their own styles and ignore
	// it.
	BodyStyle buffer.Style
	// ActionStyle is the rendition of an action whose own Style is unset.
	ActionStyle buffer.Style
	// FocusStyle is the rendition of the focused action. An unset value means
	// "the action style with AttrReverse", so focus is visible with no
	// configuration at all and under NO_COLOR.
	FocusStyle buffer.Style
	// ChoiceStyle is the rendition of an unfocused choice.
	ChoiceStyle buffer.Style
	// ChoiceFocusStyle is the rendition of the focused choice. An unset value
	// means "the choice style with AttrReverse".
	ChoiceFocusStyle buffer.Style

	// OnAction is called with the action index when a button is activated —
	// by Enter, by a press, or by Escape on a dialog whose cancel action is
	// that button.
	OnAction func(int)
	// OnChoice is called with the choice index when a choice is activated.
	OnChoice func(int)
	// OnCancel is called on the cancel path — Escape, or Dismiss(). It fires
	// whether or not the dialog has a cancel action, so an application wired to
	// callbacks alone is never left waiting for a dismissal that only a poll of
	// Dismissed() would have reported.
	OnCancel func()

	// cancel is the index of the action Escape activates, or -1 when the dialog
	// has none.
	cancel int

	// regions is the vertical budget in stack order: body, choices, actions. It
	// is a fixed array rather than a slice so adapt can hand Budget a view of it
	// without building one per adapt.
	regions [nRegions]geometry.Region

	// stale reports that the cached layout cannot be trusted. Every mutator sets
	// it and adapt clears it, which is what keeps one change to one rebuild
	// rather than one per frame (ADR 0007 §3's amendment: a cache that depends on
	// a field must be dropped by the setter for that field).
	stale bool
	// cachedIn is the interior the cache was built for.
	cachedIn buffer.Rect
	// cacheStyles is the resolved style set the cache was built for, so a caller
	// that writes a Style field directly still gets a correct frame.
	cacheStyles dialogStyles

	// bodyLines is the body wrapped to the cached width.
	bodyLines buffer.Wrapped
	// bodyShown and choiceShown are the resolved row counts.
	bodyShown, choiceShown int
	// bodyY is the first body row and choiceTop the first choice row, in screen
	// cells.
	bodyY, choiceTop int
	// actRows is how many rows the action row wraps to, and actTop the first of
	// them in screen cells.
	actRows, actTop int
	// choiceMarkW is the marker column's width.
	choiceMarkW int
	// mark is the truncation marker for the current ASCII rung.
	mark rune

	// actX, actW and actRow are each action's column, width and row within the
	// action area, from adapt's greedy wrap.
	actX, actW, actRow []int
	// actLabels[i] is action i's label, truncated to its own width less the ring,
	// and actFocusLabels[i] is the same label in the focused rendition.
	//
	// Both are cached rather than derived at paint time, because a span carries
	// its own style and the only allocation-free way to write a label is to have
	// the spans already built. Caching both is what lets moving the focus stay a
	// cache hit: nothing about the LAYOUT depends on the focus, only about which
	// of two already-built spans gets written.
	actLabels, actFocusLabels [][]buffer.Span
	// choiceLabels holds the truncated label of each VISIBLE choice, from the
	// scroll offset, so a thousand choices cost three.
	choiceLabels [][]buffer.Span
	// in is the interior the current cache was built for.
	in buffer.Rect
}

// dialogStyles is the resolved style set a Dialog's cached layout depends on.
type dialogStyles struct {
	action, focus, choice, choiceFocus buffer.Style
}

// New returns a Dialog of the given variant, sized r.
//
// The preset supplies the default actions and the default focus; the title, the
// body and the choices are the caller's to set. The border is BorderRounded,
// which is the one place in the catalog where a distinct frame is right: a
// dialog sits on top of other widgets, and a single-line box would not read as
// being in front of them.
func New(r buffer.Rect, v Variant) *Dialog {
	d := &Dialog{
		blk:     block.New(r),
		bounds:  r,
		variant: v,
		vm:      virtual.New(0),
		cancel:  -1,
		stale:   true,
	}
	d.blk.SetBorder(buffer.BorderRounded)
	switch v {
	case VariantConfirm:
		d.actions = []Action{{Label: DefaultCancelLabel}, {Label: DefaultOKLabel}}
		d.cancel = 0
		// The affirmative action, not the safe one.
		d.focus = 1
	case VariantChoice:
		d.actions = []Action{{Label: DefaultCancelLabel}}
		d.cancel = 0
	default:
		d.actions = []Action{{Label: DefaultDismissLabel}}
		d.cancel = 0
	}
	return d
}

// Block returns the block that draws this dialog's border, title, padding and
// background, so a caller can configure the chrome without Dialog re-exporting
// every Block method. It is the documented composition point of ADR 0008 §2.
//
// Its Ascii field is the single source of truth for the ASCII rung: Dialog reads
// blk.Ascii rather than keeping a second copy of the same boolean, because two
// copies of one boolean is one of them being wrong.
func (d *Dialog) Block() *block.Block { return d.blk }

// Variant returns the preset this dialog was built with.
func (d *Dialog) Variant() Variant { return d.variant }

// Bounds returns the dialog's rectangle, safe to call before the first Draw.
func (d *Dialog) Bounds() buffer.Rect { return d.bounds }

// SetBounds sets the dialog's rectangle. The layout cache is keyed on the
// interior, so the next Draw re-decides what fits.
func (d *Dialog) SetBounds(r buffer.Rect) {
	d.bounds = r
	d.blk.SetBounds(r)
}

// SetTitle sets the title from a single styled run, overwriting the block's
// title. An empty title clears it.
func (d *Dialog) SetTitle(s string, st buffer.Style) { d.blk.SetTitleString(s, st) }

// SetBody sets the message from styled spans.
//
// The spans are wrapped to the dialog's interior, so the caller writes the
// message once and never re-flows it. The slice is retained BY REFERENCE: a
// caller that mutates it afterwards must call Invalidate, exactly as with
// Block.SetTitle.
func (d *Dialog) SetBody(spans []buffer.Span) {
	d.body = spans
	d.stale = true
}

// SetBodyString sets the message from one string in BodyStyle. It is the uniform
// convenience over SetBody.
func (d *Dialog) SetBodyString(s string) {
	d.SetBody([]buffer.Span{buffer.NewSpan(s, d.BodyStyle)})
}

// Actions returns the dialog's buttons. The result aliases Dialog's own slice
// and must not be modified.
func (d *Dialog) Actions() []Action { return d.actions }

// SetActions replaces the buttons.
//
// It re-clamps the focus, the cancel index and the scroll offset into the new
// ranges, so a dialog whose actions were replaced by a shorter list cannot be
// left focused on a button that no longer exists.
func (d *Dialog) SetActions(actions ...Action) {
	d.actions = actions
	d.clampState()
	d.stale = true
}

// SetCancelAction makes action i the one Escape activates, which is what makes
// a dialog's cancel mean the same as the button labelled Cancel.
//
// i is clamped into range, and a dialog with no actions takes -1: Escape still
// dismisses, there is simply no button to press.
func (d *Dialog) SetCancelAction(i int) {
	if len(d.actions) == 0 {
		d.cancel = -1
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(d.actions) {
		i = len(d.actions) - 1
	}
	d.cancel = i
}

// CancelAction returns the index of the action Escape activates, or -1 when the
// dialog has none.
func (d *Dialog) CancelAction() int { return d.cancel }

// Choices returns the choice labels. The result aliases Dialog's own slice and
// must not be modified.
func (d *Dialog) Choices() []string { return d.choices }

// SetChoices replaces the choice labels and re-clamps the scroll offset.
//
// The focus is NOT moved onto the list: a dialog the user has already tabbed to
// a button keeps that focus when the choices change, because a content change is
// not a navigation. A dialog built with VariantChoice starts on the first choice
// and stays there.
func (d *Dialog) SetChoices(labels []string) {
	d.choices = labels
	d.vm.SetCount(len(labels))
	d.clampState()
	d.stale = true
}

// ChoiceCount returns the number of choices.
func (d *Dialog) ChoiceCount() int { return len(d.choices) }

// Focused reports whether the dialog has keyboard focus.
func (d *Dialog) Focused() bool { return d.focused }

// SetFocused gives or removes focus.
//
// It does not move the focus WITHIN the dialog: losing and regaining focus
// leaves the ring where the user left it, so tabbing away and back does not
// reset a dialog the user had already navigated.
func (d *Dialog) SetFocused(v bool) { d.focused = v }

// Focus returns the index into the focus ring, which is the choices first and
// then the actions. It is 0 on a dialog with nothing to focus.
func (d *Dialog) Focus() int { return d.focus }

// SetFocus focuses ring item i, clamped into range.
//
// A ring item that is a choice is scrolled into view HERE rather than in adapt,
// because moving the focus must not depend on a Draw having happened: keys
// arrive before the first frame in any layout that hands out rectangles and
// then delivers input.
func (d *Dialog) SetFocus(i int) {
	n := d.ringLen()
	if n == 0 {
		d.focus = 0
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	d.focus = i
	d.revealFocus()
}

// FocusCount returns the length of the focus ring: the choices followed by the
// actions.
func (d *Dialog) FocusCount() int { return d.ringLen() }

// FocusedChoice returns the index of the focused choice, or -1 when the focus is
// on an action or the dialog has no choices.
func (d *Dialog) FocusedChoice() int {
	if d.focus < 0 || d.focus >= len(d.choices) {
		return -1
	}
	return d.focus
}

// FocusedAction returns the index of the focused action, or -1 when the focus is
// on a choice.
func (d *Dialog) FocusedAction() int {
	i := d.focus - len(d.choices)
	if i < 0 || i >= len(d.actions) {
		return -1
	}
	return i
}

// Dismissed reports whether the dialog has ended — by an activation or by the
// cancel path — since construction or the last Reset.
//
// It is set whether or not a callback is installed, so an application that polls
// and one that is called back to agree about when the dialog is finished.
func (d *Dialog) Dismissed() bool { return d.dismissed }

// Reset clears the dismissed flag, returns the focus to the ring's first item and
// scrolls the choice list to the top, which is what an application re-showing
// the same dialog wants.
func (d *Dialog) Reset() {
	d.dismissed = false
	d.focus = 0
	d.vm.ScrollToStart()
}

// MinSize returns the smallest dialog worth showing: a title, one body line, one
// action row, and — for a dialog with choices — one choice row.
//
// It is the WHOLE widget including the border and padding the block contributes
// (ADR 0007 §2), it is pure, and it reads nothing that Draw mutates, so it is
// safe before the first draw and safe for a layout to cache.
func (d *Dialog) MinSize() buffer.Size {
	inset := d.blk.Padding
	if d.blk.Border != buffer.BorderNone {
		inset++
	}

	hasBody := buffer.SpansWidth(d.body) > 0
	hasChoices := len(d.choices) > 0

	// The content is one body line unless there is no body at all, and it is
	// every action on ONE row: wrapping is what a narrow dialog falls back to,
	// not what it asks for.
	content := 1
	if hasBody {
		content = minBodyW
	}
	// The TITLE counts too, and this is the one part of MinSize that cannot be
	// delegated to the block: block's own minimum asks whether a title FITS
	// (MinTitleW), not how wide this one IS, because a block may be given no title
	// at all and a dialog is titled by construction. Omitting it would let a dialog
	// with a fifty-cell title report a minimum that truncates it, which is the one
	// thing MinSize exists to prevent.
	if w := buffer.SpansWidth(d.blk.Title()); w > content {
		content = w
	}
	if w := d.actionsWidth(); w > content {
		content = w
	}
	if hasChoices {
		if w := d.choiceMarkWidth() + minChoiceW; w > content {
			content = w
		}
	}

	rows := 0
	if hasBody {
		rows++
	}
	if hasChoices {
		rows++
	}
	rows++ // the action row, which every dialog has

	size := d.blk.MinSize()
	if want := content + 2*inset; size.W < want {
		size.W = want
	}
	if want := rows + 2*inset; size.H < want {
		size.H = want
	}
	return size
}

// Invalidate satisfies termmosaic.Widget, and drops the layout cache.
//
// The dialog repaints its whole rectangle every frame, so there is no dirty
// region to mark — but there IS state derived from the rect and from every
// exported field, and this is the documented way to say it may now be wrong
// (ADR 0007 §3's amendment). A caller that writes Body, BodyStyle or the
// block's own fields directly calls Invalidate; the setters here call it for
// you, and Draw additionally compares the resolved styles so even a direct write
// to a Style field gets a correct frame.
func (d *Dialog) Invalidate() { d.stale = true }

// Handle offers ev to the dialog and reports whether it took it.
//
// Keys are consumed only while the dialog has focus. A press or a wheel notch
// inside Bounds is consumed whether or not it has focus, because pointing at a
// dialog and using it is a request to interact with it — the same rule the rest
// of the catalog follows. A press OUTSIDE Bounds is not consumed, so an
// ancestor can have it.
func (d *Dialog) Handle(ev termmosaic.Event) bool {
	switch ev.Kind {
	case termmosaic.EventKey:
		if !d.focused || ev.Type == termmosaic.KeyRelease {
			return false
		}
		// The layout has to exist before a focus move can scroll a choice into
		// view, and an application may deliver a key before the first frame.
		d.ensureLayout()
		return d.handleKey(ev)
	case termmosaic.EventMouse:
		d.ensureLayout()
		return d.handleMouse(ev)
	default:
		return false
	}
}

// handleKey applies one key. The switch IS the key contract, one case per
// documented binding.
func (d *Dialog) handleKey(ev termmosaic.Event) bool {
	n := d.ringLen()
	switch ev.Key {
	case termmosaic.KeyEscape:
		d.cancelNow()
		return true
	case termmosaic.KeyEnter, termmosaic.KeySpace:
		d.activate()
		return true
	case termmosaic.KeyTab, termmosaic.KeyDown, termmosaic.KeyRight:
		return d.step(1, n)
	case termmosaic.KeyBacktab, termmosaic.KeyUp, termmosaic.KeyLeft:
		return d.step(-1, n)
	case termmosaic.KeyHome:
		return d.jump(0, n)
	case termmosaic.KeyEnd:
		return d.jump(n-1, n)
	case termmosaic.KeyPageUp:
		return d.page(-1, n)
	case termmosaic.KeyPageDown:
		return d.page(1, n)
	}
	// The space bar is a PRINTABLE rune rather than a Key, because ADR 0005
	// §2 routes anything a terminal can type through as a rune. Checking only
	// KeySpace would make "space activates" true in the contract and false in
	// practice, which is the worst kind of divergence there is.
	if isSpace(ev) {
		d.activate()
		return true
	}
	return false
}

// isSpace reports whether ev is an unmodified space bar.
//
// Ctrl+Space and Alt+Space are excluded, because they are chord prefixes rather
// than a second way to press Return, and a modal that consumed them would swallow
// a shortcut an application had every reason to handle itself.
func isSpace(ev termmosaic.Event) bool {
	if ev.Key != termmosaic.KeyNone || ev.Rune != ' ' {
		return false
	}
	return !ev.Mod.Has(termmosaic.ModCtrl) && !ev.Mod.Has(termmosaic.ModAlt)
}

// step moves the focus by delta items, wrapping. It reports whether it consumed
// the key, which is false ONLY when the ring holds a single item: there is
// nowhere to move to, and the application is better placed than this widget to
// decide what Tab means for a one-button dialog.
func (d *Dialog) step(delta, n int) bool {
	if n < 2 {
		return false
	}
	i := d.focus + delta
	if i >= n {
		i = 0
	}
	if i < 0 {
		i = n - 1
	}
	d.SetFocus(i)
	return true
}

// jump focuses ring item i, consuming the key under the same rule step does.
//
// The same n < 2 guard as step, and for the same reason: with one item in the ring
// Home and End have nowhere to go either, and an application that binds Home to
// "scroll to the top of the page behind this dialog" has to be able to have it.
func (d *Dialog) jump(i, n int) bool {
	if n < 2 {
		return false
	}
	d.SetFocus(i)
	return true
}

// page moves the focus by a viewport's worth of ring items, which is what a list
// longer than the dialog needs: without it, reaching choice ninety means ninety
// presses, and Home and End jump past everything in between.
//
// The distance is one screen LESS one item, which is virtual.Model's own rule and
// is not restated here — a dialog that re-derived it would be a second scroll
// arithmetic for one wheel.
//
// The focus lands on an ACTION when the page runs off the end of the choices, which
// is the ring behaving as one list rather than as two: a page from the last choice
// reaches the buttons, and a page from the first button wraps to the last item.
func (d *Dialog) page(delta, n int) bool {
	if n < 2 {
		return false
	}
	step := d.choiceShown - 1
	if step < 1 {
		step = 1
	}
	i := d.focus + delta*step
	// Wrap rather than clamp, because a clamped page key is "nothing happens" at
	// the end of the ring, which the contract does not permit.
	i %= n
	if i < 0 {
		i += n
	}
	d.SetFocus(i)
	return true
}

// activate does whatever the focused ring item means: a choice is chosen, a
// button is pressed.
//
// An empty ring activates nothing and Enter is STILL consumed — a modal that
// lets Enter fall through to the widgets behind it is the bug this whole
// contract exists to prevent.
func (d *Dialog) activate() {
	if i := d.FocusedChoice(); i >= 0 {
		d.dismissed = true
		if d.OnChoice != nil {
			d.OnChoice(i)
		}
		return
	}
	if i := d.FocusedAction(); i >= 0 {
		d.fireAction(i)
	}
}

// cancelNow is the dismissal path, reached by Escape or by Dismiss.
//
// It always sets dismissed and always fires OnCancel, so "nothing happened" is
// not an outcome this package can produce. It additionally fires OnAction when
// the dialog HAS a cancel action, so a dialog whose only wiring is OnAction
// still hears about a dismissal. The two callbacks answer different questions —
// "did the user cancel" and "which button" — and a caller that installs both
// gets one call to each, which is the honest reading of one Escape.
func (d *Dialog) cancelNow() {
	if i := d.cancel; i >= 0 && i < len(d.actions) {
		d.fireAction(i)
	}
	d.dismissed = true
	if d.OnCancel != nil {
		d.OnCancel()
	}
}

// Dismiss ends the dialog from outside the keyboard: an application closing it on
// a timeout, or after work it started itself has finished.
func (d *Dialog) Dismiss() { d.cancelNow() }

// fireAction reports an activation of action i.
func (d *Dialog) fireAction(i int) {
	d.dismissed = true
	if d.OnAction != nil {
		d.OnAction(i)
	}
}

// handleMouse consumes a press or a wheel notch inside Bounds.
func (d *Dialog) handleMouse(ev termmosaic.Event) bool {
	if !d.bounds.Contains(ev.Mouse.X, ev.Mouse.Y) {
		return false
	}
	switch ev.Mouse.Button {
	case termmosaic.MouseWheelUp:
		if len(d.choices) == 0 {
			return false
		}
		d.vm.LineUp(wheelLines)
		return true
	case termmosaic.MouseWheelDown:
		if len(d.choices) == 0 {
			return false
		}
		d.vm.LineDown(wheelLines)
		return true
	}
	if ev.Mouse.Button != termmosaic.MouseLeft || ev.Mouse.Action != termmosaic.MousePress {
		return false
	}

	d.focused = true
	for i := range d.actions {
		if !d.actionRect(i).Contains(ev.Mouse.X, ev.Mouse.Y) {
			continue
		}
		d.SetFocus(len(d.choices) + i)
		d.activate()
		return true
	}
	if i, ok := d.choiceAt(ev.Mouse.X, ev.Mouse.Y); ok {
		d.SetFocus(i)
		d.activate()
		return true
	}
	// Inside the box but on nothing: the dialog takes the focus and nothing else
	// happens. Consuming the press is the point — it was aimed at a modal.
	return true
}

// ringLen returns the length of the focus ring: the choices, then the actions.
func (d *Dialog) ringLen() int { return len(d.choices) + len(d.actions) }

// clampState re-pins everything that indexes into the choices or the actions:
// the focus, the cancel action and the scroll offset. It runs from every mutator
// that changes either list, which is what stops a dialog being left focused on a
// choice that no longer exists.
func (d *Dialog) clampState() {
	switch n := d.ringLen(); {
	case n == 0:
		d.focus = 0
	case d.focus >= n:
		d.focus = n - 1
	case d.focus < 0:
		d.focus = 0
	}
	switch {
	case d.cancel >= len(d.actions):
		d.cancel = len(d.actions) - 1
	case d.cancel < -1:
		d.cancel = -1
	}
	d.vm.ScrollIntoView(d.FocusedChoice())
}

// revealFocus scrolls the focused choice into view and does nothing when the
// focus is on an action. It is O(1) and allocates nothing.
func (d *Dialog) revealFocus() { d.vm.ScrollIntoView(d.FocusedChoice()) }

// styles returns the resolved style set, applying the documented unset
// sentinels: an unset FocusStyle is the action style reversed and an unset
// ChoiceFocusStyle is the choice style reversed.
//
// Both defaults are ATTRIBUTES rather than colours, so focus survives NO_COLOR
// with nothing configured at all — which is the whole reason the ring and the
// attribute are both there.
func (d *Dialog) styles() dialogStyles {
	action := d.ActionStyle.Resolved()
	focus := d.FocusStyle.Resolved()
	if d.FocusStyle.IsUnset() {
		focus = action.WithAttr(buffer.AttrReverse)
	}
	choice := d.ChoiceStyle.Resolved()
	choiceFocus := d.ChoiceFocusStyle.Resolved()
	if d.ChoiceFocusStyle.IsUnset() {
		choiceFocus = choice.WithAttr(buffer.AttrReverse)
	}
	return dialogStyles{action: action, focus: focus, choice: choice, choiceFocus: choiceFocus}
}

// Draw paints the block's chrome, then the body, then the choices, then the
// actions.
//
// It is total for every rect — 0x0, 1x1 and anything below MinSize — and it
// allocates nothing. block.Draw fills the whole of Bounds before drawing
// anything over it, which is ADR 0007 §1 rule 3 expressed the sanctioned way:
// the renderer diffs and never clears, so a dialog that dropped three body rows
// because the terminal shrank must repaint what it no longer draws.
func (d *Dialog) Draw(buf *buffer.Buffer) {
	if d.bounds.Empty() {
		return
	}
	d.blk.Draw(buf)

	in := d.blk.Interior()
	if in.Empty() {
		return
	}
	d.ensureLayout()

	for i := 0; i < d.bodyShown; i++ {
		line := d.bodyLines.Line(i)
		if line == nil {
			break
		}
		y := d.bodyY + i
		if y >= in.Bottom() {
			break
		}
		if i == d.bodyShown-1 && d.bodyLines.Height() > d.bodyShown {
			// The last body line that fits carries the marker, so a message with
			// more BELOW says so rather than stopping mid-sentence without
			// comment. buffer.Truncate owns the horizontal marker, so this writes
			// the vertical one: the line is given up its final cell, and the
			// marker is a single cell write rather than a rebuilt slice.
			buf.SetSpansIn(in.X, in.Right()-1, y, line)
			buf.SetCell(in.Right()-1, y, lastStyle(line).Cell(d.mark))
			continue
		}
		buf.SetSpansIn(in.X, in.Right(), y, line)
	}

	d.drawChoices(buf, in)
	d.drawActions(buf)
}

// ensureLayout rebuilds the cached layout when it cannot describe what Draw would
// draw now: a stale cache, a different interior, or different resolved styles.
//
// The style comparison is the expensive half of ADR 0007 §3's amendment done
// cheaply: a caller that writes ActionStyle or FocusStyle directly rather than
// through a setter still gets a correct frame, because Resolved is a value
// comparison and costs nothing.
func (d *Dialog) ensureLayout() {
	in := d.blk.Interior()
	styles := d.styles()
	if !d.stale && in == d.cachedIn && styles == d.cacheStyles {
		return
	}
	d.adapt(in, styles)
}

// adapt recomputes everything derived from the interior: the wrapped body, the
// action row's wrapping, the vertical budget, and the truncated text of every
// visible row.
//
// It runs at most once per distinct interior, so a drag producing eighteen
// different heights recomputes eighteen times and a steady frame computes
// nothing (ADR 0007 §3). Everything it calls that allocates — Wrap, Truncate,
// geometry.Budget — is called from here and never from Draw (ADR 0008 §4).
func (d *Dialog) adapt(in buffer.Rect, styles dialogStyles) {
	d.stale = false
	d.cachedIn = in
	d.in = in
	d.cacheStyles = styles
	d.mark = truncMark(d.blk.Ascii)
	d.choiceMarkW = d.choiceMarkWidth()

	// The body wraps to the interior. Wrap is total for width <= 0, so a
	// one-column dialog has no body rows rather than a broken one.
	if in.W > 0 {
		d.bodyLines = buffer.Wrap(d.body, in.W)
	} else {
		d.bodyLines = buffer.Wrapped{}
	}

	d.layoutActions(in)

	// The budget asks a coarse question — is there a row for each region at all —
	// and ClampCount then answers the fine one. The split matters: Budget reports
	// a region it cannot wholly fit as DROPPED, so asking it for the body's whole
	// height would throw away the message on a short terminal instead of showing
	// two lines of it.
	d.regions[regionBody] = geometry.Region{Size: present(d.bodyLines.Height()), Prio: geometry.PrioHigh}
	d.regions[regionChoices] = geometry.Region{Size: present(len(d.choices)), Prio: geometry.PrioNormal}
	d.regions[regionActions] = geometry.Region{Size: d.actRows, Prio: geometry.PrioAlways}
	keep := geometry.Budget(d.regions[:], in.H)

	avail := in.H
	if keep[regionActions] {
		avail -= d.actRows
	}
	if avail < 0 {
		avail = 0
	}

	// The body keeps its rows against the choice list's claim on ONE of them, so
	// a dialog too short for both shows the question rather than showing neither.
	// A row is reserved for the choice list only when a choice can actually be
	// drawn there: reserving one for a list the marker column has already made
	// impossible would cost the body a row to buy nothing.
	reserved := 0
	if len(d.choices) > 0 && keep[regionChoices] && d.choiceMarkW < in.W {
		reserved = 1
	}
	d.bodyShown = 0
	if keep[regionBody] {
		d.bodyShown = geometry.ClampCount(d.bodyLines.Height(), avail-reserved)
	}
	d.choiceShown = 0
	if keep[regionChoices] {
		d.choiceShown = geometry.ClampCount(len(d.choices), avail-d.bodyShown)
	}

	// The stack — body, then choices — is centred in the space above the action
	// row, and the actions are pinned to the BOTTOM of the box. Pinning them is
	// what keeps a dialog's buttons still while its choice list scrolls under
	// them, and it is the placement a user expects from a modal.
	contentH := d.bodyShown + d.choiceShown
	top := in.Y
	if slack := avail - contentH; slack > 0 {
		top += slack / 2
	}
	d.bodyY = top
	d.choiceTop = top + d.bodyShown
	d.actTop = in.Bottom() - d.actRows
	if d.actTop < in.Y {
		d.actTop = in.Y
	}

	d.vm.SetViewport(d.choiceShown)
	d.revealFocus()
	d.rebuildChoices(in, styles)
}

// present returns 1 when n is positive and 0 otherwise: a region's Size in the
// budget is the smallest one that still shows something, so an absent region
// asks for nothing and never survives on its own.
func present(n int) int {
	if n > 0 {
		return 1
	}
	return 0
}

// layoutActions wraps the action row to the interior width and records each
// action's column, width and row, plus its truncated label.
//
// The wrap is greedy and left to right, and it is this widget's one genuinely
// size-DEPENDENT structure: a confirm dialog is two buttons on one row at 40
// cells and two buttons on two rows at 12. That is a different arrangement, not
// the same one squeezed — which is what "responsive" has to mean in a cell grid.
func (d *Dialog) layoutActions(in buffer.Rect) {
	n := len(d.actions)
	d.actX = resizeInts(d.actX, n)
	d.actW = resizeInts(d.actW, n)
	d.actRow = resizeInts(d.actRow, n)
	d.actLabels = resizeSpans(d.actLabels, n)
	d.actFocusLabels = resizeSpans(d.actFocusLabels, n)

	row, x := 0, 0
	for i := range d.actions {
		w := buffer.StringWidth(d.actions[i].Label) + actionRing
		if w < 1 {
			w = 1
		}
		if i > 0 {
			if x > 0 && x+actionGap+w > in.W {
				row++
				x = 0
			} else {
				x += actionGap
			}
		}
		if w > in.W {
			// An action wider than the interior is clipped to it rather than
			// allowed to spill over the border beside it.
			w = in.W
		}
		d.actX[i], d.actW[i], d.actRow[i] = x, w, row
		x += w

		label := d.cacheStyles.action
		if !d.actions[i].Style.IsUnset() {
			label = d.actions[i].Style.Resolved()
		}
		// An action's own style wins over ActionStyle for both renditions, and
		// the focused one only GAINS the attribute — a bespoke button keeps its
		// colours and becomes reverse, which is what "focus is an attribute" has
		// to mean when a caller has already chosen colours.
		d.actLabels[i] = capRow(d.blk.Ascii, d.actions[i].Label, label, w-actionRing)
		d.actFocusLabels[i] = capRow(d.blk.Ascii, d.actions[i].Label,
			label.WithAttr(d.cacheStyles.focus.Attr), w-actionRing)
	}
	d.actRows = row + 1
	if n == 0 {
		d.actRows = 0
	}
}

// rebuildChoices truncates the labels of the VISIBLE choices.
//
// Only the visible window is prepared, so the cost is O(what is on screen) per
// size change rather than O(how many choices there are) — ADR 0007 §6's rule,
// and the reason a choice dialog with a thousand entries is not slower than one
// with three.
func (d *Dialog) rebuildChoices(in buffer.Rect, styles dialogStyles) {
	d.choiceLabels = d.choiceLabels[:0]
	if d.choiceShown <= 0 || d.choiceMarkW >= in.W {
		return
	}
	first, _ := d.vm.Range()
	textW := in.W - d.choiceMarkW
	// The loop bound is the VISIBLE WINDOW, and that is the whole O(visible) claim:
	// a thousand-choice dialog and a three-choice one cost the same per resize,
	// because nothing here ranges over the collection. Both bounds are kept because
	// the window can extend past the collection when a shrink clamps the offset.
	for i := first; i < first+d.choiceShown && i < len(d.choices); i++ {
		d.choiceLabels = append(d.choiceLabels,
			capRow(d.blk.Ascii, d.choices[i], styles.choice, textW))
	}
}

// actionRect returns action i's rectangle in screen cells: its column within the
// interior, on its row of the action area, clipped to the interior on both axes.
//
// The clipping is what makes a press and a paint agree: a hit test on a
// half-visible button outside the box would activate a button the user cannot
// see.
func (d *Dialog) actionRect(i int) buffer.Rect {
	if i < 0 || i >= len(d.actions) || i >= len(d.actX) {
		return buffer.Rect{}
	}
	if d.actW[i] < 1 || d.actRows < 1 {
		return buffer.Rect{}
	}
	w := d.actW[i]
	if x := d.in.X + d.actX[i]; w > d.in.Right()-x {
		w = d.in.Right() - x
	}
	y := d.actTop + d.actRow[i]
	if y >= d.in.Bottom() {
		return buffer.Rect{}
	}
	if y < d.in.Y {
		return buffer.Rect{}
	}
	return buffer.Rect{X: d.in.X + d.actX[i], Y: y, W: w, H: 1}
}

// drawChoices paints the visible choices: the marker in its own column, the
// focused row in ChoiceFocusStyle across its whole width, and the label clipped
// to what is left.
//
// A choice with no room beside the marker is left empty rather than spilling over
// its neighbour, which is the same rule the rest of the catalog follows. No
// choice row carries a truncation marker for the list continuing below it: the
// marker column and the scroll offset already say how many there are, and a
// second marker on the same row would be a second thing to read.
func (d *Dialog) drawChoices(buf *buffer.Buffer, in buffer.Rect) {
	if d.choiceShown <= 0 || d.choiceMarkW >= in.W {
		return
	}
	textX := in.X + d.choiceMarkW
	first, _ := d.vm.Range()
	for row := 0; row < d.choiceShown && row < len(d.choiceLabels); row++ {
		i := first + row
		if i >= len(d.choices) {
			break
		}
		y := d.choiceTop + row
		if y < in.Y || y >= in.Bottom() {
			break
		}
		style := d.cacheStyles.choice
		if i == d.FocusedChoice() {
			style = d.cacheStyles.choiceFocus
		}
		buf.FillRect(buffer.Rect{X: in.X, Y: y, W: in.W, H: 1}, style.Blank())
		if i == d.FocusedChoice() {
			buf.SetStringIn(in.X, textX, y, ChoiceMarker, style)
		}
		buf.SetSpansCappedIn(textX, in.Right(), y, d.choiceLabels[row], d.mark)
	}
}

// drawActions paints the action row: each button's background across its whole
// width, then the focus ring, then the label.
//
// The ring is drawn rather than baked into the cached label spans, so moving the
// focus does not invalidate the layout cache — and moving the focus is the one
// thing a dialog does constantly.
func (d *Dialog) drawActions(buf *buffer.Buffer) {
	for i := range d.actions {
		row := d.actionRect(i)
		if row.Empty() {
			continue
		}
		style := d.cacheStyles.action
		label := d.actLabels[i]
		focused := i == d.FocusedAction()
		if focused {
			style, label = d.cacheStyles.focus, d.actFocusLabels[i]
		}
		// The fill comes FIRST: it repaints the whole button (ADR 0007 §1 rule
		// 3), and a ring written before it would be painted over by its own
		// background.
		buf.FillRect(row, style.Blank())
		if focused {
			buf.SetStringIn(row.X, row.X+1, row.Y, ActionRingFocused[:1], style)
			if row.W >= 2 {
				buf.SetStringIn(row.Right()-1, row.Right(), row.Y, ActionRingFocused[1:], style)
			}
		}
		if row.W > actionRing && len(label) > 0 {
			buf.SetSpansIn(row.X+1, row.Right()-1, row.Y, label)
		}
	}
}

// choiceAt returns the choice index at a cell, or false when the cell is not on a
// visible choice row. It is the inverse of drawChoices, for a press.
func (d *Dialog) choiceAt(x, y int) (int, bool) {
	in := d.in
	if in.Empty() || d.choiceShown <= 0 {
		return 0, false
	}
	if x < in.X || x >= in.Right() || y < d.choiceTop || y >= d.choiceTop+d.choiceShown {
		return 0, false
	}
	return d.vm.ItemAt(y - d.choiceTop)
}

// actionsWidth returns the width every action needs on ONE row, gaps included,
// which is what MinSize asks for: a wrapped action row is what a narrow dialog
// falls back to, not what it wants.
func (d *Dialog) actionsWidth() int {
	w := 0
	for i := range d.actions {
		a := buffer.StringWidth(d.actions[i].Label) + actionRing
		if a < 1 {
			a = 1
		}
		if i > 0 {
			w += actionGap
		}
		w += a
	}
	return w
}

// choiceMarkWidth returns the marker column's width: ChoiceMarker plus the pad,
// and at least one cell even for an empty marker so every label starts on the
// same column.
func (d *Dialog) choiceMarkWidth() int {
	w := buffer.StringWidth(ChoiceMarker)
	if w < 1 {
		w = 1
	}
	return w + choiceMarkPad
}

// itoa is strconv.Itoa without the import, for a diagnostic string on an error
// path only. A small int converted here cannot allocate, because the result
// never escapes.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// firstRune returns the first rune of s, or 0 when it is empty.
//
// It is for a one-cell marker: a caller may write a multi-byte glyph and must not
// have to index its bytes, which would split the UTF-8 sequence and write a
// replacement character.
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// truncMark returns the one-cell truncation marker for the given ASCII rung.
// Both rungs are one cell wide — buffer's invariant — so picking between them
// cannot shift a layout, only which glyph the reader sees.
func truncMark(ascii bool) rune {
	if ascii {
		return firstRune(buffer.AscTruncSuffix)
	}
	return firstRune(buffer.TruncSuffix)
}

// capRow builds the truncated spans for one row of text of at most width cells.
//
// It allocates, so it is called from adapt and never from Draw — which is exactly
// the rule ADR 0008 §4 states for Truncate.
func capRow(ascii bool, text string, st buffer.Style, width int) []buffer.Span {
	if width <= 0 {
		return nil
	}
	spans := []buffer.Span{buffer.NewSpan(text, st)}
	if buffer.StringWidth(text) <= width {
		return spans
	}
	if ascii {
		return buffer.TruncateASCII(spans, width)
	}
	return buffer.Truncate(spans, width)
}

// lastStyle returns the rendition of the last non-empty span, which is the style
// a truncation marker written beside a line of mixed-styled text takes.
//
// It is buffer.Truncate's own rule for the marker, restated here because the body
// writes its marker directly rather than through Truncate: a line whose last span
// is bold must not end in a faint ellipsis that belongs to a span that was clipped
// away entirely.
func lastStyle(spans []buffer.Span) buffer.Style {
	for i := len(spans) - 1; i >= 0; i-- {
		if spans[i].Text != "" {
			return spans[i].Style.Resolved()
		}
	}
	return buffer.DefaultStyle
}

// resizeInts returns s resized to n elements, reusing its capacity. It is the one
// place this package grows a cache, so a dialog's per-frame cost is zero whether
// its action list grew once at construction or never.
func resizeInts(s []int, n int) []int {
	if cap(s) < n {
		return make([]int, n)
	}
	return s[:n]
}

// resizeSpans is resizeInts for span slices.
func resizeSpans(s [][]buffer.Span, n int) [][]buffer.Span {
	if cap(s) < n {
		return make([][]buffer.Span, n)
	}
	return s[:n]
}

// Dialog is a Widget, a Focusable and a Minimizable.
var (
	_ termmosaic.Widget      = (*Dialog)(nil)
	_ termmosaic.Focusable   = (*Dialog)(nil)
	_ termmosaic.Minimizable = (*Dialog)(nil)
)
