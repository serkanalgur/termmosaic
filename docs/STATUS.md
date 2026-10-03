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
| Backend strategy (own vs wrapped vs pluggable) | **OPEN** | Under active design. See [ARCHITECTURE.md](ARCHITECTURE.md). |
| Cell buffer representation (AoS vs SoA) | **OPEN** | OpenTUI uses struct-of-arrays, which enables cheap row-level diff skips. Trade-off is complexity. |
| Renderer mode (immediate vs retained vs hybrid) | **OPEN** | |
| Layout engine (own constraints vs flexbox) | **OPEN** | |
| Color model and degradation ladder | **OPEN** | |
| Theme and styling system | **OPEN** | |

## Widget catalog

All **OPEN** pending the architecture decision. The intended catalog is:

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

- Do we wrap a terminal library (e.g. `golang.org/x/term`, `tcell`) or own the
  raw-mode/escape-sequence layer outright?
- Is a headless memory-sink backend a v1 requirement or a v0.5 stretch?
- Do we target Kitty graphics protocol in v1, or stay text-only?
- How much of the layout engine do we own vs adapt?

## Definition of "usable library"

The bar this project is measured against:

- SemVer honored from v0.1; **no behavioral change in a patch release**.
- A hand-maintained `CHANGELOG.md` with Breaking / Added / Fixed /
  Known Limitations sections.
- CI green on Linux, macOS, and Windows across supported architectures.
- Every widget has a runnable example and a documented public API.
- Known limitations are enumerated in the docs, not discovered by users.