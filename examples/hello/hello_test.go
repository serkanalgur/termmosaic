package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/render"
)

// update is set by -update to rewrite the golden files.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// goldenScreen is the screen the golden files describe.
const (
	goldenW = 60
	goldenH = 14
)

// renderGolden renders n frames of the example's block into a MemorySink and
// returns the sink.
//
// It exercises the whole stack the way a real application does: the renderer,
// the two-tier diff, the ANSI encoder, and the headless screen model that turns
// the emitted bytes back into cells.
func renderGolden(t *testing.T, frames int) *headless.MemorySink {
	t.Helper()
	sink := headless.NewMemorySink(goldenW, goldenH)
	r := render.New(sink, render.Config{
		Width:  goldenW,
		Height: goldenH,
		Caps:   termmosaic.DefaultCaps(),
	})
	root := &hello{
		depth:  buffer.DepthTrueColor,
		bounds: centred(goldenW, goldenH, blockW, blockH),
	}
	r.SetRoot(root)
	for i := 0; i < frames; i++ {
		if _, err := r.Render(); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	if got := sink.UnknownSequences(); got != 0 {
		t.Fatalf("the headless screen saw %d unrecognised sequences; assertions about it are unsound.\n%s",
			got, sink.RawString())
	}
	return sink
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file (run `go test ./examples/hello -update` to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s\n--- diff in words ---\n%s",
			name, got, want, describeDiff(string(want), got))
	}
}

// describeDiff renders a minimal word-level difference, so a failing golden
// test says what changed rather than only dumping two screens.
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
			fmt.Fprintf(&b, "row %d:\n  want %q\n  got  %q\n", i, wl, gl)
		}
	}
	if b.Len() == 0 {
		return "(lines differ only in trailing content)"
	}
	return b.String()
}

// TestGoldenScreen is the regression net for the entire foundation: the buffer
// primitives, the layout solver's output, the two-tier diff, the ANSI encoder and
// the renderer all have to agree to produce exactly this screen.
func TestGoldenScreen(t *testing.T) {
	sink := renderGolden(t, 1)
	checkGolden(t, "hello.txt", sink.String())
}

// TestGoldenStream pins the exact bytes, which catches encoder regressions that
// leave the visible screen unchanged: a redundant SGR, a missing default colour,
// a cursor move that was not needed.
func TestGoldenStream(t *testing.T) {
	sink := renderGolden(t, 1)
	checkGolden(t, "hello.sgr", sink.RawString())
}

// TestSecondFrameChangesOnlyTheCounter is the ADR 0002 property stated as a test
// at the application level: static chrome must cost nothing between frames.
func TestSecondFrameChangesOnlyTheCounter(t *testing.T) {
	sink := headless.NewMemorySink(goldenW, goldenH)
	r := render.New(sink, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(),
	})
	r.SetRoot(&hello{depth: buffer.DepthTrueColor, bounds: centred(goldenW, goldenH, blockW, blockH)})

	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	first := sink.String()

	sink.MarkFrame()
	r.InvalidateAll()
	n, err := r.Render()
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("the second frame changes the counter, so it must write bytes")
	}
	// A full repaint of a 60x14 screen would be well over a thousand bytes. The
	// second frame changes two digits, so it must be a tiny fraction of that.
	if n > 120 {
		t.Errorf("second frame wrote %d bytes; static chrome is not being skipped", n)
	}
	if got := sink.String(); got == first {
		t.Error("the counter did not change between frames")
	}
	checkGolden(t, "hello_frame2.txt", sink.String())
}

func TestIdleFrameWritesNothing(t *testing.T) {
	sink := renderGolden(t, 1)
	sink.MarkFrame()
	r := render.New(sink, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(),
	})
	r.SetRoot(&hello{depth: buffer.DepthTrueColor, bounds: centred(goldenW, goldenH, blockW, blockH)})
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	before := sink.Writes()
	for i := 0; i < 5; i++ {
		n, err := r.Render()
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("idle frame %d wrote %d bytes", i, n)
		}
	}
	if sink.Writes() != before {
		t.Error("an idle frame reached the Sink")
	}
}

// TestDegradedColourGolden renders at each rung of the ladder, so a change to
// the degradation mapping shows up as a golden diff rather than as a surprise in
// someone's terminal.
func TestDegradedColourGolden(t *testing.T) {
	cases := []struct {
		name string
		caps termmosaic.Caps
	}{
		{"truecolor", termmosaic.Caps{TrueColor: true, Color256: true}},
		{"256", termmosaic.Caps{Color256: true}},
		{"16", termmosaic.Caps{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := headless.NewMemorySink(goldenW, goldenH)
			r := render.New(sink, render.Config{
				Width: goldenW, Height: goldenH, Caps: tc.caps,
			})
			root := &hello{depth: tc.caps.ColourDepth(), bounds: centred(goldenW, goldenH, blockW, blockH)}
			r.SetRoot(root)
			if _, err := r.Render(); err != nil {
				t.Fatal(err)
			}
			if got := sink.UnknownSequences(); got != 0 {
				t.Fatalf("%d unknown sequences at depth %v", got, tc.caps.ColourDepth())
			}
			checkGolden(t, "hello_"+tc.name+".sgr", sink.RawString())
		})
	}
}

// TestNoColorGolden proves NO_COLOR changes the byte stream and nothing else
// about the visible text.
func TestNoColorGolden(t *testing.T) {
	withColor := headless.NewMemorySink(goldenW, goldenH)
	r1 := render.New(withColor, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(),
	})
	r1.SetRoot(&hello{depth: buffer.DepthTrueColor, bounds: centred(goldenW, goldenH, blockW, blockH)})
	if _, err := r1.Render(); err != nil {
		t.Fatal(err)
	}

	noColor := headless.NewMemorySink(goldenW, goldenH)
	r2 := render.New(noColor, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(), NoColor: true,
	})
	r2.SetRoot(&hello{depth: buffer.DepthTrueColor, bounds: centred(goldenW, goldenH, blockW, blockH)})
	if _, err := r2.Render(); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(noColor.RawString(), "38;2") {
		t.Error("NO_COLOR frame emitted a truecolor sequence")
	}
	if len(noColor.RawString()) >= len(withColor.RawString()) {
		t.Errorf("NO_COLOR frame is not smaller: %d vs %d bytes",
			len(noColor.RawString()), len(withColor.RawString()))
	}
	if noColor.String() != withColor.String() {
		t.Error("NO_COLOR must not change the visible text")
	}
	checkGolden(t, "hello_nocolor.sgr", noColor.RawString())
}

func TestResizeGolden(t *testing.T) {
	sink := headless.NewMemorySink(goldenW, goldenH)
	r := render.New(sink, render.Config{
		Width: goldenW, Height: goldenH, Caps: termmosaic.DefaultCaps(),
	})
	root := &hello{depth: buffer.DepthTrueColor, bounds: centred(goldenW, goldenH, blockW, blockH)}
	r.SetRoot(root)
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}

	// Grow the terminal: the renderer must recompute the block's bounds and
	// repaint everything, because the previous frame described a screen that no
	// longer exists.
	const newW, newH = 72, 18
	sink.Resize(newW, newH)
	r.Resize(newW, newH)
	root.bounds = centred(newW, newH, blockW, blockH)
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "hello_resized.txt", sink.String())
}

// TestGoldenFilesExist fails with a clear instruction rather than a file-not-found
// error, because that is the first thing anyone hits here.
func TestGoldenFilesExist(t *testing.T) {
	for _, name := range []string{"hello.txt", "hello.sgr", "hello_frame2.txt"} {
		if _, err := os.Stat(filepath.Join("testdata", name)); err != nil {
			t.Errorf("missing golden file %s: %v", name, err)
		}
	}
}
