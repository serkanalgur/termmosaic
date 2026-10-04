# Changelog

All notable changes to TermMosaic are recorded here by hand.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
from v0.1.0.

## Release policy before v1.0.0

**The API is not stable and will break without notice.** Every release before
v1.0.0 is a pre-release, and a minor version may contain behavioural changes.
What *is* promised: **no behavioural change in a patch release.** If v0.1.1
changes behaviour, that is a bug in the release, not a policy.

`0.0.x` versions, if any, are not semver and may do anything.

Sections below use these markers:

- **Breaking** — an existing program will not compile, or will compile and behave
  differently. Every one is listed with what to change.
- **Added** — new capability.
- **Fixed** — a defect in a previous version.
- **Known Limitations** — what this release does *not* do. This section is not
  padding: a framework that ships an honest list of its gaps is more useful than
  one that leaves users to discover them.

Anything marked **PROPOSED** in [docs/STATUS.md](docs/STATUS.md) may change or be
reversed before v1.0.0.

---

## [0.1.0] — 2026-10-04

The first release. Everything below is new; there is no previous version to
break.

TermMosaic is a cell-buffer renderer plus a catalog of widgets for terminal
applications in Go. It owns the terminal layer outright through two narrow
interfaces and wraps no terminal library, and it is built so that a widget test
can assert on cells without a terminal being involved at any point.

### Breaking

There is nothing to break: v0.1.0 is the first release, so there are no earlier
call sites to break. What follows is the compatibility promise starting *here*.

- **The public API is unstable until v1.0.0.** Minor versions may contain
  behavioural changes. The `Widget` interface is the one thing deliberately held
  fixed — four methods, and it has not changed across the project's eight
  architecture decisions.
- **TermMosaic targets Go 1.23+** and depends only on `golang.org/x/sys` and
  `golang.org/x/term`. Builds are `CGO_ENABLED=0`, verified in CI for
  `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`
  and `windows/arm64`.
- **Nothing wraps `tcell`, Bubble Tea, Textual or any other terminal library.**
  Programs migrating from one of those will find no drop-in compatibility layer
  here; the shape of a TermMosaic program is its own.

### Added

**Rendering.** A double-buffered cell renderer with a two-tier diff: a
per-row byte skip over a per-cell pass, driven by dirty rectangles and paced by
a frame pacer. The frame path is **0 allocations per frame**, asserted rather
than benchmarked. Measured on a 200×60 scene that is 99% static chrome, the
diff writes 141 bytes where a full repaint writes 19,979 — a ~141× reduction at
~7,133 ns/op. A frame in which nothing is dirty writes zero bytes and does not
call the sink at all, so an idle application costs nothing rather than burning
CPU at the frame rate.

**A terminal layer we own.** `Terminal` (size, raw mode, alternate screen,
capability probing, reading) and `Sink` (write, flush) are two small interfaces
with a direct `x/sys` implementation. A headless in-memory `Sink` maintains the
cell grid the emitted bytes *would have produced*, which is what makes widget
tests assert on cells rather than on escape sequences.

**Layout.** A constraint solver of our own: `Length`, `Min`, `Max`,
`Percentage`, `Ratio`, `Fill`, plus spacing and nested composition. Pure Go with
no cgo, so cross-compilation stays clean and the whole solver is testable in CI
with no terminal. `Fill` is order-insensitive, which is a deliberate divergence
from tmux and is pinned by a test.

**Input.** A pure `Decode` under a resumable `Parser`, driven by a `Source` that
merges input bytes and resize notifications onto **one ordered channel** — so a
resize can never be delivered between the halves of an escape sequence, and no
input event is dropped to make room for one. Kitty keyboard disambiguation,
bracketed paste (always one event carrying the whole payload), mouse decoding
in SGR / urxvt / X10, and focus decoding. The key path is 0 allocations, pinned
by test. Mouse capture and focus reporting are **off by default**: enabling
either takes text selection and scrollback copying away from the user's shell.

**Twenty-two widgets**, in four groups:

- *Core* — `Block`, `Text`, `Paragraph`, `Split`. `Block` is the only thing in
  the project that draws a border or a title.
- *Forms* — `TextInput`, `TextArea`, `Select`, `Checkbox`, `Radio`, `Toggle`,
  `Tabs`, `Button`, `KeyHint`.
- *Data* — `List`, `Table`, `Tree`, `Pager`, sharing a row-virtualization engine.
  Cost is flat in item count and asserted: `List` renders 10,000 items in
  13,320 ns and 100,000 in 14,242 — seven percent for ten times the data, both
  at zero allocations.
- *Visualization* — `ProgressBar`, `Gauge`, `Meter`, `Sparkline`, `BarChart`.
  `Sparkline` and `Gauge` use Braille for sub-cell resolution, and `Gauge`
  degrades to a bar when the rectangle or the terminal cannot hold a dial.

**Text and styling.** One `buffer.Style` value (foreground, background,
attributes, twelve bytes, passed by value) and one `Span` type for styled text
parsed once at construction rather than per frame. Range-clipped cell writers
(`SetSpansIn`, `SetStringIn`, `SetSpansCappedIn`, `SetSpansWindowIn`) let a
widget draw text that ends before the screen's edge — a table cell, a gauge
label — without pre-truncating (which allocates) or re-deriving the wide-glyph
rules (which is how two widget packages each grew their own forty-line copy).

**Responsiveness as shared arithmetic.** `geometry.ClampCount`,
`geometry.Priority` with its four-value scale, `geometry.Region` and
`geometry.Budget`, plus the optional `termmosaic.Minimizable` interface. There
are deliberately **no framework breakpoints and no size classes**: each widget's
threshold is a local constant beside its own `Draw`, because a size class is a
lossy function of two numbers and a product decision in the wrong layer. What is
shared is the arithmetic, and the degenerate-size contract: no widget panics,
none blanks itself, and a resize always repaints the whole screen.

**Colour.** Truecolor → 256 → 16, with a redmean quantiser and a `Quantiser`
interface as the escape hatch for a better mapping. `NO_COLOR` is honoured at
encode time, so no widget path consults the environment.

**Two runnable examples.** `examples/hello` is a bordered, resize-aware panel
with golden files covering the rendered screen *and* the exact byte stream at
each rung of the colour ladder. `examples/dashboard` is a live dashboard built
from the catalog.

**Documentation.** Eight ADRs recording what was decided, what was rejected and
why, with benchmark output quoted inline — including the workloads that did not
produce a clean result. `docs/STATUS.md` distinguishes DECIDED / PROPOSED /
OPEN / DEFERRED and names the trigger for revisiting each deferral.

### Fixed

Nothing to fix: v0.1.0 is the first release. The fixes below are defects found
and corrected *before* v0.1.0 shipped, recorded because the project's own
standard is that a decision which turned out to be wrong is documented rather
than quietly rewritten.

- **The example composed a second border implementation.** `examples/hello` drew
  its own border and title with its own width thresholds. It now composes
  `Block`, and spells no border rune and invents no threshold. A test asserts the
  example's source contains no border vocabulary at all.
- **The terminal resize watcher dropped the newest size.** On a full channel it
  kept the oldest, so a drag-resize settled on a stale geometry. It now keeps
  the latest, which is the size the terminal is actually at.
- **`nil` files were rejected on Unix but accepted on Windows**, so the same
  program behaved differently per platform. The Windows backend now rejects them
  too.
- **The example scanned raw input bytes for `'q'`,** which could not tell an arrow
  key from four unrelated bytes, and maintained a second goroutine for resizes.
  It now reads keys and resizes off the one ordered stream.
- **`Wrap` silently discarded newlines,** because a newline is zero-width. A
  multi-line paragraph came back as one unwrapped block. Wrapping is now
  newline-aware, and the rune-index mapping that makes editable wrapped text
  correct is exposed rather than re-derived privately by one widget — which is
  how a single combining mark was found to be shifting every caret position
  after it by one.
- **The range-clipped writers could strand a wide glyph's continuation cell** when
  a window filled its range exactly, leaving an unpaired glyph and a row that
  never compared equal to the previous frame — permanent flicker.
- **A frame that produced no bytes still flushed the sink,** which broke the
  documented contract that a zero-sized screen touches the terminal not at all.
  A detached terminal's event loop now costs nothing.

### Known Limitations

These are real gaps, listed so nobody has to find them. Several are deliberate
deferrals with a stated trigger; where that is so it is said.

**Platform.**

- **Windows is a stub that returns a loud error from every console operation.**
  The package compiles and cross-compiles cleanly for `windows/amd64` and
  `windows/arm64`, so the packaging works and the runtime does not. On Windows
  this framework does not currently draw anything. Linux and macOS are the
  supported platforms.
- **tmux and GNU screen DCS passthrough is missing.** Under tmux on a modern
  terminal, a TermMosaic program can lose key and mouse reporting, because the
  sequences TermMosaic emits are not wrapped for the multiplexer. Deferred with a
  trigger: any tmux user reporting broken keys or mouse, or v1.0, whichever comes
  first.

**Text and internationalization.**

- **No IME or preedit support.** This is a deliberate deferral, not an oversight,
  and the cost is worth stating plainly: composing Japanese, Chinese or Korean
  in a `TextInput` produces *wrong* behaviour rather than degraded behaviour. On
  most terminals the committed text arrives as a burst of ordinary key events,
  which inserts correctly but pollutes the undo stack with one entry per
  character. The event model reserves `EventCompose` and a `Compose` payload so
  this can be added later as a feature rather than as a rewrite of every widget.
- **Wide glyphs and grapheme clusters are handled, but the handling is
  provisional.** A double-width glyph occupies two cells and its continuation
  cell takes its owning span's style, so rows do not flicker — and this release
  is the first in which those paths were actually benchmarked rather than assumed
  (see below). What is *not* done: the cell-width table is hand-written from
  East Asian Width ranges rather than generated from Unicode data, so newly
  assigned wide blocks are wrong until the table is updated; and grapheme
  clusters are not composed, so a flag emoji renders as two cells' worth of junk
  and a zero-width-joiner sequence renders as several glyphs.
- **`geometry.ClampCount` and `geometry.Budget` count cells, not glyphs,** so a
  row budget computed from them can be one row optimistic once wide characters
  are in play.
- **`TextArea` has no rendered selection.** It tracks and moves a caret and
  supports editing, but the selected range is not drawn.

**Colour.**

- **The colour quantiser is unvalidated.** The redmean mapping from truecolor to
  the 256- and 16-colour rungs is implemented and works, but nobody has checked
  that its output is perceptually acceptable. Treat the 256 and 16 rungs as
  provisional. The `buffer.Quantiser` interface exists so a Lab-space mapping can
  replace it without touching anything else.
- **`Caps.Unicode` is a proxy, not a probe.** It is the honest one available
  without querying the terminal out of band, and a terminal configured
  out-of-band will disagree with it.

**Behaviour and scope.**

- **The API is unstable before v1.0.0**, as stated at the top of this file.
- **There is no theme system in v1, by decision.** Widgets carry `Style` fields
  and the framework's defaults are the terminal's own colours plus named
  attribute styles. The trigger for adding one is the first style *role* that two
  widgets must share; until then a theme layer would be an abstraction over
  nothing.
- **There is no `Form` container,** no table column selection, no pager
  selection and no redo stack. Layout composition and focus traversal cover the
  first; the others are not built.
- **A resize allocates two full cell grids per resize event** — about 6.9 MB over
  an 18-event drag at 200×60. That is GC-visible rather than latency-visible, and
  it has not been measured under load. The drag itself coalesces to one repaint
  per frame tick, automatically, with no code from the application.
- **Nothing in this release has been run against a real terminal being
  resized.** Every cost quoted for resize is derived from code and from other
  measurements, not from an observed drag.
- **Partial repaint on resize is not possible by construction,** because resizing
  the buffer discards its cells. This is structural rather than a missing feature.
- **A wide glyph costs one cursor-position escape per glyph** in the diff, so a
  screen that is entirely wide text writes roughly eleven times the bytes of the
  same screen in ASCII. The output is correct; this release measures the cost and
  does not fix it.

### Measured, and newly so

Two performance claims that this release turns from assertion into measurement:

- **The frame path allocates nothing.** On a 200×60 scene that is 99% static
  chrome: 141 bytes written, ~7,133 ns/op, **0 allocs/op**.
- **Wide-glyph paths, measured for the first time.** Every wide path was
  implemented and none had ever been benchmarked. On an Apple M1, darwin/arm64,
  Go 1.23.0:

  | Scene | ns/op | allocs/op | bytes |
  |---|---|---|---|
  | Diff, 99% static, ASCII | 531 | 0 | 141 |
  | Diff, 99% static, wide glyphs | 565 | 0 | 300 |
  | Diff, all rows, ASCII | 6,713 | 0 | — |
  | Diff, all rows, wide glyphs | 6,757 | 0 | — |
  | Full repaint, 200×60, ASCII | 74,078 | 0 | 19,979 |
  | Full repaint, 200×60, wide glyphs | 74,139 | 0 | 21,678 |
  | Row skip, static, ASCII | 272 | 0 | 0 |
  | Row skip, static, wide glyphs | 251 | 0 | 0 |

  **The wide paths did not regress.** On the same geometry a wide-glyph scene
  costs 6% more on a partial diff, 0.1% more on a full repaint, and is
  indistinguishable on the row-skip tier — because a double-width rune costs one
  extra branch and one extra cell write, and saves the loop iteration and the
  width lookup that two narrow runes would have needed. The one genuine cost is
  bytes, and it is listed under Known Limitations above.

  The measurement also surfaced a defect, which **this release does fix**: the
  diff's cursor-run suppression assumed one cell per rune, so on a screen of
  nothing but wide text every glyph was preceded by a cursor-position escape —
  6,000 moves against 30, and 68,832 bytes against 6,233, for identical output.
  The run tracker now advances by the glyph's cell width. On the same scene:
  **60 cursor moves and 19,443 bytes, 3.12× the narrow frame** rather than 11×.
  The ASCII path is unchanged. See ADR 0008's amendment, finding 4.

[0.1.0]: https://github.com/serkanalgur/termmosaic/releases/tag/v0.1.0
