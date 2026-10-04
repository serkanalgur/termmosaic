package menu

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/input"
)

// decodeKey runs seq through input.Decode and returns the single event it
// produced.
//
// Every keyboard assertion in this package goes through the real decoder rather
// than a hand-built Event, so a test cannot pass on an event the decoder would
// never emit — which is the failure mode a hand-built event invites.
func decodeKey(t *testing.T, seq string) termmosaic.Event {
	t.Helper()
	ev, n, status := input.Decode([]byte(seq), input.DefaultConfig())
	if status != input.StatusOK {
		t.Fatalf("decoding %q: status %v, want ok", seq, status)
	}
	if n != len(seq) {
		t.Errorf("decoding %q: consumed %d bytes of %d", seq, n, len(seq))
	}
	if ev.Kind == termmosaic.EventNone {
		t.Fatalf("decoding %q produced no event", seq)
	}
	return ev
}

// ---------------------------------------------------------------------------
// navigation: the state machine
// ---------------------------------------------------------------------------

// TestMenuStartsClosedAndOpenable is the constructor's documented promise. A menu
// that appears already open has no key the application can rely on to dismiss
// it, so New returns a closed one and Open is a separate, idempotent act.
func TestMenuStartsClosedAndOpenable(t *testing.T) {
	m := New(rect(30, 10), tree()...)
	if m.IsOpen() {
		t.Error("a new menu is open; New must return a CLOSED menu")
	}
	// A closed menu draws its frame and nothing else.
	got := rows(t, 30, 10, m)
	for y, r := range got {
		if r != "" {
			t.Errorf("closed menu row %d = %q, want blank: a closed menu shows its frame only", y, r)
		}
	}
	m.Open()
	if !m.IsOpen() {
		t.Error("Open did not open the menu")
	}
	m.Open()
	if d := m.Depth(); d != 1 {
		t.Errorf("Open twice: depth %d, want 1: Open must be idempotent", d)
	}
}

// TestMenuKeysIgnoredWhileClosedOrUnfocused is the focus contract stated in cells
// rather than in prose: a navigation key must not move anything unless the menu
// is both focused and open.
func TestMenuKeysIgnoredWhileClosedOrUnfocused(t *testing.T) {
	cases := []struct {
		name   string
		closed bool
		focus  bool
		want   bool // whether the key may be consumed
	}{
		{"closed and focused", true, true, false},
		{"open and unfocused", false, false, false},
		{"closed and unfocused", true, false, false},
		{"open and focused", false, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMenu(t, 30, 10, tree()...)
			m.SetFocused(tc.focus)
			if tc.closed {
				m.Close()
			}
			if got := tap(t, m, "\x1b[B"); got != tc.want {
				t.Errorf("down consumed = %v, want %v", got, tc.want)
			}
			// A refused key must ALSO leave the selection alone, which is a
			// separate property from not being consumed: a widget could decline the
			// key and still have acted on it, and the only way to see that is to
			// look at the state afterwards.
			wantSel := 2
			if !tc.want {
				wantSel = 0
			}
			if got := m.Selected(); got != wantSel {
				t.Errorf("selection is %d, want %d (a refused key must not move it)", got, wantSel)
			}
		})
	}
}

// TestMenuKeyReleaseIsNotAnActivation pins the half of the key contract that the
// kitty keyboard protocol makes reachable. Without the check, a release would
// activate an item a second time — once on press and once on release — and an
// application binding OnActivate to a destructive command would run it twice.
func TestMenuKeyReleaseIsNotAnActivation(t *testing.T) {
	var activated int
	m := newMenu(t, 30, 10, tree()...)
	m.OnActivate = func(int, int) { activated++ }
	// Sep is the third root item, a leaf with no submenu.
	m.selectIndex(0, 2)
	ev := decodeKey(t, "\r")
	ev.Type = termmosaic.KeyRelease
	if m.Handle(ev) {
		t.Error("a key RELEASE was consumed; releases must not act on a menu")
	}
	if activated != 0 {
		t.Errorf("OnActivate fired %d times on a release, want 0", activated)
	}
	press(t, m, "\r")
	if activated != 1 {
		t.Errorf("after a press OnActivate fired %d times, want 1", activated)
	}
}

// TestMenuNavigationWrapsAroundAndSkipsDisabled is the movement contract, asserted
// on the state machine rather than on a picture of it.
//
// Both halves matter and they are separate rules: navigation WRAPS rather than
// clamping, and it never lands on a disabled item. A test that only walked
// forwards down the middle of the list would pass against a widget that clamped
// and that selected disabled items.
func TestMenuNavigationWrapsAroundAndSkipsDisabled(t *testing.T) {
	m := newMenu(t, 30, 10, tree()...)

	// The fixture: New(0) Save(1, disabled) Sep(2) Wrap(3) Help(4).
	// Down from New skips the disabled Save and lands on Sep.
	if got := m.Selected(); got != 0 {
		t.Fatalf("fresh menu selected %d, want 0", got)
	}
	press(t, m, "\x1b[B") // down
	if got := m.Selected(); got != 2 {
		t.Errorf("one down from New: selected %d, want 2 — the disabled Save was selected", got)
	}
	press(t, m, "\x1b[B") // down
	if got := m.Selected(); got != 3 {
		t.Errorf("two downs: selected %d, want 3", got)
	}
	press(t, m, "\x1b[B") // down
	if got := m.Selected(); got != 4 {
		t.Errorf("three downs: selected %d, want 4", got)
	}
	press(t, m, "\x1b[B") // down, past the end
	if got := m.Selected(); got != 0 {
		t.Errorf("down past the end: selected %d, want 0 — Down must WRAP", got)
	}
	press(t, m, "\x1b[A") // up, past the start
	if got := m.Selected(); got != 4 {
		t.Errorf("up past the start: selected %d, want 4 — Up must WRAP", got)
	}
	press(t, m, "\x1b[A") // up: skips nothing at the tail
	if got := m.Selected(); got != 3 {
		t.Errorf("up from Help: selected %d, want 3", got)
	}
	press(t, m, "\x1b[A") // up: skips the disabled Save going backwards
	if got := m.Selected(); got != 2 {
		t.Errorf("up from Wrap: selected %d, want 2 — Up must skip the disabled Save too", got)
	}
}

// TestMenuNavigationVisitsEverySelectableItemExactlyOnce walks the whole level and
// asserts the set of visited indices, which is the one form of this test that
// cannot pass against a widget which skips an enabled item or stops early.
//
// It is stronger than counting downs: a widget that skipped one item and wrapped
// twice would reach the same count.
func TestMenuNavigationVisitsEverySelectableItemExactlyOnce(t *testing.T) {
	items := []Item{
		{Label: "a"},
		{Label: "b", Disabled: true},
		{Label: "c"},
		{Label: "d", Disabled: true},
		{Label: "e"},
	}
	m := newMenu(t, 30, 10, items...)
	visited := map[int]int{}
	for range cap(items) {
		press(t, m, "\x1b[B")
		visited[m.Selected()]++
	}
	for _, want := range []int{0, 2, 4} {
		if visited[want] == 0 {
			t.Errorf("item %d was never selected while walking the level with Down", want)
		}
	}
	for _, never := range []int{1, 3} {
		if visited[never] != 0 {
			t.Errorf("disabled item %d was selected %d times", never, visited[never])
		}
	}
}

// TestMenuHomeAndEndLandOnSelectableItems pins the ends-of-list keys against the
// fixture's disabled item, which sits at index 1: End must not land there.
func TestMenuHomeAndEndLandOnSelectableItems(t *testing.T) {
	m := newMenu(t, 30, 10, tree()...)
	press(t, m, "\x1b[F") // end
	if got := m.Selected(); got != 4 {
		t.Errorf("End: selected %d, want 4", got)
	}
	press(t, m, "\x1b[H") // home
	if got := m.Selected(); got != 0 {
		t.Errorf("Home: selected %d, want 0", got)
	}
}

// TestMenuAllDisabledLevelReportsMinusOneAndConsumesTheKey covers the level with
// no selectable item at all.
//
// -1 is a real answer and a distinct one: a caller storing the index must be able
// to tell "nothing here can be selected" from "the first is selected". The key is
// still consumed, because it was still a menu key and refusing it would let it
// reach a parent widget that has different bindings.
func TestMenuAllDisabledLevelReportsMinusOneAndConsumesTheKey(t *testing.T) {
	m := newMenu(t, 30, 10,
		Item{Label: "no", Disabled: true},
		Item{Label: "nope", Disabled: true},
	)
	if got := m.Selected(); got != -1 {
		t.Errorf("all-disabled level: selected %d, want -1", got)
	}
	for _, seq := range []string{"\x1b[A", "\x1b[B", "\x1b[H", "\x1b[F"} {
		if !tap(t, m, seq) {
			t.Errorf("%q was not consumed on an all-disabled level; it was still a menu key", seq)
		}
		if got := m.Selected(); got != -1 {
			t.Errorf("after %q: selected %d, want -1", seq, got)
		}
	}
}

// TestMenuPageKeysMoveByAScreen is the page contract. The step is derived from the
// visible rows, so the test asserts against the layout the widget built rather
// than against a hard-coded number that would pass for the wrong reason.
func TestMenuPageKeysMoveByAScreen(t *testing.T) {
	// 8 rows: a header and 7 items, so pageStep is 7 and End is at 19.
	var items []Item
	for i := range 20 {
		items = append(items, Item{Label: itoa(i)})
	}
	m := newMenu(t, 20, 8, items...)
	if got := m.pageStep(); got != 7 {
		t.Fatalf("pageStep at 8 rows = %d, want 7 (7 item rows under one header)", got)
	}
	press(t, m, "\x1b[6~") // page down
	if got := m.Selected(); got != 7 {
		t.Errorf("page down: selected %d, want 7", got)
	}
	press(t, m, "\x1b[5~") // page up
	if got := m.Selected(); got != 0 {
		t.Errorf("page up: selected %d, want 0", got)
	}
}

// TestMenuPageStepIsOneWhenNothingIsMeasured covers a key arriving before the
// first Draw. A step of zero would make the key a no-op, which is never what a
// page key means.
func TestMenuPageStepIsOneWhenNothingIsMeasured(t *testing.T) {
	m := New(rect(20, 8), tree()...)
	m.Open()
	m.SetFocused(true)
	// No Draw yet: the interior is unknown to the cached layout, so the step must
	// come from the block rather than from a zero-height column.
	if got := m.pageStep(); got < 1 {
		t.Errorf("pageStep before the first Draw = %d, want at least 1", got)
	}
	press(t, m, "\x1b[6~")
	if got := m.Selected(); got < 0 {
		t.Errorf("page down before the first Draw: selected %d, want a real index", got)
	}
}

// ---------------------------------------------------------------------------
// submenus
// ---------------------------------------------------------------------------

// TestMenuRightOpensAndLeftClosesOneLevel is the push and pop contract at two
// levels, and the reason the cursor path is a stack.
//
// Left is asserted to restore the PARENT's selection, not to step to the previous
// item of the current level: that is the whole difference between a tree and a
// list that got confused.
func TestMenuRightOpensAndLeftClosesOneLevel(t *testing.T) {
	m := newMenu(t, 60, 12, tree()...)
	if got := m.Depth(); got != 1 {
		t.Fatalf("fresh menu depth %d, want 1", got)
	}
	// New is the selected root item and is a branch.
	press(t, m, "\x1b[C") // right
	if got := m.Depth(); got != 2 {
		t.Fatalf("right on New: depth %d, want 2", got)
	}
	if got := m.SelectedAt(0); got != 0 {
		t.Errorf("opening a submenu changed the parent selection to %d, want 0", got)
	}
	if got := m.SelectedAt(1); got != 0 {
		t.Errorf("a new submenu selected %d, want 0 (its first selectable item)", got)
	}

	// Move in the submenu so the pop has something to restore.
	press(t, m, "\x1b[B") // down to Plain
	if got := m.SelectedAt(1); got != 1 {
		t.Fatalf("in the submenu: selected %d, want 1", got)
	}
	press(t, m, "\x1b[D") // left
	if got := m.Depth(); got != 1 {
		t.Fatalf("left: depth %d, want 1", got)
	}
	if got := m.SelectedAt(0); got != 0 {
		t.Errorf("after left: the root selection is %d, want 0 — Left must restore the parent's", got)
	}
}

// TestMenuThreeLevelsDeepOpensAndPopsOneAtATime exercises the arbitrary depth the
// task requires: the fixture nests three levels, and each Right adds exactly one.
func TestMenuThreeLevelsDeepOpensAndPopsOneAtATime(t *testing.T) {
	m := newMenu(t, 80, 12, tree()...)
	press(t, m, "\x1b[C") // New's submenu
	if d := m.Depth(); d != 2 {
		t.Fatalf("depth after one right: %d, want 2", d)
	}
	press(t, m, "\x1b[C") // Deep's submenu
	if d := m.Depth(); d != 3 {
		t.Fatalf("depth after two rights: %d, want 3", d)
	}
	if items := m.Level(2); len(items) != 2 {
		t.Fatalf("level 2 has %d items, want 2: the third level is Deep's children", len(items))
	}
	press(t, m, "\x1b[D") // pop to level 1
	if d := m.Depth(); d != 2 {
		t.Fatalf("depth after one left: %d, want 2", d)
	}
	if got := m.SelectedAt(1); got != 0 {
		t.Errorf("after popping, level 1's selection is %d, want 0 (Deep, which opened level 2)", got)
	}
	press(t, m, "\x1b[D") // pop to the root
	if d := m.Depth(); d != 1 {
		t.Fatalf("depth after two lefts: %d, want 1", d)
	}
}

// TestMenuRightOnALeafIsNotConsumed is the asymmetry in the contract, and it is
// deliberate: Right on a branch opens it and consumes the key, Right on a leaf
// does nothing and leaves the key for the application.
//
// A menu that consumed right everywhere would take a binding the application
// needed for its own use, with no way to tell that it had.
func TestMenuRightOnALeafIsNotConsumed(t *testing.T) {
	m := newMenu(t, 60, 12, tree()...)
	m.selectIndex(0, 2) // Sep, a leaf
	if tap(t, m, "\x1b[C") {
		t.Error("right on a leaf was consumed; a leaf has no submenu to open")
	}
	if d := m.Depth(); d != 1 {
		t.Errorf("right on a leaf changed the depth to %d, want 1", d)
	}
	// And on a branch it IS consumed, which is the other half of the same rule.
	m.selectIndex(0, 0)
	if !tap(t, m, "\x1b[C") {
		t.Error("right on a branch was not consumed")
	}
}

// TestMenuEscapeClosesLevelThenMenu is the documented order, in one test. Escape
// at a nested level closes THAT level; Escape at the root closes the whole menu.
//
// They are asserted separately because the failure mode is a menu that closes
// everything on the first Escape, which reads as working right up until the user
// is three levels deep.
func TestMenuEscapeClosesLevelThenMenu(t *testing.T) {
	m := newMenu(t, 80, 12, tree()...)
	press(t, m, "\x1b[C") // level 2
	press(t, m, "\x1b[C") // level 3
	if d := m.Depth(); d != 3 {
		t.Fatalf("setup: depth %d, want 3", d)
	}
	mustEscape(t, m)
	if d := m.Depth(); d != 2 {
		t.Errorf("escape at level 3: depth %d, want 2 — Escape must close ONE level", d)
	}
	if !m.IsOpen() {
		t.Error("escape at a nested level closed the whole menu")
	}
	mustEscape(t, m)
	if d := m.Depth(); d != 1 {
		t.Errorf("escape at level 2: depth %d, want 1", d)
	}
	mustEscape(t, m)
	if m.IsOpen() {
		t.Error("escape at the root did not close the menu")
	}
}

// TestMenuLeftAtRootClosesMenuAndKeepsThePath pins two things at once: Left at the
// root has the same effect as Escape there, and closing a menu does NOT throw
// away where the user was.
//
// The second half is what a menu bar needs — dismiss and re-open, and be back
// where you left off — so it is a contract rather than an implementation detail.
func TestMenuLeftAtRootClosesMenuAndKeepsThePath(t *testing.T) {
	t.Run("left at the root closes the menu", func(t *testing.T) {
		m := newMenu(t, 80, 12, tree()...)
		press(t, m, "\x1b[C")
		press(t, m, "\x1b[D") // pop to the root
		press(t, m, "\x1b[D") // at the root: closes the whole menu
		if m.IsOpen() {
			t.Error("left at the root did not close the menu")
		}
		if d := m.Depth(); d != 1 {
			t.Errorf("after closing at the root: depth %d, want 1", d)
		}
	})

	// The path SURVIVES a Close, which is what a menu bar needs: dismiss and
	// re-open, and be back where you left off.
	//
	// This is distinct from Left, which POPS a frame and therefore discards it —
	// so this asserts on Close alone rather than on a sequence that ends in a pop.
	t.Run("close keeps the cursor path", func(t *testing.T) {
		m := newMenu(t, 80, 12, tree()...)
		press(t, m, "\x1b[C")
		press(t, m, "\x1b[C") // three deep
		press(t, m, "\x1b[B") // move at the deepest level
		depth, sel := m.Depth(), m.SelectedAt(2)
		if depth != 3 || sel != 1 {
			t.Fatalf("setup: depth %d, level 2 selection %d; want 3 and 1", depth, sel)
		}
		m.Close()
		m.Open()
		if got := m.Depth(); got != depth {
			t.Errorf("after close and reopen: depth %d, want %d", got, depth)
		}
		if got := m.SelectedAt(2); got != sel {
			t.Errorf("after close and reopen: level 2's selection is %d, want %d — Close must keep the path", got, sel)
		}
	})
}

// TestMenuPushingDeeperReplacesTheStalePath covers the stack discipline. Pressing
// Right on a different branch at a shallower level must discard the deeper frames,
// because a frame whose parent is no longer what opened it is unreachable and
// would show items from the wrong branch.
func TestMenuPushingDeeperReplacesTheStalePath(t *testing.T) {
	m := newMenu(t, 80, 12, tree()...)
	press(t, m, "\x1b[C") // open New
	press(t, m, "\x1b[C") // open Deep: depth 3
	if d := m.Depth(); d != 3 {
		t.Fatalf("setup: depth %d, want 3", d)
	}
	// Close back to the root and open a different branch.
	press(t, m, "\x1b[D")
	press(t, m, "\x1b[D")
	m.selectIndex(0, 4) // Help is a branch with no children in this fixture, so use
	// a fresh menu to open a different branch at depth 1 instead.
	m2 := newMenu(t, 80, 12,
		Item{Label: "A", Items: []Item{{Label: "A1", Items: []Item{{Label: "deep"}}}}},
		Item{Label: "B", Items: []Item{{Label: "B1"}}},
	)
	press(t, m2, "\x1b[C")
	press(t, m2, "\x1b[C")
	if d := m2.Depth(); d != 3 {
		t.Fatalf("m2 setup: depth %d, want 3", d)
	}
	press(t, m2, "\x1b[D")
	press(t, m2, "\x1b[D")
	m2.selectIndex(0, 1) // B
	press(t, m2, "\x1b[C")
	if d := m2.Depth(); d != 2 {
		t.Errorf("opening B after being three deep: depth %d, want 2", d)
	}
	if items := m2.Level(1); len(items) != 1 || items[0].Label != "B1" {
		t.Errorf("level 1 = %v, want B's single child", labels(items))
	}
}

// labels renders items for a failure message.
func labels(items []Item) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].Label
	}
	return out
}

// ---------------------------------------------------------------------------
// activation
// ---------------------------------------------------------------------------

// TestMenuActivateFiresOnALeafAndCarriesTheLevel is the OnActivate contract. The
// callback receives the DEPTH as well as the index, which is what lets one
// handler serve a three-level tree without a closure per level.
func TestMenuActivateFiresOnALeafAndCarriesTheLevel(t *testing.T) {
	var gotLevel, gotIndex = -1, -1
	calls := 0
	m := newMenu(t, 80, 12, tree()...)
	m.OnActivate = func(level, index int) {
		gotLevel, gotIndex = level, index
		calls++
	}
	m.selectIndex(0, 2) // Sep, a leaf at the root
	press(t, m, "\r")
	if calls != 1 || gotLevel != 0 || gotIndex != 2 {
		t.Errorf("OnActivate: calls=%d level=%d index=%d, want 1/0/2", calls, gotLevel, gotIndex)
	}
	// At level 1, "Plain" is a leaf there. New must be selected first: the key
	// contract is that right opens the SELECTED item's submenu, and Sep has none.
	m.selectIndex(0, 0)
	press(t, m, "\x1b[C")
	m.selectIndex(1, 1)
	press(t, m, "\r")
	if calls != 2 || gotLevel != 1 || gotIndex != 1 {
		t.Errorf("nested OnActivate: calls=%d level=%d index=%d, want 2/1/1", calls, gotLevel, gotIndex)
	}
}

// TestMenuEnterOnABranchOpensItAndDoesNotActivate is the first half of the ordered
// activation contract. Firing OnActivate for a branch as well would make the
// application handle one action twice.
func TestMenuEnterOnABranchOpensItAndDoesNotActivate(t *testing.T) {
	activations := 0
	m := newMenu(t, 80, 12, tree()...)
	m.OnActivate = func(int, int) { activations++ }
	m.selectIndex(0, 0) // New, a branch
	press(t, m, "\r")
	if d := m.Depth(); d != 2 {
		t.Errorf("Enter on a branch: depth %d, want 2", d)
	}
	if activations != 0 {
		t.Errorf("Enter on a branch fired OnActivate %d times, want 0", activations)
	}
}

// TestMenuEnterOnACheckableTogglesAndDoesNotActivate is the second half, and the
// order is why Checkable is checked before the branch test: a toggle's meaning is
// its state.
func TestMenuEnterOnACheckableTogglesAndDoesNotActivate(t *testing.T) {
	activations, toggles := 0, 0
	var gotLevel, gotIndex int
	m := newMenu(t, 80, 12, tree()...)
	m.OnActivate = func(int, int) { activations++ }
	m.OnToggle = func(level, index int) {
		toggles++
		gotLevel, gotIndex = level, index
	}
	// Wrap is a toggle at the root, initially checked.
	m.selectIndex(0, 3)
	if !m.Checked(0, 3) {
		t.Fatal("setup: Wrap is not checked")
	}
	press(t, m, "\r")
	if m.Checked(0, 3) {
		t.Error("Enter did not clear Wrap's check")
	}
	if toggles != 1 || gotLevel != 0 || gotIndex != 3 {
		t.Errorf("OnToggle: calls=%d level=%d index=%d, want 1/0/3", toggles, gotLevel, gotIndex)
	}
	if activations != 0 {
		t.Errorf("Enter on a toggle fired OnActivate %d times, want 0", activations)
	}
	press(t, m, "\r")
	if !m.Checked(0, 3) {
		t.Error("a second Enter did not set Wrap's check back")
	}
}

// TestMenuEnterOnACheckableBranchTogglesAndDoesNotOpen covers the case where both
// flags are set, which is the one that makes the order observable.
func TestMenuEnterOnACheckableBranchTogglesAndDoesNotOpen(t *testing.T) {
	m := newMenu(t, 80, 12, Item{
		Label:     "Both",
		Checkable: true,
		Checked:   false,
		Items:     []Item{{Label: "child"}},
	})
	press(t, m, "\r")
	if !m.Checked(0, 0) {
		t.Error("Enter on a checkable branch did not toggle it")
	}
	if d := m.Depth(); d != 1 {
		t.Errorf("Enter on a checkable branch opened a level: depth %d, want 1", d)
	}
}

// TestMenuToggleStateSurvivesNavigatingAwayAndBack is the reason Item.Checked is
// read through the item tree rather than copied into the level frames.
//
// A menu that kept its toggle state in the cursor path would forget it as soon as
// the user moved, which is the difference between a working settings menu and one
// that reverts every setting you touch.
func TestMenuToggleStateSurvivesNavigatingAwayAndBack(t *testing.T) {
	m := newMenu(t, 80, 12, tree()...)
	m.selectIndex(0, 3) // Wrap, the root's toggle
	press(t, m, "\r")
	if m.Checked(0, 3) {
		t.Fatal("setup: Wrap should be unchecked after the toggle")
	}
	// Navigate away and back within the level, and into and out of a submenu: the
	// state is read through the item tree, so none of these can lose it.
	m.selectIndex(0, 0)
	press(t, m, "\x1b[C")
	if d := m.Depth(); d != 2 {
		t.Fatalf("setup: depth %d, want 2", d)
	}
	press(t, m, "\x1b[D")
	m.selectIndex(0, 3)
	if m.Checked(0, 3) {
		t.Error("Wrap's check state did not survive navigating away and back")
	}
}

// TestMenuOnOpenOnSelectFireWithTheRightDepth checks the two informational
// callbacks, because a menu whose callbacks carry the wrong depth is a menu whose
// application cannot address its own tree.
func TestMenuOnOpenOnSelectFireWithTheRightDepth(t *testing.T) {
	var openLevel, openIndex = -1, -1
	var selLevel, selIndex = -1, -1
	opens, selects := 0, 0
	m := newMenu(t, 80, 12, tree()...)
	m.OnOpen = func(level, index int) { opens++; openLevel, openIndex = level, index }
	m.OnSelect = func(level, index int) { selects++; selLevel, selIndex = level, index }
	press(t, m, "\x1b[C") // open New
	if opens != 1 || openLevel != 0 || openIndex != 0 {
		t.Errorf("OnOpen: calls=%d level=%d index=%d, want 1/0/0", opens, openLevel, openIndex)
	}
	press(t, m, "\x1b[B") // move inside the submenu
	if selects != 1 || selLevel != 1 || selIndex != 1 {
		t.Errorf("OnSelect: calls=%d level=%d index=%d, want 1/1/1", selects, selLevel, selIndex)
	}
	// A move that does not change the selection must not fire OnSelect, which is
	// what makes the callback safe to do work in.
	before := selects
	m.selectIndex(1, 1)
	if selects != before {
		t.Errorf("selecting the item already selected fired OnSelect %d extra times", selects-before)
	}
}

// TestMenuSetCheckedIsSilent is the programmatic counterpart of Enter: it changes
// the state and does NOT fire OnToggle, because a caller restoring persisted
// settings does not want a callback per item.
func TestMenuSetCheckedIsSilent(t *testing.T) {
	toggles := 0
	m := newMenu(t, 80, 12, tree()...)
	m.OnToggle = func(int, int) { toggles++ }
	if !m.SetChecked(0, 3, false) {
		t.Fatal("SetChecked on a toggle returned false")
	}
	if m.Checked(0, 3) {
		t.Error("SetChecked(false) did not clear the state")
	}
	if toggles != 0 {
		t.Errorf("SetChecked fired OnToggle %d times, want 0", toggles)
	}
	// A non-checkable item and an out-of-range index are both refused rather than
	// panicking, because an index from a stale tree is ordinary input.
	if m.SetChecked(0, 2, true) {
		t.Error("SetChecked on a non-toggle returned true")
	}
	if m.SetChecked(9, 0, true) {
		t.Error("SetChecked at an unreachable depth returned true")
	}
}

// TestMenuKeyTabIsNotConsumed is the cross-widget contract: a form must be able to
// move focus out of a menu with the keyboard, exactly as it moves out of a text
// input. Every widget in the catalog keeps this promise, so it is asserted here
// too rather than assumed.
func TestMenuKeyTabIsNotConsumed(t *testing.T) {
	m := newMenu(t, 80, 12, tree()...)
	if tap(t, m, "\t") {
		t.Error("Tab was consumed by a menu; focus could not leave it")
	}
}

// TestMenuHandleIgnoresNonKeyNonMouseEvents is totality over Event.Kind: a resize,
// a paste or a focus event offered to a menu must be declined rather than
// interpreted.
func TestMenuHandleIgnoresNonKeyNonMouseEvents(t *testing.T) {
	m := newMenu(t, 80, 12, tree()...)
	for _, ev := range []termmosaic.Event{
		{},
		{Kind: termmosaic.EventResize, Size: termmosaic.Size{W: 10, H: 3}},
		{Kind: termmosaic.EventPaste, Text: "hello"},
		{Kind: termmosaic.EventFocus},
	} {
		if m.Handle(ev) {
			t.Errorf("Handle consumed a %v event", ev.Kind)
		}
	}
}
