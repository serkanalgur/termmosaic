# Architecture

This document records architecture decisions for TermMosaic.

Status: **the core architecture is still under design.** The questions below
are open and are being worked through. Once a decision is made, it moves to an
ADR in `docs/adr/` and gains a **DECIDED** marker in [STATUS.md](STATUS.md).

---

## Decision 1: Backend strategy

**State: OPEN**

We need raw-mode control, alternate screen, resize handling, and input
decoding. Three options:

### Option A — Wrap an existing library

Candidates: `tcell/v2` (mature, cross-platform, handles Windows console
abstraction), `golang.org/x/term` (raw mode only, no rendering).

- Pros: fast to start, battle-tested terminal quirks, Windows handled.
- Cons: constrains our buffer and diff design to what the library exposes.
  `tcell`'s model is a mutable screen buffer we draw into, which is closer to
  immediate-mode than we may want. Adds a heavy dependency with its own
  release cadence.

### Option B — Own the terminal layer outright

Write raw mode setup, escape sequences, and input parsing against
`golang.org/x/sys` directly.

- Pros: total control over input decoding and frame output, no ceiling, no
  dependency risk.
- Cons: 6–12 months. Terminal quirks are where TUI projects die: paste
  handling, IME, mouse encoding variants, resize races, tmux passthrough,
  Windows console mode flags.

### Option C — Pluggable, own the core, adapt at the edges

Define our own narrow interfaces — a terminal control interface (raw mode,
alt screen, size, capabilities) and a writer sink. Implement it directly,
possibly behind a build-tag-selected adapter.

- Pros: headless memory-sink implementation falls out naturally for testing.
  Later we can add a second backend without touching the renderer.
  The SSH / piped-output use case (which OpenTUI supports via `FeedBackend`)
  becomes possible.
- Cons: more upfront design than A.

**Current leaning: Option C.** The testability argument alone justifies it —
every widget must be testable in CI without a terminal — and it is the only
option that does not put a ceiling on the renderer.

## Decision 2: Cell buffer representation

**State: OPEN**

- **AoS** — `[]Cell{Char, FG, BG, Attr}`. Simple, cache-friendly for
  per-cell access, but comparing a row means comparing structs.
- **SoA** — separate `[]rune`, `[]Color`, `[]Color`, `[]Attr` arrays. A row
  compare becomes four `mem.Equal` calls on contiguous slices, which enables a
  cheap row-level skip in the diff. This is what OpenTUI does, and it is the
  reason their diff is fast.

**Current leaning: SoA**, accepting the added complexity, because the
row-level skip is the single biggest lever on frame cost.

## Decision 3: Renderer mode

**State: OPEN**

- **Immediate-mode** — describe the whole UI each frame. Simplest mental
  model, trivial async integration, but verbose and wasteful for static
  chrome.
- **Retained-mode** — a persistent widget tree with change notification. Cheap
  static rendering, but needs an invalidation discipline that is easy to get
  wrong.
- **Hybrid** — retained structure, but widgets describe themselves on demand.

**Current leaning: hybrid.** Static chrome (borders, panels) should not be
redrawn; dynamic regions must be. Also worth noting: OpenTUI calls itself
"imperative" but actually ships a React reconciler, and the reconciler is the
part users notice least.

## Decision 4: Layout engine

**State: OPEN**

Constraint-based (length/percentage/ratio, like tmux or Ratatui) versus
flexbox-style (row/column with grow/shrink/basis). Flexbox matches what web
developers expect; constraints match how TUIs are actually laid out.

## Decision 5: Color model and degradation

**State: OPEN**

Truecolor → 256 → 16, with a documented ladder and a `NO_COLOR` path. Needs a
perceptual mapping, not naive truncation.

---

## Non-goals

- **Not a web framework.** No HTTP server, no browser target, no WASM build.
- **Not a widget-styling DSL.** Styling stays plain Go values. No layout-in-CSS.
- **No image support in v1.** Kitty graphics is explicitly deferred; the
  grid-of-cells assumption is the foundation and images undermine it.
- **Not opinionated about your application.** We provide widgets and a
  renderer, not an application framework. No required `App` struct, no
  prescribed message loop.