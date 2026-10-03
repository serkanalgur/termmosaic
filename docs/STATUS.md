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
| Color model and degradation ladder | **OPEN** | |
| Theme and styling system | **OPEN** | |

### Decisions

The four core architecture rows above are **DECIDED** as of 2026-10-03 and are
recorded in full, with rejected alternatives, in [docs/adr/](adr/README.md).

Decisions 1 and 2 were made **empirically** — a scratch benchmark module was
built outside the repo and measured on darwin/arm64 (Apple M1). The headline
result: a two-tier diff on a 200×60 scene that is 99% static chrome writes
**107 bytes against 23,240 for a full repaint** (217× less), at **7,133 ns/op**
with **0 allocs/op**, comfortably inside a 16 ms frame budget.

Two findings contradict earlier assumptions and are recorded rather than
quietly dropped: **OpenTUI's struct-of-arrays advantage does not transfer from
Zig to Go**, and **`tcell` is not usable as the buffer layer** — its flush walks
every cell per frame and its headless backend cannot expose the cell buffer.

Raw benchmark output is quoted inline in ADR 0001 and ADR 0002, including the
workloads that did not produce a clean result. Decisions 3 and 4 were made on
API-surface and dependency grounds and involve no measurements.

## Widget catalog

All **OPEN** pending implementation. The intended catalog is:

**Core** — Buffer, Canvas, Layout, Block/Border, Text, Line, Span.

**Forms** — TextInput, TextArea, Select, Checkbox, Radio, Toggle, Tabs,
Button, KeyHint, Form.

**Data** — List, Table, Tree, Virtual Scroll, Pager.

**Visualization** — ProgressBar, Gauge, Meter, Sparkline, BarChart.

## Known gaps in comparable frameworks

Recorded because they define our opportunity. Sources verified 2026-10.

- OpenTUI ships no progressbar / gauge / meter / sparkline / bar chart.
- OpenTUI ships no list / tree / pager / virtual scroll; its roadmap still
  lists text virtualization as unfinished.
- OpenTUI has no CHANGELOG; release bodies are auto-generated PR lists.
- OpenTUI pre-1.0 patch releases carry behavioral changes; the v1.0 refactor
  is already on the roadmap.
- OpenTUI has no IME/preedit support.
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
- **IME / preedit scope, and what it costs.** No measurement, no design, no
  scope. We have no idea whether this is a parser, a protocol negotiation, or
  an architectural change. OpenTUI has no IME support either, so there is no
  ready reference. This is the largest unquantified item on this list.
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
  expected first; Windows is currently an unquantified v1.0 risk.
- **Color model and degradation ladder**, and the **theme/styling system** — see
  the OPEN rows above.

## Definition of "usable library"

The bar this project is measured against:

- SemVer honored from v0.1; **no behavioral change in a patch release**.
- A hand-maintained `CHANGELOG.md` with Breaking / Added / Fixed /
  Known Limitations sections.
- CI green on Linux, macOS, and Windows across supported architectures.
- Every widget has a runnable example and a documented public API.
- Known limitations are enumerated in the docs, not discovered by users.