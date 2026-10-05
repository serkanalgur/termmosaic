package keymap

import (
	"testing"

	"github.com/serkanalgur/termmosaic"
)

// modSubsets is every subset of the four modifier bits, in the order §3's round
// trip wants to try them. The zero subset is first because a bare key is the
// common case and the one a notation table most often forgets.
var modSubsets = []termmosaic.KeyMod{
	0,
	termmosaic.ModShift,
	termmosaic.ModAlt,
	termmosaic.ModCtrl,
	termmosaic.ModSuper,
	termmosaic.ModCtrl | termmosaic.ModAlt,
	termmosaic.ModCtrl | termmosaic.ModShift,
	termmosaic.ModAlt | termmosaic.ModShift,
	termmosaic.ModCtrl | termmosaic.ModSuper,
	termmosaic.ModAlt | termmosaic.ModSuper,
	termmosaic.ModShift | termmosaic.ModSuper,
	termmosaic.ModCtrl | termmosaic.ModAlt | termmosaic.ModShift,
	termmosaic.ModCtrl | termmosaic.ModAlt | termmosaic.ModSuper,
	termmosaic.ModCtrl | termmosaic.ModShift | termmosaic.ModSuper,
	termmosaic.ModAlt | termmosaic.ModShift | termmosaic.ModSuper,
	termmosaic.ModCtrl | termmosaic.ModAlt | termmosaic.ModShift | termmosaic.ModSuper,
}

// allKeys is every non-zero Key in the enum, in declaration order.
var allKeys = []termmosaic.Key{
	termmosaic.KeyEnter, termmosaic.KeyTab, termmosaic.KeyBacktab,
	termmosaic.KeyBackspace, termmosaic.KeyEscape, termmosaic.KeySpace,
	termmosaic.KeyUp, termmosaic.KeyDown, termmosaic.KeyLeft, termmosaic.KeyRight,
	termmosaic.KeyHome, termmosaic.KeyEnd, termmosaic.KeyPageUp, termmosaic.KeyPageDown,
	termmosaic.KeyDelete, termmosaic.KeyInsert,
	termmosaic.KeyF1, termmosaic.KeyF2, termmosaic.KeyF3, termmosaic.KeyF4,
	termmosaic.KeyF5, termmosaic.KeyF6, termmosaic.KeyF7, termmosaic.KeyF8,
	termmosaic.KeyF9, termmosaic.KeyF10, termmosaic.KeyF11, termmosaic.KeyF12,
}

// printableRunes is a spread chosen for what it breaks: letters whose Shift form
// is a different rune, digits, punctuation that shares a C0 control code
// (Ctrl+I, Ctrl+J, Ctrl+M, Ctrl+H), a multi-byte rune, a combining-free
// non-ASCII letter, and the two characters that are modifiers themselves.
var printableRunes = []rune{
	'a', 'A', 'z', 'Z', '0', '9',
	'?', '/', '\\', ']', '[', ';', '\'', '`', '-', '=', '~',
	'.', ',', '+', ' ',
	'é', 'ü', '日',
}

// TestParseChordRoundTrips is the pinning ADR 0009 §3 asks for: every chord
// String produces must parse back to itself, over a GENERATED table rather than
// a hand-written list of pairs.
//
// The generated part is the point. A hand-written table proves the cases
// somebody thought of, and the notation bug that costs a user a keybinding is
// always in the case nobody thought of — the twelfth function key, the modifier
// nobody uses, the rune whose uppercase is itself. Here the table is every Key
// in the enum × every modifier subset × a spread of printable runes, so
// appending KeyF13 to the iota block (ADR 0005 §10) fails this test by itself.
//
// Two chords in the generated table are deliberately NOT round-trippable and
// are skipped by construction rather than by an exception list:
//
//   - A printable rune with ModShift, because Shift is FOLDED INTO THE RUNE.
//     The chord {Rune 'a', ModShift} is not a chord; {Rune 'A'} is. This is
//     §3's rule 2 and it is the reason "ctrl+shift+s" canonicalises to "Ctrl+S".
//   - A zero Chord, which is not a legal binding.
func TestParseChordRoundTrips(t *testing.T) {
	var checked int
	for _, k := range allKeys {
		for _, mod := range modSubsets {
			// A non-printable key keeps Shift as a bit: Shift+Enter is a real
			// gesture, and the decoder reports it as one.
			c, ok := normalise(k, 0, mod)
			if !ok {
				t.Fatalf("Chord{%v, %v} normalised away; a non-printable key is always a chord", k, mod)
			}
			assertRoundTrips(t, c)
			checked++
		}
	}
	for _, r := range printableRunes {
		for _, mod := range modSubsets {
			// ModShift is dropped: it is folded into the rune, so the pair
			// (rune, ModShift) is not a chord and Chord.String would never
			// print it.
			effective := mod &^ termmosaic.ModShift
			c, ok := normalise(termmosaic.KeyNone, r, effective)
			if !ok {
				// A rune the normalisation folds to nothing — the space bar,
				// which becomes KeySpace, is handled; a literal NUL is not a
				// gesture at all.
				continue
			}
			if c.Rune != 0 && c.Mod&termmosaic.ModShift != 0 {
				t.Fatalf("Chord{%q, %v} has Shift on a printable rune; the invariant is that ModShift is never set when Rune is non-zero", c.Rune, c.Mod)
			}
			assertRoundTrips(t, c)
			checked++
		}
	}
	if checked < 400 {
		t.Errorf("only %d chords round-tripped; the table is smaller than intended and is not proving much", checked)
	}
	t.Logf("round-tripped %d chords", checked)
}

// assertRoundTrips checks that c.String() parses back to c, and reports both
// forms on failure so the diagnostic names the disagreement.
func assertRoundTrips(t *testing.T, c Chord) {
	t.Helper()
	s := c.String()
	got, err := ParseChord(s)
	if err != nil {
		t.Errorf("Chord%+v printed %q, which does not parse: %v", c, s, err)
		return
	}
	if got != c {
		t.Errorf("Chord%+v printed %q, which parsed back as Chord%+v; display and parsing disagree", c, s, got)
	}
}

// TestKeyNameTableCoversTheEnum is the other half of §3's claim that the
// notation table picks up new keys automatically. It cannot be automatic —
// a new Key has no name until somebody writes one — so the property that is
// actually enforceable is that the table and the enum have not drifted apart,
// and that failure is a loud one rather than a key that quietly does nothing.
func TestKeyNameTableCoversTheEnum(t *testing.T) {
	for _, k := range allKeys {
		if _, ok := keyDisplay[k]; !ok {
			t.Errorf("Key(%d) has no display name in keymap's table; String() would print an empty key and a help screen would document nothing", int(k))
		}
	}
	// And nothing in the table that the enum does not have, which catches a
	// key removed or renumbered.
	for k := range keyDisplay {
		if k == termmosaic.KeyNone {
			t.Errorf("keyDisplay must not name KeyNone; the zero key is not a key")
			continue
		}
		if _, ok := keyByName(keyDisplay[k]); !ok {
			t.Errorf("keyDisplay names Key(%d) as %q but the reverse table cannot find it", int(k), keyDisplay[k])
		}
	}
	// Key.String() is the authority for the names; the display table must not
	// have invented a name for a key the root package calls something else.
	for k, display := range keyDisplay {
		if got := k.String(); got == "" {
			t.Errorf("Key(%d) is named %q in keymap but termmosaic.Key.String has no name for it", int(k), display)
		}
	}
}

// TestParseChordAcceptsTheSpellingsTheTablePromises walks §3's notation table
// row by row, so the table in the document and the code cannot drift.
func TestParseChordAcceptsTheSpellingsTheTablePromises(t *testing.T) {
	cases := []struct {
		written string
		want    Chord
	}{
		// Case is PRESERVED for the printable, and that is not an
		// inconsistency: a terminal sends Ctrl+Shift+K as 'K' with ModCtrl,
		// and Ctrl+K as 'k' with ModCtrl. They are two gestures a kitty
		// terminal reports separately, so merging them would lose a binding.
		// The ADR's grammar table writes "Ctrl+K | ModCtrl + the key k", which
		// conflates the two spellings; the Chord field comment's invariant —
		// "ModShift is never set when Rune is non-zero" — is the normative
		// one and it is what this implements.
		{"Ctrl+K", Chord{Rune: 'K', Mod: termmosaic.ModCtrl}},
		{"Ctrl+k", Chord{Rune: 'k', Mod: termmosaic.ModCtrl}},
		// Shift folds into the rune, so ctrl+shift+s canonicalises to Ctrl+S
		// and NOT to the ADR table's "Ctrl+Shift+S": a canonical form with
		// Shift on a printable cannot exist under the invariant.
		{"ctrl+shift+s", Chord{Rune: 'S', Mod: termmosaic.ModCtrl}},
		{"Shift+Enter", Chord{Key: termmosaic.KeyEnter, Mod: termmosaic.ModShift}},
		{"?", Chord{Rune: '?'}},
		{"F1", Chord{Key: termmosaic.KeyF1}},
		{"f12", Chord{Key: termmosaic.KeyF12}},
		{"Space", Chord{Key: termmosaic.KeySpace}},
		{" ", Chord{Key: termmosaic.KeySpace}},
		{"Esc", Chord{Key: termmosaic.KeyEscape}},
		{"Escape", Chord{Key: termmosaic.KeyEscape}},
		{"Ctrl+?", Chord{Rune: '?', Mod: termmosaic.ModCtrl}},
		{"Ctrl++", Chord{Rune: '+', Mod: termmosaic.ModCtrl}},
		{"+", Chord{Rune: '+'}},
		{"Ctrl+I", Chord{Key: termmosaic.KeyTab, Mod: termmosaic.ModCtrl}},
		{"Ctrl+M", Chord{Key: termmosaic.KeyEnter, Mod: termmosaic.ModCtrl}},
		{"Alt+F4", Chord{Key: termmosaic.KeyF4, Mod: termmosaic.ModAlt}},
		{"Super+Up", Chord{Key: termmosaic.KeyUp, Mod: termmosaic.ModSuper}},
	}
	for _, tc := range cases {
		got, err := ParseChord(tc.written)
		if err != nil {
			t.Errorf("ParseChord(%q): %v", tc.written, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseChord(%q) = %+v, want %+v", tc.written, got, tc.want)
		}
	}
}

// TestParseChordRejectsNonsense is the other half: a rebinding UI has to be
// able to say "that is not a key", because the alternative is storing a binding
// that will never fire.
func TestParseChordRejectsNonsense(t *testing.T) {
	for _, s := range []string{"", "Ctrl", "Ctrl+", "nope", "F13", "F0", "Ctrl+nope", "Ctrl++x"} {
		if got, err := ParseChord(s); err == nil {
			t.Errorf("ParseChord(%q) = %+v, want an error", s, got)
		}
	}
}

// TestParseChordFoldsShiftIntoTheRune states §3's rule 2 as a test rather than
// as prose, because it is the folding rule most likely to be "improved" away by
// someone who has not met a terminal that sends Shift+A as 'a' with a bit.
func TestParseChordFoldsShiftIntoTheRune(t *testing.T) {
	// An event that says Shift+a and an event that says 'A' are one gesture.
	lower, ok := ChordOf(termmosaic.KeyEvent('a', termmosaic.ModShift))
	if !ok {
		t.Fatal("ChordOf(Shift+a) failed")
	}
	upper, ok := ChordOf(termmosaic.KeyEvent('A', 0))
	if !ok {
		t.Fatal("ChordOf(A) failed")
	}
	if lower != upper {
		t.Errorf("Shift+a = %+v and A = %+v, want the same chord; a terminal sends one of them and the other is the same gesture", lower, upper)
	}
	if lower.Mod&termmosaic.ModShift != 0 {
		t.Errorf("Chord{%q} still carries ModShift; the invariant is that a printable rune never does", lower.Rune)
	}
	// And Ctrl IS kept, because Ctrl+a and a are genuinely distinguishable.
	ctrl, ok := ChordOf(termmosaic.KeyEvent('a', termmosaic.ModCtrl))
	if !ok {
		t.Fatal("ChordOf(Ctrl+a) failed")
	}
	if !ctrl.Mod.Has(termmosaic.ModCtrl) {
		t.Errorf("Chord{%q} lost ModCtrl; Ctrl on a printable is kept, not folded", ctrl.Rune)
	}
	if ctrl == upper {
		t.Error("Ctrl+a and a must be different chords")
	}
}

// TestChordFoldsTheSpaceBarBothWays is the workaround that made this type
// exist, named after the widget that had to write it by hand: form's
// activateKey accepted KeyEnter OR KeySpace because two terminals disagree
// about which one the space bar is.
func TestChordFoldsTheSpaceBarBothWays(t *testing.T) {
	asKey, ok := ChordOf(termmosaic.SpecialKeyEvent(termmosaic.KeySpace, 0))
	if !ok {
		t.Fatal("ChordOf(KeySpace) failed")
	}
	asRune, ok := ChordOf(termmosaic.KeyEvent(' ', 0))
	if !ok {
		t.Fatal("ChordOf(' ') failed")
	}
	if asKey != asRune {
		t.Errorf("KeySpace = %+v and Rune ' ' = %+v, want the same chord", asKey, asRune)
	}
	// A literal space in a binding string is the same chord, so a config file
	// written either way works.
	parsedSpace, err := ParseChord(" ")
	if err != nil {
		t.Fatal(err)
	}
	if parsedSpace != asKey {
		t.Errorf("ParseChord(\" \") = %+v, want %+v", parsedSpace, asKey)
	}
	// And the display form is the word, never a bare space character, because a
	// hint brackets its keys and a bracket around nothing is invisible.
	if got := asKey.String(); got != "Space" {
		t.Errorf("Chord{KeySpace}.String() = %q, want %q", got, "Space")
	}
}

// TestChordFoldsTheControlCodes is §3's rule 3, and it is inherited from the
// decoder rather than invented here: where input folds Ctrl+I to Tab, a Chord
// folds it too, so a binding cannot exist for a gesture the terminal cannot
// express.
//
// The cost is real and is recorded in ADR 0009 §Consequences: on a legacy
// encoding Ctrl+M and Enter are one chord, and a user who binds Ctrl+M on kitty
// and then runs elsewhere gets Enter. That is ADR 0005's accepted limitation
// inherited, not a new one.
func TestChordFoldsTheControlCodes(t *testing.T) {
	cases := []struct {
		event termmosaic.Event
		want  Chord
	}{
		{termmosaic.KeyEvent('i', termmosaic.ModCtrl), Chord{Key: termmosaic.KeyTab, Mod: termmosaic.ModCtrl}},
		{termmosaic.KeyEvent('m', termmosaic.ModCtrl), Chord{Key: termmosaic.KeyEnter, Mod: termmosaic.ModCtrl}},
		{termmosaic.KeyEvent('j', termmosaic.ModCtrl), Chord{Key: termmosaic.KeyEnter, Mod: termmosaic.ModCtrl}},
		{termmosaic.KeyEvent('h', termmosaic.ModCtrl), Chord{Key: termmosaic.KeyBackspace, Mod: termmosaic.ModCtrl}},
		// Upper case too, because a terminal sends Ctrl+I as 0x09 and Ctrl+M as
		// 0x0d, and the decoder's C0 path is case-blind.
		{termmosaic.KeyEvent('I', termmosaic.ModCtrl), Chord{Key: termmosaic.KeyTab, Mod: termmosaic.ModCtrl}},
		{termmosaic.KeyEvent('M', termmosaic.ModCtrl), Chord{Key: termmosaic.KeyEnter, Mod: termmosaic.ModCtrl}},
		// A control code the decoder does NOT fold stays a printable chord, or
		// Ctrl+A would silently become Alt+A or nothing at all.
		{termmosaic.KeyEvent('a', termmosaic.ModCtrl), Chord{Rune: 'a', Mod: termmosaic.ModCtrl}},
		{termmosaic.KeyEvent('z', termmosaic.ModCtrl), Chord{Rune: 'z', Mod: termmosaic.ModCtrl}},
		// A Shift-modified control code is a gesture kitty CAN express, so it
		// is not folded. Merging it would break the terminal that reports the
		// two separately.
		{termmosaic.KeyEvent('I', termmosaic.ModCtrl|termmosaic.ModShift), Chord{Rune: 'I', Mod: termmosaic.ModCtrl}},
	}
	for _, tc := range cases {
		got, ok := ChordOf(tc.event)
		if !ok {
			t.Errorf("ChordOf(%+v) failed", tc.event)
			continue
		}
		if got != tc.want {
			t.Errorf("ChordOf(%+v) = %+v, want %+v", tc.event, got, tc.want)
		}
	}
}

// TestChordOfRejectsNonKeys is the reason ChordOf is a function and not a
// method: an Event that is not a key event has no gesture, and saying so is
// better than returning the zero Chord and letting a caller bind nothing.
func TestChordOfRejectsNonKeys(t *testing.T) {
	notKeys := []termmosaic.Event{
		{},
		{Kind: termmosaic.EventResize, Size: termmosaic.ResizeEvent(80, 24).Size},
		{Kind: termmosaic.EventPaste, Text: "hello"},
		{Kind: termmosaic.EventFocus, Focused: true},
		{Kind: termmosaic.EventMouse, Mouse: termmosaic.Mouse{Action: termmosaic.MousePress}},
		// A key event with neither a key nor a rune is not a gesture. This is
		// the zero-Event case an application synthesises by mistake, and
		// binding it would make a key that never arrives a live binding.
		{Kind: termmosaic.EventKey},
	}
	for _, ev := range notKeys {
		if got, ok := ChordOf(ev); ok {
			t.Errorf("ChordOf(%+v) = %+v, true; want false", ev, got)
		}
	}
}

// TestChordStringIsStableForTheZeroChord pins the one case where display and
// parsing cannot be inverses, and says why: the zero Chord prints empty, and
// empty does not parse. That is correct — the zero Chord is not a legal
// binding — and Bind rejects it rather than storing something unfireable.
func TestChordStringIsStableForTheZeroChord(t *testing.T) {
	var c Chord
	if got := c.String(); got != "" {
		t.Errorf("the zero Chord printed %q, want %q", got, "")
	}
	if !c.IsZero() {
		t.Error("the zero Chord must report IsZero")
	}
	r := New()
	r.Register(Command{ID: "app.quit", Desc: "quit", Run: func(Ctx) bool { return true }})
	r.Bind(Binding{ID: "app.quit"})
	if got := len(r.bindings); got != 0 {
		t.Errorf("Bind stored %d bindings for a zero chord, want 0; a zero chord is not a gesture", got)
	}
}

// TestModifiersAreMatchedCaseInsensitively is the grammar's one lexical rule,
// and it is the difference between a config file that works and one that
// silently does not.
func TestModifiersAreMatchedCaseInsensitively(t *testing.T) {
	want := Chord{Rune: 'k', Mod: termmosaic.ModCtrl | termmosaic.ModAlt}
	// The MODIFIER names are case-insensitive. The trailing key is not, and
	// must not be: 'k' and 'K' are Ctrl+K and Ctrl+Shift+K, which a kitty
	// terminal reports as two separate gestures.
	for _, s := range []string{"Ctrl+Alt+k", "ctrl+alt+k", "CTRL+ALT+k", "Ctrl+ALT+k", "cTrL+aLt+k"} {
		got, err := ParseChord(s)
		if err != nil {
			t.Errorf("ParseChord(%q): %v", s, err)
			continue
		}
		if got != want {
			t.Errorf("ParseChord(%q) = %+v, want %+v", s, got, want)
		}
	}
	// Key names are case-insensitive too, for the same reason.
	for _, s := range []string{"enter", "Enter", "ENTER", "eNtEr"} {
		got, err := ParseChord(s)
		if err != nil {
			t.Errorf("ParseChord(%q): %v", s, err)
			continue
		}
		if got != (Chord{Key: termmosaic.KeyEnter}) {
			t.Errorf("ParseChord(%q) = %+v, want Enter", s, got)
		}
	}
}

// TestScopeNamesItself is the discoverability half of a three-value enum: help
// and diagnostics both print a scope, and an unnamed one would print a number.
func TestScopeNamesItself(t *testing.T) {
	for s, want := range map[Scope]string{
		ScopeGlobal: "global",
		ScopeScreen: "screen",
		ScopeFocus:  "focus",
		Scope(99):   "unknown",
	} {
		if got := s.String(); got != want {
			t.Errorf("Scope(%d).String() = %q, want %q", int(s), got, want)
		}
	}
	// The zero value is ScopeGlobal, which is what makes a bare
	// Binding{Chord: c, ID: "x"} mean what it looks like.
	var zero Scope
	if zero != ScopeGlobal {
		t.Errorf("the zero Scope is %v, want ScopeGlobal; a Binding with no Scope set must mean global", zero)
	}
}
