# Status

This document is the honest state of TermMosaic. It is updated as decisions are
made. If something here is stale, that is a bug — please open an issue.

**Project stage: pre-alpha. Do not use this in production. The public API will
break without notice until v1.0.0.**

## Decision states

| Marker | Meaning |
|---|---|
| **DECIDED** | Agreed, will not be reopened without a strong reason. |
| **PROPOSED** | A concrete proposal awaiting review. Expect this to move. |
| **OPEN** | Deliberately undecided. Options are being explored. |
| **DEFERRED** | Known to be needed, intentionally postponed with a stated trigger. |

## Language and packaging

| Item | State | Notes |
|---|---|---|
| Implementation language | **DECIDED** — Go 1.23+ | Single static binary, trivial cross-compilation, low CI cost. |
| Module path | **DECIDED** — `github.com/serkanalgur/termmosaic` | |
| License | **DECIDED** — MIT | |
| Minimum Go version | **DECIDED** — 1.23 | Matches current stable. |

## Core architecture

| Item | State | Notes |
|---|---|---|
| Backend strategy (own vs wrapped vs pluggable) | **DECIDED** — pluggable, own the core | Two narrow interfaces (`Terminal`, `Sink`), direct `golang.org/x/sys` impl, headless memory sink. Wraps no terminal library: tcell's flush measured 280,814 ns/op vs our 7,133 ns/op on a one-row-dirty frame. [ADR 0001](adr/0001-backend-strategy.md) |
| Cell buffer representation (AoS vs SoA) | **DECIDED** — AoS, padding-free 16-byte `Cell` | **Overturned the prior SoA leaning.** Measured: packed-AoS row skip ties with SoA (5,373 vs 5,128 ns/op) and is 4.4× faster when all rows are dirty (147.2 vs 636.7). OpenTUI's advantage is Zig's `mem.eql`, not SoA. [ADR 0002](adr/0002-buffer-representation.md) |
| Renderer mode (immediate vs retained vs hybrid) | **DECIDED** — hybrid | Retained widget tree invalidated by rectangle; widgets describe themselves on demand. No reconciler, no Elm loop. [ADR 0003](adr/0003-renderer-mode.md) |
| Layout engine (own constraints vs flexbox) | **DECIDED** — constraint-based, own solver | `Length`/`Min`/`Max`/`Percentage`/`Ratio`/`Fill`. Yoga rejected: cgo breaks `CGO_ENABLED=0` cross-compilation. [ADR 0004](adr/0004-layout-engine.md) |
| Input decoding (where the parser lives, what it decodes) | **DECIDED** — a pure `Decode` under a resumable `Parser` in a new `input` package | Kitty keyboard **in** (progressive enhancement, `disambiguate` only); paste **in** and always **one `EventPaste` carrying the whole payload**; mouse decoding **in** (SGR 1006 / urxvt 1015 / X10) but **capture off by default**; focus decoding **in**, reporting off by default; **IME scoped out and deferred**, with `EventCompose` reserved. [ADR 0005](adr/0005-input-decoding.md) |
| Cell access on sub-buffers (`Cells()`, `RowBytes`) | **DECIDED** — `Row(y) []Cell` replaces `Cells()`; `RowBytes` is a `*Buffer` method that panics on a view | The flat slice `Cells()` returns is silently wrong for a sub-buffer. The stride never leaves `buffer`. Closes ADR 0002's v1.0 risk 1b now. [ADR 0006](adr/0006-subbuffer-cell-access.md) |
| Responsive screen composition | **DECIDED** — a **budget, not a reflow**; no size classes, no framework breakpoints | Framework shares the arithmetic (`geometry.ClampCount`, `geometry.Budget` + `Priority`, optional `termmosaic.Minimizable`); policy stays per widget. **`Widget` is unchanged.** Degenerate sizes are a contract: **no panic ever, clip never blank**; a resize always repaints the whole screen because `buffer.Resize` discards the cells. [ADR 0007](adr/0007-responsive-screens.md) |
| Color model and degradation ladder | **OPEN** | |
| Theme and styling system | **DECIDED** — **no theme in v1**; widgets carry `Style` fields, framework defaults are the terminal's own colours plus named attribute styles | One `buffer.Style` value (fg/bg/attr, by value, 12 bytes, 0 allocs) replaces the loose-argument write API; `ansi.Style` becomes an alias of it. Trigger for a theme: the first role two widgets must share. [ADR 0008](adr/0008-style-and-text.md) |
| Text and span rendering | **DECIDED** — `Span` + `Buffer.SetSpans`, parsed once, wrapped outside `Draw` | A wide glyph's continuation cell takes its **owning span's** style or the row flickers forever. `Wrap`/`Truncate` allocate and are banned from `Draw`. Borders and titles have one vocabulary (`BorderPlain`/`Rounded`/`Double`/`Thick`/`ASCII`, one `Block`). [ADR 0008](adr/0008-style-and-text.md) |

### Decisions

The seven core architecture rows above are **DECIDED** — the first four on
2026-10-03, input decoding, sub-buffer cell access, responsive screen
composition and style/theme/text on 2026-10-04 — and are recorded in full, with
rejected alternatives, in [docs/adr/](adr/README.md).

Decisions 1 and 2 were made **empirically** — a scratch benchmark module was
built outside the repo and measured on darwin/arm64 (Apple M1). The headline
result: a two-tier diff on a 200×60 scene that is 99% static chrome writes
**~141× fewer bytes than a full repaint** (141 vs 19,979 bytes as measured in
the committed implementation), at **~7,133 ns/op** with **0 allocs/op**,
comfortably inside a 16 ms frame budget.

Two findings contradict earlier assumptions and are recorded rather than
quietly dropped: **OpenTUI's struct-of-arrays advantage does not transfer from
Zig to Go**, and **`tcell` is not usable as the buffer layer** — its flush walks
every cell per frame and its headless backend cannot expose the cell buffer.

Raw benchmark output is quoted inline in ADR 0001 and ADR 0002, including the
workloads that did not produce a clean result. Decisions 3, 4 and 5 were made on
API-surface, testability and dependency grounds and involve no measurements.
ADR 0005 says so explicitly: input decoding is I/O-bound, and the one number
that matters — 0 allocations on the key path — is a property the ADR specifies
and a test must pin, not a measurement taken today. ADR 0007 makes the same
disclosure: its drag-resize costs are derived from existing code and from
ADR 0002/0003's measurements, and no resize has ever been observed against a
real terminal.

### Amendments

Four accepted ADRs were amended on 2026-10-04, after the core implementation
exposed claims that no longer described the code. Each amendment notes the date
and reason in the ADR's header; the original reasoning is preserved in place
rather than rewritten.

- **ADR 0002 — the byte-compare soundness condition was half-stated.** Padding
  is one precondition; contiguity of the compared range is the other, and it is
  a property of the call site, not of `Cell`. A `SubBuffer` that returned a
  tightly packed slice violated it while `Cell` was still exactly 16 bytes.
  Now stated as two preconditions, with `Buffer.stride` as the structural fix.
- **ADR 0002 — the byte-count figure.** "107 bytes" was an artifact of a
  single-byte-glyph benchmark scene. The claim is the **ratio (~141×)**, not the
  absolute number.
- **ADR 0004 — `Fill` is not order-sensitive.** The original ADR recorded this
  as its top usability sharp edge; the committed solver resolves all fixed
  constraints before any `Fill`, and a test pins that as a guarantee. The ADR
  now documents order-insensitivity as a deliberate divergence from tmux.
- **ADR 0002 — the `RowBytes` receiver question is settled.** Risk item 1b asked
  whether to make `RowBytes` structurally refuse non-top-level buffers by moving
  it behind a `*Buffer` receiver. [ADR 0006](adr/0006-subbuffer-cell-access.md)
  answers **yes**, and removes the v1.0 deferral: `RowBytes` becomes
  `(*Buffer).RowBytes(y, x0, n)`, panics on a sub-buffer, and `diff.Frame`
  carries `*buffer.Buffer` so the old call sites do not compile.

## Widget catalog

All **OPEN** pending implementation. The intended catalog is:

**Core** — Buffer, Canvas, Layout, Block/Border, Text, Line, Span.

**Forms** — TextInput, TextArea, Select, Checkbox, Radio, Toggle, Tabs,
Button, KeyHint, Form.

**Data** — List, Table, Tree, Virtual Scroll, Pager.

**Visualization** — ProgressBar, Gauge, Meter, Sparkline, BarChart.

**Responsiveness is decided before implementation**, in
[ADR 0007](adr/0007-responsive-screens.md), because three coders are about to
build independent widget sets against a shared vocabulary. This closes the
question the catalog previously left open — *should the framework define
breakpoints or size classes?* — with a **no**: a size class is a lossy function
of two numbers and a product decision in the wrong layer, so each widget's
threshold is a local named constant beside its own `Draw`. What is shared is
`geometry.ClampCount`, `geometry.Budget` with the four-value `Priority` scale,
and the optional `termmosaic.Minimizable` interface. Two catalog-wide rules
inherit directly and are non-negotiable: **a widget's available space is
`Bounds()`, never `buf.Size()`**, and **a widget repaints its whole `Bounds()`
before drawing content into it**, or shrinking leaves stale cells. `Widget` is
unchanged.

**Styling and text are also decided before implementation**, in
[ADR 0008](adr/0008-style-and-text.md), on the same reasoning and against the
same collision: every one of the thirty widgets will style text and draw a
border. One `buffer.Style` value, one `Span` type, one border glyph vocabulary
owned by `buffer`, and one `Block` as the only thing that draws a border or a
title. There is **no theme in v1** — widgets carry `Style` fields and the
framework's defaults are the terminal's own colours plus named attribute styles.
`NO_COLOR` and the 16-colour rung stay encode-time only, so no widget path
consults them.

## Known gaps in comparable frameworks

Recorded because they define our opportunity. Sources verified 2026-10.

- OpenTUI ships no progressbar / gauge / meter / sparkline / bar chart.
- OpenTUI ships no list / tree / pager / virtual scroll; its roadmap still
  lists text virtualization as unfinished.
- OpenTUI has no CHANGELOG; release bodies are auto-generated PR lists.
- OpenTUI pre-1.0 patch releases carry behavioral changes; the v1.0 refactor
  is already on the roadmap.
- OpenTUI has no IME/preedit support. **We do not either, by decision**
  ([ADR 0005](adr/0005-input-decoding.md) §7) — the parity here is real, and it
  is not something to present as a gap-closing feature.
- Ink repaints the full screen on update, which shows up as input lag at
  scale.

## Open questions

Answered questions have been removed; the reasoning is preserved in
[docs/adr/](adr/README.md). Still genuinely undecided:

- **Kitty graphics protocol in v1, or stay text-only?** Leaning no. Images
  undermine the grid-of-cells assumption that the whole renderer rests on, and
  the feature is not in the widget catalog. Still open because "no" has not
  been formally decided, and because a future `Image` widget may force the
  question.
- **IME / preedit scope — DEFERRED, and scoped out.** This was "the largest
  unquantified item on this list"; [ADR 0005](adr/0005-input-decoding.md) closes
  it as a deliberate deferral rather than leaving it open. **Honest note about
  what that costs:** users composing Japanese, Chinese or Korean in a
  `TextInput` get *wrong* behaviour, not degraded behaviour — on most terminals
  the committed text arrives as a burst of ordinary key events, which inserts
  correctly but pollutes the undo stack with one entry per character. We accept
  that; the mitigation is documentation. The door is deliberately left open at
  three named seams so this is a deferred feature rather than a deferred rewrite:
  `EventCompose` is declared (and never emitted in v0.x), `Event` carries a
  `Compose *Compose` payload field, and the parser's entry point is the byte
  stream rather than the event type. **Trigger to revisit:** two or more
  independent reports of CJK/IME input being unusable; or `TextInput` shipping
  with undo groups large enough to be obviously wrong on composition-shaped
  bursts; or a terminal shipping a preedit protocol with real adoption. Not
  before — there is no `TextInput` to design against and no reference
  implementation in any comparable framework, OpenTUI included.
- **tmux / screen DCS passthrough — DEFERRED, and a real gap.** It fell out of
  the input-decoding work rather than being designed by it: without it, a
  TermMosaic program under tmux on a modern terminal can lose key and mouse
  reporting. Trigger: any tmux user reporting broken keys or mouse, or v1.0,
  whichever comes first. See [ADR 0005 §10](adr/0005-input-decoding.md).
- **Wide characters (CJK, emoji) and grapheme clusters.** A wide glyph occupies
  two cells; the continuation cell must compare equal across frames or it will
  flicker. Needs its own decision; no benchmark has been run. Deferred until
  internationalization is scoped.
- **Headless backend: v1 or v0.5?** Largely settled — [ADR 0001](adr/0001-backend-strategy.md)
  makes the headless memory sink a v1 deliverable, because the whole testability
  pillar depends on it. The remaining open sub-question is its **assertion
  surface**: it must expose the cell buffer, not just recorded bytes, since
  tcell's headless backend cannot do this and widget tests need it.
- **Windows console support.** ADR 0001 commits to owning the terminal layer,
  which means Windows console mode flags are our problem. Linux and macOS are
  expected first; Windows is currently an unquantified v1.0 risk. **Updated
  2026-10-04:** `CGO_ENABLED=0` builds are now verified for `windows` and
  `linux/arm64`, so the packaging half of the risk is closed — but the Windows
  backend is a **stub that returns a loud error** from every console operation,
  not a working console. The remaining risk is entirely the runtime half.
- **Color model and degradation ladder** — still OPEN; see the OPEN row above.
  The **theme/styling system** is no longer in this list:
  [ADR 0008](adr/0008-style-and-text.md) decides it as "no theme in v1".

## Definition of "usable library"

The bar this project is measured against:

- SemVer honored from v0.1; **no behavioral change in a patch release**.
- A hand-maintained `CHANGELOG.md` with Breaking / Added / Fixed /
  Known Limitations sections.
- CI green on Linux, macOS, and Windows across supported architectures.
- Every widget has a runnable example and a documented public API.
- Known limitations are enumerated in the docs, not discovered by users.