package input

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/headless"
)

// recorder captures the bytes a Source writes to the terminal.
type recorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *recorder) write(p []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.buf.Write(p)
	return err
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// harness is a Source over a headless terminal, plus the teardown every test
// needs. Closing the terminal before the Source is what unblocks the reader,
// because a Source deliberately does not close a terminal it was only reading.
type harness struct {
	t    *headless.Terminal
	s    *Source
	rec  *recorder
	done func()
}

func newHarness(t *testing.T, cfg Config) *harness {
	t.Helper()
	ht := headless.New(80, 24)
	rec := &recorder{}
	if cfg.WriteProbe == nil {
		cfg.WriteProbe = rec.write
	}
	s := NewSource(ht, cfg)
	h := &harness{t: ht, s: s, rec: rec}
	h.done = func() {
		_ = ht.Close()
		_ = s.Close()
	}
	t.Cleanup(h.done)
	return h
}

// next waits for one event, failing the test if none arrives.
func (h *harness) next(t *testing.T) termmosaic.Event {
	t.Helper()
	select {
	case ev, ok := <-h.s.Events():
		if !ok {
			t.Fatal("the event channel closed while waiting for an event")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return termmosaic.Event{}
	}
}

// drain collects everything currently available without blocking.
func (h *harness) drain() []termmosaic.Event {
	var out []termmosaic.Event
	for {
		select {
		case ev, ok := <-h.s.Events():
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestSourceDeliversKeys(t *testing.T) {
	h := newHarness(t, DefaultConfig())
	h.t.FeedString("hi")
	for _, want := range []rune{'h', 'i'} {
		ev := h.next(t)
		if ev.Kind != termmosaic.EventKey || ev.Rune != want {
			t.Fatalf("event = %+v, want rune %q", ev, want)
		}
	}
}

func TestSourceDeliversAResize(t *testing.T) {
	h := newHarness(t, DefaultConfig())
	h.t.PushResize(120, 40)
	ev := h.next(t)
	if ev.Kind != termmosaic.EventResize {
		t.Fatalf("kind = %v, want EventResize", ev.Kind)
	}
	if ev.Size.W != 120 || ev.Size.H != 40 {
		t.Errorf("size = %+v, want 120x40", ev.Size)
	}
}

func TestSourceDoesNotDropWhenTheQueueIsFull(t *testing.T) {
	// The backpressure rule: a full queue STOPS THE READ, it does not drop. The
	// failure mode of a full queue is latency and latency is recoverable; the
	// failure mode of a dropped keystroke is a text input that lost a character.
	h := newHarness(t, Config{EventQueue: 2})

	want := strings.Repeat("abcdefghij", 20) // 200 keys through a 2-deep queue
	h.t.FeedString(want)

	got := make([]byte, 0, len(want))
	deadline := time.After(5 * time.Second)
	for len(got) < len(want) {
		select {
		case ev := <-h.s.Events():
			if ev.Kind != termmosaic.EventKey {
				t.Fatalf("unexpected %v", ev.Kind)
			}
			got = append(got, byte(ev.Rune))
		case <-deadline:
			t.Fatalf("only %d of %d keys arrived", len(got), len(want))
		}
	}
	if string(got) != want {
		t.Errorf("the stream arrived as %q, want %q", got, want)
	}
}

func TestSourceAppliesPasteAPasteEndToEnd(t *testing.T) {
	h := newHarness(t, DefaultConfig())
	h.t.FeedString("\x1b[200~pasted\x1b[201~")
	ev := h.next(t)
	if ev.Kind != termmosaic.EventPaste || ev.Text != "pasted" {
		t.Fatalf("event = %+v, want one EventPaste carrying %q", ev, "pasted")
	}
}

func TestSourceWritesOnlyTheModesItIsAskedFor(t *testing.T) {
	// The defaults are the point: a TermMosaic program must not take text
	// selection, middle-click paste and scrollback copying away from the user's
	// shell just by running.
	h := newHarness(t, DefaultConfig())
	got := h.rec.String()
	if strings.Contains(got, "1000") || strings.Contains(got, "1006") {
		t.Errorf("DefaultConfig enabled mouse reporting: %q", got)
	}
	if strings.Contains(got, "1004") {
		t.Errorf("DefaultConfig enabled focus reporting: %q", got)
	}
	if !strings.Contains(got, "2004h") {
		t.Errorf("DefaultConfig did not request bracketed paste: %q", got)
	}
}

func TestSourceEnablesMouseAndFocusWhenAsked(t *testing.T) {
	h := newHarness(t, Config{
		MouseMode:            MouseClick,
		EnableFocusReporting: true,
		BracketedPaste:       true,
		ProbeKitty:           false,
	})
	got := h.rec.String()
	for _, want := range []string{"?1000h", "?1006h", "?1004h", "?2004h"} {
		if !strings.Contains(got, want) {
			t.Errorf("enable sequence %q missing from %q", want, got)
		}
	}
}

func TestSourceRestoresModesOnClose(t *testing.T) {
	h := newHarness(t, Config{
		MouseMode:            MouseClick,
		EnableFocusReporting: true,
		BracketedPaste:       true,
		ProbeKitty:           false,
	})
	// Both closes matter and neither is optional: the test is about the modes
	// being restored, so a transport that failed to close is a failure of the
	// thing under test rather than noise to discard.
	if err := h.t.Close(); err != nil {
		t.Fatalf("transport close: %v", err)
	}
	if err := h.s.Close(); err != nil {
		t.Fatal(err)
	}
	got := h.rec.String()
	for _, want := range []string{"?1000l", "?1006l", "?1004l", "?2004l"} {
		if !strings.Contains(got, want) {
			t.Errorf("disable sequence %q missing from %q; a TUI that leaves the terminal in a mode it turned on is a broken shell", want, got)
		}
	}
}

func TestSourceKittyHandshakeNegotiatesDisambiguateOnly(t *testing.T) {
	// The whole handshake: query, reply, push. Only flag 0b1 is requested, because
	// event types would double or triple event volume for information no widget in
	// the catalog consumes, and associated text would change what Event.Rune means
	// before a TextInput exists to define it.
	h := newHarness(t, Config{ProbeKitty: true, KittyFlags: 0b1})
	waitFor(t, func() bool { return strings.Contains(h.rec.String(), "\x1b[?u") }, "the kitty query")

	h.t.FeedString("\x1b[?5u")

	select {
	case flags := <-h.s.KittyFlags():
		if flags != 5 {
			t.Errorf("KittyFlags yielded %d, want the terminal's 5", flags)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the kitty flags")
	}
	waitFor(t, func() bool { return h.s.KittyActive() }, "the kitty push to land")
	waitFor(t, func() bool { return strings.Contains(h.rec.String(), "\x1b[>1u") }, "the disambiguate push")
	if strings.Contains(h.rec.String(), ">3u") {
		t.Errorf("an event-types or associated-text flag was requested: %q", h.rec.String())
	}
}

func TestSourceKittyHandshakeTimesOut(t *testing.T) {
	// 100 ms, hard. Startup must never block on a terminal that ignores the
	// query, and 100 ms is well inside what a user perceives as instant for a
	// terminal that will never answer.
	h := newHarness(t, Config{ProbeKitty: true})
	waitFor(t, func() bool { return strings.Contains(h.rec.String(), "\x1b[?u") }, "the kitty query")

	time.Sleep(2 * KittyProbeTimeout)
	if h.s.KittyActive() {
		t.Error("KittyActive = true after the probe timed out")
	}
	if strings.Contains(h.rec.String(), "\x1b[>") {
		t.Errorf("a flag set was pushed to a terminal that never answered: %q", h.rec.String())
	}
	// And a late reply must still be published rather than lost.
	h.t.FeedString("\x1b[?1u")
	select {
	case flags := <-h.s.KittyFlags():
		if flags != 1 {
			t.Errorf("KittyFlags = %d, want 1", flags)
		}
	case <-time.After(2 * time.Second):
		t.Error("a late kitty reply was swallowed")
	}
}

func TestSourceWithoutWriteProbeIsAPureDecoder(t *testing.T) {
	// A nil WriteProbe disables probing and every enable sequence, leaving the
	// Source as a decoder. This is the whole reason ADR 0005 did not widen
	// Terminal: the write path is optional and a headless run needs none.
	ht := headless.New(80, 24)
	s := NewSource(ht, Config{ProbeKitty: true, BracketedPaste: true})
	t.Cleanup(func() {
		_ = ht.Close()
		_ = s.Close()
	})
	ht.FeedString("q")
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Rune == 'q' {
				return
			}
		case <-deadline:
			t.Fatal("no event arrived with a nil WriteProbe")
		}
	}
}

func TestSourceClosesItsEventChannelAtEOF(t *testing.T) {
	ht := headless.New(80, 24)
	s := NewSource(ht, Config{})
	ht.FeedString("a")
	// Read the one event, then end the terminal.
	select {
	case <-s.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
	}
	_ = ht.Close()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-s.Events():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the event channel did not close at EOF")
		}
	}
}

func TestSourceCloseIsIdempotent(t *testing.T) {
	h := newHarness(t, DefaultConfig())
	_ = h.t.Close()
	if err := h.s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Close(); err != nil {
		t.Fatalf("second Close returned %v, want nil", err)
	}
	if _, ok := <-h.s.Events(); ok {
		t.Error("the event channel is still open after Close")
	}
}

func TestSourceNeverSplitsOneReadsEventsAroundAResize(t *testing.T) {
	// ADR 0005 §5 guarantee 4, as far as it is reachable: the events decoded
	// from ONE read(2) are delivered together, and a resize cannot land in the
	// middle of them. Feeding a mouse report and a key in one chunk, then a
	// resize, and asserting that the two input events are adjacent is the
	// observable form of that.
	//
	// What is NOT asserted, and cannot be, is that input still sitting in the
	// kernel's tty buffer beats a resize that arrived first: Terminal.Read cannot
	// be interrupted to ask whether anything is pending, and ADR 0005 explicitly
	// declines to add a method that could. The limitation is stated on Source.
	h := newHarness(t, DefaultConfig())

	// The chunk must arrive as ONE read, which the harness guarantees by waiting
	// for the mouse event before pushing anything else.
	h.t.FeedString("\x1b[<0;3;4Mb")
	mouseEv := h.next(t)
	if mouseEv.Kind != termmosaic.EventMouse {
		t.Fatalf("first event = %+v, want the mouse report", mouseEv)
	}

	h.t.PushResize(120, 50)
	// The key that arrived in the same read must come before the resize, even
	// though the resize was pushed afterwards.
	keyEv := h.next(t)
	if keyEv.Rune != 'b' {
		t.Fatalf("second event = %+v, want the 'b' that arrived in the same read as the mouse report", keyEv)
	}
	resizeEv := h.next(t)
	if resizeEv.Kind != termmosaic.EventResize || resizeEv.Size.W != 120 {
		t.Fatalf("third event = %+v, want the 120x50 resize after the pending input", resizeEv)
	}
}

func TestSourceResolvesABareEscapeThroughTheDeadline(t *testing.T) {
	// The end-to-end version of the escape-delay rule, with a real timer: a bare
	// ESC is not an event until the delay expires.
	h := newHarness(t, DefaultConfig())
	h.t.FeedString("\x1b")

	// Nothing yet, or it was instant.
	if got := h.drain(); len(got) != 0 {
		t.Fatalf("a bare ESC produced %+v immediately, want it to wait out the delay", got)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-h.s.Events():
			if ev.Kind != termmosaic.EventKey || ev.Key != termmosaic.KeyEscape {
				t.Fatalf("event = %+v, want KeyEscape", ev)
			}
			return
		case <-deadline:
			t.Fatal("the bare ESC was never reported as KeyEscape")
		}
	}
}

func TestSourceResolvesABareEscapeAsAnArrowWhenBytesArrive(t *testing.T) {
	h := newHarness(t, DefaultConfig())
	h.t.FeedString("\x1b")
	// Arrive inside the delay, so it is a sequence rather than the Escape key.
	h.t.FeedString("[A")

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-h.s.Events():
			if ev.Kind != termmosaic.EventKey || ev.Key != termmosaic.KeyUp {
				t.Fatalf("event = %+v, want KeyUp and no stray KeyEscape", ev)
			}
			// Give the delay time to misfire and prove it did not.
			time.Sleep(3 * DefaultEscapeDelay)
			if got := h.drain(); len(got) != 0 {
				t.Fatalf("the delay misfired after the sequence completed: %+v", got)
			}
			return
		case <-deadline:
			t.Fatal("no event arrived")
		}
	}
}

// waitFor polls until cond holds or the test times out.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
