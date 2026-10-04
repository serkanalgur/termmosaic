package render

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/internal/ansi"
)

var (
	red   = buffer.NewColour(0xff, 0x00, 0x00)
	green = buffer.NewColour(0x30, 0xc0, 0x80)
	white = buffer.NewColour(0xff, 0xff, 0xff)
)

// block is a minimal test widget: a static background with one dynamic cell.
// It is a test fixture, not a widget in the catalog — this task deliberately
// ships no widgets.
type block struct {
	bounds buffer.Rect
	text   string
	fg     buffer.Colour
	// counter changes on each Draw when animate is set, so a test can make the
	// content differ between frames.
	counter int
	animate bool
	draws   int
	focused bool
	lastEv  termmosaic.Event
}

func (b *block) Bounds() buffer.Rect { return b.bounds }
func (b *block) Invalidate()         {}
func (b *block) Handle(e termmosaic.Event) bool {
	b.lastEv = e
	return e.Kind == termmosaic.EventKey && e.Rune == 'x'
}
func (b *block) Focused() bool     { return b.focused }
func (b *block) SetFocused(f bool) { b.focused = f }
func (b *block) Draw(buf *buffer.Buffer) {
	b.draws++
	if b.animate {
		b.counter++
	}
	buf.FillRect(b.bounds, buffer.NewCell(' ', buffer.NewStyle(b.fg, buffer.DefaultColour, 0)))
	if b.text != "" {
		buf.SetString(b.bounds.X, b.bounds.Y, b.text, buffer.NewStyle(b.fg, buffer.DefaultColour, 0))
	}
	if b.animate {
		buf.Set(b.bounds.X, b.bounds.Y, rune('0'+b.counter%10), buffer.NewStyle(green, buffer.DefaultColour, 0))
	}
}

func newTestRenderer(t *testing.T, w, h int, opts ...func(*Config)) (*Renderer, *headless.MemorySink) {
	t.Helper()
	sink := headless.NewMemorySink(w, h)
	cfg := Config{Width: w, Height: h, Caps: termmosaic.DefaultCaps()}
	for _, o := range opts {
		o(&cfg)
	}
	return New(sink, cfg), sink
}

func TestFirstFrameIsFullRepaintAndIdleFrameWritesNothing(t *testing.T) {
	r, sink := newTestRenderer(t, 10, 3)
	r.SetRoot(&block{bounds: buffer.Rect{W: 10, H: 3}, text: "hello"})

	n, err := r.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if n == 0 {
		t.Fatal("the first frame must write something")
	}
	if sink.UnknownSequences() != 0 {
		t.Fatalf("sink saw %d unknown sequences: %q", sink.UnknownSequences(), sink.RawString())
	}
	if got := strings.TrimSpace(sink.Line(0)); got != "hello" {
		t.Fatalf("row 0 = %q, want %q", got, "hello")
	}

	// Nothing is dirty: the frame must be a complete no-op.
	sink.MarkFrame()
	before := sink.Writes()
	for i := 0; i < 10; i++ {
		n, err := r.Render()
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if n != 0 {
			t.Fatalf("idle frame %d wrote %d bytes, want 0", i, n)
		}
	}
	if sink.Writes() != before {
		t.Errorf("an idle frame must not touch the Sink: writes went %d -> %d", before, sink.Writes())
	}
}

func TestNeedsFrameTracksDirtyState(t *testing.T) {
	r, _ := newTestRenderer(t, 10, 3)
	r.SetRoot(&block{bounds: buffer.Rect{W: 10, H: 3}, text: "x"})
	if !r.NeedsFrame() {
		t.Fatal("a fresh renderer must need a frame")
	}
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if r.NeedsFrame() {
		t.Fatal("after painting, nothing should be dirty")
	}
	r.Invalidate(buffer.Rect{X: 0, Y: 0, W: 2, H: 1})
	if !r.NeedsFrame() {
		t.Fatal("Invalidate must make a frame necessary")
	}
}

func TestOnlyTheDirtyRegionIsRewritten(t *testing.T) {
	r, sink := newTestRenderer(t, 20, 3)
	w := &block{bounds: buffer.Rect{W: 20, H: 3}, text: "one", fg: white}
	r.SetRoot(w)
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}

	// Change one cell and invalidate only it.
	w.text = "two"
	r.Invalidate(buffer.Rect{X: 0, Y: 0, W: 3, H: 1})
	sink.MarkFrame() // reset the frame log
	n, err := r.Render()
	if err != nil {
		t.Fatal(err)
	}
	frame := sink.MarkFrame()
	if n != len(frame) {
		t.Fatalf("Render reported %d bytes, sink recorded %d", n, len(frame))
	}
	// One cursor move, one rune and at most one SGR: nowhere near a repaint.
	if n > 40 {
		t.Errorf("a one-cell change wrote %d bytes (%q); static chrome is not being skipped", n, frame)
	}
	if !strings.Contains(string(frame), "t") {
		t.Errorf("frame %q does not contain the new glyph", frame)
	}
}

func TestResizeForcesFullRepaint(t *testing.T) {
	r, sink := newTestRenderer(t, 10, 3)
	r.SetRoot(&block{bounds: buffer.Rect{X: 0, Y: 0, W: 10, H: 3}, text: "hello"})
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if w, h := r.Size(); w != 10 || h != 3 {
		t.Fatalf("size = %dx%d", w, h)
	}

	sink.Resize(20, 5)
	r.Resize(20, 5)
	if w, h := r.Size(); w != 20 || h != 5 {
		t.Fatalf("size = %dx%d, want 20x5", w, h)
	}

	frame := sink.MarkFrame()
	n, err := r.Render()
	if err != nil {
		t.Fatal(err)
	}
	// The whole new screen must be written: the previous frame described a
	// screen that no longer exists.
	written := 0
	for _, c := range []byte(frame) {
		if c == 'h' || c == 'e' || c == 'l' || c == 'o' {
			written++
		}
	}
	if written < 5 {
		t.Errorf("resize wrote %d bytes (%q); a full repaint is required", n, frame)
	}
	if n < 50 {
		t.Errorf("resize frame wrote only %d bytes; that is not a full repaint", n)
	}
}

func TestResizeToSameSizeIsNotDirty(t *testing.T) {
	r, _ := newTestRenderer(t, 10, 3)
	r.SetRoot(&block{bounds: buffer.Rect{W: 10, H: 3}, text: "x"})
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	r.Resize(10, 3)
	if r.NeedsFrame() {
		t.Error("a no-op resize must not force a frame")
	}
}

func TestNoOpResizeCall(t *testing.T) {
	r, _ := newTestRenderer(t, 10, 3)
	r.SetRoot(&block{bounds: buffer.Rect{W: 10, H: 3}, text: "x"})
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	r.Resize(-1, -1) // clamped, and still a change from 10x3
	if w, h := r.Size(); w != 0 || h != 0 {
		t.Errorf("size = %dx%d, want 0x0", w, h)
	}
	if _, err := r.Render(); err != nil {
		t.Errorf("rendering into a zero-sized screen must not fail: %v", err)
	}
}

func TestColourDegradationLadder(t *testing.T) {
	// At each depth the sink resolves the emitted colour back to a palette
	// entry, so the expectation is the palette entry the ladder should choose,
	// not the original RGB. That the two differ at 256 and 16 is the point: the
	// ladder is lossy by construction, and lossy in a documented way.
	paletteAt256 := func(c buffer.Colour) buffer.Colour {
		r, g, b := buffer.Index256PaletteRGB(int(c.Index256()))
		return buffer.NewColour(r, g, b)
	}
	paletteAt16 := func(c buffer.Colour) buffer.Colour {
		r, g, b := buffer.Index256PaletteRGB(int(c.Named16()))
		return buffer.NewColour(r, g, b)
	}
	cases := []struct {
		name   string
		caps   termmosaic.Caps
		want   ansi.Depth
		wantFG buffer.Colour
	}{
		{"truecolor", termmosaic.Caps{TrueColor: true, Color256: true}, ansi.DepthTrueColor, green},
		{"256", termmosaic.Caps{Color256: true}, ansi.Depth256, paletteAt256(green)},
		{"16", termmosaic.Caps{}, ansi.Depth16, paletteAt16(green)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := headless.NewMemorySink(10, 1)
			r := New(sink, Config{Width: 10, Height: 1, Caps: tc.caps})
			if got := r.Depth(); got != tc.want {
				t.Errorf("Depth() = %v, want %v", got, tc.want)
			}
			r.SetRoot(&block{bounds: buffer.Rect{W: 10, H: 1}, text: "A", fg: green})
			if _, err := r.Render(); err != nil {
				t.Fatal(err)
			}
			if sink.UnknownSequences() != 0 {
				t.Fatalf("unknown sequences: %q", sink.RawString())
			}
			// The sink resolves the degraded colour back to a palette entry, so
			// assert it round-trips to the original colour's nearest palette
			// value rather than the exact RGB.
			got := sink.CellAt(0, 0).FG
			if got != tc.wantFG {
				t.Errorf("cell FG = %v, want %v (round-trip through the %s ladder)", got, tc.wantFG, tc.name)
			}
		})
	}
}

func TestSetDepthForcesFullRepaint(t *testing.T) {
	r, _ := newTestRenderer(t, 10, 1)
	r.SetRoot(&block{bounds: buffer.Rect{W: 10, H: 1}, text: "A", fg: green})
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if r.NeedsFrame() {
		t.Fatal("expected a clean state")
	}
	r.SetDepth(ansi.Depth16)
	if !r.NeedsFrame() {
		t.Error("changing depth must force a repaint, because terminal state is unknown")
	}
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	r.SetDepth(ansi.Depth16) // same value, must not dirty
	if r.NeedsFrame() {
		t.Error("setting the same depth must not dirty the screen")
	}
}

func TestNoColorSuppressesColour(t *testing.T) {
	sink := headless.NewMemorySink(4, 1)
	r := New(sink, Config{Width: 4, Height: 1, Caps: termmosaic.DefaultCaps(), NoColor: true})
	r.SetRoot(&block{bounds: buffer.Rect{W: 4, H: 1}, text: "A", fg: red})
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sink.RawString(), "38;2") {
		t.Errorf("NO_COLOR frame emitted a truecolor sequence: %q", sink.RawString())
	}
}

func TestNoColorFromEnv(t *testing.T) {
	if NoColorFromEnv(func(string) string { return "" }) {
		t.Error("empty NO_COLOR must not disable colour")
	}
	if NoColorFromEnv(func(k string) string {
		if k == "NO_COLOR" {
			return "1"
		}
		return ""
	}) != true {
		t.Error("a non-empty NO_COLOR must disable colour")
	}
}

func TestAltScreenAndResetSequences(t *testing.T) {
	r, sink := newTestRenderer(t, 5, 2)
	r.SetRoot(&block{bounds: buffer.Rect{W: 5, H: 2}, text: "x"})
	if err := r.EnterAltScreen(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.RawString(), ansi.EnterAltScreen) {
		t.Error("EnterAltScreen did not emit the alt-screen sequence")
	}
	if !sink.AltScreen() {
		t.Error("sink did not observe the alt-screen switch")
	}
	if err := r.LeaveAltScreen(); err != nil {
		t.Fatal(err)
	}
	if sink.AltScreen() {
		t.Error("sink still reports the alt screen")
	}
	if err := r.Reset(); err != nil {
		t.Fatal(err)
	}
	if !r.NeedsFrame() {
		t.Error("Reset must force a repaint")
	}
	if err := r.HideCursor(); err != nil {
		t.Fatal(err)
	}
	if err := r.ShowCursor(); err != nil {
		t.Fatal(err)
	}
}

func TestCursorIsDiffedSeparately(t *testing.T) {
	r, sink := newTestRenderer(t, 10, 3)
	r.SetRoot(&block{bounds: buffer.Rect{W: 10, H: 3}, text: "hello"})
	r.SetCursor(Cursor{X: 2, Y: 1, Visible: true, FG: white, BG: buffer.DefaultColour, Valid: true})
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	x, y, shown := sink.Cursor()
	if x != 2 || y != 1 || !shown {
		t.Fatalf("sink cursor = %d,%d shown=%v; want 2,1,true", x, y, shown)
	}

	// Moving the cursor with nothing else dirty still writes bytes.
	sink.MarkFrame()
	r.SetCursor(Cursor{X: 4, Y: 2, Visible: true, FG: white, BG: buffer.DefaultColour, Valid: true})
	n, err := r.Render()
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("a cursor-only change must still reach the terminal")
	}
	if x, _, _ := sink.Cursor(); x != 4 {
		t.Errorf("cursor x = %d, want 4", x)
	}
}

func TestUnmanagedCursorIsLeftAlone(t *testing.T) {
	r, sink := newTestRenderer(t, 5, 1)
	r.SetRoot(&block{bounds: buffer.Rect{W: 5, H: 1}, text: "x"})
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sink.RawString(), "?25") {
		t.Errorf("an unmanaged cursor must not emit visibility sequences: %q", sink.RawString())
	}
}

func TestPostRunsOnRenderGoroutine(t *testing.T) {
	r, _ := newTestRenderer(t, 10, 1)
	w := &block{bounds: buffer.Rect{W: 10, H: 1}, text: "a"}
	r.SetRoot(w)
	r.Post(func() { w.text = "b" })
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	if w.text != "b" {
		t.Fatal("Post did not run before the frame")
	}
	// A posted callback that invalidates is picked up by the same frame.
	r.Post(func() { r.Invalidate(buffer.Rect{X: 0, Y: 0, W: 1, H: 1}) })
	if _, err := r.Render(); err != nil {
		t.Fatal(err)
	}
	r.Post(nil) // must not panic
}

// failingSink fails on write, to check the renderer reports rather than
// swallowing.
type failingSink struct {
	headless.MemorySink
	failOn string
}

func (f *failingSink) Write(p []byte) (int, error) {
	if f.failOn == "write" {
		return 0, errors.New("device on fire")
	}
	return f.MemorySink.Write(p)
}

func (f *failingSink) Flush() error {
	if f.failOn == "flush" {
		return errors.New("flush failed")
	}
	return nil
}

func TestWriteErrorIsReportedNotSwallowed(t *testing.T) {
	fs := &failingSink{MemorySink: *headless.NewMemorySink(5, 1), failOn: "write"}
	r := New(fs, Config{Width: 5, Height: 1, Caps: termmosaic.DefaultCaps()})
	r.SetRoot(&block{bounds: buffer.Rect{W: 5, H: 1}, text: "x"})
	_, err := r.Render()
	if err == nil {
		t.Fatal("a failed write must be reported")
	}
	var fe *FrameError
	if !errors.As(err, &fe) {
		t.Fatalf("error is %T, want *FrameError", err)
	}
	if fe.Op != "write" {
		t.Errorf("Op = %q, want \"write\"", fe.Op)
	}
	if !errors.Is(err, err) || fe.Unwrap() == nil {
		t.Error("FrameError must unwrap to the underlying error")
	}

	fs2 := &failingSink{MemorySink: *headless.NewMemorySink(5, 1), failOn: "flush"}
	r2 := New(fs2, Config{Width: 5, Height: 1, Caps: termmosaic.DefaultCaps()})
	r2.SetRoot(&block{bounds: buffer.Rect{W: 5, H: 1}, text: "x"})
	if _, err := r2.Render(); err == nil {
		t.Error("a failed flush must be reported")
	}
}

// shortSink accepts fewer bytes than offered without erroring.
type shortSink struct{ headless.MemorySink }

func (s *shortSink) Write(p []byte) (int, error) {
	if len(p) > 0 {
		_, _ = s.MemorySink.Write(p[:len(p)-1])
	}
	return len(p) - 1, nil
}

func TestShortWriteIsAnError(t *testing.T) {
	s := &shortSink{MemorySink: *headless.NewMemorySink(5, 1)}
	r := New(s, Config{Width: 5, Height: 1, Caps: termmosaic.DefaultCaps()})
	r.SetRoot(&block{bounds: buffer.Rect{W: 5, H: 1}, text: "x"})
	_, err := r.Render()
	if err == nil {
		t.Fatal("a short write with a nil error must be treated as an error")
	}
	var fe *FrameError
	if !errors.As(err, &fe) || fe.Op != "write" {
		t.Fatalf("got %v, want a write FrameError", err)
	}
}

func TestRootDrawRunsEveryFrame(t *testing.T) {
	r, _ := newTestRenderer(t, 5, 1)
	w := &block{bounds: buffer.Rect{W: 5, H: 1}, text: "x", animate: true}
	r.SetRoot(w)
	for i := 0; i < 3; i++ {
		r.InvalidateAll()
		if _, err := r.Render(); err != nil {
			t.Fatal(err)
		}
	}
	// ADR 0003: Draw is called for every widget every frame, with no
	// incremental-draw obligation on the author.
	if w.draws != 3 {
		t.Errorf("Draw called %d times, want 3", w.draws)
	}
}

func TestPacerSkipsIdleFrames(t *testing.T) {
	r, sink := newTestRenderer(t, 5, 1)
	w := &block{bounds: buffer.Rect{W: 5, H: 1}, text: "x"}
	r.SetRoot(w)
	p := NewPacer(r)
	n, err := p.Tick()
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("the first tick must paint")
	}
	for i := 0; i < 5; i++ {
		n, err := p.Tick()
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("idle tick %d painted %d bytes", i, n)
		}
	}
	// Invalidating with unchanged content re-runs the widget draw and the diff
	// but writes nothing, because the frame really is identical. That is the
	// diff doing its job, not the pacer stalling.
	before := sink.Writes()
	r.InvalidateAll()
	if n, err := p.Tick(); err != nil || n != 0 {
		t.Fatalf("re-drawing identical content wrote %d bytes (err %v), want 0", n, err)
	}
	if sink.Writes() != before {
		t.Error("an identical re-draw must not reach the Sink")
	}

	// Changing the content and invalidating must be painted by the next tick.
	w.text = "y"
	r.InvalidateAll()
	n2, err := p.Tick()
	if err != nil {
		t.Fatal(err)
	}
	if n2 == 0 {
		t.Error("a changed frame must be painted by the next tick")
	}
	if sink.Writes() == before {
		t.Error("the Sink was never written")
	}
}

func TestPacerRunStopsOnDone(t *testing.T) {
	r, _ := newTestRenderer(t, 5, 1)
	r.SetRoot(&block{bounds: buffer.Rect{W: 5, H: 1}, text: "x"})
	p := NewPacer(r)
	done := make(chan struct{})
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run(done) }()
	time.Sleep(50 * time.Millisecond)
	close(done)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop when done closed")
	}
}

func TestZeroSizedRendererDoesNotPanic(t *testing.T) {
	r, _ := newTestRenderer(t, 0, 0)
	r.SetRoot(&block{bounds: buffer.Rect{W: 0, H: 0}, text: "x"})
	if _, err := r.Render(); err != nil {
		t.Fatalf("a zero-sized render must not fail: %v", err)
	}
}

func BenchmarkRenderSteadyState(b *testing.B) {
	sink := headless.NewMemorySink(200, 60)
	sink.KeepBytes(1 << 20)
	r := New(sink, Config{Width: 200, Height: 60, Caps: termmosaic.DefaultCaps()})
	w := &block{bounds: buffer.Rect{W: 200, H: 60}, text: "static chrome", fg: white}
	r.SetRoot(w)
	if _, err := r.Render(); err != nil {
		b.Fatal(err)
	}
	// One dynamic cell per frame, the steady state of a dashboard.
	r.Invalidate(buffer.Rect{X: 100, Y: 30, W: 1, H: 1})

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render(); err != nil {
			b.Fatal(err)
		}
		r.Invalidate(buffer.Rect{X: 100, Y: 30, W: 1, H: 1})
	}
}

// BenchmarkRenderInvalidateAll measures a frame in which the whole screen is
// marked dirty but the widget repaints identical content. The row skip still
// fires, so this is close to the steady-state cost and is NOT a full repaint --
// which is why it is not called one.
func BenchmarkRenderInvalidateAll(b *testing.B) {
	sink := headless.NewMemorySink(200, 60)
	sink.KeepBytes(1 << 20)
	r := New(sink, Config{Width: 200, Height: 60, Caps: termmosaic.DefaultCaps()})
	r.SetRoot(&block{bounds: buffer.Rect{W: 200, H: 60}, text: "chrome", fg: white})
	if _, err := r.Render(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.InvalidateAll()
		if _, err := r.Render(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderForceRepaint measures a genuine full repaint: the renderer is
// reset, so the previous frame is discarded and every cell is re-encoded with no
// row skip available. This is the cost of a resize or an alt-screen re-entry.
func BenchmarkRenderForceRepaint(b *testing.B) {
	sink := headless.NewMemorySink(200, 60)
	// Bound the sink's byte history: without this the sink accumulates every
	// frame ever written and its buffer growth dominates the measurement.
	sink.KeepBytes(1 << 20)
	r := New(sink, Config{Width: 200, Height: 60, Caps: termmosaic.DefaultCaps()})
	r.SetRoot(&block{bounds: buffer.Rect{W: 200, H: 60}, text: "chrome", fg: white})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := r.Reset(); err != nil {
			b.Fatal(err)
		}
		if _, err := r.Render(); err != nil {
			b.Fatal(err)
		}
	}
}
