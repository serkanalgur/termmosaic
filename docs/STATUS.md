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
| Color model and degradation ladder | **PROPOSED** | Built and working: `Colour` is truecolor/named-16/256 with a redmean quantiser and a `ColourDepth` rung, plus `NO_COLOR`. **Not yet validated.** Nobody has checked the redmean mapping is perceptually acceptable, so treat the 256 and 16 rungs as provisional. The `buffer.Quantiser` interface is the escape hatch for a Lab-space replacement. |
| Theme and styling system | **DECIDED** — **no theme in v1**; widgets carry `Style` fields, framework defaults are the terminal's own colours plus named attribute styles | One `buffer.Style` value (fg/bg/attr, by value, 12 bytes, 0 allocs) replaces the loose-argument write API; `ansi.Style` becomes an alias of it. Trigger for a theme: the first role two widgets must share. [ADR 0008](adr/0008-style-and-text.md) |
| Text and span rendering | **DECIDED** — `Span` + `Buffer.SetSpans`, parsed once, wrapped outside `Draw` | A wide glyph's continuation cell takes its **owning span's** style or the row flickers forever. `Wrap`/`Truncate` allocate and are banned from `Draw`. Borders and titles have one vocabulary (`BorderPlain`/`Rounded`/`Double`/`Thick`/`ASCII`, one `Block`). [ADR 0008](adr/0008-style-and-text.md) |
| `docs/ARCHITECTURE.md` | **DECIDED** — a short orientation document, not a summary | Reduced to 105 lines at the v0.1.0 release gate. It had grown to 251 lines duplicating ADR reasoning, its decision numbering (5=colour, 6=theme, 7=input) did not match the ADR set, and it still called the colour model OPEN after this table moved it to PROPOSED. It now states what the pieces are, how they fit, and links each ADR — no duplicated reasoning — and preserves the **Non-goals** section verbatim, which is not duplicated anywhere else. |
| Documentation site | **PROPOSED** — Hugo + Pagefind on GitHub Pages; captures generated in Go from `MemorySink` cells, not screenshots | No browser TTY exists, so the only truthful picture of a widget is the cell grid the renderer produced — which is what `widgets/widgettest` already builds and what the golden tests assert on, so the docs cannot drift from behaviour. **Not built.** A live WASM playground is rejected: `docs/ARCHITECTURE.md` lists "no WASM build" as a written non-goal and `term/terminal_windows.go` is a stub, so there is no seam to port. Plan, page tree, per-widget template and effort: [docs/SITE-PLAN.md](SITE-PLAN.md). |

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

**A fifth amendment, also on 2026-10-04, closed ADR 0008's risk 5** — "the
wide-glyph interaction is implemented but unbenchmarked" — by benchmarking it, on
the diff, the cell writers, `Wrap` and a whole rendered frame. The amendment
records the measurements, states that the wide paths did not regress, and records
one defect the benchmark surfaced and this release deliberately does not fix: the
diff's cursor-run suppression assumes one cell per rune, so every wide glyph is
preceded by a cursor-position escape. It is the only change made to any ADR for
v0.1.0.

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

**22 widgets, built and tested.** (`buffer.Buffer` is deliberately not counted:
it has `Invalidate()` but no `Bounds`/`Draw`/`Handle`, so it is not a `Widget`
— it is what widgets draw into.) Flat per-frame cost is the
claim that matters and it is asserted: List renders 10k items in 13,320 ns and
100k in 14,242 — 7% for ten times the data — and Table likewise, both at zero
allocations.

**Core** — `Block`, `Text`, `Paragraph`, `Split`. Plus the non-widget
primitives they build on: `buffer.Buffer`, `layout`, `buffer.Span`.
`Block` is the only thing in the catalog that draws a border or a title.

**Forms** — `TextInput`, `TextArea`, `Select`, `Checkbox`, `Radio`, `Toggle`,
`Tabs`, `Button`, `KeyHint`. A `Form` container was **not** built; ADR 0004's
solver plus `layout` covers composition, and a `Form` type would have been a
second way to do the same thing.

**Data** — `List`, `Table`, `Tree`, `Pager`, plus the `virtual/` engine they
share (flat cost from 10 to 1,000,000 items, asserted).

**Visualization** — `ProgressBar`, `Gauge`, `Meter`, `Sparkline`, `BarChart`.
`Sparkline` and `Gauge` use Braille for sub-cell resolution; `Gauge` degrades
to a bar when the rect or terminal cannot hold a dial. These five are the gap in
every comparable framework — OpenTUI has 13.4k stars and ships none of them.

**Responsiveness is decided**, in [ADR 0007](adr/0007-responsive-screens.md),
because three coders build independent widget sets against a shared vocabulary.
This closes the question the catalog previously left open — *should the framework define
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

## Release gate for v0.1.0 — what closed, and what did not

Recorded so that nothing below is silently open. "Closed" means it is done and
verified; "not done" means it is still open and is named as such rather than
left for someone to rediscover.

**Closed.**

- **`CHANGELOG.md` now exists**, hand-maintained, with Breaking / Added / Fixed /
  Known Limitations sections. It was a stated requirement of the definition of
  usable library and had never been written. The Known Limitations section lists
  what is genuinely unsupported rather than what is merely unpolished.
- **The four ADR 0007 §3 renderer tests exist** — `TestRenderAtZeroSizeWritesNothing`,
  `TestResizeShrinksAndRepaintsWholeRect`, `TestResizeCoalescedToOneRepaintPerTick`
  and `TestRootBoundsClippedToScreen` — each pinned by mutation rather than merely
  by presence. Writing them **found a real defect**: `Render` flushed the sink even
  when it wrote nothing, breaking ADR 0007 §4's contract. Fixed.
- **`SetSpansWindowIn`'s `skip` path question is settled.** It was recorded as
  unreachable; it is reachable, via `clampColOffset`'s ceiling rather than via
  `scrollCols`. `Table`'s behaviour is unchanged; the reasoning was wrong, and both
  the code comment and this document are corrected.
- **`examples/hello` no longer uses the clamp-and-centre pattern.** `centred(sw,
  sh, 46, 9)` is replaced by a bounds computation expressed as two `layout.Solve`
  calls, which is the documented path rather than the anti-pattern ADR 0007 was
  written about. The golden files are regenerated; the visible consequence is that
  the block sits one row lower on an odd slack, because largest-remainder
  distribution hands the leftover cell to the earliest-declared `Fill`.
- **`docs/ARCHITECTURE.md` is a pointer.** 251 lines of duplicated and stale ADR
  summary replaced by 105 lines of orientation with a link per ADR.
- **Wide-glyph paths are benchmarked.** See the open-questions entry below for the
  numbers and for the one defect found and deliberately not fixed.

**Not done, and deliberately so.**

- **The cache-poisoning debug mode is still not built.** ADR 0007's expensive half,
  still a convention rather than a check. Not a release gate.
- **The diff's cursor-move overhead on wide glyphs is not fixed.** One line, and
  specified in ADR 0008's amendment, but `internal/diff` is the most load-bearing
  code in the project and this is not the task to change it in.
- **IME / preedit is not implemented**, by ADR 0005's deliberate deferral. The cost
  is documented rather than mitigated.
- **tmux / screen DCS passthrough is still missing.** Deferred with a trigger.
- **The colour quantiser is still unvalidated.** PROPOSED, not DECIDED.
- **The documentation site is not built.** Separate work, separate repository.
- **`TextArea` has no rendered selection, and there is no `Form` container, no table
  column selection, no pager selection and no redo stack.** Each would be a widget
  API addition; none is in scope for a release gate.
- **`docs/adr/README.md`'s "Still open" list is stale in one entry**: it lists the
  colour model as undecided, where this table records it as PROPOSED. Correcting it
  means editing an ADR, which the v0.1.0 gate forbids except for ADR 0008's risk 5,
  so it is recorded here instead.

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
- **Wide characters (CJK, emoji) and grapheme clusters — STILL OPEN, and now
  MEASURED.** A wide glyph occupies two cells and the continuation cell must
  compare equal across frames or the row flickers; that rule is implemented,
  tested, and was **benchmarked for the first time in v0.1.0**. The wide paths
  did **not** regress: on a 200x60 scene a full repaint costs 74,139 ns/op against
  the ASCII scene's 74,078, and the row-skip tier is indistinguishable. All paths
  are 0 allocs/op. Numbers and method: the 2026-10-04 amendment to
  [ADR 0008](adr/0008-style-and-text.md) risk 5; benchmarks in
  `internal/diff/wideglyph_test.go`, `buffer/wideglyph_test.go` and
  `render/wideglyph_bench_test.go`.
  **What is still open is the decision, not the performance:** `RuneWidth`'s table
  is hand-written from East Asian Width ranges rather than generated from Unicode
  data, and grapheme clusters are not composed (a flag emoji renders as two cells'
  worth of junk). Deferred until internationalization is scoped.
  **A defect the benchmark surfaced and this release does NOT fix:** the diff's
  cursor-run suppression assumes one cell per rune, so every wide glyph is
  preceded by a cursor-position escape — 68,832 bytes against 6,233 for the same
  6,000 runes on a dense wide scene. Correct output, 11x the bytes. The fix is one
  line and is specified in the ADR 0008 amendment; it is out of scope for a
  release gate because `internal/diff` is the most load-bearing code here and ADR
  0002's headline numbers are quoted from it.
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
- **Color model and degradation ladder** — moved from OPEN to **PROPOSED**;
  see the table row above. Built and working, not yet perceptually validated.
  The **theme/styling system** is no longer in this list:
  [ADR 0008](adr/0008-style-and-text.md) decides it as "no theme in v1".
- **A cache-poisoning debug mode — STILL OPEN. The four ADR 0007 §3 tests — now
  written.** Two separate items, previously recorded as one:
  - **The cache-poisoning debug mode remains unbuilt.** The catalog surfaced
    that a rect-keyed cache is only half the contract — see ADR 0007's 2026-10-04
    amendment. The cheap half is a stated convention; the mechanical check is not
    built. A debug mode that corrupts a widget's cache after `Draw` and asserts the
    next frame is identical would catch that whole class instead of by review.
  - **`TestRenderAtZeroSizeWritesNothing`, `TestResizeShrinksAndRepaintsWholeRect`,
    `TestResizeCoalescedToOneRepaintPerTick` and `TestRootBoundsClippedToScreen`
    now exist**, in `render/responsive_contract_test.go`, together with
    `TestRootBoundsEntirelyOffScreenClipsToNothing`. Each is pinned by mutation, not
    merely by presence. **Writing them found a real defect:** `Renderer.Render`
    called `Sink.Flush` even when it wrote zero bytes, which broke ADR 0007 §4's
    "does not call the Sink" contract for a zero-sized screen; `Render` now flushes
    only when bytes went out. The degenerate-size resize sweep
    (`TestResizeShrinksAndRepaintsWholeRect/through_a_degenerate_size`) and the
    example's own grow/shrink/degenerate/recover sweep
    (`examples/hello`'s `TestResizeGolden`, with two new golden files) are the
    scripted resize sweep ADR 0007 risk 6 asks for.
- **`SetSpansWindowIn`'s `skip` path — RESOLVED, and the recorded reason was
  WRONG.** This said the path was unreachable from `Table` because `scrollCols`
  always positions on a column start. That is false, and `widgets/data`'s
  `table_window_test.go` proves it. `Table` keeps two horizontal offsets: a column
  index and a CELL position, and the cell position is set to a column start only
  when a caller asks for one. `clampColOffset`'s **ceiling** is
  `totalW - contentW` — the right-hand edge of the content — which is generally not
  a column start. Two reachable consequences, both pinned: scrolling to the end
  (shift+End) clamps onto that ceiling and leaves the column before it partially
  visible; and any resize that changes `contentW` or `totalW` re-clamps onto it, so
  the partial column appears with no scrolling key pressed at all. So `skip` is on
  the production path and `Table` needs no new behaviour.
  **Neither of the two options this question offered is the right one**, which is
  why it took a measurement to settle: the primitive did not need teaching to
  scroll by cell, and it did not need documenting as unused. `Table`'s behaviour is
  **unchanged**; a misleading comment on
  `TestTableWideColumnIsMarkedRatherThanBleedingIntoTheFrame` that repeated the false
  claim has been corrected in place.

## Definition of "usable library"

The bar this project is measured against:

- SemVer honored from v0.1; **no behavioral change in a patch release**.
- A hand-maintained `CHANGELOG.md` with Breaking / Added / Fixed /
  Known Limitations sections. **MET for v0.1.0** — [CHANGELOG.md](../CHANGELOG.md)
  is written and carries all four sections.
- CI green on Linux, macOS, and Windows across supported architectures.
- Every widget has a runnable example and a documented public API.
- Known limitations are enumerated in the docs, not discovered by users.