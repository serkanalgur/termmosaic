# Architecture

This document records architecture decisions for TermMosaic in condensed form.

The four core architecture decisions below are **DECIDED** as of 2026-10-03,
and input decoding was added as Decision 7 on 2026-10-04. Full rationale,
evidence, benchmark output, consequences (including the bad ones), and rejected
alternatives live in **[docs/adr/](adr/README.md)**. Each section here links to
its ADR. Colour model and theme remain open.

---

## Decision 1: Backend strategy — DECIDED

**Pluggable: own the terminal layer outright, expose it through two narrow
interfaces.** → [ADR 0001](adr/0001-backend-strategy.md)

```go
type Terminal interface { /* size, raw mode, alt screen, capabilities, read, resize */ }
type Sink     interface { /* Write, Flush */ }
```

Implemented directly on `golang.org/x/sys` / `golang.org/x/term`, plus a
headless memory `Sink` and fake `Terminal` for CI. **No terminal library is
wrapped.** Dependencies are `x/sys` and `x/term` and nothing else; the Go 1.23
floor and `CGO_ENABLED=0` cross-compilation are preserved.

**Evidence.** `tcell`'s internal cell carries four string headers per cell, its
`draw()` walks every cell on every `Show()` with only a per-cell dirty flag, and
its `GetCells()` lives in an unexported interface so headless tests cannot see
the buffer. Measured on a 200×60 screen with one of sixty rows dirty: tcell's
flush **280,814 ns/op** against our two-tier diff's **7,133 ns/op** (~39×), and
tcell's draw path alone is 466,481 ns/op.

**Cost, stated plainly.** We now own paste coalescing, mouse encoding variants,
resize races, tmux passthrough, and Windows console mode flags. Windows is
expected late.

## Decision 2: Cell buffer representation — DECIDED

**Array-of-structs with a padding-free 16-byte `Cell`** and a byte-wise row
skip. → [ADR 0002](adr/0002-buffer-representation.md)

```go
type Cell struct {
    Ch   rune   // int32
    FG   Colour // uint32
    BG   Colour // uint32
    Attr Attr   // uint16
    _    [2]byte  // INVARIANT: keeps sizeof(Cell)==16 so a row is byte-comparable
}
```

**This overturned the earlier leaning toward struct-of-arrays.** OpenTUI's SoA
row-skip advantage is an advantage of Zig's `mem.eql`; it does not transfer to
Go, where the equivalent is `bytes.Equal` over byte-addressable memory.

**Evidence.** Row-level skip on a 99%-static 200×60 scene:

| Row compare | ns/op |
|---|---|
| Packed AoS — one 3,200-byte memcmp | 5,373 |
| SoA, 4 byte planes — four 800-byte memcmps | 5,128 |
| SoA with `[]rune` planes — typed loops | 20,777 |
| AoS with a natural padded struct | 14,241 |

The skip is a **tie** between packed AoS and SoA — the win is padding-free
contiguity, not SoA-ness. When *every* row is dirty the skip never fires and
packed AoS is **4.4× faster** (147.2 vs 636.7 ns/op). Full two-tier diff: 7,133
ns/op for packed AoS vs 8,003 for SoA, both 0 allocs/op. Bytes written are
identical across all representations — 141 for the diff against 19,979 for a
full repaint, a ~141× reduction.

**The Go-specific hazard, which is not the expected one.** A byte-wise row
compare over a *padded* Go struct is unsound: we demonstrated two rows with
every meaningful field identical comparing as different because of 2 bytes of
undefined tail padding. SoA has no equivalent hazard; we trade it away and
mitigate with a mandatory guard test that fails on any change to
`sizeof(Cell)`.

## Decision 3: Renderer mode — DECIDED

**Hybrid.** A retained widget tree, invalidated by rectangle, with widgets
describing themselves on demand. → [ADR 0003](adr/0003-renderer-mode.md)

```go
type Widget interface {
    Bounds() Rect
    Draw(buf *Buffer)
    Invalidate()
    Handle(Event) bool
}
```

Static chrome is cheap because of the two-tier diff (~141× fewer bytes), not
because of the renderer mode — so the mode does not have to carry that weight.
Every widget's `Draw` runs every frame; widgets are **not** required to
implement incremental drawing, because Go cannot enforce invalidation discipline
the way a borrow checker does, and retained-mode invalidation bugs fail
*silently*.

No reconciler and no Elm loop. Async is one documented rule — mutate widget state
inside `Post(func())`, or call `Invalidate()` from any goroutine. This is a
deliberate, thinner async story than Bubble Tea's `Cmd`.

## Decision 4: Layout engine — DECIDED

**Constraint-based, our own solver, no cgo, no native dependencies.**
→ [ADR 0004](adr/0004-layout-engine.md)

```go
Layout{ Direction, Constraints, Spacing }
Length(n) | Min(n) | Max(n) | Percentage(p) | Ratio(n,d) | Fill(weight)
```

One top-down pass in declaration order; `Fill` consumes what is left after fixed
constraints. Nesting is composition (run the solver on a sub-rect), and
splitting/drag-resize are just a mutated `Length`. The solver is a pure function
with no terminal dependency, so it is exhaustively testable in CI.

**Flexbox via vendored Yoga was rejected on packaging grounds:** cgo breaks
`CGO_ENABLED=0` cross-compilation, which contradicts the single-static-binary
goal. Writing our own flexbox was rejected as over-engineering — grow/shrink/
basis resolution is complex and nearly all of it is dead weight in a terminal.
What is genuinely lost: wrap, `order`, baseline alignment, and weak intrinsic
content sizing.

## Decision 5: Color model and degradation

**State: OPEN**

Truecolor → 256 → 16, with a documented ladder and a `NO_COLOR` path. Needs a
perceptual mapping, not naive truncation.

## Decision 6: Theme and styling system

**DECIDED** — see [ADR 0008](adr/0008-style-and-text.md).

A `buffer.Style{FG, BG, Attr}` passed by value replaces loose colour/attribute
arguments at the write API, and styled text arrives as `Span` values parsed **once
at construction** rather than per frame. **No theme in v1:** widgets carry
`Style` fields, and the framework ships *no* default colours at all — the
terminal's own, plus named attribute styles. The dangerous version of "no theme"
is hard-coded colours, so the rule is stated as a prohibition rather than a
default. The trigger for a theme is the first style role two widgets must share.

Degradation (`NO_COLOR`, the 16-colour rung) is **encode-time only**; nothing in
the widget path reads caps or the environment.

## Decision 7: Input decoding — DECIDED

**A pure decoder under a resumable driver, in a new `input` package.**
→ [ADR 0005](adr/0005-input-decoding.md)

```go
// Pure: no I/O, no clock, no retained state. The three statuses are the whole
// contract, and StatusIncomplete is what makes a sequence straddling two
// read(2) calls a return value rather than a bug.
func Decode(seq []byte, cfg Config) (termmosaic.Event, int, Status)

// Resumable. Holds only the unavoidable state: partial sequence bytes, the
// in-progress paste, and the escape deadline.
type Parser struct{ /* ... */ }

// One ordered event stream, merging input bytes and resize notifications.
type Source struct{ /* ... */ }
```

The existing tagged-struct `Event` union is kept and extended, not replaced: it
gains `Type` (kitty press/repeat/release) and a reserved `Compose *Compose`
payload. Nothing becomes an interface, so the key path stays at **0
allocations** and `Widget.Handle(Event) bool` is untouched. As in ADR 0002, that
invariant gets a guard test — here pinning `sizeof(Event)` rather than
`sizeof(Cell)`, because the hazard is an unbounded struct on the hot path.

**Why a pure function.** Every input bug is a function of the bytes, so
decoding is testable with no I/O, no goroutine and no terminal — the testability
pillar applied to the one subsystem ADR 0001 named as our risk list. The state
that cannot be made pure (partial bytes, paste accumulation, the ESC timeout)
lives one layer up, in `Parser`.

**Scope verdicts.**

| Area | Verdict | Note |
|---|---|---|
| Kitty keyboard protocol | **IN** | Progressive enhancement via a `CSI ? u` query with a 100 ms hard timeout, then push `disambiguate` only. `eventTypes`, `alternateKeys` and `associatedText` are requestable but off by default — nothing in the widget catalog consumes them. |
| Bracketed paste | **IN** | Always **one `EventPaste` carrying the whole payload**. A 10k-char paste is one undoable operation, one layout pass, one callback — not 10k of each. |
| Mouse | decode **IN**, capture **OFF by default** | SGR 1006, urxvt 1015 and X10 are all parsed; only 1006 is requested. Capture is opt-in because it takes text selection and scrollback copying away from the user's shell. Enabling gives `MouseClick` (buttons + wheel); all-motion is not included. |
| Focus (`ESC [ I` / `ESC [ O`) | decode **IN**, reporting **OFF by default** | Ten lines of decoder for an event kind the union already declares; terminal focus and widget focus are not the same thing, so enabling it is opt-in. |
| IME / composition | **DEFERRED — scoped out and documented** | See below. |

**Cost of the IME deferral, stated plainly.** Users composing CJK in a
`TextInput` get wrong behaviour, not degraded behaviour: on most terminals the
commit arrives as a burst of ordinary key events, which inserts correctly but
makes the undo stack useless. We accept that and document it. What we refuse to
accept is designing it into a corner, so the union already reserves
`EventCompose` (declared, never emitted in v0.x) and a `Compose *Compose`
payload, and the parser's entry point is the byte stream rather than the event
type — an IME sub-decoder slots in before the CSI state machine and nothing else
has to change.

**Also deferred, with triggers in ADR 0005 §10:** kitty `F13`–`F35`, tmux/screen
DCS passthrough (a real gap — without it a program under tmux on a modern
terminal can lose key and mouse reporting), X11 UTF-8 extended mouse, and kitty
`associated-text`.

**Forced changes to existing code** are enumerated in ADR 0005 §11. The
load-bearing ones: `examples/hello/main.go` replaces its raw-byte scan for `'q'`
with `source.Events()`; `term/terminal_unix.go`'s resize watcher currently drops
the *newest* size when its channel is full, which is backwards and must become
keep-latest; and `Caps.KittyKeyboard` is re-documented as "may support", with
negotiation state on `Source`. `Terminal` is **not** widened — the kitty query
goes through `Config.WriteProbe`, so ADR 0001's interfaces stay as chosen.

**No benchmark informed this decision**, and the ADR says so rather than
implying evidence: input decoding is I/O-bound, and the allocation claim is a
property to be pinned by test rather than a number measured today.

---

## Non-goals

- **Not a web framework.** No HTTP server, no browser target, no WASM build.
- **Not a widget-styling DSL.** Styling stays plain Go values. No layout-in-CSS.
- **No image support in v1.** Kitty graphics is explicitly deferred; the
  grid-of-cells assumption is the foundation and images undermine it.
- **Not opinionated about your application.** We provide widgets and a
  renderer, not an application framework. No required `App` struct, no
  prescribed message loop.

## Still open

Answered questions were removed from this document; their reasoning is preserved
in [docs/adr/](adr/README.md). Genuinely unresolved, and tracked in
[STATUS.md](STATUS.md):

- **Kitty graphics in v1** — leaning no; not formally decided.
- **IME / preedit scope and cost** — *no longer an open question.*
  [ADR 0005](adr/0005-input-decoding.md) scopes it out and documents the
  resulting limitation honestly; the event model reserves `EventCompose` and a
  `Compose` payload so it can be added later as a feature. See Decision 7.
- **Wide characters and grapheme clusters** — a wide glyph spans two cells and
  the continuation cell must compare equal across frames or it flickers. Needs
  its own decision and a benchmark.
- **Headless backend assertion surface** — v1 is settled by ADR 0001; whether it
  exposes the cell buffer or only recorded bytes is still open, and widget tests
  need the cell buffer.
- **Windows console support** — an unquantified v1.0 risk created by owning the
  terminal layer. ADR 0005 does not help here: the Windows console delivers
  key/mouse *records*, not escape sequences, so Windows input is a second
  decoder behind the same `Event` union rather than a solved problem.
- **Colour model / degradation ladder** and **theme system** — see above.