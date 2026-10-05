package keymap

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/serkanalgur/termmosaic"
)

// Chord is one user-input gesture, normalised: exactly one key, plus exactly
// the modifiers that were held.
//
// It is comparable, which is the load-bearing property: a Chord is a map key,
// so resolution is one map lookup and zero allocations. It is also exactly 16
// bytes, and TestChordIsSixteenBytes pins that the way TestCellHasNoPadding
// pins ADR 0002's Cell — because a struct on the hot path that nobody measures
// is how a hot path becomes slow.
//
// NORMALISATION, and this is the whole reason Chord exists:
//
//   - Space is ONE chord. KeySpace and Rune ' ' are the same gesture, because
//     terminals disagree about which they send and form.activateKey already
//     works around this; the Chord type makes the workaround the default.
//   - Shift on a printable rune is FOLDED INTO THE RUNE. A terminal sends
//     Shift+A as 'A', never as 'a' with a shift bit, so Chord{'A'} and
//     Chord{'a', ModShift} would be two chords for one gesture.
//   - Ctrl on a printable rune is KEPT, because 'a' with Ctrl is genuinely
//     distinguishable from 'a'.
//   - Ctrl+I, Ctrl+M, Ctrl+J and Ctrl+H are folded to Tab, Enter, Enter and
//     Backspace where the decoder folds them, so a chord the terminal cannot
//     distinguish is not two chords either. The consequence — that Ctrl+M and
//     Enter are the same chord on a legacy encoding, and different ones under
//     kitty disambiguation — is recorded in §Consequences rather than papered
//     over.
//
// A Shift-modified printable rune is NOT re-folded by the Ctrl rules above, so
// Ctrl+Shift+I stays Ctrl+Shift+I rather than becoming Ctrl+Tab. That is
// deliberate and it is the one place where a decoder that CAN distinguish the
// two gestures must not be second-guessed: kitty reports them separately and
// the merge is exactly the "a binding that documents a key the terminal cannot
// express" failure this type exists to remove.
type Chord struct {
	// Key is the non-printable key, or KeyNone when Rune carries the gesture.
	Key termmosaic.Key
	// Rune is the printable character, and is 0 whenever Key is not KeyNone.
	Rune rune
	// Mod is the normalised modifier set. ModShift is never set when Rune is
	// non-zero.
	Mod termmosaic.KeyMod
}

// IsZero reports whether c is the zero Chord, which is not a legal binding and
// is what a click, a palette activation or a direct Invoke carries.
func (c Chord) IsZero() bool { return c == Chord{} }

// String returns the canonical display form: "Ctrl+K", "Shift+Enter", "F1",
// "?", "Space" (never a bare space character, which is why the hint brackets
// its keys).
//
// Display is deliberately the SAME function as parsing, modulo case: what help
// prints is what ParseChord accepts. A help screen that documents a key nobody
// can bind is worse than one that documents fewer keys, and the way to make
// that impossible is for there to be one function, not two that agree.
func (c Chord) String() string {
	if c.IsZero() {
		return ""
	}
	var b strings.Builder
	// The fixed order is the grammar's: Ctrl, Alt, Shift, Super.
	for _, m := range []struct {
		bit  termmosaic.KeyMod
		name string
	}{
		{termmosaic.ModCtrl, "Ctrl"},
		{termmosaic.ModAlt, "Alt"},
		{termmosaic.ModShift, "Shift"},
		{termmosaic.ModSuper, "Super"},
	} {
		if c.Mod&m.bit != 0 {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	if c.Rune != 0 {
		b.WriteRune(c.Rune)
	} else {
		b.WriteString(keyDisplay[c.Key])
	}
	return b.String()
}

// ParseChord parses the canonical display form back into a Chord, applying the
// same normalisation as ChordOf. It is the inverse of Chord.String() for every
// chord that can be produced by a terminal, and TestParseChordRoundTrips pins
// that over a generated table.
//
// It is the API a REBINDING UI uses: the user types a key, the application
// parses it, and if it fails the application says so rather than storing a
// binding that will never fire.
func ParseChord(s string) (Chord, error) {
	if s == "" {
		return Chord{}, fmt.Errorf("keymap: empty chord")
	}

	// Modifiers are matched case-insensitively and peeled from the left, so
	// "ctrl+shift+s" and "Ctrl+Shift+S" are the same binding. Peeling rather
	// than splitting on the last '+' is what makes "Ctrl++" — Ctrl and the
	// plus key — parse at all.
	rest := s
	var mod termmosaic.KeyMod
	for {
		lower := strings.ToLower(rest)
		matched := false
		for _, m := range modNames {
			if len(rest) > len(m.name) && lower[:len(m.name)] == m.name && rest[len(m.name)] == '+' {
				mod |= m.bit
				rest = rest[len(m.name)+1:]
				matched = true
				break
			}
		}
		if !matched {
			break
		}
	}

	switch rest {
	case " ":
		// A literal space is the space bar, the same chord as "Space".
		return parsed(normalise(termmosaic.KeySpace, 0, mod))
	case "+":
		return parsed(normalise(termmosaic.KeyNone, '+', mod))
	}

	if k, ok := keyByName(rest); ok {
		return parsed(normalise(k, 0, mod))
	}
	if r, n := firstRune(rest); n == 1 {
		return parsed(normalise(termmosaic.KeyNone, r, mod))
	}
	return Chord{}, fmt.Errorf("keymap: %q is not a chord", s)
}

// ChordOf converts a decoded key event into its Chord, applying §3's
// normalisation. It returns false for any event that is not an EventKey, and
// for a key event that normalises to nothing.
//
// This is the one place in TermMosaic where an Event becomes a gesture, and it
// is exported so that a widget keeping its own switch can ask "is this the key I
// care about?" without re-deriving the folding rules.
func ChordOf(ev termmosaic.Event) (Chord, bool) {
	if ev.Kind != termmosaic.EventKey {
		return Chord{}, false
	}
	return normalise(ev.Key, ev.Rune, ev.Mod)
}

// parsed turns normalise's (Chord, bool) into ParseChord's (Chord, error),
// reporting the input string so a rebinding UI can say which key it could not
// read. normalise only fails on a gesture that carries no key at all, which for
// a non-empty string means the user typed something that normalised away.
func parsed(c Chord, ok bool) (Chord, error) {
	if !ok {
		return Chord{}, fmt.Errorf("keymap: chord carries no key")
	}
	return c, nil
}

// normalise is the single folding step both ParseChord and ChordOf apply. It is
// one function rather than two because the ADR's reason for having a Chord type
// at all is that two Event values mean one gesture, and a second copy of these
// rules is how form.activateKey came to exist.
func normalise(key termmosaic.Key, r rune, mod termmosaic.KeyMod) (Chord, bool) {
	// Space is one chord whichever way the terminal spells it.
	if r == ' ' {
		key, r = termmosaic.KeySpace, 0
	}
	if r != 0 {
		// Shift is folded into the rune, so it is never a separate bit here and
		// the display form never says "Shift+a".
		shifted := mod&termmosaic.ModShift != 0
		if shifted {
			r = unicode.ToUpper(r)
			mod &^= termmosaic.ModShift
		}
		// Ctrl on a printable is kept, but the four control codes the legacy
		// encoding cannot distinguish are folded to the key they stand for. A
		// Shift-modified chord is not folded, because under kitty
		// disambiguation it is a gesture the terminal CAN express.
		if !shifted && mod&termmosaic.ModCtrl != 0 {
			switch unicode.ToLower(r) {
			case 'i':
				key, r = termmosaic.KeyTab, 0
			case 'j', 'm':
				key, r = termmosaic.KeyEnter, 0
			case 'h':
				key, r = termmosaic.KeyBackspace, 0
			}
		}
	}
	if key == termmosaic.KeyNone && r == 0 {
		return Chord{}, false
	}
	return Chord{Key: key, Rune: r, Mod: mod}, true
}

// ---------------------------------------------------------------------------
// The one key-name table
// ---------------------------------------------------------------------------

// keyDisplay is the single table mapping a Key to its display form. It is the
// only place a key is named, which is the property §3 wants: two hand-written
// tables drift, and drift here is a key that is documented and does not work.
//
// termmosaic.Key.String() is the lower-case authority for the NAMES; this table
// is the capitalised DISPLAY form of the same names, and ParseChord reads
// through it rather than through a second copy. TestKeyNameTableCoversTheEnum
// fails by itself when KeyF13 is appended to the iota block, which is how ADR
// 0005 §10's append-only rule stays enforceable from this side.
var keyDisplay = map[termmosaic.Key]string{
	termmosaic.KeyEnter:     "Enter",
	termmosaic.KeyTab:       "Tab",
	termmosaic.KeyBacktab:   "Backtab",
	termmosaic.KeyBackspace: "Backspace",
	termmosaic.KeyEscape:    "Esc",
	termmosaic.KeySpace:     "Space",
	termmosaic.KeyUp:        "Up",
	termmosaic.KeyDown:      "Down",
	termmosaic.KeyLeft:      "Left",
	termmosaic.KeyRight:     "Right",
	termmosaic.KeyHome:      "Home",
	termmosaic.KeyEnd:       "End",
	termmosaic.KeyPageUp:    "PgUp",
	termmosaic.KeyPageDown:  "PgDn",
	termmosaic.KeyDelete:    "Del",
	termmosaic.KeyInsert:    "Ins",
	termmosaic.KeyF1:        "F1",
	termmosaic.KeyF2:        "F2",
	termmosaic.KeyF3:        "F3",
	termmosaic.KeyF4:        "F4",
	termmosaic.KeyF5:        "F5",
	termmosaic.KeyF6:        "F6",
	termmosaic.KeyF7:        "F7",
	termmosaic.KeyF8:        "F8",
	termmosaic.KeyF9:        "F9",
	termmosaic.KeyF10:       "F10",
	termmosaic.KeyF11:       "F11",
	termmosaic.KeyF12:       "F12",
}

// keyAliases are additional spellings ParseChord accepts, mapped to the same
// Key. They are parse-only: String never emits one, so a binding written
// "Escape" is displayed as "Esc" and help shows one spelling, not two.
var keyAliases = map[string]termmosaic.Key{
	"escape":   termmosaic.KeyEscape,
	"return":   termmosaic.KeyEnter,
	"pageup":   termmosaic.KeyPageUp,
	"pagedown": termmosaic.KeyPageDown,
	"delete":   termmosaic.KeyDelete,
	"insert":   termmosaic.KeyInsert,
	"del":      termmosaic.KeyDelete,
	"ins":      termmosaic.KeyInsert,
	"esc":      termmosaic.KeyEscape,
	"space":    termmosaic.KeySpace,
}

// keyByIndex is the reverse of keyDisplay, built once so ParseChord does not
// build a string per call and so there is still exactly one table.
var keyByIndex = func() map[string]termmosaic.Key {
	m := make(map[string]termmosaic.Key, len(keyDisplay)+len(keyAliases))
	for k, name := range keyDisplay {
		m[strings.ToLower(name)] = k
	}
	for name, k := range keyAliases {
		m[name] = k
	}
	return m
}()

// keyByName resolves a key name case-insensitively through the one table.
func keyByName(s string) (termmosaic.Key, bool) {
	k, ok := keyByIndex[strings.ToLower(s)]
	return k, ok
}

// modNames is the fixed modifier order of the grammar, shared by String and
// ParseChord so the two cannot disagree about what a modifier is called.
var modNames = []struct {
	bit  termmosaic.KeyMod
	name string
}{
	{termmosaic.ModCtrl, "ctrl"},
	{termmosaic.ModAlt, "alt"},
	{termmosaic.ModShift, "shift"},
	{termmosaic.ModSuper, "super"},
}

// firstRune returns s's first rune and the number of runes in s, so ParseChord
// can tell a one-rune key from a name it did not recognise.
func firstRune(s string) (rune, int) {
	for _, r := range s {
		return r, len([]rune(s))
	}
	return 0, 0
}
