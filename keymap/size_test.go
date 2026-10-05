package keymap

import (
	"os"
	"strings"
	"testing"
	"unsafe"

	"github.com/serkanalgur/termmosaic"
)

// TestChordIsSixteenBytes is the guard ADR 0009 §1 asks for, in the same spirit
// and for the same reason as ADR 0002's TestCellHasNoPadding and ADR 0005's
// TestEventSizeIsBounded.
//
// The property that makes Chord work is COMPARABILITY, not size: it is a map
// key, so resolution is one lookup. The size is pinned anyway, because a Chord
// is built on the hot path once per keystroke and the natural way to make it
// slower later is to add a field — a string label, a []string aliases slice —
// that no allocation test would catch, since a string field is still a
// comparable struct.
//
// Key is an int, Rune an int32 and Mod a uint8: 8 + 4 + 1 = 13, padded to the
// struct's 8-byte alignment, which is 16. KeyF13+ lands at the end of the iota
// block and does not change this.
func TestChordIsSixteenBytes(t *testing.T) {
	const want = 16
	if got := unsafe.Sizeof(Chord{}); got != want {
		t.Fatalf("sizeof(Chord) = %d, want %d; a Chord is built once per keystroke and is a map key on the dispatch path (ADR 0009)", got, want)
	}
}

// TestCtxIsOneHundredFiftyTwoBytes pins the size of the value passed to every
// command handler.
//
// It is NOT the 128 bytes ADR 0009 §1's prose claims, and the discrepancy is
// deliberate rather than an implementation slip. The ADR's own field list lays
// out at 112 (Event, pinned by ADR 0005) + 16 (Chord, pinned above) + 16 (a
// Widget interface is two words) + 1 (Synthesised) = 145, which rounds up to
// the struct's 8-byte alignment: 152.
//
// 128 is reachable only by dropping a field, and every candidate is a real one:
// 112 + 16 is exactly 128, which is Event and Chord and nothing else — no
// focus, no Synthesised. A command handler that cannot see what had focus
// cannot act on the focused widget, which is the most common thing a command
// does. So the field list is the specification and the prose figure is the
// error, and this test records what the compiler does so the next reader does
// not have to re-derive it.
//
// The zero-allocation property is unaffected and is what actually matters: Ctx
// is passed BY VALUE, so it is a stack copy and not a heap object per
// keystroke. TestDispatchIsZeroAllocation is the test that proves it.
func TestCtxIsOneHundredFiftyTwoBytes(t *testing.T) {
	const want = 152
	if got := unsafe.Sizeof(Ctx{}); got != want {
		t.Fatalf("sizeof(Ctx) = %d, want %d; Ctx is passed by value to every command handler, so its size is a deliberate decision and not an accident (ADR 0009)", got, want)
	}
}

// TestChordIsUsableAsAMapKey states the property resolution actually depends
// on, as code rather than as a comment in a doc block. A Chord that stopped
// being comparable would not fail to compile here — it would fail to compile
// the Registry, which is a worse place to discover it.
func TestChordIsUsableAsAMapKey(t *testing.T) {
	m := map[Chord]int{
		{Key: termmosaic.KeyEnter}: 1,
		{Rune: 'q'}:                2,
		{Key: termmosaic.KeyF1, Mod: termmosaic.ModCtrl | termmosaic.ModShift}: 3,
		// Two Chords that differ only in a modifier bit are different map
		// entries, which is the whole reason Mod is a field and not a bool.
		{Rune: 'q', Mod: termmosaic.ModCtrl}: 4,
	}
	if len(m) != 4 {
		t.Fatalf("map has %d entries, want 4; a Chord that collided with another would make two bindings unreachable", len(m))
	}
	if got := m[Chord{Rune: 'q', Mod: termmosaic.ModCtrl}]; got != 4 {
		t.Errorf("lookup of Ctrl+q = %d, want 4", got)
	}
	if _, ok := m[Chord{Rune: 'Q', Mod: termmosaic.ModCtrl}]; ok {
		t.Error("Ctrl+Q must not collide with Ctrl+q; a terminal cannot send one without the other")
	}
	// A zero Chord is not a legal binding, and its being a valid map key is
	// precisely why Registry.Bind has to reject it explicitly.
	var zero Chord
	if !zero.IsZero() {
		t.Error("the zero Chord must report IsZero")
	}
	if _, ok := m[zero]; ok {
		t.Error("the zero Chord must not be present in a table of legal chords")
	}
}

// TestEventIsUnchangedByThisPackage is the shape of "the keymap is above the
// tree, not inside it": nothing here touched Event, and the root package's own
// guard still holds. It is duplicated here because the failure it guards
// against is an edit to event.go made in the name of this package, and the
// guard that would catch it lives in a different package's test.
func TestEventIsUnchangedByThisPackage(t *testing.T) {
	const want = 112
	if got := unsafe.Sizeof(termmosaic.Event{}); got != want {
		t.Fatalf("sizeof(Event) = %d, want %d; keymap derives a Chord FROM an Event and never adds a payload to one (ADR 0005 §8, ADR 0009)", got, want)
	}
}

// TestKeymapImportsNothingButTermmosaic pins the import direction ADR 0009 §1
// calls forced and safe: keymap may hold Widget and Event values, and the root
// package may not hold keymap values.
//
// A keymap that imported, say, widgets/form would close a cycle the moment form
// gained SetEntries, and a cycle found by the compiler is a much worse
// experience than one found by a test that says why the rule exists.
func TestKeymapImportsNothingButTermmosaic(t *testing.T) {
	allowed := map[string]bool{
		// The whole of what a Chord, a Ctx and a Registry need. There is no
		// third entry because there has never been a reason for one.
		"github.com/serkanalgur/termmosaic":        true,
		"github.com/serkanalgur/termmosaic/buffer": true,
	}
	for _, imp := range keymapImports(t) {
		if !allowed[imp] {
			t.Errorf("keymap imports %s; keymap may import termmosaic and buffer and nothing else, so that termmosaic never has to import it", imp)
		}
	}
}

// keymapImports returns the module-internal import paths of every non-test .go
// file in this directory.
//
// It reads the source rather than asking `go list`, so the check runs with no
// toolchain, no network and no module cache — a test about the import graph
// that needs the import graph resolved first would be a poor place to be
// strict.
func keymapImports(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the keymap directory: %v", err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		out = append(out, moduleImports(string(src))...)
	}
	return out
}

// moduleImports returns the github.com/serkanalgur/termmosaic paths in one
// file's import block, dropping stdlib and the package's own name.
func moduleImports(src string) []string {
	const self = "github.com/serkanalgur/termmosaic"
	block := src
	if i := strings.Index(block, "import ("); i >= 0 {
		block = block[i+len("import ("):]
		if j := strings.Index(block, ")"); j >= 0 {
			block = block[:j]
		}
	} else if i := strings.Index(block, "import "); i >= 0 {
		block = block[i+len("import "):]
		if j := strings.IndexAny(block, "\n;"); j >= 0 {
			block = block[:j]
		}
	} else {
		return nil
	}
	var out []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		p := strings.Trim(line, `"`)
		if p == self || !strings.HasPrefix(p, self) {
			continue
		}
		if strings.HasPrefix(p, self+"/") {
			out = append(out, p)
		}
	}
	return out
}
