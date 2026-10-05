package form_test

import (
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/keymap"
	"github.com/serkanalgur/termmosaic/widgets/form"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// TestKeyHintSetEntriesIsTheRegistryPath is §4.1's one addition to the widget
// catalog, tested for the property the ADR says it has: a form's hints cannot
// disagree with the program's bindings, because they ARE the program's bindings.
//
// The test renders through the whole stack rather than calling Draw, per
// widgettest's rule — a hint is a line of text in a pane, and what matters is
// what the user sees in it.
func TestKeyHintSetEntriesIsTheRegistryPath(t *testing.T) {
	km := keymap.New()
	km.Register(
		keymap.Command{ID: "file.save", Desc: "save the file", Group: "file", Run: func(keymap.Ctx) bool { return true }},
		keymap.Command{ID: "search.submit", Desc: "run the search", Group: "search", Run: func(keymap.Ctx) bool { return true }},
		keymap.Command{ID: "button.activate", Desc: "press the button", Group: "button", Run: func(keymap.Ctx) bool { return true }},
	)
	for s, id := range map[string]keymap.CommandID{
		"Ctrl+s": "file.save",
		"Enter":  "search.submit",
	} {
		if err := km.BindString(s, id, keymap.ScopeGlobal, nil); err != nil {
			t.Fatal(err)
		}
	}
	km.Seal()

	hint := form.NewKeyHint(buffer.Rect{X: 0, Y: 0, W: 72, H: 1}, nil)
	hint.SetEntries(km.Describe(keymap.ScopeGlobal))

	// The hint's rows are what Describe produced, in Describe's order: groups
	// alphabetically, so "button", then "file", then "search".
	want := []form.Binding{
		{Key: "", Help: "press the button"},
		{Key: "Ctrl+s", Help: "save the file"},
		{Key: "Enter", Help: "run the search"},
	}
	if len(hint.Bindings) != len(want) {
		t.Fatalf("the hint holds %d rows, want %d: %+v", len(hint.Bindings), len(want), hint.Bindings)
	}
	for i, w := range want {
		if hint.Bindings[i] != w {
			t.Errorf("row %d = %+v, want %+v", i, hint.Bindings[i], w)
		}
	}

	// And it renders, with the brackets that carry the shape without colour.
	sink := widgettest.Render(t, 72, 3, 1, hint)
	frame := sink.Line(0)
	for _, want := range []string{"[Enter]", "run the search", "[Ctrl+s]", "save the file", "press the button"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the rendered hint does not contain %q.\nGot:\n%s", want, frame)
		}
	}
}

// TestKeyHintSetEntriesAndSetBindingsAgree is the ADR's claim that both paths
// "produce identical rows", and it is the reason SetEntries is a method rather
// than a change to the Bindings field's type: an application with no registry
// writes []form.Binding by hand and gets the same widget either way.
func TestKeyHintSetEntriesAndSetBindingsAgree(t *testing.T) {
	entries := []keymap.Entry{
		{ID: "file.save", Desc: "save the file", Group: "file", Chords: []keymap.Chord{mustParse(t, "Ctrl+s")}},
		{ID: "search.submit", Desc: "run the search", Chords: []keymap.Chord{mustParse(t, "Enter")}},
		// A command with no chord: a palette-only or mouse-only command, which
		// a key-only hint would otherwise hide.
		{ID: "button.activate", Desc: "press the button"},
	}
	fromEntries := form.NewKeyHint(buffer.Rect{W: 72, H: 1}, nil)
	fromEntries.SetEntries(entries)

	fromBindings := form.NewKeyHint(buffer.Rect{W: 72, H: 1}, []form.Binding{
		{Key: entries[0].Chords[0].String(), Help: entries[0].Desc},
		{Key: entries[1].Chords[0].String(), Help: entries[1].Desc},
		{Key: "", Help: entries[2].Desc},
	})

	if len(fromEntries.Bindings) != len(fromBindings.Bindings) {
		t.Fatalf("SetEntries produced %d rows and SetBindings %d", len(fromEntries.Bindings), len(fromBindings.Bindings))
	}
	for i := range fromBindings.Bindings {
		if fromEntries.Bindings[i] != fromBindings.Bindings[i] {
			t.Errorf("row %d: SetEntries gave %+v, SetBindings gave %+v; the two paths must agree",
				i, fromEntries.Bindings[i], fromBindings.Bindings[i])
		}
	}
}

// TestKeyHintSetEntriesHandlesSeveralChords covers the case where a command
// answers to more than one key and a hint has a single key column. The chords
// are joined rather than dropped, because a hint that shows only the first of
// three ways to save is a hint that under-reports.
func TestKeyHintSetEntriesHandlesSeveralChords(t *testing.T) {
	hint := form.NewKeyHint(buffer.Rect{W: 72, H: 1}, nil)
	// The chords are built by parsing rather than by writing literals, because a
	// raw Key value in a test is a number that means nothing at the failure
	// message and that breaks silently when the enum grows.
	ctrlS := mustParse(t, "Ctrl+s")
	hint.SetEntries([]keymap.Entry{{
		ID:     "file.save",
		Desc:   "save the file",
		Chords: []keymap.Chord{mustParse(t, "F1"), ctrlS},
	}})
	if len(hint.Bindings) != 1 {
		t.Fatalf("the hint holds %d rows, want 1", len(hint.Bindings))
	}
	if got := hint.Bindings[0].Key; got != "F1 Ctrl+s" {
		t.Errorf("the key column is %q, want %q", got, "F1 Ctrl+s")
	}
}

// TestKeyHintSetEntriesEmptyClearsTheHint is the edge that a hint changes
// whenever the state does, and it is why SetEntries has to handle the empty
// slice rather than assume one is coming: a palette filtered to nothing must
// leave no stale row behind, or the user reads a key that no longer works.
func TestKeyHintSetEntriesEmptyClearsTheHint(t *testing.T) {
	hint := form.NewKeyHint(buffer.Rect{W: 72, H: 1}, []form.Binding{{Key: "q", Help: "quit"}})
	hint.SetEntries(nil)
	if len(hint.Bindings) != 0 {
		t.Errorf("SetEntries(nil) left %d rows: %+v", len(hint.Bindings), hint.Bindings)
	}
	// And an empty slice, which is not nil, does the same thing.
	hint.SetEntries([]keymap.Entry{{ID: "x", Desc: "x"}})
	hint.SetEntries([]keymap.Entry{})
	if len(hint.Bindings) != 0 {
		t.Errorf("SetEntries(empty) left %d rows: %+v", len(hint.Bindings), hint.Bindings)
	}
}

// mustParse is ParseChord with the failure reported against the test rather than
// a nil chord, because a nil Chord in a hint row is a blank key and the failure
// would read as a rendering bug.
func mustParse(t *testing.T, s string) keymap.Chord {
	t.Helper()
	c, err := keymap.ParseChord(s)
	if err != nil {
		t.Fatalf("ParseChord(%q): %v", s, err)
	}
	return c
}

// TestKeyHintStillRendersNothingWhenEmpty is the total-function property: a hint
// with no rows draws its background and nothing else, rather than blanking or
// panicking, which is what makes SetEntries(nil) safe to call on every frame.
func TestKeyHintStillRendersNothingWhenEmpty(t *testing.T) {
	hint := form.NewKeyHint(buffer.Rect{X: 0, Y: 0, W: 20, H: 1}, nil)
	sink := widgettest.Render(t, 20, 2, 1, hint)
	if got := strings.TrimSpace(sink.Line(0)); got != "" {
		t.Errorf("an empty hint rendered %q, want nothing", got)
	}
}
