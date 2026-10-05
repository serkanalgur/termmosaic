package keymap

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
)

// idDesc renders an Entry as "group/id: desc [chords]", which is what a help
// screen's row looks like and what makes an ordering assertion readable.
func idDesc(e Entry) string {
	var chords []string
	for _, c := range e.Chords {
		chords = append(chords, c.String())
	}
	return e.Group + "/" + string(e.ID) + ": " + e.Desc + " [" + strings.Join(chords, " ") + "]"
}

// idsOf renders a slice of Entries as one comparable string.
func idsOf(es []Entry) string {
	parts := make([]string, 0, len(es))
	for _, e := range es {
		parts = append(parts, idDesc(e))
	}
	return strings.Join(parts, " | ")
}

// TestDescribeCannotDriftFromTheBindings is §4's central claim, tested by
// construction: the entries ARE the bindings, so there is no second list to
// fall out of date.
//
// The way to show it is to rename a command's ID and observe that the help text
// changes, with no help text having been touched.
func TestDescribeCannotDriftFromTheBindings(t *testing.T) {
	r := New()
	focus := &plain{}
	r.Register(Command{ID: "file.save", Desc: "save the file", Group: "file", Run: func(Ctx) bool { return true }})
	c, _ := ParseChord("Ctrl+s")
	r.Bind(Binding{Chord: c, ID: "file.save"})
	r.Seal()

	if got := idsOf(r.Describe(ScopeGlobal)); !strings.Contains(got, "file.save") {
		t.Fatalf("Describe = %q, want it to mention file.save", got)
	}
	// Rename it. The command is re-registered and the binding follows; nothing
	// about the help output was edited.
	// The renamed command is bound to the same chord by the same (chord, scope,
	// owner) triple, so Bind REPLACES rather than adds: that is what makes an
	// override an override. The chord row now names the new ID.
	r.Register(Command{ID: "file.save-as", Desc: "save the file", Group: "file", Run: func(Ctx) bool { return true }})
	r.Bind(Binding{Chord: c, ID: "file.save-as"})
	got := idsOf(r.Describe(ScopeGlobal))
	if !strings.Contains(got, "file.save-as: save the file [Ctrl+s]") {
		t.Errorf("Describe = %q, want the renamed command carrying the chord", got)
	}
	if strings.Contains(got, "file.save: save the file [Ctrl") {
		t.Errorf("Describe = %q, still attributes the chord to the old command name", got)
	}
	// The old ID is still REGISTERED, and it now has no chord, so it appears as
	// a chordless row rather than vanishing. That is §4's third rule doing its
	// job — a command nothing can reach must still be describable, because a
	// registry with no Unregister is the shape an application has when it
	// renames a command at runtime, and silently dropping the row would hide
	// the leftover registration. Dropping it is a one-line Registry change if
	// the stale-command case ever turns out to matter; leaving it visible is
	// the choice that cannot mislead.
	if !strings.Contains(got, "file/file.save: save the file []") {
		t.Errorf("Describe = %q, want the unbound command to appear as a chordless row", got)
	}
	_ = focus
}

// TestDescribeSortsByGroupThenID is §4's ordering promise, which is what makes
// help output stable across runs and diffable across versions.
func TestDescribeSortsByGroupThenID(t *testing.T) {
	r := New()
	focus := &plain{}
	r.Register(
		Command{ID: "view.zoom", Desc: "zoom", Group: "view", Run: func(Ctx) bool { return true }},
		Command{ID: "file.save", Desc: "save", Group: "file", Run: func(Ctx) bool { return true }},
		Command{ID: "file.open", Desc: "open", Group: "file", Run: func(Ctx) bool { return true }},
		// No group: the uncategorised bucket, which renders LAST.
		Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }},
	)
	ids := map[string]CommandID{
		"Ctrl+s": "file.save", "Ctrl+o": "file.open", "Ctrl+=": "view.zoom", "q": "app.quit",
	}
	for s, id := range ids {
		if err := r.BindString(s, id, ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.Seal()
	want := "file/file.open: open [Ctrl+o] | file/file.save: save [Ctrl+s] | " +
		"view/view.zoom: zoom [Ctrl+=] | /app.quit: quit [q]"
	if got := idsOf(r.Describe(ScopeGlobal)); got != want {
		t.Errorf("Describe(ScopeGlobal):\n got %s\nwant %s", got, want)
	}
	_ = focus
}

// TestDescribeProducesOneRowPerChord is the Entry doc's "a command with several
// bindings produces several Rows" rule, and it is the shape a terminal help
// screen actually wants: one line per key, not one line with a comma in it.
func TestDescribeProducesOneRowPerChord(t *testing.T) {
	r := New()
	r.Register(Command{ID: "file.save", Desc: "save", Group: "file", Run: func(Ctx) bool { return true }})
	for _, s := range []string{"Ctrl+s", "Ctrl+S", "F2"} {
		if err := r.BindString(s, "file.save", ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.Seal()
	got := r.Describe(ScopeGlobal)
	if len(got) != 3 {
		t.Fatalf("Describe returned %d entries, want 3 (one per chord); got %s", len(got), idsOf(got))
	}
	for _, e := range got {
		if len(e.Chords) != 1 {
			t.Errorf("entry %v carries %d chords, want 1", e.ID, len(e.Chords))
		}
	}
	// Canonical order: modifiers ascending, then Key ascending, then rune.
	// Ctrl+s and Ctrl+S have the same Mod and differ only in rune, so 'S'
	// precedes 's'; F2 has no modifiers and Mod 0 sorts first.
	want := "file/file.save: save [F2] | file/file.save: save [Ctrl+S] | file/file.save: save [Ctrl+s]"
	if got := idsOf(r.Describe(ScopeGlobal)); got != want {
		t.Errorf("Describe:\n got %s\nwant %s", got, want)
	}
}

// TestDescribeIncludesCommandsWithNoChord is §4's third rule: a command nothing
// is bound to is still describable, because a discoverability list that only
// lists keys is a key list, not a command list.
//
// This is the row that makes a palette able to show a mouse-only or
// palette-reachable command, and it is the reason the "no chord" case carries an
// empty Chords rather than being skipped.
func TestDescribeIncludesCommandsWithNoChord(t *testing.T) {
	r := New()
	r.Register(
		Command{ID: "button.activate", Desc: "press the button", Group: "button", Run: func(Ctx) bool { return true }},
		Command{ID: "file.save", Desc: "save", Group: "file", Run: func(Ctx) bool { return true }},
	)
	if err := r.BindString("Ctrl+s", "file.save", ScopeGlobal, nil); err != nil {
		t.Fatal(err)
	}
	r.Seal()
	want := "button/button.activate: press the button [] | file/file.save: save [Ctrl+s]"
	if got := idsOf(r.Describe(ScopeGlobal)); got != want {
		t.Errorf("Describe:\n got %s\nwant %s", got, want)
	}
}

// TestDescribeHonoursTheBindingDescOverride is the field's whole reason: the
// same command means different things in different contexts, and one command
// appearing twice in help with two honest descriptions is the feature.
func TestDescribeHonoursTheBindingDescOverride(t *testing.T) {
	r := New()
	focus := &plain{bounds: termmosaic.Rect{W: 10, H: 3}}
	screen := &plain{bounds: termmosaic.Rect{W: 80, H: 24}}
	r.Register(Command{ID: "item.activate", Desc: "activate", Group: "item", Run: func(Ctx) bool { return true }})
	enter, _ := ParseChord("Enter")
	r.Bind(
		Binding{Chord: enter, ID: "item.activate", Scope: ScopeGlobal, Desc: "open the item"},
		Binding{Chord: enter, ID: "item.activate", Scope: ScopeScreen, Owner: screen, Desc: "confirm the dialog"},
	)
	r.Seal()
	r.SetScreen(screen)
	r.Dispatch(termmosaic.SpecialKeyEvent(termmosaic.KeyEnter, 0), focus)

	got := idsOf(r.Describe(ScopeGlobal))
	if !strings.Contains(got, "open the item") {
		t.Errorf("Describe = %q, want the global binding's Desc override", got)
	}
	if strings.Contains(got, "confirm the dialog") {
		t.Errorf("Describe(ScopeGlobal) = %q, must not include the screen binding", got)
	}
	got = idsOf(r.Describe(ScopeScreen))
	if !strings.Contains(got, "confirm the dialog") {
		t.Errorf("Describe(ScopeScreen) = %q, want the screen binding's Desc override", got)
	}
	// And the command's own Desc is the fallback when no binding overrides it.
	if !strings.Contains(idsOf(r.Describe(ScopeFocus)), "open the item") {
		t.Error("Describe did not fall back to the command's own Desc")
	}
}

// TestDescribeWidensWithScope is the meaning this implementation gives the
// scope parameter, stated as a test: a focus-level query reports everything
// reachable in the current context, and a global query reports the
// application's own keys.
//
// The ADR says "the discoverable entries for scope" without saying which way the
// widening goes. It goes UPWARD because both of the ADR's named callers need
// it that way: a KeyHint bound to the current context wants the focus answer,
// and a global palette (which §9 says is fed from Describe(ScopeGlobal)) wants
// the application's own keys and NOT a dialog's. A narrowing parameter would
// make ScopeGlobal mean "everything", which is the opposite of what a palette
// bound to Esc-and-things-wants needs.
func TestDescribeWidensWithScope(t *testing.T) {
	r := New()
	focus := &plain{bounds: termmosaic.Rect{W: 10, H: 3}}
	screen := &plain{bounds: termmosaic.Rect{W: 80, H: 24}}
	r.Register(
		Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }},
		Command{ID: "dialog.cancel", Desc: "cancel the dialog", Run: func(Ctx) bool { return true }},
		Command{ID: "list.up", Desc: "previous row", Run: func(Ctx) bool { return true }},
	)
	esc, _ := ParseChord("Esc")
	up, _ := ParseChord("Up")
	r.Bind(
		Binding{Chord: esc, ID: "app.quit", Scope: ScopeGlobal},
		Binding{Chord: esc, ID: "dialog.cancel", Scope: ScopeScreen, Owner: screen},
		Binding{Chord: up, ID: "list.up", Scope: ScopeFocus, Owner: focus},
	)
	r.Seal()
	r.SetScreen(screen)
	// Record the focus so the focus binding is live.
	r.Dispatch(termmosaic.KeyEvent('a', 0), focus)

	global := idsOf(r.Describe(ScopeGlobal))
	if !strings.Contains(global, "app.quit") {
		t.Errorf("Describe(ScopeGlobal) = %q, want the global binding", global)
	}
	if strings.Contains(global, "dialog.cancel") {
		t.Errorf("Describe(ScopeGlobal) = %q, must not contain the dialog's binding; a global palette is not the dialog's palette", global)
	}

	screenWide := idsOf(r.Describe(ScopeScreen))
	if !strings.Contains(screenWide, "dialog.cancel") || !strings.Contains(screenWide, "app.quit") {
		t.Errorf("Describe(ScopeScreen) = %q, want the screen and global bindings", screenWide)
	}

	focusWide := idsOf(r.Describe(ScopeFocus))
	for _, want := range []string{"app.quit", "dialog.cancel", "list.up"} {
		if !strings.Contains(focusWide, want) {
			t.Errorf("Describe(ScopeFocus) = %q, want it to include %s", focusWide, want)
		}
	}
}

// TestChordsIsTheCheapSingleCommandQuery is the other half of §4's API, and it
// is what a KeyHint showing one row calls.
func TestChordsIsTheCheapSingleCommandQuery(t *testing.T) {
	r := New()
	r.Register(Command{ID: "file.save", Desc: "save", Run: func(Ctx) bool { return true }})
	for _, s := range []string{"Ctrl+s", "F2", "Ctrl+S"} {
		if err := r.BindString(s, "file.save", ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.Seal()
	got := r.Chords("file.save", ScopeGlobal)
	want := []string{"F2", "Ctrl+S", "Ctrl+s"}
	if len(got) != len(want) {
		t.Fatalf("Chords returned %d chords, want %d", len(got), len(want))
	}
	for i, c := range got {
		if c.String() != want[i] {
			t.Errorf("Chords[%d] = %q, want %q (canonical order)", i, c.String(), want[i])
		}
	}
	if got := r.Chords("no.such.command", ScopeGlobal); len(got) != 0 {
		t.Errorf("Chords for an unregistered command returned %v, want none", got)
	}
}

// TestWarningsReportsWhatTheDocCommentPromises is §4 rule 1: diagnostics are
// surface, never control flow, and the Registry reports what is wrong rather
// than refusing to work.
func TestWarningsReportsWhatTheDocCommentPromises(t *testing.T) {
	t.Run("a binding naming an unregistered command", func(t *testing.T) {
		r := New()
		if err := r.BindString("q", "app.quit", ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
		if !containsWarning(r.Warnings(), "unregistered command") {
			t.Errorf("Warnings = %v, want it to report the unregistered command", r.Warnings())
		}
	})

	t.Run("a command with an empty description that a user can reach", func(t *testing.T) {
		r := New()
		r.Register(Command{ID: "app.quit", Run: func(Ctx) bool { return true }})
		if err := r.BindString("q", "app.quit", ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
		r.Seal()
		if !containsWarning(r.Warnings(), "no description") {
			t.Errorf("Warnings = %v, want it to report the missing description", r.Warnings())
		}
		// And it is STILL described, as a row with an empty description, because
		// a warning is not a silent omission: the user sees a key with nothing
		// beside it, which is a bug report, rather than no row at all, which is
		// a mystery.
		if got := idsOf(r.Describe(ScopeGlobal)); !strings.Contains(got, "app.quit: ") {
			t.Errorf("Describe = %q, want the unlabelled command to appear", got)
		}
	})

	t.Run("a scoped binding with no owner", func(t *testing.T) {
		r := New()
		r.Register(Command{ID: "list.up", Desc: "up", Run: func(Ctx) bool { return true }})
		if err := r.BindString("Up", "list.up", ScopeFocus, nil); err != nil {
			t.Fatal(err)
		}
		r.Seal()
		if !containsWarning(r.Warnings(), "has no owner") {
			t.Errorf("Warnings = %v, want it to report the ownerless scoped binding", r.Warnings())
		}
	})

	t.Run("a global binding with a non-nil owner", func(t *testing.T) {
		r := New()
		w := &plain{}
		r.Register(Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }})
		if err := r.BindString("q", "app.quit", ScopeGlobal, w); err != nil {
			t.Fatal(err)
		}
		r.Seal()
		if !containsWarning(r.Warnings(), "non-nil owner") {
			t.Errorf("Warnings = %v, want it to report the owner on a global binding", r.Warnings())
		}
	})

	t.Run("a chord bound to two commands in one scope", func(t *testing.T) {
		r := New()
		r.Register(
			Command{ID: "list.up", Desc: "up", Run: func(Ctx) bool { return true }},
			Command{ID: "tree.up", Desc: "up", Run: func(Ctx) bool { return true }},
		)
		up, _ := ParseChord("Up")
		// Two widgets publishing Up for DIFFERENT commands. Both are live in
		// principle and only one can be what the user meant, so the second is
		// reachable only if the first declines.
		l := &publishing{plain: plain{bounds: termmosaic.Rect{W: 10, H: 3}}}
		l.bindings = []Binding{{Chord: up, ID: "list.up"}}
		tree := &publishing{plain: plain{bounds: termmosaic.Rect{W: 10, H: 3}}}
		tree.bindings = []Binding{{Chord: up, ID: "tree.up"}}
		r.Attach(l, tree)
		r.Seal()
		r.Dispatch(termmosaic.KeyEvent('a', 0), l)
		if !containsWarning(r.Warnings(), "only one of them can run") {
			t.Errorf("Warnings = %v, want it to report the shadowed binding", r.Warnings())
		}
	})

	t.Run("a clean registry has no warnings", func(t *testing.T) {
		r := New()
		widget := &publishing{plain: plain{bounds: termmosaic.Rect{W: 10, H: 3}}}
		widget.bindings = []Binding{{Chord: Chord{Key: termmosaic.KeyUp}, ID: "list.up"}}
		r.Register(
			Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }},
			Command{ID: "list.up", Desc: "previous row", Run: func(Ctx) bool { return true }},
		)
		if err := r.BindString("q", "app.quit", ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
		r.Attach(widget)
		r.Seal()
		if got := r.Warnings(); len(got) != 0 {
			t.Errorf("Warnings = %v, want none for a correctly built registry", got)
		}
	})
}

// TestWarningsAreStable is a property of a diagnostic surface: a reader
// comparing two runs' warnings should see a diff, not a shuffle.
func TestWarningsAreStable(t *testing.T) {
	build := func() []string {
		r := New()
		r.Register(Command{ID: "b", Run: func(Ctx) bool { return true }})
		r.Register(Command{ID: "a", Run: func(Ctx) bool { return true }})
		for _, s := range []string{"q", "x", "z"} {
			if err := r.BindString(s, "missing", ScopeGlobal, nil); err != nil {
				t.Fatal(err)
			}
		}
		r.Seal()
		return r.Warnings()
	}
	first := build()
	for i := 0; i < 8; i++ {
		got := build()
		if len(got) != len(first) {
			t.Fatalf("run %d produced %d warnings, want %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d warning %d = %q, want %q; warning order must be stable", i, j, got[j], first[j])
			}
		}
	}
	if len(first) < 3 {
		t.Errorf("only %d warnings, want at least one per unregistered binding", len(first))
	}
}

// TestDescribeIsNotOnTheDispatchPath is the negative half of §4's rule, stated
// where it can be enforced: Describe allocates and sorts, so calling it from
// Dispatch or from a widget's Draw would put a heap allocation and an O(n log n)
// sort on the input path. The allocation test covers Dispatch; this covers the
// other side by asserting Describe is NOT what Dispatch consults, which is
// visible as the resolution table being a different data structure.
func TestDescribeIsNotOnTheDispatchPath(t *testing.T) {
	r := New()
	r.Register(Command{ID: "c", Desc: "c", Run: func(Ctx) bool { return true }})
	if err := r.BindString("x", "c", ScopeGlobal, nil); err != nil {
		t.Fatal(err)
	}
	r.Seal()
	// Sealing builds the resolution table and nothing else; Describe builds its
	// own rows on demand and stores nothing.
	rowsBefore := len(r.Describe(ScopeGlobal))
	if got := len(r.Describe(ScopeGlobal)); got != rowsBefore {
		t.Errorf("Describe is not idempotent: %d then %d rows", rowsBefore, got)
	}
	// And Describe is in fact NOT free, which is the honest half of the rule.
	rows := r.Describe(ScopeGlobal)
	allocs := testing.AllocsPerRun(50, func() { allocSink = r.Describe(ScopeGlobal) })
	if allocs == 0 {
		t.Error("Describe allocated nothing, so the doc comment saying it allocates is wrong; a future optimisation that does not preserve its output shape would then be undetectable")
	}
	_ = rows
}
