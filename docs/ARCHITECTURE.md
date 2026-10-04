# Architecture

An **orientation document**: what the pieces are, how they fit together, and where
each decision's reasoning lives.

It is deliberately not a second copy of that reasoning. Every architecture decision
TermMosaic has made is recorded in full — with its evidence, its rejected
alternatives, and the consequences including the unwelcome ones — in
**[docs/adr/](adr/README.md)**, which is 4,175 lines across eight ADRs. An earlier
version of this file summarised those decisions here and went stale doing it: its
decision numbering did not match the ADR set, and it still described the colour model
as OPEN after STATUS.md had moved it to PROPOSED. A summary that drifts is worse than
a link, because a reader cannot tell which one to trust.

- **What is decided, and what is still open** →
  [docs/STATUS.md](STATUS.md)
- **Why each decision was made, and what was rejected** →
  [docs/adr/](adr/README.md)
- **How to write a CHANGELOG, a release, or a patch** →
  [docs/CONTRIBUTING.md](CONTRIBUTING.md)

## The frame

A frame is exactly this, and there is no fourth step:

```
input bytes + resize  ->  Event (one ordered stream)   input, term
                             |
                             v
              application recomputes its root's rect
                             |
                             v
              widget tree Draw() into the back buffer   widgets/
                             |
                             v
              dirty rectangles                           buffer
                             |
                             v
              two-tier diff -> ANSI bytes -> Sink        internal/diff, internal/ansi
```

Two properties of that pipeline are load-bearing and both are pinned by tests rather
than by comment:

- **A frame in which nothing is dirty writes zero bytes and does not call the Sink.**
  The renderer does not walk the tree, does not run the diff and does not touch the
  terminal. This is what makes an idle application free rather than a 60 Hz CPU
  burner.
- **A resize forces a full repaint**, because `buffer.Resize` discards the cells: the
  previous frame described a screen that no longer exists, so there is nothing valid
  to diff against.

## The pieces

Roughly bottom-up. Each row links the ADR that decided it.

| Package | What it owns | Decided in |
|---|---|---|
| `termmosaic` (root) | The `Event` union, the `Widget` / `Focusable` / `Minimizable` interfaces, `Caps`, the `Sink` interface. Four methods on `Widget` and they have never changed. | [0003](adr/0003-renderer-mode.md), [0005](adr/0005-input-decoding.md), [0007](adr/0007-responsive-screens.md) |
| `geometry` | Pure ints and rectangles, importing nothing. `ClampCount`, `Priority`, `Region`, `Budget`, `Align`. The shared arithmetic of responsiveness. | [0007](adr/0007-responsive-screens.md) |
| `buffer` | `Cell` (16 bytes, padding-free), `Colour` and the degradation ladder, `Style`, `Span`, borders, wrapping, and the cell writers including the range-clipped ones. | [0002](adr/0002-buffer-representation.md), [0006](adr/0006-subbuffer-cell-access.md), [0008](adr/0008-style-and-text.md) |
| `layout` | The constraint solver: `Length` / `Min` / `Max` / `Percentage` / `Ratio` / `Fill`, plus `Solve` and rectangle composition. Pure Go, no cgo, no terminal. | [0004](adr/0004-layout-engine.md) |
| `input` | A pure `Decode` under a resumable `Parser`, driven by a `Source` that merges bytes and resize notifications into one ordered stream. | [0005](adr/0005-input-decoding.md) |
| `term` | The `Terminal` implementation: raw mode, alt screen, capabilities, size. Direct on `golang.org/x/sys`, wrapping no terminal library. | [0001](adr/0001-backend-strategy.md) |
| `headless` | `MemorySink`: a Sink that records the bytes **and maintains the cell grid they would have produced**. This is the assertion surface every widget test uses. | [0001](adr/0001-backend-strategy.md) |
| `render` | `Renderer`, `Pacer`, `Config`. The retained tree, the dirty rectangles, and the call into the diff. | [0003](adr/0003-renderer-mode.md) |
| `internal/diff` | The two-tier diff: a per-row byte skip over a per-cell pass, 0 allocations per frame. The most load-bearing code in the project. | [0002](adr/0002-buffer-representation.md) |
| `internal/ansi` | The encoder. Colour degradation and the escape sequences. | [0001](adr/0001-backend-strategy.md), [0008](adr/0008-style-and-text.md) |
| `virtual` | The row virtualization engine the List, Table and Tree share: O(visible rows), not O(item count). | [0007](adr/0007-responsive-screens.md) §6 |
| `widgets/*` | The catalog, in five groups. `block` is the only thing that draws a border or a title; `split` composes children; `basic`, `form`, `data` and `viz` are the widgets. | [0007](adr/0007-responsive-screens.md), [0008](adr/0008-style-and-text.md) |
| `widgets/widgettest` | Renders a widget through the whole stack into a `MemorySink`, so a widget test asserts on cells rather than on escape sequences. | — |

Dependencies run one way: `widgets` → `render` → `internal/diff` → `buffer` → `geometry`.
`geometry` imports nothing. There is no cycle, and adding a package at any level does
not disturb the ones above it.

## Three rules that run through all of it

These are the ones a new contributor breaks first, so they are stated here rather
than only in the ADRs:

1. **A widget's available space is `Bounds()`, never `buf.Size()`.** The buffer is
   the screen; the widget's rectangle is the widget's space. A widget that reads
   `buf.Width()` as its own width is a bug even when it is the root.
2. **A widget repaints its whole `Bounds()` before drawing content into it**, on
   every size change and not only at construction. The renderer never clears — it
   diffs — so a widget that drew ten rows and now draws three leaves seven stale
   rows on screen.
3. **`Draw` allocates nothing and is total over every rectangle**, including the
   empty one. `Wrap` and `Truncate` allocate and are therefore banned from `Draw`;
   anything derived from the size is cached against the rectangle it was computed
   for.

Full statements, with the reasoning behind them: [ADR 0007 §1 and §3](adr/0007-responsive-screens.md),
[ADR 0008 §4](adr/0008-style-and-text.md).

## Non-goals

- **Not a web framework.** No HTTP server, no browser target, no WASM build.
- **Not a widget-styling DSL.** Styling stays plain Go values. No layout-in-CSS.
- **No image support in v1.** Kitty graphics is explicitly deferred; the
  grid-of-cells assumption is the foundation and images undermine it.
- **Not opinionated about your application.** We provide widgets and a
  renderer, not an application framework. No required `App` struct, no
  prescribed message loop.
