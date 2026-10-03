package input

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/serkanalgur/termmosaic"
)

// Terminal-mode enable and disable sequences Source writes through
// Config.WriteProbe.
//
// They are assembled as literal byte slices rather than via strconv because they
// are constants; the only one with a value in it is the kitty push, and that
// number is at most 255.
var (
	seqEnableBracketedPaste  = []byte("\x1b[?2004h")
	seqDisableBracketedPaste = []byte("\x1b[?2004l")

	seqEnableFocus  = []byte("\x1b[?1004h")
	seqDisableFocus = []byte("\x1b[?1004l")

	// Mouse: 1000 is press and release (and therefore the wheel), 1002 adds
	// button-event motion, 1003 adds all motion, and 1006 is the SGR encoding
	// this decoder prefers. Only 1006 is requested, which is why MouseClick
	// rather than MouseDrag is the default level.
	seqEnableMouseClick  = []byte("\x1b[?1000h\x1b[?1006h")
	seqEnableMouseDrag   = []byte("\x1b[?1002h\x1b[?1006h")
	seqEnableMouseAll    = []byte("\x1b[?1003h\x1b[?1006h")
	seqDisableMouseClick = []byte("\x1b[?1006l\x1b[?1000l")
	seqDisableMouseDrag  = []byte("\x1b[?1006l\x1b[?1002l\x1b[?1000l")
	seqDisableMouseAll   = []byte("\x1b[?1006l\x1b[?1003l\x1b[?1000l")

	seqKittyQuery = []byte("\x1b[?u")
	seqKittyReset = []byte("\x1b[?u") // CSI ? 0 u, written on Close
)

// kittyPushBuf is large enough for "CSI > <flags> u" for any uint8 flag set.
const kittyPushBuf = 16

// readChunkSize is how many bytes the reader asks for at a time.
//
// 256 rather than a larger 4 KiB because the batch of decoded events must be able
// to hold one event per byte of a read (see readLoop), and an Event is 112 bytes:
// 256 bytes buys a 28 KiB batch, where 4 KiB would buy 458 KiB. On a raw-mode tty
// a read returns whatever the user typed since the last one, so the size only
// affects syscall count, not latency.
const readChunkSize = 256

// Source merges a Terminal's input bytes and its resize notifications into one
// ordered event stream.
//
// One goroutine reads input, one watches resizes, and a third exists only while
// a bare ESC is waiting out its delay. None of them is the consumer, and none of
// them drops input: when the event queue is full the reader stops reading the
// tty, because the failure mode of a full queue is latency and latency is
// recoverable, whereas a silently dropped keystroke is a text input that loses
// a character.
//
// The ordering guarantees it makes are:
//  1. No input event is ever dropped, coalesced or reordered. Only resizes are
//     eligible for coalescing, and that happens in the transport, not here.
//  2. Every resize a consumer actually receives reports a size that was real.
//  3. A resize does not flush pending input, and does not interrupt a
//     half-parsed sequence: bytes already read are decoded and enqueued first, so
//     a mouse event is interpreted against the size in effect when the terminal
//     emitted it, which is the only ordering in which a click at (40, 12) means
//     what the user saw.
//
// One honest limit, stated rather than hidden: guarantee 3 holds between the
// reader and the resize watcher, but a resize can overtake input that is still
// sitting in a kernel tty buffer, because termmosaic.Terminal.Read cannot be
// interrupted to ask whether anything is pending. Interrupting it would need a
// method on Terminal, which ADR 0005 explicitly declines to add.
type Source struct {
	t   termmosaic.Terminal
	cfg Config
	p   *Parser

	events     chan termmosaic.Event
	kittyFlags chan uint8
	kittyReply chan struct{}

	// kittyOn records that the enhancement was pushed. It is atomic because the
	// handshake runs on its own goroutine while an application may poll it.
	kittyOn atomic.Bool

	done       chan struct{}
	eof        chan struct{}
	closeOnce  sync.Once
	eventsOnce sync.Once
	wg         sync.WaitGroup

	// sendMu guards the event channel against being closed while a sender is
	// inside deliver, and closed tells a sender that the channel is already gone.
	// This is the mechanism that lets Close return without waiting for the reader:
	// see Close for why waiting is not an option.
	sendMu sync.RWMutex
	closed atomic.Bool

	// feedMu serialises delivery between the reader and the resize watcher for the
	// duration of one read's worth of events, so a resize can never land between
	// two events that came out of the same read(2).
	feedMu sync.Mutex

	// escMu guards escRunning, which keeps at most one escape-delay goroutine
	// alive.
	escMu      sync.Mutex
	escRunning bool
	escWake    chan struct{}

	// modesOff guards the one-shot terminal-mode teardown.
	modesOff atomic.Bool
}

// NewSource starts reading from t. It does not enter raw mode, does not write to
// the terminal except for the enable sequences cfg asks for, and returns
// immediately. Call Close to stop the goroutines.
//
// Probing happens here rather than in NewParser because the kitty query is
// answered on the input stream and is meaningless before raw mode is entered:
// the caller enters raw mode first, then constructs a Source.
func NewSource(t termmosaic.Terminal, cfg Config) *Source {
	s := &Source{
		t:          t,
		cfg:        cfg,
		p:          NewParser(cfg),
		events:     make(chan termmosaic.Event, cfg.eventQueue()),
		kittyFlags: make(chan uint8, 1),
		kittyReply: make(chan struct{}),
		done:       make(chan struct{}),
		eof:        make(chan struct{}),
		escWake:    make(chan struct{}, 1),
	}
	s.p.onKittyFlags = s.onFlags

	// A terminal that refuses an enable sequence is not a terminal we can drive,
	// but it is still a terminal whose keys we can read, so a failed write is not
	// worth refusing to start over: the decoder recognises every one of these
	// modes unconditionally, so the worst outcome is that the feature silently
	// does not arrive. It is not returned because NewSource has no error to
	// return and the alternative — refusing to start — is worse than a missing
	// optional mode.
	_ = s.enterModes()

	// The reader is deliberately NOT in the WaitGroup: Close cannot wait for it,
	// because Terminal.Read cannot be interrupted. It still closes the stream
	// when it finally exits, so an EOF is still reported.
	go s.readLoop()

	s.wg.Add(1)
	go s.resizeLoop()

	if s.cfg.ProbeKitty && s.cfg.WriteProbe != nil {
		s.wg.Add(1)
		go s.negotiateKitty()
	}
	// The supervisor is deliberately not in the WaitGroup: it is the thing that
	// WAITS, so counting it would deadlock.
	go s.supervise()
	return s
}

// Events yields decoded events in order. It is closed when the Source is closed
// or the terminal reaches EOF.
func (s *Source) Events() <-chan termmosaic.Event { return s.events }

// KittyFlags yields the negotiated kitty keyboard flags exactly once, when the
// terminal answers Config.WriteProbe's query.
//
// If the terminal never answers — the common case, since almost none of them
// implement the protocol — NO value is ever sent and the channel stays open. That
// is deliberate rather than an oversight: "no value" and "flag set 0" are
// different answers, and collapsing them would mean a terminal reporting an empty
// flag set looks identical to a terminal that is not there. So the portable way to
// use it is with a timeout, and a timeout means "no kitty".
func (s *Source) KittyFlags() <-chan uint8 { return s.kittyFlags }

// KittyActive reports whether the disambiguate flag was successfully pushed.
func (s *Source) KittyActive() bool { return s.kittyOn.Load() }

// Parser exposes the underlying parser so an application can read Stats and call
// Reset without holding its own reference.
func (s *Source) Parser() *Parser { return s.p }

// enterModes writes the terminal-mode enable sequences cfg asks for.
//
// Every one of them is off by default except bracketed paste. Mouse capture is
// the load-bearing omission: enabling it takes text selection, middle-click
// paste and scrollback copying away from the user's shell, with no terminal-side
// indication of why, and a framework that does that by default breaks a thing
// users rely on and did not ask to lose.
func (s *Source) enterModes() error {
	w := s.cfg.WriteProbe
	if w == nil {
		return nil
	}
	if s.cfg.BracketedPaste {
		if err := w(seqEnableBracketedPaste); err != nil {
			return err
		}
	}
	if s.cfg.EnableFocusReporting {
		if err := w(seqEnableFocus); err != nil {
			return err
		}
	}
	switch s.cfg.MouseMode {
	case MouseClick:
		return w(seqEnableMouseClick)
	case MouseDrag:
		return w(seqEnableMouseDrag)
	case MouseAll:
		return w(seqEnableMouseAll)
	}
	return nil
}

// leaveModes undoes enterModes. It runs from Close before the goroutines are
// stopped, so the terminal is never left in a mode this program turned on.
//
// This is written here rather than left to Terminal.Close because ADR 0005 §6
// assumed Terminal.Close already restores modes, and it does not: Close restores
// raw mode and the alternate screen, which are the modes it turned on itself.
func (s *Source) leaveModes() {
	w := s.cfg.WriteProbe
	if w == nil || !s.modesOff.CompareAndSwap(false, true) {
		return
	}
	switch s.cfg.MouseMode {
	case MouseClick:
		_ = w(seqDisableMouseClick)
	case MouseDrag:
		_ = w(seqDisableMouseDrag)
	case MouseAll:
		_ = w(seqDisableMouseAll)
	}
	if s.cfg.EnableFocusReporting {
		_ = w(seqDisableFocus)
	}
	if s.cfg.BracketedPaste {
		_ = w(seqDisableBracketedPaste)
	}
	if s.kittyOn.Load() {
		// Release every flag we pushed: CSI ? 0 u.
		_ = w(seqKittyReset)
	}
}

// readLoop decodes input bytes and delivers the events.
//
// It owns eof: closing it is how the Source learns the terminal is finished,
// because a terminal that has gone away says so through Read returning an error
// and through nothing else.
func (s *Source) readLoop() {
	defer close(s.eof)
	defer s.stop()
	defer s.finish()

	buf := make([]byte, readChunkSize)
	// One event slice reused for every read, so a keystroke does not allocate.
	//
	// Its capacity is readChunkSize, not an arbitrary 64, and that is a
	// correctness requirement rather than a tuning choice. Feed takes the
	// caller's slice by value and returns only a COUNT, so a caller that cannot
	// see the caller's own reallocation would silently lose every event past the
	// capacity. One read of n bytes can produce at most n events — every event
	// consumes at least one byte, and a paste is many bytes for one event — so a
	// batch of the read's size can never overflow.
	batch := make([]termmosaic.Event, 0, readChunkSize)

	for {
		n, err := s.t.Read(buf)
		if n > 0 {
			// One read's events go out together. Holding feedMu across them is
			// what makes "a resize never interrupts the events decoded from one
			// read" true rather than merely likely: without it the resize
			// watcher could slip a resize between a mouse report and the letter
			// that arrived in the same read, and the widget would act on a click
			// against the wrong coordinate space.
			s.feedMu.Lock()
			added := s.p.Feed(batch[:0], buf[:n])
			for _, ev := range batch[:added] {
				if !s.deliver(ev) {
					s.feedMu.Unlock()
					return
				}
			}
			s.feedMu.Unlock()
			s.armEscape()
		}
		if err != nil {
			// io.EOF is the documented end of a Terminal's input. Any other
			// error is terminal-specific and unhandled; ending the stream beats
			// spinning on an error nobody is handling, and the consumer sees a
			// closed channel either way.
			return
		}
	}
}

// deliver sends ev, reporting false when the Source is closing. It never drops:
// a full queue blocks the read, which is the backpressure the design requires.
//
// The read lock is what makes it safe for the event channel to be closed while a
// reader that has come out of a blocked Read still intends to deliver: such a
// sender waits, then finds closed set, and returns without touching the channel.
// A sender already inside the select is released by the done case, so it cannot
// hold the read lock open forever.
func (s *Source) deliver(ev termmosaic.Event) bool {
	s.sendMu.RLock()
	defer s.sendMu.RUnlock()
	if s.closed.Load() {
		return false
	}
	select {
	case s.events <- ev:
		return true
	case <-s.done:
		return false
	}
}

// resizeLoop turns resize notifications into events.
//
// The send is BLOCKING, so a resize is never dropped either. Coalescing happens
// one layer down, in the transport: term's SIGWINCH watcher is keep-latest, so a
// consumer that has fallen behind gets the newest size rather than a queue of
// stale ones, which is the inversion ADR 0005 §5 forced.
func (s *Source) resizeLoop() {
	defer s.wg.Done()
	var batch [1]termmosaic.Event
	for {
		select {
		case <-s.done:
			return
		case sz, ok := <-s.t.ResizeEvents():
			if !ok {
				return
			}
			s.feedMu.Lock()
			added := s.p.Push(batch[:0], termmosaic.ResizeEvent(sz.W, sz.H))
			for _, ev := range batch[:added] {
				if !s.deliver(ev) {
					s.feedMu.Unlock()
					return
				}
			}
			s.feedMu.Unlock()
		}
	}
}

// onFlags publishes a kitty flags reply. It is called from the read goroutine,
// at most once per reply.
func (s *Source) onFlags(flags uint8) {
	select {
	case s.kittyFlags <- flags:
	default:
		// Already published. A second reply cannot happen in one session, and
		// blocking here would stall the read path on a protocol curiosity.
	}
	select {
	case <-s.kittyReply:
	default:
		close(s.kittyReply)
	}
}

// negotiateKitty runs the kitty keyboard handshake.
//
// The query is answered on the input stream, so the reader goroutine has to be
// running before the answer can arrive — which is why this is a goroutine rather
// than something NewSource does inline. The timeout is HARD at 100 ms: startup
// must never block on a terminal that ignores the query.
func (s *Source) negotiateKitty() {
	defer s.wg.Done()
	if err := s.cfg.WriteProbe(seqKittyQuery); err != nil {
		return
	}
	timer := time.NewTimer(KittyProbeTimeout)
	defer timer.Stop()
	select {
	case <-s.kittyReply:
	case <-timer.C:
		return
	case <-s.done:
		return
	}

	// The set pushed is what the application ASKED for, not what the terminal
	// reported: the reply tells us the protocol exists, not which flags are safe.
	flags := s.cfg.kittyFlags()
	var buf [kittyPushBuf]byte
	n := appendKittyPush(buf[:0], flags)
	// A terminal that answered the query understands the push; one that did not
	// never gets here, because there is no reply to wait for.
	if s.cfg.WriteProbe(n) != nil {
		return
	}
	s.kittyOn.Store(true)
}

// appendKittyPush appends "CSI > <flags> u" to dst.
func appendKittyPush(dst []byte, flags uint8) []byte {
	dst = append(dst, "\x1b[>"...)
	if flags >= 100 {
		dst = append(dst, byte('0'+flags/100))
	}
	if flags >= 10 {
		dst = append(dst, byte('0'+(flags/10)%10))
	}
	dst = append(dst, byte('0'+flags%10))
	return append(dst, 'u')
}

// armEscape starts the escape-delay goroutine if a bare ESC is waiting and no
// such goroutine is already running.
//
// The goroutine is woken rather than left to sleep out a delay that these bytes
// may have just invalidated: if the pending ESC has become half an arrow, letting
// it sleep first costs latency but no correctness, and correctness is what the
// re-check inside escapeLoop is for.
func (s *Source) armEscape() {
	select {
	case s.escWake <- struct{}{}:
	default:
	}
	if !s.p.EscapePending() {
		return
	}
	s.escMu.Lock()
	if s.escRunning {
		s.escMu.Unlock()
		return
	}
	s.escRunning = true
	s.escMu.Unlock()

	s.wg.Add(1)
	go s.escapeLoop()
}

// escapeLoop waits out the escape delay and emits KeyEscape if the bare ESC is
// still alone when it expires.
//
// It re-checks on every wake rather than sleeping once through, because the
// answer to "is it still a bare ESC" can change before the delay runs out, and
// firing KeyEscape into the middle of a sequence that has since arrived would
// turn a resolution into a lost keystroke.
func (s *Source) escapeLoop() {
	defer s.wg.Done()
	defer func() {
		s.escMu.Lock()
		s.escRunning = false
		s.escMu.Unlock()
	}()

	for {
		remaining, pending := s.p.escapeRemaining()
		if !pending {
			return
		}
		if remaining > 0 {
			timer := time.NewTimer(remaining)
			select {
			case <-timer.C:
			case <-s.escWake:
				timer.Stop()
				continue
			case <-s.done:
				timer.Stop()
				return
			}
		}
		ev, ok := s.p.expireEscape()
		if !ok {
			return
		}
		if !s.deliver(ev) {
			return
		}
		return
	}
}

// Close stops the goroutines and restores the terminal modes this Source turned
// on. It is idempotent, and no event is delivered after it returns: the event
// channel is closed only once no sender can still be inside it.
//
// Close does NOT close the underlying Terminal: the caller owns that, and a
// Source that closed a terminal it was merely reading would make the two
// impossible to use together.
//
// It also does NOT wait for the reader, and that is a deliberate consequence of
// not widening Terminal rather than an oversight. Terminal.Read blocks, and
// there is no way to interrupt it without a method on Terminal that ADR 0005
// explicitly declines to add — so on a live tty the reader is blocked in a
// syscall that will not return until the terminal itself is closed or produces
// input. A Close that waited for it would never return, which is a far worse bug
// than an abandoned goroutine. The reader notices the closed channel the next
// time it tries to deliver and exits; it never touches the channel afterwards.
// Every other goroutine IS waited for, because all of them select on done.
func (s *Source) Close() error {
	s.stop()
	s.wg.Wait()
	s.finish()
	return nil
}

// stop signals every goroutine to finish and restores the terminal modes. It
// runs exactly once whether it came from Close or from the terminal reaching EOF.
func (s *Source) stop() {
	s.closeOnce.Do(func() {
		s.leaveModes()
		close(s.done)
	})
}

// finish closes the event channel, excluding every sender.
func (s *Source) finish() {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.eventsOnce.Do(func() {
		s.closed.Store(true)
		close(s.events)
	})
}

// supervise ends the stream when the terminal reaches EOF, so a consumer ranging
// over Events sees a closed channel without having to call Close.
func (s *Source) supervise() {
	select {
	case <-s.eof:
	case <-s.done:
		return
	}
	s.stop()
	s.finish()
}
