package menu

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// update rewrites the golden files. It is set with `go test ./widgets/menu -update`,
// exactly as examples/markets does, so regenerating a golden is a visible act
// rather than something a failing test does behind your back.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// The sizes the goldens capture, and WHY each one is here.
//
// Five, because five DIFFERENT LAYOUTS is the point. A golden at one size is a
// snapshot; a golden at five sizes is a specification of how the widget gives up
// content as space runs out, and a change to any threshold shows up as a diff in
// exactly the file that owns that threshold.
var (
	// goldenRoot is the root level with its keybinding hints and its toggle: the
	// commonest menu there is.
	goldenRootW, goldenRootH = 48, 8
	// goldenTwoLevel has a submenu open, so both the two-column layout and the
	// active-level bracket are in the file.
	goldenTwoLevelW, goldenTwoLevelH = 72, 10
	// goldenNarrow is below the width at which the hint column survives, so the
	// priority order — label before hint — is visible.
	goldenNarrowW, goldenNarrowH = 22, 8
	// goldenShort has room for a header and two items, so the row budget is what is
	// being captured rather than the item count.
	goldenShortW, goldenShortH = 30, 3
	// goldenTiny is below MinSize, where the clipped minimum layout must still show
	// a marker and a label.
	goldenTinyW, goldenTinyH = 6, 3
)

// TestMenuGoldenLayouts is the golden suite. Every frame goes through
// widgettest.Render, so what is asserted is what the terminal would show rather than
// what a buffer holds.
func TestMenuGoldenLayouts(t *testing.T) {
	cases := []struct {
		name  string
		w, h  int
		build func(t *testing.T) *Menu
	}{
		{
			name: "root", w: goldenRootW, h: goldenRootH,
			build: func(t *testing.T) *Menu { return newMenu(t, goldenRootW, goldenRootH, tree()...) },
		},
		{
			name: "two_level", w: goldenTwoLevelW, h: goldenTwoLevelH,
			build: func(t *testing.T) *Menu {
				m := newMenu(t, goldenTwoLevelW, goldenTwoLevelH, tree()...)
				press(t, m, "\x1b[C")
				return m
			},
		},
		{
			name: "narrow", w: goldenNarrowW, h: goldenNarrowH,
			build: func(t *testing.T) *Menu { return newMenu(t, goldenNarrowW, goldenNarrowH, tree()...) },
		},
		{
			name: "short", w: goldenShortW, h: goldenShortH,
			build: func(t *testing.T) *Menu { return newMenu(t, goldenShortW, goldenShortH, tree()...) },
		},
		{
			name: "tiny", w: goldenTinyW, h: goldenTinyH,
			build: func(t *testing.T) *Menu { return newMenu(t, goldenTinyW, goldenTinyH, tree()...) },
		},
		{
			name: "three_level_narrow", w: 40, h: 9,
			build: func(t *testing.T) *Menu {
				m := newMenu(t, 40, 9, tree()...)
				press(t, m, "\x1b[C")
				press(t, m, "\x1b[C")
				return m
			},
		},
		{
			name: "closed", w: goldenRootW, h: goldenRootH,
			build: func(t *testing.T) *Menu {
				m := newMenu(t, goldenRootW, goldenRootH, tree()...)
				m.Close()
				return m
			},
		},
		{
			name: "ascii", w: goldenRootW, h: goldenRootH,
			build: func(t *testing.T) *Menu {
				m := newMenu(t, goldenRootW, goldenRootH, tree()...)
				m.Ascii = true
				m.Block().Ascii = true
				return m
			},
		},
		{
			name: "selected_below", w: goldenRootW, h: goldenRootH,
			build: func(t *testing.T) *Menu {
				// The selection BELOW the fold, so the scroll offset and the marker
				// are captured together.
				m := newMenu(t, goldenRootW, goldenRootH, tree()...)
				press(t, m, "\x1b[F")
				return m
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.build(t)
			sink := widgettest.Render(t, tc.w, tc.h, 1, m)
			got := widgettest.Screen(sink)
			// The update path is taken BEFORE the read, so recording a golden does
			// not require the file to exist already — which is the whole point of
			// `-update`, and the reason a first run reports nothing but a log line.
			if *update {
				writeGolden(t, tc.name, got)
				return
			}
			want := readGolden(t, tc.name)
			if got != want {
				t.Errorf("golden %q mismatch\n--- got ---\n%s\n--- want ---\n%s\n%s",
					tc.name, got, want, goldenDiff(got, want))
			}
			if sink.UnknownSequences() != 0 {
				t.Errorf("golden %q: the headless screen saw %d unrecognised sequences, so the frame "+
					"it captured is a picture of something unsound", tc.name, sink.UnknownSequences())
			}
		})
	}
}

// TestMenuGoldenIsStableAcrossRenders is the guard on the guard: a golden suite
// whose files churn between runs is worse than none, because a developer learns to
// re-record without reading. Two consecutive renders of the same menu must produce
// identical bytes.
func TestMenuGoldenIsStableAcrossRenders(t *testing.T) {
	m := newMenu(t, goldenTwoLevelW, goldenTwoLevelH, tree()...)
	press(t, m, "\x1b[C")
	first := widgettest.Render(t, goldenTwoLevelW, goldenTwoLevelH, 2, m)
	second := widgettest.Render(t, goldenTwoLevelW, goldenTwoLevelH, 2, m)
	if first.String() != second.String() {
		t.Errorf("two identical menus rendered different bytes:\n--- one ---\n%s\n--- two ---\n%s",
			first.String(), second.String())
	}
}

// goldenDir is where the golden files live.
const goldenDir = "testdata"

// goldenPath returns the file a golden case reads and writes.
func goldenPath(name string) string {
	return filepath.Join(goldenDir, "menu_"+name+".txt")
}

// readGolden returns the golden file for name, failing with the command to create it.
func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("reading golden %q: %v (run `go test ./widgets/menu -update` to create it)", name, err)
	}
	return string(b)
}

// writeGolden records got as the golden for name.
func writeGolden(t *testing.T, name, got string) {
	t.Helper()
	if err := os.WriteFile(goldenPath(name), []byte(got), 0o644); err != nil {
		t.Fatalf("writing golden %q: %v", name, err)
	}
	t.Logf("recorded golden %q", name)
}

// goldenDiff renders a row-by-row difference, so a failing golden names the row that
// changed rather than leaving the reader to diff two blocks by eye.
func goldenDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	var b strings.Builder
	b.WriteString("--- rows that differ ---\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gr, wr string
		if i < len(g) {
			gr = g[i]
		}
		if i < len(w) {
			wr = w[i]
		}
		if gr == wr {
			continue
		}
		b.WriteString("row " + itoa(i) + "\n")
		b.WriteString("  got  " + quoted(gr) + "\n")
		b.WriteString("  want " + quoted(wr) + "\n")
	}
	return b.String()
}

// quoted renders s for a golden diff with every space shown as a middle dot, so a
// difference in trailing blanks is visible in the message instead of being trimmed
// by the reader's terminal.
func quoted(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == ' ' {
			b.WriteString("·")
			continue
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
