package keymap

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
)

// TestDescribeGroupedIsOneEntryPerCommand is the whole point of the second
// query: the same registry, asked one row per command, produces one entry per
// command carrying every chord that reaches it. It is the answer a one-line hint
// wants, where Describe's one row per chord would print the same description
// three times.
func TestDescribeGroupedIsOneEntryPerCommand(t *testing.T) {
	r := New()
	r.Register(
		Command{ID: "file.save", Desc: "save", Group: "file", Run: func(Ctx) bool { return true }},
		Command{ID: "file.open", Desc: "open", Group: "file", Run: func(Ctx) bool { return true }},
	)
	for _, s := range []string{"Ctrl+s", "Ctrl+S", "F2"} {
		if err := r.BindString(s, "file.save", ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.BindString("Ctrl+o", "file.open", ScopeGlobal, nil); err != nil {
		t.Fatal(err)
	}
	r.Seal()

	got := idsOf(r.DescribeGrouped(ScopeGlobal))
	want := "file/file.open: open [Ctrl+o] | file/file.save: save [F2 Ctrl+S Ctrl+s]"
	if got != want {
		t.Errorf("DescribeGrouped(ScopeGlobal):\n got %s\nwant %s", got, want)
	}
	// And the shape it promises: two commands in, two entries out, with no
	// entry carrying more than one command's worth of chords.
	for _, e := range r.DescribeGrouped(ScopeGlobal) {
		if len(e.Chords) == 0 {
			t.Errorf("entry %v has no chords", e.ID)
		}
	}
}

// TestDescribeGroupedChordOrderMatchesDescribe states the ordering requirement as
// a comparison rather than as a literal: the chords inside an entry must be in
// the order Describe used, because that is Describe's canonical order (modifier,
// then Key, then rune) and it is the order §4 promises. A literal would state the
// order and not check that the two queries agree about it, which is the property
// that would actually rot.
func TestDescribeGroupedChordOrderMatchesDescribe(t *testing.T) {
	r := New()
	r.Register(
		Command{ID: "file.save", Desc: "save", Group: "file", Run: func(Ctx) bool { return true }},
		Command{ID: "nav.next", Desc: "next", Group: "nav", Run: func(Ctx) bool { return true }},
	)
	// Registered out of chord order deliberately, and across two commands and two
	// scopes, so a DescribeGrouped that sorted by itself rather than inheriting
	// Describe's order would come out different.
	for _, s := range []string{"Ctrl+s", "F2", "Ctrl+S", "Esc"} {
		if err := r.BindString(s, "file.save", ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
	}
	navOwner := &plain{bounds: termmosaic.Rect{W: 4, H: 2}}
	for _, s := range []string{"Down", "Tab", "Right"} {
		if err := r.BindString(s, "nav.next", ScopeScreen, navOwner); err != nil {
			t.Fatal(err)
		}
	}
	r.Seal()

	perChord := r.Describe(ScopeScreen)
	grouped := r.DescribeGrouped(ScopeScreen)

	// Flatten Describe into the grouped entry's own order and compare.
	flat := make([]string, 0, len(perChord))
	for _, e := range perChord {
		if len(e.Chords) == 0 {
			continue
		}
		flat = append(flat, string(e.ID)+" "+e.Chords[0].String())
	}
	var merged []string
	for _, e := range grouped {
		for _, c := range e.Chords {
			merged = append(merged, string(e.ID)+" "+c.String())
		}
	}
	if strings.Join(merged, " | ") != strings.Join(flat, " | ") {
		t.Errorf("chord order differs from Describe:\n grouped %v\n describe %v", merged, flat)
	}
	// And the canonical order is the one §4 names, spelled out once so the
	// comparison above has something to be right about: the two unmodified keys
	// first (Mod 0), ascending by Key, then the two modified ones (Mod > 0),
	// ascending by rune — 'S' before 's'.
	if got, want := chordStrings(grouped[0].Chords), "Esc F2 Ctrl+S Ctrl+s"; strings.Join(got, " ") != want {
		t.Errorf("file.save chords = %v, want %s (modifiers ascending, then Key, then rune)", got, want)
	}
}

// TestDescribeGroupedEntryOrderMatchesDescribe is the other half of the ordering
// requirement: the entry ORDER across commands is Describe's command order, so a
// hint's rows are stable across runs and diffable across versions.
//
// It is compared against Describe rather than written out, because the property
// is agreement between the two queries — a literal here would let DescribeGrouped
// silently develop a second ordering.
func TestDescribeGroupedEntryOrderMatchesDescribe(t *testing.T) {
	r := New()
	r.Register(
		Command{ID: "view.zoom", Desc: "zoom", Group: "view", Run: func(Ctx) bool { return true }},
		Command{ID: "file.save", Desc: "save", Group: "file", Run: func(Ctx) bool { return true }},
		Command{ID: "file.open", Desc: "open", Group: "file", Run: func(Ctx) bool { return true }},
		// No group: the uncategorised bucket, which renders LAST.
		Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }},
	)
	for s, id := range map[string]CommandID{
		"q": "app.quit", "Ctrl+o": "file.open", "Ctrl+s": "file.save", "Ctrl+=": "view.zoom",
	} {
		if err := r.BindString(s, id, ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.Seal()

	// Every command here has exactly one chord, so this is also the equivalence
	// case: the two queries must be identical apart from the chord list.
	var want []string
	seen := map[CommandID]bool{}
	for _, e := range r.Describe(ScopeGlobal) {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		want = append(want, string(e.ID))
	}
	var got []string
	for _, e := range r.DescribeGrouped(ScopeGlobal) {
		got = append(got, string(e.ID))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("DescribeGrouped order = %v, want Describe's command order %v", got, want)
	}
	if strings.Join(got, ",") != "file.open,file.save,view.zoom,app.quit" {
		t.Errorf("DescribeGrouped order = %v, want group then ID with the uncategorised bucket last", got)
	}
}

// TestDescribeGroupedOmitsCommandsOutOfScope is the scope half. A command bound
// only in a narrower scope belongs to that scope's answer and not to this one, so
// it is absent — and an absent command is absent rather than present with an
// empty Chords, because the invariant is that Chords is never empty.
//
// The chordless command is a DIFFERENT case and is deliberately not covered here:
// Describe includes a command nothing is bound to, and DescribeGrouped does not.
// That decision is tested on its own in
// TestDescribeGroupedOmitsChordlessCommands.
func TestDescribeGroupedOmitsCommandsOutOfScope(t *testing.T) {
	r := New()
	screen := &plain{bounds: termmosaic.Rect{W: 80, H: 24}}
	r.Register(
		Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }},
		Command{ID: "dialog.cancel", Desc: "cancel the dialog", Run: func(Ctx) bool { return true }},
	)
	esc, _ := ParseChord("Esc")
	r.Bind(
		Binding{Chord: esc, ID: "app.quit", Scope: ScopeGlobal},
		Binding{Chord: esc, ID: "dialog.cancel", Scope: ScopeScreen, Owner: screen},
	)
	r.Seal()
	r.SetScreen(screen)

	global := idsOf(r.DescribeGrouped(ScopeGlobal))
	if !strings.Contains(global, "app.quit") {
		t.Errorf("DescribeGrouped(ScopeGlobal) = %q, want the global binding", global)
	}
	if strings.Contains(global, "dialog.cancel") {
		t.Errorf("DescribeGrouped(ScopeGlobal) = %q, must not contain the dialog's binding", global)
	}
	// Widening is Describe's rule and DescribeGrouped inherits it: the screen's
	// answer includes the application's own keys.
	wide := idsOf(r.DescribeGrouped(ScopeScreen))
	if !strings.Contains(wide, "dialog.cancel") || !strings.Contains(wide, "app.quit") {
		t.Errorf("DescribeGrouped(ScopeScreen) = %q, want the screen and global bindings", wide)
	}
	for _, e := range r.DescribeGrouped(ScopeGlobal) {
		if len(e.Chords) == 0 {
			t.Errorf("entry %v has an empty Chords; an entry must always carry its key", e.ID)
		}
	}
}

// TestDescribeGroupedOmitsChordlessCommands is the decision the doc has to make
// explicit, because the two queries genuinely differ here and a reader will look
// for the answer.
//
// Describe gives a command that NO key reaches — a palette-only or mouse-only
// command — a chordless row in EVERY scope. DescribeGrouped omits it. The reason
// is the consumer, not the data: a chordless row exists so that a help screen
// listing keys does not hide a command the user can still reach, and the screen
// that consumes those rows is a palette built from Describe. A hint line has
// nothing to gain from the row — its key column would be empty, so the hint
// renders a bare description with nothing to press, and a hint whose entries are
// mostly bare descriptions has stopped being a hint.
//
// The alternative — include it with empty Chords — would also break the invariant
// that Chords is never empty, and KeyHint.SetEntries's chordsLabel already has a
// case for zero chords precisely because hand-written bindings can produce one,
// which is not a reason for a registry query to produce one.
func TestDescribeGroupedOmitsChordlessCommands(t *testing.T) {
	r := New()
	r.Register(
		Command{ID: "button.activate", Desc: "press the button", Group: "button", Run: func(Ctx) bool { return true }},
		Command{ID: "file.save", Desc: "save", Group: "file", Run: func(Ctx) bool { return true }},
	)
	if err := r.BindString("Ctrl+s", "file.save", ScopeGlobal, nil); err != nil {
		t.Fatal(err)
	}
	r.Seal()

	// Describe keeps it: a help screen must still be able to say it exists.
	if got := idsOf(r.Describe(ScopeGlobal)); !strings.Contains(got, "button.activate: press the button []") {
		t.Errorf("Describe = %q, want the chordless command still described", got)
	}
	// DescribeGrouped omits it, and the whole output is the one bound command.
	if got := idsOf(r.DescribeGrouped(ScopeGlobal)); got != "file/file.save: save [Ctrl+s]" {
		t.Errorf("DescribeGrouped = %q, want only the bound command", got)
	}
}

// TestDescribeGroupedAgreesWithDescribeForSingleChordCommands is the equivalence
// property, stated over a registry built to be uninteresting: every command has
// exactly one chord. In that case the per-chord query and the per-command query
// are the same list, and if they ever are not, one of them has a bug that no
// literal comparison would catch.
func TestDescribeGroupedAgreesWithDescribeForSingleChordCommands(t *testing.T) {
	r := New()
	r.Register(
		Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }},
		Command{ID: "file.save", Desc: "save", Group: "file", Run: func(Ctx) bool { return true }},
		Command{ID: "file.open", Desc: "open", Group: "file", Run: func(Ctx) bool { return true }},
		Command{ID: "nav.next", Desc: "next", Group: "nav", Run: func(Ctx) bool { return true }},
		Command{ID: "view.zoom", Desc: "zoom", Group: "view", Run: func(Ctx) bool { return true }},
	)
	for s, id := range map[string]CommandID{
		"q": "app.quit", "Ctrl+s": "file.save", "Ctrl+o": "file.open", "Right": "nav.next", "Ctrl+=": "view.zoom",
	} {
		if err := r.BindString(s, id, ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.Seal()

	for _, scope := range []Scope{ScopeGlobal, ScopeScreen, ScopeFocus} {
		perChord := r.Describe(scope)
		grouped := r.DescribeGrouped(scope)
		if len(perChord) != len(grouped) {
			t.Errorf("%v: Describe returned %d entries, DescribeGrouped %d; with one chord per command they must be equal", scope, len(perChord), len(grouped))
			continue
		}
		for i := range perChord {
			if idsOf(perChord[i:i+1]) != idsOf(grouped[i:i+1]) {
				t.Errorf("%v: entry %d differs:\n Describe     %s\n Grouped     %s",
					scope, i, idsOf(perChord[i:i+1]), idsOf(grouped[i:i+1]))
			}
		}
	}
}

// TestDescribeGroupedIsNotOnTheDispatchPath is the negative half of the rule
// Describe's own test states, for the second query. DescribeGrouped allocates —
// it builds a slice per entry and a chord list per command — so it must never be
// reachable from Dispatch.
//
// Nothing about zero allocation is asserted: unlike Dispatch, this query is meant
// to allocate. What is asserted is that it does not, and cannot quietly, become
// what Dispatch consults — the same evidence TestDescribeIsNotOnTheDispatchPath
// uses, which is that the query is idempotent, builds its rows on demand, and
// stores nothing on the registry.
func TestDescribeGroupedIsNotOnTheDispatchPath(t *testing.T) {
	r := New()
	r.Register(Command{ID: "c", Desc: "c", Run: func(Ctx) bool { return true }})
	if err := r.BindString("x", "c", ScopeGlobal, nil); err != nil {
		t.Fatal(err)
	}
	r.Seal()

	first := idsOf(r.DescribeGrouped(ScopeGlobal))
	for i := 0; i < 4; i++ {
		if got := idsOf(r.DescribeGrouped(ScopeGlobal)); got != first {
			t.Fatalf("run %d = %q, want %q; the query must be idempotent", i, got, first)
		}
	}
	// It genuinely allocates, which is what keeps it off the input path: a
	// Dispatch that reached for it would allocate per keystroke.
	allocs := testing.AllocsPerRun(50, func() { allocSink = r.DescribeGrouped(ScopeGlobal) })
	if allocs == 0 {
		t.Error("DescribeGrouped allocated nothing; the doc comment saying it allocates would then be wrong, and a future optimisation that changed its output shape would be undetectable")
	}
	// And the returned chords are the caller's: mutating them must not reach back
	// into the registry, or a caller sorting its hint rows would corrupt the next
	// query. This is what "not on the dispatch path" needs beyond the allocation
	// count — a shared slice would make the second caller's answer depend on the
	// first caller's.
	rows := r.DescribeGrouped(ScopeGlobal)
	rows[0].Chords[0] = Chord{Rune: 'z'}
	if got := idsOf(r.DescribeGrouped(ScopeGlobal)); got != first {
		t.Errorf("DescribeGrouped returned a registry-owned slice: mutating it changed the next answer to %q, want %q", got, first)
	}
}

// chordStrings renders a chord list for a failure message.
func chordStrings(chords []Chord) []string {
	out := make([]string, 0, len(chords))
	for _, c := range chords {
		out = append(out, c.String())
	}
	return out
}
