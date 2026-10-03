# 0005 — Input decoding

- **Status:** Accepted
- **Date:** 2026-10-04
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / Input decoding
- **Depends on:** [ADR 0001](0001-backend-strategy.md),
  [ADR 0003](0003-renderer-mode.md)
- **Answers:** the input-decoding gap named in the `term` package documentation
  and in `examples/hello/main.go`.

## Context

`Terminal.Read` returns raw bytes. Nothing in the repository turns those bytes
into a `termmosaic.Event`, and `examples/hello/main.go` scans its own buffer for
`'q'`:

```go
// Input: TermMosaic has no input decoder yet — see the term package
// documentation — so this example scans raw bytes for a quit key itself.
// That is a documented gap, not a demonstration of the intended API.
for i := 0; i < n; i++ {
    switch b[i] {
    case 'q', 'Q', 0x03, 0x1b: // q, Q, Ctrl-C, Esc
```

This is the last major unbuilt subsystem. Every form widget in the catalog
(TextInput, TextArea, Select, Checkbox, Radio, Tabs) and every List/Table/Pager
keybinding is written against this model, so changing it after thirty widgets
exist is expensive — hence deciding it now rather than when the first widget
forces the issue.

The existing code constrains the design more than it might appear, and the
constraints are worth stating first because several of them are already
load-bearing:

- **`Event` is already a tagged struct union**, not an interface, and its
  doc comment gives the reason: *"events are allocated per keystroke and an
  interface here would mean a heap allocation on the input path for no
  benefit."* `EventKind` already reserves `EventResize`, `EventMouse`,
  `EventPaste`, `EventFocus`, and `Event.Text` is already the paste payload.
  The union is therefore mostly right, and this ADR extends it rather than
  replacing it.
- **`Widget.Handle(Event) bool`** ([ADR 0003](0003-renderer-mode.md)) takes one
  concrete struct. Changing to an interface or to a generic is a breaking change
  to every widget that does not exist yet — cheap now, expensive later.
- **`Key` is a contiguous `iota` block with a `keyNames` map.** Appending
  `KeyF13` after `KeyF12` is additive; inserting one in the middle silently
  renumbers every later constant. Any new key is appended at the end.
- **`Caps` already has `Mouse`, `KittyKeyboard`, `BracketedPaste`**, and
  `DefaultCaps()` sets all three optimistically. `term/caps.go` explicitly
  notes that mouse and bracketed paste "are not detectable from the
  environment" and that the kitty flag is a `TERM` heuristic.
- **The ANSI encoder is allocation-free and appends to caller storage**
  (`internal/ansi`), and the diff holds a 0 allocs/op bar from
  [ADR 0002](0002-buffer-representation.md). The decoder sits on the same
  critical-path philosophy and should be held to a comparable bar.
- **`internal/ansi` is output-only.** It has no input-side vocabulary — no CSI
  parameter parsing, no final-byte dispatch. A decoder cannot be built by
  mirroring the encoder; it needs its own scanner.
- **No benchmark informed this decision.** Input decoding is I/O-bound in
  practice: a keystroke costs a `read(2)`, which is microseconds, while a frame
  budget is 16 ms. Unlike ADRs 0001 and 0002 there is nothing to measure, and
  this ADR says so rather than implying evidence it does not have. The one
  number that matters is the allocation count on the key path, and that is a
  property we specify and test rather than measure today.

### What the state of the art actually looks like

OpenTUI owns raw mode, alt screen, mouse, bracketed paste, focus tracking and
kitty keyboard, and has **no IME support at all** ([ADR 0001](0001-backend-strategy.md)).
There is no reference implementation to port for the one part of this problem
that is genuinely open (composition), which is why the IME scope below is
decided by argument rather than by evidence.

## Options considered

### Where the parser lives

- **Inline in `Terminal`.** `Terminal.Read` would return `Event` values.
  Rejected: it makes `Read` untestable without a tty, breaks the headless
  terminal's byte-level `Feed`, and puts a state machine in the one package
  whose I/O path is already documented as unverified by tests
  (`term/term.go`: *"nothing in this package's I/O path is exercised by the test
  suite"*). The worst possible place for the code we most need to test.
- **A goroutine plus a channel, inside `Terminal`.** Rejected for the same
  reason: it puts decoding behind a channel in the transport layer, so a test
  cannot get an event without starting a goroutine, and `Terminal` grows a
  lifecycle it does not have today.
- **A dedicated `input` package.** Rejected nothing; chosen.

### Pure function versus stateful parser

- **One stateful `Parser` with a `Next()` method that pulls from an
  `io.Reader`.** Rejected. It entangles the state machine with blocking I/O, so
  testing a split escape sequence requires a pipe and a goroutine, and the
  "one byte at a time" case — the single most valuable test in the whole
  subsystem — becomes awkward rather than trivial.
- **A pure function over a byte slice returning `(Event, consumed int, status)`.**
  Adopted, for the whole reason above: *every* input bug is a function of the
  bytes, so a pure decoder is exhaustively testable with no I/O, no goroutine,
  and no terminal. Splitting a sequence across `read(2)` boundaries becomes
  `Decode` returning `StatusIncomplete`, which is a one-line test rather than a
  flaky timing test.

Pure alone is not enough, though, and pretending otherwise would be the design
error. Some state is genuinely unavoidable: the bytes of a sequence that
arrived incomplete, and the accumulation of a bracketed paste. So the shape is
**two layers, with the purity in the layer that has the logic**:

- `Decode` — pure. No retained state, no clock, no allocation on the key path.
  Takes the whole configuration by value.
- `Parser` — a resumable driver holding only the unavoidable bytes, plus the
  escape-delay deadline, which lives here and *not* in `Decode` so that
  `Decode` needs no clock.

The deadline is the honest example of why the split is two layers and not one:
`Decode(["\x1b"])` cannot know whether that is the Escape key or the first byte
of an arrow key, and answering that question requires time, not bytes. Putting
the timer inside `Decode` would make the pure function impure; putting it in
`Parser` keeps `Decode` total and the policy in one auditable place.

## Decision

### 1. A new `input` package; a pure decoder under a resumable driver

```go
package input

// Status is the outcome of decoding one sequence from the front of seq.
type Status int

const (
    // StatusOK means an event was produced and n bytes were consumed.
    StatusOK Status = iota
    // StatusIncomplete means seq holds a proper prefix of a sequence and more
    // bytes are needed. No event is produced and the caller must retain every
    // byte it was given.
    StatusIncomplete
    // StatusInvalid means the bytes cannot be a valid sequence. The consumed
    // bytes are discarded and the caller resumes at the first unconsumed byte.
    StatusInvalid
)

// Decode decodes exactly one input sequence from the front of seq.
//
// Decode is pure: it reads seq and returns values. It does not block, does not
// consult a clock, does not retain seq, and — on the key path — does not
// allocate. Config is passed by value.
//
// The three statuses are the whole contract, and StatusIncomplete is the
// important one: it is what makes a sequence that straddles two reads a
// return value rather than a bug.
func Decode(seq []byte, cfg Config) (termmosaic.Event, int, Status)

// Parser drives Decode over a byte stream that arrives in arbitrary chunks.
//
// The zero value is not usable; call NewParser. A Parser is not safe for
// concurrent use.
type Parser struct { /* unexported */ }

func NewParser(cfg Config) *Parser

// Feed consumes b and appends every decoded event to dst, returning the number
// appended. Any trailing bytes that form an incomplete sequence are retained
// for the next call.
func (p *Parser) Feed(dst []termmosaic.Event, b []byte) int

// Push appends an event the parser did not decode from bytes — today only a
// resize — to dst. It exists so that a Source can present one ordered stream
// without the parser needing to know what a resize is.
func (p *Parser) Push(dst []termmosaic.Event, ev termmosaic.Event) int

// EscapePending reports whether p is holding exactly a bare ESC and is waiting
// out Config.EscapeDelay before deciding it was the Escape key.
func (p *Parser) EscapePending() bool

// Reset drops any partial sequence or in-progress paste. Called on raw-mode
// entry so a half-read sequence from before the mode change cannot leak into
// the new mode.
func (p *Parser) Reset()

// Stats reports counters for sequences discarded as malformed or truncated,
// so an application can log input corruption rather than silently lose it.
func (p *Parser) Stats() Stats

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
```

**Backpressure.** `Source.Events()` is a buffered channel, 256 by default
(`Config.EventQueue`). When it is full the reader goroutine **stops reading the
tty**. It does not drop, coalesce, or reorder key, mouse, or paste events. The
failure mode of a full queue is latency, and latency is recoverable; the
failure mode of a silently dropped keystroke is a text input that loses a
character. This is the same reasoning `term/terminal_unix.go:231` already
applies to resize notifications — *"a stale size is worth less than the latest
one, so drop this notification"* — extended to input, where the rule is
stronger because input is not replaceable.

**Partial-sequence buffering** lives in `Parser` and is bounded by
`maxSequenceLength = 64` bytes. A paste body is *not* a sequence and is not
subject to that bound; see §4.

**Overflow, specified rather than left to the implementation.**

- A non-paste sequence that exceeds 64 bytes without reaching a final byte is
  declared `StatusInvalid`, its bytes are discarded, `Stats.Malformed` is
  incremented, and scanning resumes at the first byte after the discarded run.
  We do **not** attempt a resynchronisation search for a plausible introducer:
  a 64-byte runaway is almost always a stream the terminal is not really
  sending, and hunting for a `[` inside it can manufacture events from noise.
- A bracketed paste whose payload exceeds `Config.MaxPasteBytes` (default 4 MiB)
  keeps the **first** `MaxPasteBytes`, sets `Event.Truncated`, and keeps
  scanning to the closing `ESC [ 201 ~`. Discarding-and-stopping would leave us
  out of sync with the terminal for every subsequent byte, which is a far worse
  failure than a short paste.

### 2. Key encoding: CSI, SS3, legacy modifiers, and kitty keyboard in v1

**In scope for v1:** printable UTF-8; C0 control characters; `CSI` sequences
covering arrows, Home, End, PgUp, PgDn, Delete, Insert and F1–F12; `SS3`
(`ESC O <final>`), which is how F1–F4 and, on some terminals, Home/End arrive;
the Linux console's `ESC [ [ A`–`E` form; legacy xterm modifier encoding
(`CSI 1 ; <mod> <final>`); and the kitty keyboard protocol.

`KeyF13`–`KeyF35` are **deferred** — see §10.

**Kitty keyboard protocol is IN v1, with progressive enhancement.** This is the
one place we deliberately take on protocol work ahead of demand, and the reason
is that `Caps.KittyKeyboard` and the `Key` doc comment already promise it:

> *"TermMosaic supports the kitty keyboard protocol when the terminal
> advertises it (Caps.KittyKeyboard), which is how keys like Hyper and Super
> and unambiguous Ctrl+Shift+letter arrive."*

Promises in doc comments are API. Shipping the decoder without it would leave
that comment lying.

**The handshake, precisely.**

1. The application enters raw mode, then constructs a `Source`. Probing happens
   in `Source`, after raw mode, because the query is answered on the input
   stream and is meaningless without it.
2. `Source` writes `CSI ? u` (query current flags) through `Config.WriteProbe`,
   if that function is non-nil. `Config.WriteProbe` exists because `Terminal`
   has no write method and **we are not widening `Terminal`** — [ADR 0001](0001-backend-strategy.md)
   chose two narrow interfaces and a third method on one of them would reopen
   it. An application wires it to its own `Sink`.
3. The reply `CSI ? <flags> u` arrives on the input stream and is decoded by the
   parser like any other CSI sequence; the parser recognises it, does **not**
   emit it as a key, and publishes the flags once on
   `Source.KittyFlags() <-chan uint8`.
4. **Timeout: 100 ms, hard.** On timeout `Source` proceeds with no
   enhancement. Startup must never block on a terminal that ignores the query,
   and 100 ms is well inside what a user perceives as instant for a terminal
   that will never answer.
5. If flags came back, `Source` writes `CSI > 1 u` — **disambiguate escape
   codes only** — and sets `Source.KittyActive()`.

**Why request only flag bit 0 (`0b1`, disambiguate).** The kitty flags are
independent and we could ask for more:

| Flag | Value | Requested? | Reason |
|---|---|---|---|
| Disambiguate escape codes | `0b1` | **yes** | The whole point: makes Ctrl+I / Ctrl+M, and Ctrl+letter combinations, unambiguous. Costs nothing in event volume. |
| Report event types | `0b10` | no by default | Lets us distinguish press/repeat/release. It doubles or triples event volume, and **no widget in the catalog consumes a key release**. |
| Report alternate keys | `0b100` | no | Produces shifted-symbol variants we do not use. |
| Report all keys as escape codes | `0b1000` | no | Would break plain-text key input; the flag exists for image protocols. |
| Report associated text | `0b10000` | no by default | Gives the text a key would have produced, which is the correct input for a text widget — but it changes what `Rune` means, and `TextInput` is not written yet. |

`Config.KittyFlags` lets an application request more; the default is `0b1`.
`Source.KittyActive()` and `Config.KittyFlags` are the whole surface.

**Release events are decoded but dropped by default.** With
`ReportEventTypes` requested, `Event.Type` distinguishes `KeyPress`,
`KeyRepeat` and `KeyRelease`. A release is not emitted unless
`Config.KittyReportReleases` is set. Same reasoning as the flags table: we carry
the information because the protocol gives it to us cheaply, and we suppress it
because nothing consumes it.

**`Caps.KittyKeyboard` semantics change**, and this is a forced code change: it
means *"this terminal may support the protocol"* (the `TERM` heuristic), not
*"the protocol is active"*. Negotiation state lives on `Source`, because it is
a property of a running session rather than of a device. See §11.

**Legacy xterm modifiers.** `CSI 1 ; <mod> <final>`, where `mod - 1` is a
bitmask (shift 1, alt 2, ctrl 4, meta 8). Note the difference from kitty's
modifier parameter, which is a bitmap in the same layout as `KeyMod` plus more
bits. `Decode` handles both encodings and both are documented, because a
terminal that ignores our kitty request will use the legacy one and we cannot
choose which we get.

**`KeyMod` is 4 bits and kitty has 8 modifier bits.** kitty also encodes
hyper (16), meta (32), caps-lock (64) and num-lock (128). Those are decoded
and **dropped**, because `KeyMod` is `uint8` with four defined bits and widening
it is a change to every `Mod` consumer. Dropping caps-lock is arguably wrong;
it is harmless in practice and recorded as an accepted limitation rather than
engineered around.

**Backspace reconciliation**, which `event.go:68` explicitly left open:

> *"Note that a terminal may deliver it as a control character rather than as
> an escape sequence, depending on the terminfo setting; the input decoder will
> have to reconcile that."*

Decision: `0x7f` and `0x08` both decode to `KeyBackspace`. `Config.BackspaceByte`
can pin it to one (`BackspaceDelete` / `BackspaceControl`); the default accepts
both. Guessing one is how users end up with a TextInput whose backspace does
nothing on their terminal and works on the developer's.

**Escape ambiguity and Alt chords.** A bare `ESC` is either the Escape key or
the first byte of a sequence, and `ESC` followed by a printable rune is an
Alt-chord. The rule:

- If `ESC` is followed by more bytes within the **same `Feed` call** or within
  `Config.EscapeDelay` (default 25 ms), it is a sequence introducer or an
  Alt-chord, never the Escape key.
- If the delay expires with nothing following, it is `KeyEscape`.
- The delay lives in `Parser`, not `Decode`, because `Decode` has no clock.

25 ms is the value every terminal library converged on and is above typical
human inter-key latency. The cost is stated plainly: pressing Escape and then
another key within 25 ms yields a chord. This is a real, unavoidable ambiguity
in the protocol, not a defect in our decoding.

### 3. Mouse: decode three encodings, enable none of them by default

**Decoded:** SGR 1006 (`CSI < b ; x ; y M|m`) — the modern one, the only one we
request. SGR 1015/urxvt (`CSI < b ; x ; y M`) and the legacy X10/normal forms
(`CSI M Cb Cx Cy`, including the 32-offset encoding) are also **parsed**, because
a terminal that ignores our 1006 request still emits one of them and receiving
mouse bytes we cannot parse is worse than not enabling mouse at all. Roughly
thirty lines each, and they are pure functions over byte slices.

**Not decoded:** X11 UTF-8 extended mouse (`CSI < b ; x ; y M` with a
`\x1b[<` preamble), 1016 SGR-pixel mode, and urxvt's 1015 pixel variants.
Recorded as a gap, not an oversight — they are vanishingly rare and all of them
carry coordinates we would have to convert anyway.

**Mouse capture is OFF by default.** This is the one place where the honest
answer is that the feature is opt-in, because enabling mouse reporting in a
TUI **steals the mouse from the shell**: text selection, middle-click paste,
and scrollback copying all stop working while the program runs, with no
terminal-side indication of why. A framework that turns that on by default
breaks a thing users rely on and did not ask to lose. `Config.MouseMode`:

| Mode | Reports | Default |
|---|---|---|
| `MouseNone` | nothing | **yes** |
| `MouseClick` | press, release, wheel | |
| `MouseDrag` | + motion with a button held | |
| `MouseAll` | + all motion | |

Wheel is included in `MouseClick` because List/Table/Pager scroll with it and a
wheel that needs drag mode enabled is a usability trap. `MouseClick` is what
`EnableMouse(true)` selects.

**Motion events are off by default even when mouse is on**, for the same
reason: a program that reports all motion generates an event per cell of travel
and every one of them crosses the `Handle(Event)` boundary. `MouseNone` +
`MouseClick` is the default shape; `MouseAll` is opt-in.

### 4. Bracketed paste: one `EventPaste` per paste, always

**Decision: a paste is delivered as a single `Event{Kind: EventPaste, Text:
...}`, never as a stream of key events.** [ADR 0001](0001-backend-strategy.md)
already lists paste coalescing as a hazard we own; this settles what coalescing
means.

The reasoning is about `TextInput` specifically. A 10,000-character paste
delivered as 10,000 key events would:

- run `Handle(Event)` 10,000 times through the whole widget tree, at ~112 bytes
  of struct copy each, to perform what is one buffer insertion;
- push 10,000 entries onto an undo stack, making one Ctrl-Z useless;
- run every `onChange` callback 10,000 times, so a `TextInput` that re-lays-out
  on change re-lays-out 10,000 times — and [ADR 0003](0003-renderer-mode.md)
  already says every `Draw` runs every frame anyway, so this is latency on top
  of latency;
- produce a different widget behaviour from the same user action depending on
  paste size, which is the worst kind of bug.

A single event makes paste **one undoable operation**, which is what every
editor does and what users expect.

**Consequences widgets must honour, stated as requirements on the catalog:**

1. A paste delivered to a focused widget that does not implement paste handling
   is **not** re-expanded into key events by the framework. The tree offers the
   `EventPaste`, and if nothing consumes it the paste is dropped. Silently
   re-expanding would reintroduce exactly the problem above, one layer up.
2. A widget that supports text entry must handle `EventPaste` by inserting
   `Text` at the caret **as one operation**.
3. `Text` carries the payload as-is. No unescaping, no newline normalisation, no
   trimming. Terminals send literal bytes between `ESC [ 200 ~` and
   `ESC [ 201 ~` and we do not improve on them.

### 5. Resize: one ordered stream, coalesced only while undelivered

`Terminal.ResizeEvents()` stays exactly as it is — it is a fact about the
transport, and ADR 0001 chose that interface. What changes is that `input`
introduces a `Source` which is the single place the two streams meet:

```go
// Source merges a Terminal's input bytes and its resize notifications into one
// ordered event stream.
//
// One goroutine reads input and one watches resizes; neither is the consumer,
// and neither drops input. Resize is the only event kind Source inserts itself.
type Source struct { /* unexported */ }

// NewSource starts reading from t. It does not enter raw mode, does not write
// to the terminal except for the optional kitty keyboard query, and returns
// immediately. Call Close to stop both goroutines.
func NewSource(t termmosaic.Terminal, cfg Config) *Source

// Events yields decoded events in order. It is closed when the Source is
// closed or the terminal reaches EOF.
func (s *Source) Events() <-chan termmosaic.Event

// KittyFlags yields the negotiated kitty keyboard flags exactly once, when the
// terminal answers Config.WriteProbe's query. It is never closed-and-silent by
// design: a receive with no value available just blocks until Close.
func (s *Source) KittyFlags() <-chan uint8

// KittyActive reports whether the disambiguate flag was successfully pushed.
func (s *Source) KittyActive() bool

// Close stops both goroutines. It is idempotent.
func (s *Source) Close() error
```

**The ordering guarantees, stated as a contract:**

1. **No input event is ever dropped, coalesced, or reordered.** Only resizes are
   eligible for coalescing.
2. **Resizes may be coalesced, and only while undelivered.** `Source` holds at
   most one pending resize; a newer SIGWINCH replaces it. Every resize a widget
   actually receives reports a size that was real.
3. **The coalescing policy is keep-latest, not drop-newest.** The current code
   in `term/terminal_unix.go:224-232` drops the *new* notification when the
   channel is full and keeps stale sizes queued. That is backwards: a widget
   that acts on a stale size and misses the final one is left rendering for a
   terminal that no longer exists. This is a forced change — see §11.
4. **A resize never flushes pending input.** Bytes already read from the tty are
   decoded and delivered before the resize event.
5. **A resize does not interrupt a half-parsed sequence.** If `ESC [ 1 ; 5` has
   arrived and SIGWINCH lands, the partial bytes stay in `Parser`, the resize is
   appended *after* every event those bytes eventually produce, and the sequence
   completes normally. Discarding the partial sequence would turn a resize into a
   lost keystroke.
6. **Ordering rationale:** a resize invalidates the coordinate space. Delivering
   all input decoded from bytes already in hand first means every mouse event is
   interpreted against the size in effect when the terminal emitted it, which is
   the only ordering in which a click at (40, 12) means what the user saw.
   Widgets still clip to bounds; a click outside the new bounds is a click
   outside the widget, not an error.

`examples/hello` today runs its own goroutine over `t.ResizeEvents()` and its
own goroutine over `t.Read()`. Under this ADR it runs one goroutine over
`source.Events()` and gets both, ordered.

### 6. Focus events: decode in v1, do not enable by default

`ESC [ I` (focus in) and `ESC [ O` (focus out) decode to
`Event{Kind: EventFocus, Focused: bool}`. The decoder support is ten lines and
`EventFocus` already exists in the union, so leaving it out would mean a
documented event kind that can never be produced.

**Focus *reporting* is off by default** (`Config.EnableFocusReporting`). Two
reasons, and they are different in kind:

- Terminal focus and widget focus are not the same thing. `ESC [ I` fires on
  alt-tab, on terminal switching, and on some window managers' focus-follows-mouse
  behaviour. Turning it on by default gives every application a focus-changed
  event it has no reason to receive, and the first thing an author writes is a
  `switch` that ignores it — which is the correct thing to write and should be
  opt-in work, not default work.
- With mouse enabled, terminals that support focus-follows-mouse send an extra
  event per widget under the cursor, which is mouse motion volume by another
  name.

The decoder is unconditional; only the `CSI ? 1004 h` enable sequence is
gated. `Source` writes it when `Config.EnableFocusReporting` is set, and
`Terminal.Close` already restores modes on exit.

### 7. IME: explicitly out of scope, and the door is reserved

**Decision: IME/composition support is DEFERRED. TermMosaic does not support
it, does not pretend to, and will say so in the README and in the `TextInput`
docs.** This is the largest unquantified item in
[STATUS.md](../STATUS.md) and it is now closed as *scoped out* rather than left
as an open question.

**Why scoping it out is the honest answer, not the convenient one.** Supporting
composition well means negotiating a preedit protocol (kitty's is the only one
with any traction), rendering an in-progress composition into the cell buffer at
a cursor position, feeding commits into the text model, and interacting with
keyboard-layout-dependent key identities — across terminals that mostly do not
offer the protocol at all. That is a subsystem on the scale of the entire
renderer, for a minority of users, on the framework's most latency-sensitive
path. Building it now, before `TextInput` exists to receive a commit, is
guaranteed rework.

**And the honest cost, stated plainly:** users composing Japanese, Chinese, or
Korean in a `TextInput` will get wrong behaviour, not degraded behaviour. On
most terminals they will get the committed text as a burst of ordinary key
events, which `TextInput` will insert correctly but which will pollute the undo
stack with one entry per character. **We accept that.** The mitigation is
documentation, and the mitigation that actually matters is the next paragraph.

**What the event model reserves so that this is a deferred *feature* rather than
a deferred *rewrite*.** An `Event` union that cannot express composition is a
design error made today, so:

- `EventKind` gains `EventCompose`, which is **never emitted in v1**. It is
  declared, documented, and `String()`ed. A widget author writing a `switch`
  today sees the case and knows composition is coming.
- `Event` gains a `Compose *Compose` field, nil in v1, holding the preedit
  string, the caret offset within it, and a phase. It is a **pointer** while
  every other payload is a value, and that asymmetry is deliberate: composition
  events are rare by construction, so paying 8 bytes of pointer on every
  keystroke to avoid an allocation on a path that does not exist yet would be
  optimising the wrong thing. It is the one field in `Event` allowed to be nil.
- The parser's entry point is the **byte stream**, not the event type. An IME
  sub-decoder, when it exists, slots in *before* the CSI state machine and
  consumes bytes from the same `Parser`. No change to `Decode`'s signature, the
  `Event` union's shape, or any widget's `Handle` is required to add it.
- The parser never guesses. A byte run that is not valid UTF-8 becomes
  `U+FFFD` and a *key* event, not a swallowed sequence, matching
  `ansi.AppendRune`'s existing invalid-rune policy. **When composition is added
  this rule is what changes**, which is a deliberate, named, single-site change
  rather than an architectural one.

**Trigger to revisit.** Any one of: (a) two or more independent user reports of
CJK/IME input being unusable in `TextInput`; (b) `TextInput`/`TextArea` shipping
with undo groups large enough to be obviously wrong on paste or on
composition-shaped bursts; (c) a terminal shipping a preedit protocol with real
adoption. Design it then, when there is a `TextInput` to design against and a
measurement to make. Not before.

### 8. The `Event` model

The existing tagged struct union is the right shape and stays. Concretely:

```go
// EventKind discriminates an Event.
type EventKind int

const (
    // EventNone is the zero value and carries no event.
    EventNone EventKind = iota
    // EventKey is a key press, repeat or release.
    EventKey
    // EventResize reports a new terminal size.
    EventResize
    // EventMouse is a mouse button press, release or motion.
    EventMouse
    // EventPaste is a bracketed-paste payload, delivered whole.
    EventPaste
    // EventFocus reports gaining or losing terminal focus.
    EventFocus
    // EventCompose reports IME composition state.
    //
    // DECLARED BUT NEVER EMITTED IN v0.x. IME support is deferred (ADR 0005
    // §7); this constant exists so the union can already express composition
    // and so widget authors can see the case in a switch today. Do not emit it
    // and do not handle it as if it were live.
    EventCompose
)

// KeyType is what happened to a key. Without the kitty keyboard protocol every
// event is KeyPress and autorepeat is indistinguishable from a second press;
// that is an accepted limitation of xterm encoding, not a bug.
type KeyType uint8

const (
    KeyPress KeyType = iota
    KeyRepeat
    KeyRelease
)

// ComposePhase is the stage of an IME composition.
type ComposePhase uint8

const (
    ComposeStart ComposePhase = iota
    ComposeUpdate
    ComposeCommit
    ComposeEnd
)

// Compose is the payload of an EventCompose. Reserved, never emitted in v0.x
// (ADR 0005 §7). The field set is what an implementation needs and no more:
// Text is the preedit string as the terminal reports it, Cursor is the byte
// offset of the caret within Text, and Phase says whether this is the start of
// a composition, an update to it, the committed text, or the end.
type Compose struct {
    Text   string
    Cursor int
    Phase  ComposePhase
}

// Event is a single input event delivered to the widget tree.
//
// One struct rather than an interface, because events cross the input path per
// keystroke and an interface here would mean a heap allocation for no benefit.
// Unused fields are zero, with one exception: Compose is nil unless Kind is
// EventCompose.
type Event struct {
    // Kind discriminates the event.
    Kind EventKind
    // Key is the non-printable key for EventKey, or KeyNone for a printable
    // character.
    Key Key
    // Rune is the printable character for EventKey, or 0.
    Rune rune
    // Mod is the modifier state.
    Mod KeyMod
    // Type is press, repeat or release. Always KeyPress unless the kitty
    // keyboard protocol reported event types (ADR 0005 §2).
    Type KeyType
    // Mouse is the payload for EventMouse.
    Mouse Mouse
    // Size is the new size for EventResize.
    Size Size
    // Text is the payload for EventPaste — the entire paste, undelimited and
    // unescaped — and the committed text for EventCompose.
    Text string
    // Compose is the payload for EventCompose, and nil otherwise. This is the
    // only pointer field in Event, and it is the one field allowed to be nil;
    // see ADR 0005 §7 for why composition is reserved but deferred.
    Compose *Compose
    // Truncated reports that a paste payload exceeded the configured maximum
    // and was cut short. The text is still a valid prefix of what was pasted.
    Truncated bool
    // Focused reports the new state for EventFocus.
    Focused bool
}
```

**Allocation behaviour, decided explicitly.** [ADR 0002](0002-buffer-representation.md)
established 0 allocs/op for the diff, and the input path is held to **the same
bar for the same reason**: it is on the latency path, and a TUI that allocates
per keystroke is a TUI whose input latency is governed by the GC.

| Event | allocs | Why |
|---|---|---|
| `EventKey` | **0** | All fields are scalars or inline structs. Must be pinned by `testing.AllocsPerRun` over `Decode`, not merely asserted in a comment — this is the one performance claim in this ADR and it should be a failing test if it ever stops being true. |
| `EventMouse` | **0** | Same. |
| `EventResize` | **0** | Same. |
| `EventFocus` | **0** | Same. |
| `EventPaste` | 1+ | The payload is a string; the copy is inherent. Once per paste, not per character. |

The 112-byte `Event` is passed **by value** to `Widget.Handle`, and that is not
a problem: it is a struct copy on a path that runs at most a few hundred times
per second, against a 16 ms frame budget. The zero-allocation bar is about the
*parser*, not about the copy.

**Binary and source compatibility.** TermMosaic is pre-alpha and the README
promises only that *"the public API will break without notice until v1.0.0"*,
plus SemVer-honoured from v0.1 with no behavioural change in a patch release.
The rules this ADR sets:

- **Adding a field to `Event` is additive**, source- and behaviour-compatible
  for every consumer that does not use an unkeyed composite literal. Unkeyed
  literals of `Event` will break; that is acceptable pre-1.0 and worth a note in
  the CHANGELOG.
- **Adding an `EventKind` or `KeyType` constant is additive.** Consumers switch
  on `Kind`; a `switch` without a `default` is a compile-time concern they own.
- **Changing which events an existing configuration produces is a behavioural
  change and is forbidden in a patch release.** Concretely: mouse capture and
  focus reporting default to off, and the kitty handshake only *adds*
  disambiguation, so a patch release adding this subsystem cannot alter what an
  existing program's keyboard produces beyond making previously
  indistinguishable combinations distinguishable. Where that happens it is
  logged in the CHANGELOG's Added section, not Fixed.

**A guard test, in the spirit of ADR 0002's.** `Event` is a hot-path struct
copied on every keystroke, and the natural way to make it slow later is to add
a field nobody measures. Pin `unsafe.Sizeof(Event{})` and document the bound,
the way `TestCellHasNoPadding` pins `sizeof(Cell)`. ADR 0002's lesson was that
a padding-free type is not automatically a sound one; here the hazard is
different — an unbounded struct on the hot path — and the mitigation is the same
shape: a test that fails when the invariant breaks.

### 9. Configuration surface

```go
// Config configures a Parser or a Source. The zero value is usable and
// conservative: no mouse, no focus reporting, no kitty enhancement, no
// bracketed-paste request, accept both backspace bytes, 25 ms escape delay,
// 4 MiB paste cap, 256-deep event queue.
type Config struct {
    // MouseMode selects how much mouse reporting Source enables. Default
    // MouseNone: capturing the mouse takes text selection and scrollback
    // copying away from the user's shell (ADR 0005 §3).
    MouseMode MouseMode

    // EnableFocusReporting requests CSI ? 1004 h. Default false.
    EnableFocusReporting bool

    // BracketedPaste requests CSI ? 2004 h. Default true, because the cost of
    // guessing wrong is only that a paste arrives as key events, whereas the
    // cost of guessing right is that TextInput's undo behaves correctly.
    BracketedPaste bool

    // WriteProbe sends raw bytes to the terminal — used only for the kitty
    // keyboard query. Nil disables probing and leaves Source at the legacy
    // xterm encoding. Terminal has no write method and ADR 0001's interfaces
    // are not widened for this (ADR 0005 §2).
    WriteProbe func(p []byte) error

    // ProbeKitty enables the CSI ? u capability query. Default true. When
    // WriteProbe is nil this is ignored.
    ProbeKitty bool

    // KittyFlags is the flag set requested after a successful query. Default
    // 0b1: disambiguate escape codes, and nothing else (ADR 0005 §2).
    KittyFlags uint8

    // KittyReportReleases emits EventKey events for key releases when the
    // terminal reports event types. Default false: nothing in the widget
    // catalog consumes a release.
    KittyReportReleases bool

    // EscapeDelay is how long a bare ESC waits for a following byte before it
    // is reported as KeyEscape. Default 25ms. Zero disables the wait, making
    // ESC always the Escape key — which breaks Alt-chords.
    EscapeDelay time.Duration

    // BackspaceByte selects which control byte means backspace. The default
    // accepts both 0x7f and 0x08.
    BackspaceByte BackspaceMode

    // MaxPasteBytes caps a single paste payload. Default 4 MiB. A longer paste
    // is truncated, flagged with Event.Truncated, and still scanned to its
    // closing marker so the stream stays in sync.
    MaxPasteBytes int

    // EventQueue is the depth of Source's event channel. Default 256. When it
    // is full the reader stops reading the tty; input is never dropped.
    EventQueue int
}
```

### 10. Deferred items, with triggers

Stated here as a block so the deferrals are not scattered and cannot be lost.

| Item | Status | Trigger to revisit |
|---|---|---|
| **IME / composition** | **DEFERRED** — scoped out, documented as unsupported. The `EventCompose` kind and `Compose` payload field are reserved so the union can already express it. | Two or more independent reports of CJK/IME input being unusable in `TextInput`; or `TextInput` shipping with undo groups large enough to be obviously wrong on composition-shaped bursts; or a terminal shipping a preedit protocol with real adoption. See §7. |
| **Kitty `F13`–`F35`** | **DEFERRED.** `Key` stops at `KeyF12`. When added they go at the **end** of the iota block, never in the middle, or every later constant renumbers. | A `KeyHint` widget needing to advertise them, or any user report of an F13+ binding being unreachable under kitty. |
| **tmux / screen DCS passthrough** (`ESC P tmux; … ESC \`) | **DEFERRED**, and this is a real gap: without it a TermMosaic program under tmux on a modern terminal can lose key and mouse reporting. | Any tmux user reporting broken keys or mouse, or v1.0, whichever is first. It attaches as a pre-dispatch unwrap step in `Parser`, which is why the byte stream is the parser's entry point. |
| **X11 UTF-8 extended mouse (1004-style `\x1b[<`) and 1016 SGR-pixel coordinates** | **DEFERRED.** SGR 1006, urxvt 1015 and X10 are all decoded; these are not. | A terminal or multiplexer in the field that cannot be configured to emit SGR 1006. |
| **kitty `associated-text`** | **DEFERRED.** `KittyFlags` can request it; the decoder does not use it yet, because it changes what `Event.Rune` means and `TextInput` does not exist to define that. | When `TextInput` is designed. This is the most likely first use of `Event.Text` on a non-paste event. |

### 11. Forced changes to existing code

This ADR does not implement any of itself, but it makes the following existing
code wrong or incomplete. Recorded so the next implementer is not surprised.

1. **`examples/hello/main.go` — the raw-byte scan is replaced** by one
   goroutine over `source.Events()`, and the two existing goroutines (resize and
   heartbeat) stay but the resize goroutine folds into the event loop. The
   comment at line 255 (*"TermMosaic has no input decoder yet"*) becomes false.
2. **`term/term.go` package documentation** — the first bullet under *"What is
   NOT implemented here"* is the input-decoding gap and must be rewritten; the
   `UNVERIFIED` paragraph must be extended to say that input is now covered by
   `input`'s unit tests but has still never run against a tty.
3. **`term/terminal_unix.go:224-232` — the resize drop policy inverts.** Today a
   full channel drops the *newest* size and keeps stale queued ones. §5 requires
   keep-latest. The `resizes` channel of depth 8 and its `default:` branch are
   both suspect.
4. **`term/caps.go:71` — `caps.KittyKeyboard` is a heuristic, and its doc
   comment must say so.** It means "may support", not "active". `DetectCaps`
   does not change behaviour; its documentation does.
5. **`event.go:44-51` — the `Key` doc comment is inaccurate.** It claims the
   protocol is supported *"when the terminal advertises it (Caps.KittyKeyboard)"*.
   After this ADR it is supported when the handshake succeeds on `Source`, which
   is a different and later moment.
6. **`event.go:66-69` — the `KeyBackspace` comment** asks the decoder to
   reconcile control-byte backspace. §2 answers it; the comment should point at
   §2 rather than leaving the question open.
7. **`event.go:227-250` — `Event` gains `Type`, `Compose`, `Truncated`, and
   `EventKind` gains `EventCompose`.** `EventCompose` is never emitted in v0.x
   and `Compose` is always nil there; both are declared so downstream `switch`
   statements and tests see the case now.
8. **`Term` is not widened.** No new method on `Terminal`; the kitty query goes
   through `Config.WriteProbe`. This is a deliberate refusal to reopen ADR 0001's
   interface, and if it is ever reopened it is a new ADR rather than a quiet
   addition here.

## Consequences

**Good**

- The decoder's entire logic is a pure function over bytes, so the class of bug
  that costs the most — a sequence split across two reads — is a table-driven
  unit test rather than a flaky timing test. That is the testability pillar
  applied to the one subsystem ADR 0001 named as our risk list.
- Paste-as-one-event makes a 10k-character paste one undoable operation, one
  layout pass, and one callback, instead of ten thousand of each.
- Mouse is off by default, so a TermMosaic program does not silently take
  selection and scrollback copying away from the user's shell.
- Kitty keyboard is here rather than deferred because we already promised it in
  `event.go`, and progressive enhancement means the cost on a non-kitty terminal
  is one 100 ms-bounded query and three bytes.
- IME is scoped out honestly, with the door left open at three named seams
  (the `EventCompose` kind, the `Compose` payload field, and the byte-stream
  entry point) so adding it is a feature, not a rewrite of thirty widgets.
- One ordered event stream removes the two-goroutine input/resize dance from
  every example and every application.

**Bad — stated plainly**

- **We own more terminal quirks, which is the price ADR 0001 already warned
  about.** The list is now concrete: split sequences, ESC ambiguity, the 64-byte
  malformed-sequence bound, three mouse encodings, kitty's two modifier
  encodings, paste truncation, tmux DCS passthrough (still not designed), and
  resize coalescing. Each is a pure function with a test, which is the
  mitigation, but the bug count will not be zero and none of it can be verified
  without running against real terminals.
- **The kitty handshake adds startup latency and a failure mode.** A terminal
  that answers slowly costs up to 100 ms of startup; a terminal that answers with
  garbage is `StatusInvalid` and we fall back. Both are handled, and both are
  still more machinery than not shipping it would need.
- **The 25 ms escape delay makes Escape-then-key within 25 ms a chord.** This
  is a protocol ambiguity, not our bug, and it is not fixable without a
  terminal-side change. Users who press Escape and immediately another key will
  occasionally see the combination.
- **`KeyF13`–`KeyF35` are missing**, so a `KeyHint` row cannot advertise them.
  Appending them later is additive but must go at the **end** of the `Key` iota
  block, never in the middle.
- **Dropping kitty's hyper/meta/caps-lock/num-lock modifier bits** means
  `ModCapsLock` does not exist and a widget cannot ask about caps-lock state.
  Accepted, and narrower than the alternative of widening `KeyMod` for
  something no catalog widget needs.
- **Windows gets nothing from this.** The Windows backend is a stub returning
  `ErrWindowsStub` ([ADR 0001](0001-backend-strategy.md)), and the Windows
  console does not deliver escape sequences at all — it delivers
  `KEY_EVENT`/`MOUSE_EVENT` records that need a completely different decoder.
  **This ADR does not solve Windows.** It makes Windows input a second decoder
  behind the same `Event` union rather than a second event model, which is the
  most that can honestly be claimed now.
- **The 112-byte `Event` grows with every payload we add**, and the composition
  decision is the reason it is not larger. The guard test bounds it rather than
  optimising it.

## Rejected alternatives, specifically

- **Decode inside `Terminal`** — puts a state machine in the one package whose
  I/O is documented as untested, makes `Read` untestable without a tty, and
  breaks the headless terminal's byte-level `Feed`. Rejected on testability.
- **A stateful `Parser` with a blocking `Next()`** — couples the state machine
  to `io.Reader`, so testing the split-sequence case needs a pipe and a
  goroutine. The pure `Decode` makes the most valuable test in the subsystem a
  one-liner. Rejected on testability; the unavoidable state (partial bytes,
  paste accumulation, escape deadline) is kept in `Parser` where it belongs.
- **An interface-based event model** (`type Event interface{ isEvent() }`) —
  rejected by the existing `event.go` doc comment and still rejected: it costs a
  heap allocation per keystroke and breaks `Widget.Handle(Event) bool` for
  every widget. The tagged union is the right shape.
- **Enum + parallel payload fields** — that is what the tagged union already is.
  Listed to be dismissed rather than to be chosen.
- **Paste as a stream with a "this is a paste" marker**, letting widgets
  coalesce — pushes the 10,000-`Handle` problem and the 10,000-entry undo stack
  onto every widget author instead of solving it once in the parser. Rejected.
- **Mouse capture on by default** — takes text selection, middle-click paste
  and scrollback copying away from the user's shell with no indication why.
  Rejected on usability, not on difficulty.
- **Kitty keyboard deferred to v1.0** — `event.go:44-51` already documents the
  protocol as supported. Deferring it would mean shipping a doc comment that is
  false, and it would leave `Caps.KittyKeyboard` as a field that is read by
  nothing. Deferred *encodings* are fine; a promised decoder is not.
- **Full kitty enhancement** (`disambiguate | event-types | alternate-keys |
  associated-text`) — more event volume for information nothing in the catalog
  consumes, and `associated-text` would change what `Rune` means before
  `TextInput` exists to define it. Requested as a set, defaulting to `0b1`.
- **Any IME work now** — a subsystem the size of the renderer, for a minority of
  users, with no `TextInput` to design against and no reference implementation
  in any comparable framework. Deferred with a stated trigger; the three
  reserved seams are what make this safe rather than merely postponed.
- **tmux/screen DCS passthrough (`ESC P tmux; … ESC \`)** — genuinely required
  for TermMosaic to work under tmux with a modern terminal, and not designed
  here. **Deferred, and this is a real gap.** Trigger: any user running tmux who
  reports broken key or mouse handling, or before v1.0, whichever comes first.
  It belongs in `Parser` as a pre-dispatch unwrap step, which is why the byte
  stream is the parser's entry point.

## Risks to revisit at v1.0

1. **Never run against a real terminal.** Everything here is a pure function
   over bytes, and no pure function proves that a terminal emits what we think.
   The `term` package documentation already says its I/O path is unverified;
   after this ADR, that gap includes input. Trigger: build a matrix of recorded
   byte streams from several terminals and assert the decoder's output against
   them. Cheap, and it should happen before the widget catalog grows.
2. **The kitty handshake.** It is the newest, least-tested protocol code in the
   project and it runs before the first frame. If startup ever appears to hang,
   this is where to look; the 100 ms bound is the thing to verify.
3. **Windows input is a second decoder, not solved.** Recorded in the bad
   consequences because it is the single largest remaining platform gap and this
   ADR does not close it. Trigger: ADR 0001's v1.0 Windows escape hatch.
4. **`Event` growth.** The guard test bounds size; it does not stop us from
   needing a wider `KeyMod`, a richer `Mouse` (currently no click count, no
   button-mask for "three buttons held"), or a multi-region clipboard event.
   Each is additive and cheap now. Revisit if `Widget.Handle` ever needs a
   second parameter.
5. **Escape delay tuning.** 25 ms is a community consensus, not a measurement.
   If users complain about Escape-then-key chords, raising it is a one-line
   `Config` change; the reverse complaint (Escape feels laggy) is harder and
   would need a smarter rule than a fixed timer.
6. **Frame-budget interaction.** An event storm (mouse motion with `MouseAll`,
   plus a held key autorepeating) delivers events far faster than frames. Nothing
   in this ADR coalesces input, deliberately — but if real applications coalesce
   at the `Source` boundary, the hook is `Source.Events()`'s consumer, not the
   parser.