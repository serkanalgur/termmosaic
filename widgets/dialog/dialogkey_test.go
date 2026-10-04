package dialog

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

// recorder collects what a dialog reported, so a key-contract test can assert on
// the CALLBACK rather than on internal state — which is the only thing an
// application can actually observe.
type recorder struct {
	actions []int
	choices []int
	cancels int
}

func (r *recorder) onAction(i int) { r.actions = append(r.actions, i) }
func (r *recorder) onChoice(i int) { r.choices = append(r.choices, i) }
func (r *recorder) onCancel()      { r.cancels++ }

// watched returns d with the recorder wired to every callback, so a test can read
// what the dialog reported without each one re-assembling the wiring.
func watched(d *Dialog) (*Dialog, *recorder) {
	r := &recorder{}
	d.OnAction = r.onAction
	d.OnChoice = r.onChoice
	d.OnCancel = r.onCancel
	return d, r
}

// lastChoice returns the last choice reported, or -1 when none was.
func (r *recorder) lastChoice() int {
	if len(r.choices) == 0 {
		return -1
	}
	return r.choices[len(r.choices)-1]
}

// lastAction returns the last action reported, or -1 when none was.
func (r *recorder) lastAction() int {
	if len(r.actions) == 0 {
		return -1
	}
	return r.actions[len(r.actions)-1]
}

// The escape sequences the tests use, named rather than spelled inline so the
// contract reads as a table of KEYS and not as a table of escape sequences.
const (
	keyEnter   = "\r"
	keySpace   = " "
	keyTab     = "\t"
	keyBacktab = "\x1b[Z"
	keyUp      = "\x1b[A"
	keyDown    = "\x1b[B"
	keyLeft    = "\x1b[D"
	keyRight   = "\x1b[C"
	keyHome    = "\x1b[H"
	keyEnd     = "\x1b[F"
)

// escape is the Escape key, which cannot go through the decoder: a bare ESC byte is
// indistinguishable from the first byte of a sequence, and input.Decode reports
// StatusIncomplete for it by design (ADR 0005 §2). input.Parser resolves that
// ambiguity with a deadline; a test has no deadline, so it builds the event.
func escape() termmosaic.Event { return termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0) }

// TestEnterActivatesTheFocusedAction is the first half of the key contract, on the
// variant where it is least ambiguous: one button, ringed by default.
func TestEnterActivatesTheFocusedAction(t *testing.T) {
	d, rec := watched(infoDialog(40, 9))
	focus(d)

	mustPress(t, d, keyEnter)
	if got := rec.lastAction(); got != 0 {
		t.Errorf("Enter reported action %d, want 0 (%s)", got, d.Actions()[got].Label)
	}
	if !d.Dismissed() {
		t.Error("activating a button did not set Dismissed")
	}
	if rec.cancels != 0 {
		t.Errorf("Enter fired OnCancel %d times: activation is not a cancellation", rec.cancels)
	}
}

// TestSpaceActivatesTheFocusedAction pins the second activation key, so a user who
// has muscle memory for a button does not get nothing.
func TestSpaceActivatesTheFocusedAction(t *testing.T) {
	d, rec := watched(confirmDialog(40, 9))
	focus(d)
	d.SetFocus(0)

	mustPress(t, d, keySpace)
	if got := rec.lastAction(); got != 0 {
		t.Errorf("Space reported action %d, want 0 (%s)", got, d.Actions()[got].Label)
	}
}

// TestEnterOnAFocusedChoiceChoosesIt is the choice variant's half of the contract,
// and the reason the ring holds choices and actions together: Enter means "yes to
// whatever is ringed", and on a choice that is the choice.
func TestEnterOnAFocusedChoiceChoosesIt(t *testing.T) {
	d, rec := watched(choiceDialog(40, 12, 5))
	focus(d)
	d.SetFocus(2)

	mustPress(t, d, keyEnter)
	if got := rec.lastChoice(); got != 2 {
		t.Errorf("Enter on choice 2 reported %d", got)
	}
	if len(rec.actions) != 0 {
		t.Errorf("choosing also reported an action: %v", rec.actions)
	}
	if !d.Dismissed() {
		t.Error("choosing did not set Dismissed")
	}
}

// TestTabAndArrowsTraverseTheRingAndWrap is the traversal contract, and it is
// tested over EVERY binding that moves the focus rather than one of them: a widget
// that honours Tab and ignores the arrows has satisfied the letter of the contract
// and failed the point of it.
func TestTabAndArrowsTraverseTheRingAndWrap(t *testing.T) {
	// Six ring items: three choices, then the Cancel button, then two more actions.
	// Both directions, one key each, over a ring of six: three choices and three
	// buttons. Two items is not enough to tell a wrap from a clamp, which is why
	// the ring is walked a full turn in each direction rather than twice.
	for _, tc := range []struct {
		name     string
		forward  string
		backward string
	}{
		// One pair per DIRECTION, not one pair per key group: the point is that
		// every forward key behaves identically and every backward key behaves
		// identically, and the table has to say which is which correctly or it
		// asserts the opposite of what it claims.
		{"tab", keyTab, keyBacktab},
		{"down and left", keyDown, keyLeft},
		{"right and up", keyRight, keyUp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := choiceDialog(40, 12, 3)
			d.SetActions(Action{Label: DefaultCancelLabel}, Action{Label: DefaultOKLabel}, Action{Label: "Later"})
			focus(d)
			n := d.FocusCount()
			if n != 6 {
				t.Fatalf("the ring has %d items, want 6", n)
			}

			// A full turn forward, one item per press. The arithmetic is checked
			// against the ring LENGTH rather than a hard-coded index, so the
			// assertion cannot pass by agreeing with itself.
			for i := 0; i < n; i++ {
				want := (d.Focus() + 1) % n
				mustPress(t, d, tc.forward)
				if got := d.Focus(); got != want {
					t.Fatalf("forward went to %d, want %d", got, want)
				}
			}

			// And a full turn back, which is what proves the wrap rather than a
			// clamp at the ends.
			for i := 0; i < n; i++ {
				want := (d.Focus() - 1 + n) % n
				mustPress(t, d, tc.backward)
				if got := d.Focus(); got != want {
					t.Fatalf("backward went to %d, want %d", got, want)
				}
			}
			if got, want := d.Focus(), 0; got != want {
				t.Errorf("after a full turn forward and back the focus is %d, want %d", got, want)
			}
		})
	}
}

// TestHomeAndEndJumpToTheEndsOfTheRing covers the two jump keys, which exist so a
// user does not have to press Tab nine times to reach the third button.
func TestHomeAndEndJumpToTheEndsOfTheRing(t *testing.T) {
	d := choiceDialog(40, 12, 3)
	d.SetActions(Action{Label: DefaultCancelLabel}, Action{Label: DefaultOKLabel})
	focus(d)

	mustPress(t, d, keyEnd)
	if got := d.Focus(); got != d.FocusCount()-1 {
		t.Errorf("End went to %d, want the last ring item %d", got, d.FocusCount()-1)
	}
	if got := d.FocusedAction(); got != 1 {
		t.Errorf("the last ring item is action %d, want 1", got)
	}

	mustPress(t, d, keyHome)
	if got := d.Focus(); got != 0 {
		t.Errorf("Home went to %d, want 0", got)
	}
	if got := d.FocusedChoice(); got != 0 {
		t.Errorf("the first ring item is choice %d, want 0", got)
	}
}

// TestEscapeIsCancelAndNeverNothing is the contract's most important row, and it
// is stated as a table over EVERY variant including the degenerate ones — a dialog
// where Escape does nothing is a modal the user cannot leave.
//
// "Never nothing" is asserted three ways at once, because any one of them alone
// would pass on a widget that got the others wrong: the event is consumed, the
// dialog reports itself dismissed, and the cancellation callback fires.
func TestEscapeIsCancelAndNeverNothing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		d      *Dialog
		action int // the action Escape is expected to report, or -1 for none
	}{
		{"info", infoDialog(40, 9), 0},
		{"confirm", confirmDialog(40, 9), 0},
		{"choice", choiceDialog(40, 12, 4), 0},
		{"focused on a choice", atSize(choiceDialog(40, 12, 4), 40, 12), 0},
		{"no actions at all", New(rect(40, 9), VariantInfo), -1},
		{"empty ring", New(rect(40, 9), VariantChoice), -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, rec := watched(tc.d)
			if tc.name == "focused on a choice" {
				d.SetFocus(1)
			}
			if tc.name == "no actions at all" || tc.name == "empty ring" {
				d.SetActions()
			}
			focus(d)

			if !d.Handle(escape()) {
				t.Fatal("Escape was not consumed: the key would reach the widgets behind the modal")
			}
			if !d.Dismissed() {
				t.Error("Escape did not set Dismissed, so a polling application would never hide the dialog")
			}
			if rec.cancels != 1 {
				t.Errorf("Escape fired OnCancel %d times, want exactly 1", rec.cancels)
			}
			if tc.action >= 0 {
				if got := rec.lastAction(); got != tc.action {
					t.Errorf("Escape reported action %d, want the cancel action %d", got, tc.action)
				}
			} else if len(rec.actions) != 0 {
				t.Errorf("a dialog with no actions reported action %v", rec.actions)
			}
		})
	}
}

// TestEscapeFollowsTheCancelAction pins that Escape means CANCEL rather than
// "whatever happens to be first", so an application can put its affirmative button
// first and still have Escape mean no.
func TestEscapeFollowsTheCancelAction(t *testing.T) {
	d, rec := watched(confirmDialog(40, 9))
	d.SetActions(Action{Label: DefaultOKLabel}, Action{Label: DefaultCancelLabel})
	d.SetCancelAction(1)
	focus(d)

	d.Handle(escape())
	if got := rec.lastAction(); got != 1 {
		t.Errorf("Escape reported action %d (%s), want the cancel action 1 (%s)",
			got, d.Actions()[got].Label, DefaultCancelLabel)
	}
}

// TestDismissIsTheSamePathAsEscape covers the out-of-band close: an application
// that closes a dialog on a timeout should get exactly what Escape gives it.
func TestDismissIsTheSamePathAsEscape(t *testing.T) {
	d, rec := watched(confirmDialog(40, 9))
	focus(d)

	d.Dismiss()
	if !d.Dismissed() {
		t.Error("Dismiss did not set Dismissed")
	}
	if rec.cancels != 1 || rec.lastAction() != 0 {
		t.Errorf("Dismiss reported cancels=%d actions=%v, want one of each with action 0",
			rec.cancels, rec.actions)
	}

	d.Reset()
	if d.Dismissed() {
		t.Error("Reset did not clear Dismissed, so a re-shown dialog looks finished")
	}
	if got := d.Focus(); got != 0 {
		t.Errorf("Reset left the focus at %d, want the first ring item", got)
	}
}

// TestKeysAreIgnoredWhileUnfocused is the composability half of the contract: an
// unfocused dialog must not eat a key meant for a sibling, which is what lets a
// dialog be one focusable among several.
//
// It checks EVERY documented key, not a sample, because "consumed only while
// focused" is a claim about all of them.
func TestKeysAreIgnoredWhileUnfocused(t *testing.T) {
	for _, seq := range []string{
		keyEnter, keySpace, keyTab, keyBacktab, keyUp, keyDown, keyLeft, keyRight, keyHome, keyEnd,
	} { // every documented key, not a sample
		d, rec := watched(confirmDialog(40, 9))
		if press(t, d, seq) {
			t.Errorf("%q was consumed by a dialog without focus", seq)
		}
		if len(rec.actions) != 0 || len(rec.choices) != 0 || rec.cancels != 0 {
			t.Errorf("%q did something on an unfocused dialog: %+v", seq, rec)
		}
	}
	// And Escape, like every other key, is not consumed either: an unfocused dialog
	// must not be able to dismiss itself, or a key aimed at a sibling widget behind
	// it would close a modal the user never touched.
	if d, rec := watched(confirmDialog(40, 9)); d.Handle(escape()) {
		t.Error("Escape was consumed by a dialog without focus")
	} else if rec.cancels != 0 || d.Dismissed() {
		t.Errorf("an unfocused dialog dismissed itself: cancels=%d dismissed=%v", rec.cancels, d.Dismissed())
	}
}

// TestKeyReleaseIsNotConsumed covers the other half of the event model: without the
// kitty protocol a key release is never emitted, so honouring one would only ever
// happen under a protocol the application did not negotiate — and a release is not
// an activation however it arrives.
func TestKeyReleaseIsNotConsumed(t *testing.T) {
	d, rec := watched(confirmDialog(40, 9))
	focus(d)

	ev := termmosaic.SpecialKeyEvent(termmosaic.KeyEnter, 0)
	ev.Type = termmosaic.KeyRelease
	if d.Handle(ev) {
		t.Error("a key release was consumed as an activation")
	}
	if len(rec.actions) != 0 {
		t.Errorf("a key release reported action %v", rec.actions)
	}
}

// TestAnUnboundKeyIsNotConsumed is the negative space of the contract: a dialog
// that swallows every key is a dialog the application cannot add a shortcut to.
func TestAnUnboundKeyIsNotConsumed(t *testing.T) {
	d := infoDialog(40, 9)
	focus(d)
	for _, seq := range []string{"q", "\x1b[5~", "\x1b[15~", "\x7f"} {
		if press(t, d, seq) {
			t.Errorf("%q was consumed but is not in the key contract", seq)
		}
	}
}

// TestNavigationKeysAreNotConsumedWithOneRingItem is the documented
// non-consumption, and it is tested rather than assumed: with one item in the ring
// there is nowhere to move, so the key is the application's to interpret — and for
// a one-button dialog, Tab is the only way a user can ask for focus to move on.
func TestNavigationKeysAreNotConsumedWithOneRingItem(t *testing.T) {
	d := infoDialog(40, 9) // one action, no choices: a ring of exactly one
	if got := d.FocusCount(); got != 1 {
		t.Fatalf("the info dialog's ring has %d items, want 1", got)
	}
	focus(d)

	for _, seq := range []string{keyTab, keyBacktab, keyUp, keyDown, keyLeft, keyRight, keyHome, keyEnd} {
		if press(t, d, seq) {
			t.Errorf("%q was consumed by a dialog whose ring has one item; the application "+
				"needs it to move focus out", seq)
		}
	}

	// The other half: with TWO items every one of those keys IS consumed, which is
	// what makes the rule about the ring's length rather than about the keys.
	two := confirmDialog(40, 9)
	if got := two.FocusCount(); got != 2 {
		t.Fatalf("the confirm dialog's ring has %d items, want 2", got)
	}
	focus(two)
	for _, seq := range []string{keyTab, keyBacktab, keyUp, keyDown, keyLeft, keyRight, keyHome, keyEnd} {
		if !press(t, two, seq) {
			t.Errorf("%q was not consumed by a dialog with two ring items", seq)
		}
	}
	// The activation keys are still consumed: that is the asymmetry, and it is the
	// whole reason the rule is worth stating.
	for _, seq := range []string{keyEnter, keySpace} {
		if !press(t, d, seq) {
			t.Errorf("%q was not consumed even though it is in the contract", seq)
		}
	}
}

// TestEnterOnAnEmptyRingIsStillConsumed is the other half: a dialog with nothing to
// activate still swallows Enter, because a modal that lets it fall through sends
// the user's confirmation to whatever is behind the modal.
func TestEnterOnAnEmptyRingIsStillConsumed(t *testing.T) {
	d := New(rect(40, 9), VariantChoice)
	d.SetActions()
	focus(d)

	for _, seq := range []string{keyEnter, keySpace} {
		if !press(t, d, seq) {
			t.Errorf("%q reached the widgets behind a modal with an empty ring", seq)
		}
	}
}

// TestMovingFocusIntoTheListScrollsIt is what makes a choice list longer than the
// dialog usable: the ring moves, and the viewport follows.
//
// It uses a list far longer than the dialog, so without ScrollIntoView the focused
// choice would be off screen and Enter would choose something invisible.
func TestMovingFocusIntoTheListScrollsIt(t *testing.T) {
	d := choiceDialog(30, 9, 20)
	d.Draw(cellBuf(30, 9)) // the viewport only exists once the layout has been adapted
	focus(d)

	visible := func() (int, int) { return d.vm.Range() }
	first, last := visible()
	if first != 0 {
		t.Fatalf("a fresh list starts at offset %d, want 0", first)
	}
	if last <= first {
		t.Fatalf("the list shows %d rows at 30x9, which is too few to test with", last-first)
	}

	// Walk down past the bottom of the viewport, then check the focused choice is
	// actually among the visible ones.
	for i := 0; i < last; i++ {
		mustPress(t, d, keyDown)
	}
	f, l := visible()
	if got := d.FocusedChoice(); got < f || got >= l {
		t.Errorf("the focused choice %d is outside the visible window [%d,%d)", got, f, l)
	}
	// And it is the LAST visible one, because ScrollIntoView moves the minimum
	// amount rather than recentring — ADR 0007 §6 rule 2.
	if got := l - 1; got != d.FocusedChoice() {
		t.Errorf("the focused choice is %d and the last visible is %d: the offset moved by more "+
			"than the minimum", d.FocusedChoice(), got)
	}

	// Coming back up returns to the top without a jump.
	mustPress(t, d, keyHome)
	if f, _ := visible(); f != 0 {
		t.Errorf("Home left the list scrolled to offset %d, want 0", f)
	}
}

// TestTheWheelScrollsWithoutMovingFocus is the one binding that is deliberately NOT
// a focus move, and it is the distinction the catalog draws everywhere: scrolling
// is not navigating. A dialog where the wheel moved the selection would make a
// long list impossible to read without changing what Enter would do.
func TestTheWheelScrollsWithoutMovingFocus(t *testing.T) {
	d := choiceDialog(30, 9, 40)
	d.Draw(cellBuf(30, 9))
	focus(d)

	before, chosen := d.Focus(), d.FocusedChoice()
	at := d.vm.Offset()

	// Two notches down, then one up: scrolling DOWN then back is what distinguishes
	// a scroll from a jump-to-the-end, and a single notch would satisfy a weaker
	// assertion that the engine clamps at the ends anyway.
	for i := 0; i < 2; i++ {
		if !wheelAt(d, termmosaic.MouseWheelDown, d.Bounds().X+2, d.Bounds().Y+2) {
			t.Fatalf("wheel notch %d down inside the dialog was not consumed", i)
		}
	}
	down := d.vm.Offset()
	if down <= at {
		t.Fatalf("the wheel did not scroll: offset %d -> %d", at, down)
	}
	if down-at != 2*wheelLines {
		t.Errorf("two notches moved the offset by %d, want %d", down-at, 2*wheelLines)
	}
	if got := d.Focus(); got != before {
		t.Errorf("the wheel moved the focus from %d to %d", before, got)
	}
	if got := d.FocusedChoice(); got != chosen {
		t.Errorf("the wheel moved the chosen choice from %d to %d", chosen, got)
	}

	if !wheelAt(d, termmosaic.MouseWheelUp, d.Bounds().X+2, d.Bounds().Y+2) {
		t.Error("a wheel notch up was not consumed")
	}
	if got, want := d.vm.Offset(), down-wheelLines; got != want {
		t.Errorf("after scrolling back up the offset is %d, want %d", got, want)
	}
}

// TestTheWheelIsNotConsumedWithoutChoices keeps the wheel's contract honest: a
// dialog with no list has nothing to scroll, so the event belongs to whatever is
// behind it rather than being silently eaten.
func TestTheWheelIsNotConsumedWithoutChoices(t *testing.T) {
	d := confirmDialog(40, 9)
	if wheelAt(d, termmosaic.MouseWheelDown, 2, 2) {
		t.Error("a wheel notch was consumed by a dialog with no choice list")
	}
}

// TestAPressOnAButtonActivatesIt covers the mouse as a complete interaction: a
// click is a request to press, so the application needs no click handler.
func TestAPressOnAButtonActivatesIt(t *testing.T) {
	d, rec := watched(confirmDialog(40, 9))
	d.Draw(cellBuf(40, 9)) // so the action rects exist before the click

	// The OK button is located by LOOKING at the screen rather than by
	// re-deriving the widget's arithmetic, so this test breaks if the button
	// moves somewhere a user could not press it, rather than on a stale column.
	y, x := actionRowOf(t, d), -1
	if _, rowHas := cellOf(t, renderScreen(t, d), y, DefaultOKLabel); rowHas {
		x = -2
	}
	if x == -2 {
		at, ok := cellOf(t, renderScreen(t, d), y, DefaultOKLabel)
		if !ok {
			t.Fatalf("the OK button is not on row %d of the screen:\n%s", y, renderScreen(t, d).String())
		}
		x = at + 1 // inside the label rather than on its first rune's edge
	} else {
		t.Fatalf("the OK button is not on screen:\n%s", renderScreen(t, d).String())
	}
	if !clickAt(d, x, y) {
		t.Fatal("a press on a button was not consumed")
	}
	if got := rec.lastAction(); got != 1 {
		t.Errorf("the press reported action %d, want 1", got)
	}
	if !d.Focused() {
		t.Error("the press did not take focus")
	}
}

// TestAPressOnAChoiceChoosesIt is the same interaction on the choice list, and it
// asserts the choice index rather than the row so a layout change cannot make the
// test pass for the wrong reason.
func TestAPressOnAChoiceChoosesIt(t *testing.T) {
	d, rec := watched(choiceDialog(40, 12, 6))
	d.Draw(cellBuf(40, 12))

	// The choice row for the THIRD choice, found by looking for the marker rather
	// than by counting rows: a test that hard-codes the row would keep passing
	// after the layout changed underneath it.
	y := choiceRowOf(t, d, 2)
	if !clickAt(d, d.Bounds().X+3, y) {
		t.Fatalf("a press on choice row %d was not consumed", y)
	}
	if got := rec.lastChoice(); got != 2 {
		t.Errorf("the press chose %d, want 2: the row-to-choice mapping is off by one", got)
	}
	if got := d.FocusedChoice(); got != 2 {
		t.Errorf("after the press the focused choice is %d, want 2", got)
	}
}

// TestAPressOutsideBoundsIsNotConsumed is what lets an ancestor see a click on the
// widgets behind a dialog, which is how an application dismisses a modal by
// clicking away from it.
func TestAPressOutsideBoundsIsNotConsumed(t *testing.T) {
	d, rec := watched(infoDialog(40, 9))
	d.Draw(cellBuf(40, 9))

	// Every point here is strictly outside a 40x9 rect at the origin. (39,8) is the
	// last cell INSIDE it, which is why it belongs in the click test and not here.
	for _, p := range [][2]int{{-1, -1}, {40, 0}, {0, 9}, {40, 9}, {60, 4}, {-5, 4}, {4, -5}} {
		if clickAt(d, p[0], p[1]) {
			t.Errorf("a press at (%d,%d), outside Bounds, was consumed", p[0], p[1])
		}
	}
	if len(rec.actions) != 0 || len(rec.choices) != 0 {
		t.Errorf("a press outside Bounds did something: %+v", rec)
	}
}

// TestAPressInsideTheBoxTakesFocusAndNothingElse covers the space between the rows:
// a press on the body or on the padding is inside a modal, so it is consumed and it
// takes focus — it just does not activate anything.
func TestAPressInsideTheBoxTakesFocusAndNothingElse(t *testing.T) {
	d, rec := watched(confirmDialog(40, 9))
	d.Draw(cellBuf(40, 9))
	if d.Focused() {
		t.Fatal("setup: the dialog should start unfocused")
	}

	// Row 1 is the first interior row, which is above the body and the buttons.
	if !clickAt(d, 3, 1) {
		t.Fatal("a press inside the box was not consumed: it would reach the widgets behind a modal")
	}
	if !d.Focused() {
		t.Error("the press did not take focus")
	}
	if len(rec.actions) != 0 || len(rec.choices) != 0 || rec.cancels != 0 {
		t.Errorf("the press activated something: %+v", rec)
	}
	if d.Dismissed() {
		t.Error("the press dismissed the dialog")
	}
}

// TestANonPressMouseEventIsNotConsumed keeps the mouse contract narrow: only a left
// PRESS is an interaction. A drag, a release and a right-click belong to whatever
// else wants them.
func TestANonPressMouseEventIsNotConsumed(t *testing.T) {
	d := infoDialog(40, 9)
	d.Draw(cellBuf(40, 9))

	for _, m := range []termmosaic.Mouse{
		{X: 3, Y: 7, Button: termmosaic.MouseLeft, Action: termmosaic.MouseRelease},
		{X: 3, Y: 7, Button: termmosaic.MouseRight, Action: termmosaic.MousePress},
		{X: 3, Y: 7, Button: termmosaic.MouseMiddle, Action: termmosaic.MousePress},
		{X: 3, Y: 7, Button: termmosaic.MouseNone, Action: termmosaic.MouseMove},
	} {
		if d.Handle(termmosaic.Event{Kind: termmosaic.EventMouse, Mouse: m}) {
			t.Errorf("a %v/%v inside Bounds was consumed", m.Button, m.Action)
		}
	}
}

// TestSetActionsReclampsTheFocusAndTheCancelIndex covers the mutator's obligation:
// replacing a shorter action list must not leave the dialog focused on, or
// cancelling into, an action that no longer exists.
//
// Both halves are asserted because they are different fields with different failure
// modes, and one of them panicking on a later Escape would be a crash in an
// application rather than a wrong pixel.
func TestSetActionsReclampsTheFocusAndTheCancelIndex(t *testing.T) {
	d, rec := watched(choiceDialog(40, 12, 3))
	d.SetActions(Action{Label: DefaultCancelLabel}, Action{Label: DefaultOKLabel}, Action{Label: "Later"})
	d.SetFocus(5) // the third action
	focus(d)

	d.SetActions(Action{Label: DefaultDismissLabel})

	if got := d.FocusCount(); got != 4 {
		t.Fatalf("after replacing the actions the ring has %d items, want 3 choices and 1 button", got)
	}
	if got := d.Focus(); got != 3 {
		t.Errorf("the focus is %d, want the last of the four ring items", got)
	}
	d.Handle(escape())
	if got := rec.lastAction(); got != 0 {
		t.Errorf("Escape after the replacement reported action %d, want 0", got)
	}
}

// TestSetChoicesClampsAFocusThatNoLongerExists is the same obligation for the list,
// which is reachable in the other direction: a dialog focused on the last of twenty
// choices, told there are now two.
func TestSetChoicesClampsAFocusThatNoLongerExists(t *testing.T) {
	d := choiceDialog(40, 12, 20)
	d.Draw(cellBuf(40, 12))
	d.SetFocus(20) // the Cancel button, in a ring of 21: twenty choices then one button
	if got := d.FocusedAction(); got != 0 {
		t.Fatalf("setup: the focus should be on the button, got action %d", got)
	}

	d.SetChoices([]string{"a", "b"})

	if got := d.Focus(); got >= d.FocusCount() {
		t.Errorf("the focus %d is outside the new ring of %d", got, d.FocusCount())
	}
	if got := d.FocusedChoice(); got >= d.ChoiceCount() {
		t.Errorf("the focused choice %d is outside the %d choices that remain", got, d.ChoiceCount())
	}
}

// TestSetFocusIsClampedRatherThanPanicking covers the public setter against every
// out-of-range value, because a caller computing a focus index from a count it read
// a frame ago must not be able to crash the program.
func TestSetFocusIsClampedRatherThanPanicking(t *testing.T) {
	d := choiceDialog(40, 12, 3)
	for _, i := range []int{-100, -1, 0, 3, 4, 1000} {
		d.SetFocus(i)
		if got := d.Focus(); got < 0 || got >= d.FocusCount() {
			t.Errorf("SetFocus(%d) left the focus at %d, outside [0,%d)", i, got, d.FocusCount())
		}
	}
	d.SetActions()
	d.SetChoices(nil)
	if got := d.FocusCount(); got != 0 {
		t.Fatalf("setup: the ring has %d items after clearing both lists, want 0", got)
	}
	d.SetFocus(3)
	if got := d.Focus(); got != 0 {
		t.Errorf("an empty ring left the focus at %d, want 0", got)
	}
}

// TestKeysWorkBeforeTheFirstDraw removes the ordering dependency a layout would
// otherwise have to know about: rectangles are handed out, then input arrives, and
// the first frame may not have happened yet.
//
// SetBounds has to be enough to make the choice list's viewport correct, which is
// what Select and List already promise and what a dialog in a stack of panels will
// be relied on for.
func TestKeysWorkBeforeTheFirstDraw(t *testing.T) {
	d, rec := watched(choiceDialog(30, 9, 20))
	focus(d)

	// No Draw anywhere in this test.
	for i := 0; i < 12; i++ {
		if !press(t, d, keyDown) {
			t.Fatalf("down %d was not consumed before the first draw", i)
		}
	}
	f, l := d.vm.Range()
	if got := d.FocusedChoice(); got < f || got >= l {
		t.Errorf("the focused choice %d is outside the visible window [%d,%d) before any draw", got, f, l)
	}
	mustPress(t, d, keyEnter)
	if got := rec.lastChoice(); got != d.FocusedChoice() {
		t.Errorf("Enter chose %d, want the focused choice %d", got, d.FocusedChoice())
	}
}

// TestFocusSurvivesLosingAndRegainingIt pins that SetFocused does not reset the
// ring: tabbing away from a dialog and back must not undo the navigation the user
// did, which is the difference between focus and a reset.
func TestFocusSurvivesLosingAndRegainingIt(t *testing.T) {
	d := choiceDialog(40, 12, 5)
	d.SetFocus(3)

	d.SetFocused(true)
	d.SetFocused(false)
	d.SetFocused(true)

	if got := d.Focus(); got != 3 {
		t.Errorf("the focus is %d after regaining focus, want the 3 it was left at", got)
	}
}

// TestTheRingIsEmptySafe pins that a dialog with no actions and no choices is a
// legal, drawable, focusable widget rather than a crash waiting for an application
// that empties both lists.
func TestTheRingIsEmptySafe(t *testing.T) {
	d := New(rect(40, 9), VariantChoice)
	d.SetBodyString("nothing to choose")
	d.SetActions()
	d.SetChoices(nil)
	if got := d.FocusCount(); got != 0 {
		t.Errorf("an empty dialog's ring has %d items, want 0", got)
	}
	focus(d)
	d.Draw(cellBuf(40, 9))

	for _, seq := range []string{keyEnter, keySpace, keyTab, keyDown, keyUp, keyHome, keyEnd, keyBacktab} {
		d.Handle(keyFromSeq(t, seq))
	}
	if !d.Handle(escape()) {
		t.Error("Escape was not consumed by an empty dialog")
	}
	if got := d.CancelAction(); got != -1 {
		t.Errorf("a dialog with no actions reports cancel action %d, want -1", got)
	}
}

// TestAnActionKeepsItsOwnStyleWhenFocused asserts the documented precedence the
// other way round from TestAnActionStyleOverridesTheDialogStyle: a bespoke button
// must not lose its colours just because it is focused, it only GAINS the
// attribute.
func TestAnActionKeepsItsOwnStyleWhenFocused(t *testing.T) {
	d := New(rect(40, 9), VariantConfirm)
	d.SetActions(Action{Label: DefaultCancelLabel}, Action{
		Label: DefaultOKLabel,
		Style: buffer.NewStyle(buffer.NewColour(0xff, 0, 0), buffer.DefaultColour, 0),
	})
	buf := cellBuf(40, 9)
	d.Draw(buf)

	fg := buf.CellAt(12, 7).FG
	attr := buf.CellAt(12, 7).Attr
	if fg != buffer.NewColour(0xff, 0, 0) {
		t.Errorf("the focused bespoke button has fg %#x, want its own red", fg)
	}
	if !attr.Has(buffer.AttrReverse) {
		t.Errorf("the focused bespoke button is not reversed: attr %v", attr)
	}
}

// TestDismissedIsSetEvenWithNoCallbacks is the polling half of the contract. An
// application that polls Dismissed rather than installing a callback must see
// exactly the same moments one that is called back to does — otherwise the two
// styles of application disagree about when a modal is finished.
func TestDismissedIsSetEvenWithNoCallbacks(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    *Dialog
		key  string
	}{
		{"enter on a button", confirmDialog(40, 9), keyEnter},
		{"escape", confirmDialog(40, 9), ""},
		{"enter on a choice", choiceDialog(40, 12, 3), keyEnter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.d // no callbacks installed at all
			focus(d)
			if tc.key == "" {
				d.Handle(escape())
			} else {
				mustPress(t, d, tc.key)
			}
			if !d.Dismissed() {
				t.Error("the dialog finished without Dismissed being set and no callback to hear it")
			}
		})
	}
}

// TestDismissedStaysSetUntilReset pins that the flag is a latch rather than a
// momentary: an application that checks it after handling the callback must still
// see it.
func TestDismissedStaysSetUntilReset(t *testing.T) {
	d, rec := watched(infoDialog(40, 9))
	focus(d)
	d.Handle(escape())

	if !d.Dismissed() {
		t.Fatal("setup: Escape should have set Dismissed")
	}
	if rec.cancels != 1 {
		t.Fatalf("the callback saw %d cancellations", rec.cancels)
	}
	// A second Escape reports again — the dialog has not silently become inert.
	d.Handle(escape())
	if rec.cancels != 2 {
		t.Errorf("a second Escape reported %d cancellations, want 2", rec.cancels)
	}
}

// TestActivationAndCancellationAreDistinct distinguishes the two ways out of a
// modal, which an application almost always cares about: "the user said yes" and
// "the user walked away" are different answers even when the dialog is gone.
func TestActivationAndCancellationAreDistinct(t *testing.T) {
	d, rec := watched(confirmDialog(40, 9))
	focus(d)

	mustPress(t, d, keyEnter) // OK, the default
	if rec.cancels != 0 {
		t.Errorf("accepting fired OnCancel %d times", rec.cancels)
	}
	if got := rec.lastAction(); got != 1 {
		t.Errorf("accepting reported action %d, want the OK button 1", got)
	}

	d.Reset()
	d.Handle(escape())
	if rec.cancels != 1 {
		t.Errorf("cancelling fired OnCancel %d times, want 1", rec.cancels)
	}
	if got := rec.lastAction(); got != 0 {
		t.Errorf("cancelling reported action %d, want the Cancel button 0", got)
	}
}

// TestVariantPresetDefaults pins the three presets against the exported label
// constants, so a change to what "confirm" means is a deliberate diff rather than
// something an application discovers at runtime.
func TestVariantPresetDefaults(t *testing.T) {
	for _, tc := range []struct {
		variant Variant
		actions []string
		cancel  int
		focus   int
	}{
		{VariantInfo, []string{DefaultDismissLabel}, 0, 0},
		{VariantConfirm, []string{DefaultCancelLabel, DefaultOKLabel}, 0, 1},
		{VariantChoice, []string{DefaultCancelLabel}, 0, 0},
	} {
		t.Run(tc.variant.String(), func(t *testing.T) {
			d := New(rect(40, 9), tc.variant)
			if got := len(d.Actions()); got != len(tc.actions) {
				t.Fatalf("%s has %d actions, want %d", tc.variant, got, len(tc.actions))
			}
			for i, want := range tc.actions {
				if got := d.Actions()[i].Label; got != want {
					t.Errorf("%s action %d is %q, want %q", tc.variant, i, got, want)
				}
			}
			if got := d.CancelAction(); got != tc.cancel {
				t.Errorf("%s cancels to action %d, want %d", tc.variant, got, tc.cancel)
			}
			if got := d.Focus(); got != tc.focus {
				t.Errorf("%s starts focused on ring item %d, want %d", tc.variant, got, tc.focus)
			}
			if got := d.Variant(); got != tc.variant {
				t.Errorf("Variant reports %v, want %v", got, tc.variant)
			}
		})
	}
}

// TestVariantIsARecordedPresetNotAMode pins the design decision: the variant chose
// the defaults, and replacing the actions does not change what Variant reports or
// stop the dialog working. A widget that switched behaviour on its variant field
// would make SetActions a partial no-op, which is the kind of API that surprises
// an application at the worst moment.
func TestVariantIsARecordedPresetNotAMode(t *testing.T) {
	d := confirmDialog(40, 9)
	d.SetActions(Action{Label: "Just one"})
	if got := d.Variant(); got != VariantConfirm {
		t.Errorf("Variant reports %v after SetActions, want the recorded %v", got, VariantConfirm)
	}
	if got := len(d.Actions()); got != 1 {
		t.Errorf("SetActions did not replace the buttons: %d remain", got)
	}
	if d.FocusCount() != 1 {
		t.Errorf("the ring has %d items after the replacement, want 1", d.FocusCount())
	}
}

// TestAVariantOutsideTheDefinedRangeIsTotal covers the untrusted-input rule ADR
// 0007 §4 states for layout-derived data: a Dialog built from a persisted config
// value must render and report, not panic.
func TestAVariantOutsideTheDefinedRangeIsTotal(t *testing.T) {
	d := New(rect(40, 9), Variant(200))
	if got := d.Variant().String(); got != "variant(200)" {
		t.Errorf("an unknown variant renders as %q, want a decimal so the failure is legible", got)
	}
	d.SetBodyString("still renders")
	d.Draw(cellBuf(40, 9))
	focus(d)
	d.Handle(escape())
	if !d.Dismissed() {
		t.Error("an unknown variant did not dismiss on Escape")
	}
}
