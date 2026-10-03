# 0003 — Renderer mode

- **Status:** Accepted
- **Date:** 2026-10-03
- **Decides:** [STATUS.md](../STATUS.md) — Core architecture / Renderer mode
- **Depends on:** [ADR 0001](0001-backend-strategy.md),
  [ADR 0002](0002-buffer-representation.md)
- **Supersedes:** the "immediate / retained / hybrid" section of the former
  ARCHITECTURE.md "Decision 3".

## Context

The framework must pick how a frame gets described:

- **Immediate-mode** — the app describes the whole UI every frame.
- **Retained-mode** — a persistent widget tree that invalidates itself.
- **Hybrid** — retained structure, widgets describe their own contents on demand.

The defining requirement is the one OpenTUI and Ink both failed differently:
**static chrome must not be redrawn, and dynamic regions must update without
input lag at scale.** Ink clears and repaints the whole screen on each update,
which is why Claude Code abandoned it and rewrote its renderer. OpenTUI ships
the opposite extreme: a retained `Renderable` tree with stable ids, per-node
Yoga layout nodes, and the real `react-reconciler` in its React bindings.

## Evidence gathered

From [ADR 0002](0002-buffer-representation.md)'s benchmark: on a 200×60 scene
that is 99% static chrome, a **two-tier diff costs 7,133 ns/op and writes 107
bytes, against 23,240 bytes for a full repaint** — a 217× reduction.

That number is the whole argument. Static chrome is cheap **because the diff
skips it**, not because we chose a renderer mode. The diff makes the
static-chrome problem disappear regardless of mode.

## Options considered

### Immediate-mode

- **Pros:** simplest possible mental model; no invalidation discipline to get
  wrong; async integration is trivial because any goroutine can just call
  `Render()`.
- **Cons:** every widget's draw code runs every frame. Measured at ~81 µs for
  a 12,000-cell scene ([ADR 0002](0002-buffer-representation.md)) — only ~0.5%
  of a 16 ms budget, so this is *not* actually a performance problem at v1
  sizes. The real cost is verbosity and allocation churn in user code: the app
  author rebuilds the entire description 60 times a second.

### Retained-mode with a reconciler

- **Pros:** minimal per-frame work; stable node identity.
- **Cons:** heavyweight. OpenTUI's reconciler is a real `react-reconciler` —
  machinery that is justified when binding a general-purpose declarative
  runtime, and not justified for thirty Go widgets. **Go has no ownership
  system**, so nothing prevents a retained tree from being mutated from the
  wrong goroutine or read while being invalidated. Retained-mode invalidation
  discipline is materially easier to get *silently* wrong in Go than in Rust,
  where the borrow checker forces the discipline structurally. Silent
  invalidation bugs are the worst class of bug a TUI can have: they show up as
  a widget that mysteriously stops updating.

### Hybrid — retained structure, on-demand description ✅

Retain the *widget tree* (so the app does not rebuild a description 60×/sec and
so identity, focus order, and scroll offsets persist), but let widgets expose
`Bounds()` and `Draw(buf *Buffer)` rather than reconciling a diff of a virtual
tree. Invalidation is expressed as **rectangles on the buffer**, not as a
node-graph diff.

## Decision

**Hybrid.** A persistent widget tree with stable identity, invalidated by
rectangle, and rendered through the two-tier cell diff from
[ADR 0002](0002-buffer-representation.md).

```go
type Widget interface {
    Bounds() Rect
    Draw(buf *Buffer)      // writes cells; cheap to call every frame
    Invalidate()           // marks Bounds() dirty
    Handle(Event) bool
}
```

- `Draw` is called every frame for every widget — we do **not** require widgets
  to implement incremental drawing. The buffer diff makes this affordable, and
  requiring incremental invalidation from every widget author is exactly the
  discipline we just argued Go cannot enforce.
- Invalidation is a **dirty-rectangle accumulator** on the buffer, which also
  gives the List/Table/Tree virtualizers a cheap "only these rows changed" hint.
- The frame pipeline is: events → widget tree → `Draw` into the back buffer →
  dirty-rect accumulation → two-tier diff → `Sink`.

### Async ergonomics

Because there is no prescribed message loop (a stated non-goal), we provide the
minimum that makes background work safe rather than an Elm architecture:

- A `Post(func())` that marshals onto the render goroutine — the single rule is
  *"mutate widget state only inside a `Post` callback."*
- Widgets expose `Invalidate()` as safe to call from any goroutine; the dirty
  accumulator is atomic. A background job calls `Invalidate()` and the next tick
  picks it up.
- No `Cmd`/`Msg` machinery. Bubble Tea's Elm loop shines when an app has a
  large message algebra; our users are writing dashboard widgets, not
  reducers. We provide the escape hatch, not the framework.

**This is the deliberate divergence from Bubble Tea** and the reason our async
story is thinner. Accepted trade: less structure, less ceremony, and no way to
enforce that user code is single-goroutine-correct.

## Consequences

**Good**

- Static chrome costs nothing per frame (measured 217× byte reduction), and it
  costs nothing because of the diff, which is under our control.
- Widget authors write one `Draw` method with no incremental-draw obligation —
  the single biggest ergonomic win for thirty widgets.
- No reconciler, no node-diffing machinery, no GC pressure from a virtual tree.
- Dirty rectangles fall out naturally and serve the virtualizers.
- Async is one documented rule, not a message algebra.

**Bad — stated plainly**

- **No compile-time enforcement of the single-goroutine rule.** A user who
  mutates widget state from a background goroutine gets a data race, and Go's
  race detector only finds it if the test happens to run that path. This is a
  real support burden. We mitigate with documentation and by making `Post` the
  path of least resistance in every example.
- **Every widget's `Draw` runs every frame** (~81 µs measured for 12,000
  cells). Cheap now; if a user builds something pathological the cost is theirs
  and they have no lever to pull, because there is no incremental-draw
  interface. Accepted for v1.
- **We have no answer for very deep trees.** Recursive layout and draw over
  hundreds of nested widgets is unmeasured.
- **Verdict on OpenTUI's design:** its retained tree + reconciler is the right
  call for a TypeScript binding surface, and the wrong one for thirty Go
  widgets. We take the retained *structure* and drop the reconciler.

## Rejected alternatives, specifically

- **Full immediate-mode** — re-describes the whole UI 60×/sec. Measured cost is
  affordable (~81 µs) so this is rejected on **verbosity and allocation churn in
  user code**, not on speed. It also makes scroll offsets, focus, and selection
  state awkward, since the app is rebuilding the thing that holds them.
- **Retained-mode with a reconciler** — OpenTUI's shape. The reconciler is
  machinery for binding a general declarative runtime; it is disproportionate
  for a fixed catalog of thirty widgets, and in Go, where nothing enforces
  single-goroutine mutation, its invalidation discipline fails *silently*.
- **Adopting Bubble Tea's Elm loop** — directly conflicts with a stated
  non-goal (no opinionated `App` struct, no prescribed message loop) and with
  the widget-first thesis. We provide `Post` and stop there.

## Risks to revisit at v1.0

1. **Silent data races** are the top risk of this decision. Revisit if we ever
   see a need for concurrent widget reads (e.g. a background exporter walking
   the tree) — that would push us toward a snapshot/lock discipline.
2. **No incremental draw.** Revisit if measured widget `Draw` cost becomes a
   real issue at large tree counts; the dirty-rectangle accumulator is already
   the hook an incremental API would attach to.
3. **The `Post`-only rule is documentation, not a type.** If users routinely get
   it wrong, consider a v1 API where `Invalidate` is the only cross-goroutine
   operation permitted, enforced by making widget state inaccessible directly.