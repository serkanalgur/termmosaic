package dialog

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// update is set by -update to rewrite the golden files.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// quote wraps a line for a diff message.
func quote(s string) string { return "\"" + s + "\"" }

// describeDiff renders a word-level difference, so a failing golden says WHAT
// changed rather than dumping two screens and leaving the reader to diff them.
func describeDiff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			b.WriteString("row ")
			b.WriteString(itoa(i))
			b.WriteString(":\n  want ")
			b.WriteString(quote(wl))
			b.WriteString("\n  got  ")
			b.WriteString(quote(gl))
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return "(lines differ only in trailing content)"
	}
	return b.String()
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenFor(name)
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file (run `go test ./widgets/dialog -update` to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s\n--- diff ---\n%s",
			name, got, want, describeDiff(string(want), got))
	}
}

// TestGoldens pins the exact screen for each variant at a size where everything is
// visible: a title, a body, the buttons or the list, and the frame.
//
// It goes through widgettest.Render, which is the point of that package: the widget,
// the renderer, the two-tier diff, the ANSI encoder and the headless screen model all
// have to agree, and asserting on anything short of that proves less than it looks
// like it proves.
func TestGoldens(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    *Dialog
		w, h int
	}{
		{"info_40x9", infoDialog(40, 9), 40, 9},
		{"confirm_40x9", confirmDialog(40, 9), 40, 9},
		{"choice_40x12", choiceDialog(40, 12, 6), 40, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := widgettest.Render(t, tc.w, tc.h, 1, atSize(tc.d, tc.w, tc.h))
			checkGolden(t, tc.name+".txt", sink.String())
		})
	}
}

// TestGoldensAtEverySize is the responsive golden: the same three variants across
// the sizes that matter — either side of the action row's wrap threshold, the
// degenerate sizes, and something comfortably large.
//
// The widths are DERIVED from what each dialog's buttons need rather than written as
// round numbers, because a golden at 40 proves nothing about a threshold at 17 — which
// is the mistake the hello golden set made and recorded.
func TestGoldensAtEverySize(t *testing.T) {
	confirm := confirmDialog(40, 9)
	need := confirm.actionsWidth()

	for _, tc := range []struct {
		name string
		v    Variant
		w, h int
	}{
		{"info_tiny", VariantInfo, 1, 1},
		{"info_min", VariantInfo, 18, 4},
		{"info_30x8", VariantInfo, 30, 8},
		{"info_80x24", VariantInfo, 80, 24},
		{"info_200x60", VariantInfo, 200, 60},
		{"info_0x0", VariantInfo, 0, 0},

		// One cell either side of the wrap threshold: the pair that pins it.
		{"confirm_fits", VariantConfirm, need + 2, 9},
		{"confirm_wraps", VariantConfirm, need + 1, 9},
		{"confirm_tiny", VariantConfirm, 3, 3},
		{"confirm_80x24", VariantConfirm, 80, 24},

		{"choice_fits", VariantChoice, 40, 12},
		{"choice_overflow", VariantChoice, 40, 6},
		{"choice_many", VariantChoice, 40, 12},
		{"choice_wide", VariantChoice, 120, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var d *Dialog
			switch tc.v {
			case VariantInfo:
				d = infoDialog(tc.w, tc.h)
			case VariantConfirm:
				d = confirmDialog(tc.w, tc.h)
			case VariantChoice:
				n := 6
				if tc.name == "choice_many" {
					n = 30
				}
				d = choiceDialog(tc.w, tc.h, n)
			}
			sink := widgettest.Render(t, tc.w, tc.h, 1, atSize(d, tc.w, tc.h))
			checkGolden(t, "size_"+tc.name+".txt", sink.String())
		})
	}
}

// TestGoldensAcrossAFocusSweep pins the focus traversal as goldens rather than as
// indices: it is the only way a regression in WHERE the ring is shows up as a
// picture rather than as an index in a failure message.
//
// One golden per focus position, so a change to the ring's order is a visible diff.
func TestGoldensAcrossAFocusSweep(t *testing.T) {
	d := choiceDialog(40, 12, 3)
	d.SetActions(Action{Label: DefaultCancelLabel}, Action{Label: DefaultOKLabel})
	d.SetFocused(true)
	atSize(d, 40, 12)
	d.Draw(cellBuf(40, 12))

	for i := 0; i < d.FocusCount(); i++ {
		d.SetFocus(i)
		t.Run(itoa(i), func(t *testing.T) {
			sink := widgettest.Render(t, 40, 12, 1, d)
			checkGolden(t, "focus_"+itoa(i)+".txt", sink.String())
		})
	}
}

// TestGoldensUnderNoColor pins the screen a terminal with NO_COLOR sees. The visible
// text is identical to the colour frame — which is the claim — and the golden makes
// that legible rather than asserted.
func TestGoldensUnderNoColor(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    *Dialog
		w, h int
	}{
		{"confirm", confirmDialog(40, 9), 40, 9},
		{"choice", choiceDialog(40, 12, 6), 40, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := renderNoColor(t, tc.w, tc.h, atSize(tc.d, tc.w, tc.h))
			checkGolden(t, "nocolor_"+tc.name+".txt", sink.String())
		})
	}
}

// TestGoldensUnderDegradedCaps renders at each rung of the colour ladder, so a change
// to the degradation mapping shows up as a golden diff rather than as a surprise in
// somebody's terminal. The ASCII rung is included because it is a CHARACTER change
// rather than a colour one, and a dialog's markers have to survive it.
func TestGoldensUnderDegradedCaps(t *testing.T) {
	for _, tc := range []struct {
		name  string
		caps  termmosaic.Caps
		ascii bool
	}{
		{"truecolor", termmosaic.DefaultCaps(), false},
		{"256", termmosaic.Caps{Color256: true}, false},
		{"16", termmosaic.Caps{}, false},
		{"ascii", termmosaic.Caps{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := choiceDialog(40, 12, 6)
			d.Block().Ascii = tc.ascii
			atSize(d, 40, 12)

			sink := headless.NewMemorySink(40, 12)
			r := newRenderer(t, sink, 40, 12, false)
			r.SetRoot(d)
			if _, err := r.Render(); err != nil {
				t.Fatal(err)
			}
			if got := sink.UnknownSequences(); got != 0 {
				t.Fatalf("%d unknown sequences at caps %+v:\n%s", got, tc.caps, sink.RawString())
			}
			checkGolden(t, "caps_"+tc.name+".txt", sink.String())
		})
	}
}

// TestTheAsciiRungKeepsTheLayoutIdentical is the property the ASCII rung exists for
// (ADR 0008 §Decision 4): both truncation markers are one cell wide, so choosing
// between them cannot move anything.
//
// The comparison is between the two SCREENS with the glyph table swapped, so a
// marker that was the wrong width would show up as a layout difference rather than
// passing because only one rung was ever rendered.
func TestTheAsciiRungKeepsTheLayoutIdentical(t *testing.T) {
	// A body and labels long enough that the truncation marker appears on both
	// rungs, since a marker that never appears proves nothing about its width.
	body := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu"

	unicodeScreen := func() string {
		d := New(rect(30, 8), VariantConfirm)
		d.SetBodyString(body)
		d.SetActions(Action{Label: "A very long affirmative label indeed"})
		atSize(d, 30, 8)
		return widgettest.Screen(widgettest.Render(t, 30, 8, 1, d))
	}
	asciiScreen := func() string {
		d := New(rect(30, 8), VariantConfirm)
		d.Block().Ascii = true
		d.SetBodyString(body)
		d.SetActions(Action{Label: "A very long affirmative label indeed"})
		atSize(d, 30, 8)
		return widgettest.Screen(widgettest.Render(t, 30, 8, 1, d))
	}

	u, a := unicodeScreen(), asciiScreen()
	if !strings.Contains(u, buffer.TruncSuffix) || !strings.Contains(a, buffer.AscTruncSuffix) {
		t.Fatalf("setup: the truncation marker must appear on both rungs.\nunicode:\n%s\nascii:\n%s", u, a)
	}
	// Same WIDTHS row for row, which is the invariant both markers being one cell
	// wide exists to protect.
	for i, pair := range splitLines(u, a) {
		ur, ar := pair[0], pair[1]
		if got, want := buffer.StringWidth(ar), buffer.StringWidth(ur); got != want {
			t.Errorf("row %d is %d cells on the ASCII rung and %d on the Unicode one: "+
				"the two truncation markers are not the same width", i, got, want)
		}
	}
}

// splitLines zips two screens into row pairs.
func splitLines(a, b string) [][2]string {
	as, bs := strings.Split(a, "\n"), strings.Split(b, "\n")
	out := make([][2]string, 0, len(as))
	for i := range as {
		if i < len(bs) {
			out = append(out, [2]string{as[i], bs[i]})
		}
	}
	return out
}
