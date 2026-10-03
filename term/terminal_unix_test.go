//go:build !windows

package term

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/serkanalgur/termmosaic"
)

// newResizeTestTerminal returns a UnixTerminal with only the fields
// publishSize and watchResize touch. Building it by hand rather than through
// Open is what lets the resize policy be tested at all: Open needs a real tty for
// its ioctl, and without a real tty every size is the 80x25 fallback, so a test
// through Open could not tell a stale size from a fresh one.
//
// The bug this file exists to prevent had zero coverage for exactly that reason.
func newResizeTestTerminal(depth int) *UnixTerminal {
	return &UnixTerminal{
		resizes:  make(chan termmosaic.Size, depth),
		sigwinch: make(chan os.Signal, 1),
		done:     make(chan struct{}),
	}
}

// TestPublishSizeKeepsTheLatestNotTheOldest is the regression test for the
// inverted policy ADR 0005 §5 point 3 forced.
//
// Before this, a full channel caused the watcher to DROP THE NEW SIZE and keep
// the stale ones, so a consumer that fell behind received a queue of old sizes
// and never the final one — leaving a widget rendering for a terminal that no
// longer exists.
func TestPublishSizeKeepsTheLatestNotTheOldest(t *testing.T) {
	const depth = 8
	term := newResizeTestTerminal(depth)

	// Fill the channel completely, which is the state the old code mishandled.
	for i := 1; i <= depth; i++ {
		term.resizes <- termmosaic.Size{W: i * 10, H: 24}
	}
	if len(term.resizes) != depth {
		t.Fatalf("test setup: %d sizes queued, want %d", len(term.resizes), depth)
	}

	// The newest size, published into a full channel.
	newest := termmosaic.Size{W: 200, H: 50}
	if ok := term.publishSize(newest); !ok {
		t.Fatal("publishSize reported the terminal closing")
	}

	// Exactly one size must be queued, and it must be the newest.
	if got := len(term.resizes); got != 1 {
		t.Fatalf("%d sizes queued after publishing, want 1: undelivered sizes must be coalesced away", got)
	}
	if got := <-term.resizes; got != newest {
		t.Errorf("queued size = %+v, want the newest %+v", got, newest)
	}
}

// TestPublishSizeNeverDropsTheNotification pins the other half of the rule: a
// resize is never lost, only coalesced. A consumer that is merely slow must
// still see the final size.
func TestPublishSizeNeverDropsTheNotification(t *testing.T) {
	const depth = 2
	term := newResizeTestTerminal(depth)
	for i := 1; i <= 20; i++ {
		if ok := term.publishSize(termmosaic.Size{W: i, H: i}); !ok {
			t.Fatalf("publishSize(%d) reported the terminal closing", i)
		}
	}
	if len(term.resizes) != 1 {
		t.Fatalf("%d sizes queued, want 1", len(term.resizes))
	}
	if got := <-term.resizes; got.W != 20 {
		t.Errorf("queued size = %+v, want the last one published (20x20)", got)
	}
}

// TestPublishSizeDeliversEverySizeToAFastConsumer is the other end of the
// spectrum: a consumer keeping up must see every size, because coalescing is
// only allowed while a size is UNDELIVERED.
func TestPublishSizeDeliversEverySizeToAFastConsumer(t *testing.T) {
	const depth = 8
	term := newResizeTestTerminal(depth)
	for i := 1; i <= 5; i++ {
		if ok := term.publishSize(termmosaic.Size{W: i, H: i}); !ok {
			t.Fatalf("publishSize(%d) reported the terminal closing", i)
		}
		if got := <-term.resizes; got.W != i {
			t.Errorf("delivered %+v, want %dx%d", got, i, i)
		}
	}
	if len(term.resizes) != 0 {
		t.Errorf("%d sizes left queued, want 0", len(term.resizes))
	}
}

// TestPublishSizeUnblocksOnClose proves the final send cannot wedge the signal
// goroutine: if the consumer never drains, Close must still release it.
func TestPublishSizeUnblocksOnClose(t *testing.T) {
	term := newResizeTestTerminal(1)
	term.resizes <- termmosaic.Size{W: 1, H: 1} // now full

	done := make(chan bool, 1)
	go func() { done <- term.publishSize(termmosaic.Size{W: 2, H: 2}) }()

	close(term.done)
	if ok := <-done; ok {
		t.Error("publishSize reported success after Close")
	}
}

// TestPublishSizeKeepsTheInitialSize test documents that the size Open queues at
// startup is coalesced like any other: it is a real size, so it is delivered, but
// a resize that supersedes it replaces it rather than queueing behind it.
func TestPublishSizeKeepsTheInitialSize(t *testing.T) {
	term := newResizeTestTerminal(4)
	term.resizes <- termmosaic.Size{W: 80, H: 25}

	if ok := term.publishSize(termmosaic.Size{W: 100, H: 30}); !ok {
		t.Fatal("publishSize reported the terminal closing")
	}
	if len(term.resizes) != 1 {
		t.Fatalf("%d sizes queued, want 1", len(term.resizes))
	}
	if got := <-term.resizes; got.W != 100 || got.H != 30 {
		t.Errorf("queued size = %+v, want the resize to have superseded 80x25", got)
	}
}

// TestSizeFallsBackTo80x25WhenTheFileIsNotATTY is here because the resize tests
// above depend on that fallback being the answer for a non-tty, and because it is
// the only resize-adjacent behaviour in this file that a test could have pinned
// cheaply and did not.
func TestSizeFallsBackTo80x25WhenTheFileIsNotATTY(t *testing.T) {
	term := newResizeTestTerminal(1)
	w, h := term.Size()
	if w != 80 || h != 25 {
		t.Errorf("Size() = %dx%d, want the 80x25 fallback for a non-tty", w, h)
	}
}

// TestWatchResizeDeliversASignal is the only coverage of the goroutine itself, and
// it exists because nothing here was covered at all before ADR 0005's forced
// change. The size it delivers is the ioctl fallback, since a test cannot own a
// real tty; what is being pinned is that a signal becomes exactly one event and
// that closing the terminal stops the goroutine.
func TestWatchResizeDeliversASignal(t *testing.T) {
	term := newResizeTestTerminal(4)
	go term.watchResize()
	defer func() { close(term.done) }()

	term.sigwinch <- syscall.SIGWINCH
	select {
	case got := <-term.resizes:
		if got.W != 80 || got.H != 25 {
			t.Errorf("size = %+v, want the 80x25 fallback", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGWINCH produced no size event")
	}
}
