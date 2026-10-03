package input

import (
	"sync/atomic"
	"time"

	"github.com/serkanalgur/termmosaic"
)

// Stats counts decoding outcomes an application may want to log. It is
// diagnostic surface, not control flow.
type Stats struct {
	// Malformed counts non-paste sequences discarded after exceeding
	// maxSequenceLength or failing validation.
	Malformed uint64
	// PastesTruncated counts bracketed pastes whose payload exceeded
	// Config.MaxPasteBytes.
	PastesTruncated uint64
}

// Parser drives Decode over a byte stream that arrives in arbitrary chunks.
//
// It holds only the state that genuinely cannot be avoided: the bytes of a
// sequence that arrived incomplete, the accumulation of a bracketed paste, and
// the escape deadline. That last one is the honest reason the design is two
// layers and not one — Decode([]byte{0x1b}) cannot know whether it is the Escape
// key or the first byte of an arrow key, and answering that needs time, not
// bytes. Putting a timer inside Decode would make the pure function impure.
//
// The zero value is not usable; call NewParser. A Parser is not safe for
// concurrent use.
type Parser struct {
	cfg Config

	// partial holds a sequence that Decode reported as incomplete. It is
	// bounded by maxSequenceLength, so it never needs to grow past that.
	partial []byte
	// paste holds an in-progress bracketed-paste payload and is reused across
	// pastes, so a steady state that pastes repeatedly allocates nothing.
	paste []byte
	// inPaste records that a start marker has been seen and its end marker has
	// not.
	inPaste bool
	// pasteTruncated records that the current paste already exceeded the cap.
	pasteTruncated bool

	// base is the parser's time origin, so the escape deadline is a plain int64 of
	// nanoseconds since construction rather than a time.Time. It is an atomic
	// because the reader goroutine writes it while the escape-delay goroutine
	// reads it, and a Parser is otherwise documented as single-goroutine.
	escPending atomic.Bool
	escAt      atomic.Int64

	// base and now are the clock: base is the time origin and now is injectable,
	// which is what lets the escape deadline be tested without sleeping.
	base time.Time
	now  func() time.Time

	// onKittyFlags, when non-nil, is called once with the flags a CSI ? u
	// reply carried. Source uses it to publish the negotiation.
	onKittyFlags func(uint8)

	stats Stats
}

// NewParser returns a Parser configured by cfg.
//
// The Parser decodes bracketed paste, focus, mouse and the kitty keyboard
// replies regardless of what cfg asks for: those flags gate which enable
// sequences Source writes to the terminal, never what the decoder recognises. A
// terminal that sends them anyway is decoded correctly, which is the whole
// reason mouse is decoded even though mouse capture is off by default.
func NewParser(cfg Config) *Parser {
	return &Parser{
		cfg:     cfg,
		partial: make([]byte, 0, maxSequenceLength),
		paste:   make([]byte, 0, 4096),
		base:    time.Now(),
		now:     time.Now,
	}
}

// Feed consumes b and appends every decoded event to dst, returning the number
// appended.
//
// dst is taken BY VALUE and only a count comes back, so a caller must size dst to
// hold one event per byte of b, or Feed will grow it internally and the caller
// will lose track of where the events are. One byte always produces at most one
// event, so len(b) of capacity is always enough. Source sizes its batch to its
// read buffer for exactly this reason.
//
// Any trailing bytes that form an incomplete sequence are retained for the next
// call, and a paste body that has not reached its closing marker is accumulated
// and reported when it does. Neither case is an error: a sequence split across
// two read(2) calls is the normal case, not a bug, and StatusIncomplete is what
// Decode returns so that it is a value rather than an exception.
//
// dst is the caller's slice and growing it is the caller's allocation, not the
// parser's. Reusing one dst across many calls keeps a Source's whole decode path
// allocation-free outside pastes.
func (p *Parser) Feed(dst []termmosaic.Event, b []byte) int {
	seq := b
	if len(p.partial) > 0 {
		// Prepend the retained bytes. p.partial's capacity is bounded, so this
		// append reallocates at most once and never in the steady state.
		p.partial = append(p.partial, b...)
		seq = p.partial
	}

	// Every Feed re-evaluates the escape wait from scratch. A pending bare ESC
	// survives only if THIS call left one pending; if these bytes resolved it
	// into a sequence, or into a chord, or into nothing at all, the wait is over.
	// Carrying a stale deadline forward instead would fire a KeyEscape into the
	// middle of the sequence that resolved it.
	p.escPending.Store(false)

	added := 0
	// The loop label matters: every `break` that ends the loop is in a switch,
	// and a bare break there would break the switch and re-enter the loop with
	// the same bytes — an infinite loop rather than a wrong answer.
loop:
	for len(seq) > 0 {
		// A paste body is not a sequence, so it is consumed here rather than
		// through Decode: Decode cannot do it without rescanning the whole
		// accumulated body on every chunk, which is quadratic in the paste
		// size. The marker recognition is shared with Decode so the two paths
		// cannot disagree about what a paste is.
		if p.inPaste {
			stop := pasteEnd(seq)
			if stop < 0 {
				p.appendPaste(seq)
				break
			}
			p.appendPaste(seq[:stop])
			ev := p.takePaste()
			dst = append(dst, ev)
			added++
			seq = seq[stop+len(pasteStop):]
			continue
		}

		var flags uint8
		ev, n, st := decode(seq, p.cfg, &flags)

		switch st {
		case StatusOK:
			if p.onKittyFlags != nil && flags != 0 {
				p.onKittyFlags(flags)
			}
			// An empty-start paste marker that arrived whole is handled by
			// Decode; one that did not is StatusIncomplete and falls through
			// to the paste branch below on the next iteration.
			if ev.Kind != termmosaic.EventNone {
				if ev.Kind == termmosaic.EventPaste && ev.Truncated {
					p.stats.PastesTruncated++
				}
				dst = append(dst, ev)
				added++
			}
			if n <= 0 {
				// A consumed-but-eventless sequence. Nothing more can be
				// decoded from the bytes we were given.
				break loop
			}
			seq = seq[n:]

		case StatusIncomplete:
			if start := pasteStartLen(seq); start > 0 {
				// The paste is open and the closing marker has not arrived.
				p.beginPaste()
				p.appendPaste(seq[start:])
				break loop
			}
			if len(seq) == 1 && seq[0] == escByte {
				// A bare ESC. Without the wait it is unambiguously the Escape
				// key; with it, the deadline decides.
				if p.cfg.escapeDelay() <= 0 {
					dst = append(dst, termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0))
					added++
					seq = seq[1:]
					continue
				}
				p.escPending.Store(true)
				p.escAt.Store(p.now().Sub(p.base).Nanoseconds())
			}
			break loop

		case StatusInvalid:
			p.stats.Malformed++
			if n <= 0 {
				n = 1
			}
			seq = seq[n:]
		}
	}

	p.storePartial(seq)
	return added
}

// storePartial retains the bytes of an unfinished sequence, bounded by
// maxSequenceLength.
//
// The bound is not reachable through Decode, which already refuses a runaway, but
// it is enforced here too because the retained prefix is memory held ACROSS calls
// and an unbounded retention would be a slow leak rather than a fast one.
func (p *Parser) storePartial(seq []byte) {
	p.partial = p.partial[:0]
	if p.inPaste {
		// Mid-paste the retained bytes are payload, not a partial sequence; the
		// paste accumulator owns them.
		return
	}
	if len(seq) == 0 {
		return
	}
	if len(seq) > maxSequenceLength {
		p.stats.Malformed++
		return
	}
	p.partial = append(p.partial, seq...)
}

// beginPaste opens a bracketed paste.
func (p *Parser) beginPaste() {
	p.inPaste = true
	p.pasteTruncated = false
	p.paste = p.paste[:0]
}

// appendPaste adds payload to the in-progress paste, stopping at
// Config.MaxPasteBytes.
//
// Bytes past the cap are discarded but the paste stays OPEN, so scanning
// continues to the closing marker. Discarding-and-stopping would leave the
// decoder out of sync with the terminal for every subsequent byte, which is a
// far worse failure than a short paste.
func (p *Parser) appendPaste(b []byte) {
	room := p.cfg.maxPasteBytes() - len(p.paste)
	if room <= 0 {
		p.pasteTruncated = true
		return
	}
	if len(b) > room {
		b = b[:room]
		p.pasteTruncated = true
	}
	p.paste = append(p.paste, b...)
}

// takePaste closes the in-progress paste and returns its single event.
func (p *Parser) takePaste() termmosaic.Event {
	ev := pasteEvent(p.paste, p.pasteTruncated)
	if p.pasteTruncated {
		p.stats.PastesTruncated++
	}
	p.inPaste = false
	p.pasteTruncated = false
	// The accumulator's storage is kept for the next paste rather than dropped:
	// dropping it would make every paste allocate from scratch, and the ADR's
	// "1+ allocations per paste" allowance is about the unavoidable string copy.
	p.paste = p.paste[:0]
	return ev
}

// Push appends an event the parser did not decode from bytes — today only a
// resize — to dst, returning the number appended.
//
// It exists so that a Source can present one ordered stream without the parser
// needing to know what a resize is. It is deliberately not a way to inject
// arbitrary events: the parser's job is to be the only thing that turns bytes
// into events.
func (p *Parser) Push(dst []termmosaic.Event, ev termmosaic.Event) int {
	dst = append(dst, ev)
	// One event appended, not the new length: a caller that already had two
	// events in dst must still be told "one", or every resize would look like a
	// batch of resizes.
	return 1
}

// EscapePending reports whether p is holding exactly a bare ESC and is waiting
// out Config.EscapeDelay before deciding it was the Escape key.
//
// This is how a Source knows to arm a timer without knowing anything about the
// escape-delay policy.
func (p *Parser) EscapePending() bool { return p.escPending.Load() }

// escapeRemaining reports how long is left of the escape delay, and whether one
// is running at all. It is negative once the delay has expired.
func (p *Parser) escapeRemaining() (time.Duration, bool) {
	if !p.escPending.Load() {
		return 0, false
	}
	elapsed := time.Duration(p.now().Sub(p.base).Nanoseconds() - p.escAt.Load())
	return p.cfg.escapeDelay() - elapsed, true
}

// expireEscape closes out a bare-ESC wait and returns the KeyEscape event.
//
// It reports false when no ESC was pending, which is the ordinary outcome of a
// wait that was resolved by more bytes arriving: the parser is now mid-sequence
// and must not be interrupted, because discarding a partial sequence would turn
// a resize or a timer into a lost keystroke.
func (p *Parser) expireEscape() (termmosaic.Event, bool) {
	remaining, pending := p.escapeRemaining()
	if !pending || remaining > 0 {
		return termmosaic.Event{}, false
	}
	p.escPending.Store(false)
	p.escAt.Store(0)
	p.partial = p.partial[:0]
	return termmosaic.SpecialKeyEvent(termmosaic.KeyEscape, 0), true
}

// Reset drops any partial sequence or in-progress paste.
//
// It is called on raw-mode entry so that a half-read sequence from before the
// mode change cannot leak into the new mode: a stale ESC would otherwise be
// reported as a key press the user never made, minutes after the fact.
func (p *Parser) Reset() {
	p.partial = p.partial[:0]
	p.paste = p.paste[:0]
	p.inPaste = false
	p.pasteTruncated = false
	p.escPending.Store(false)
	p.escAt.Store(0)
}

// Stats reports counters for sequences discarded as malformed or truncated, so
// an application can log input corruption rather than silently lose it.
func (p *Parser) Stats() Stats { return p.stats }
